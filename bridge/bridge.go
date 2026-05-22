package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	BaseURL                    string
	BridgeID                   string
	Token                      string
	AgentsFile                 string
	ModelProfile               string
	ModelPolicyFile            string
	PollSeconds                int
	AgentFailureCooldown       time.Duration
	AgentTimeout               time.Duration
	LogLevel                   string
	EnableLegacyLaunchTemplate bool
	WorkReleaseFallbackToFail  bool
}

type Agent struct {
	Name           string            `json:"name"`
	Role           string            `json:"role"`
	LLMProvider    string            `json:"llm_provider"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Stdin          string            `json:"stdin,omitempty"`
	Workdir        string            `json:"workdir,omitempty"`
	LaunchTemplate string            `json:"launch_template,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Worktrees      map[string]string `json:"worktrees"`
}

type Roster struct {
	Agents            []Agent  `json:"agents"`
	AgentInstructions []string `json:"agent_instructions,omitempty"`
}

type ArgList []string

func (a *ArgList) UnmarshalJSON(data []byte) error {
	var xs []string
	if err := json.Unmarshal(data, &xs); err == nil {
		*a = xs
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*a = strings.Fields(s)
		return nil
	}
	return errors.New("args must be a string or string array")
}

type ModelSpec struct {
	Model string  `json:"model"`
	Args  ArgList `json:"args"`
}
type ModelPolicy struct {
	DefaultProfile string                          `json:"default_profile"`
	Fallbacks      map[string][]string             `json:"fallbacks"`
	Providers      map[string]map[string]ModelSpec `json:"providers"`
	RoleProfiles   map[string]string               `json:"role_profiles,omitempty"`
}
type ModelSelection struct {
	RequestedProfile, MatchedProfile, Model string
	Args                                    []string
}

type WorkPackage struct {
	ID                int64    `json:"id"`
	Repo              string   `json:"repo"`
	IssueID           int      `json:"issue_id"`
	Role              string   `json:"role"`
	Assignee          string   `json:"assignee"`
	PastWorkers       []string `json:"past_workers,omitempty"`
	LastCommentID     int64    `json:"last_comment_id"`
	CurrentStatus     string   `json:"current_status"`
	WorkflowKey       string   `json:"workflow_key,omitempty"`
	TypeLabels        []string `json:"type_labels,omitempty"`
	ValidTransitions  []string `json:"valid_transitions,omitempty"`
	NextAssigneeRoles []string `json:"next_assignee_roles,omitempty"`
	AgentInstructions []string `json:"agent_instructions,omitempty"`
}
type workNextResp struct {
	OK   bool         `json:"ok"`
	Task *WorkPackage `json:"task"`
}
type AgentResult struct {
	ExitCode  int
	ErrorText string
}

type failureClass int

const (
	unknownFailure failureClass = iota
	transientFailure
	configurationFailure
	timeoutFailure
)

func (f failureClass) String() string {
	switch f {
	case transientFailure:
		return "transient"
	case configurationFailure:
		return "configuration"
	case timeoutFailure:
		return "timeout"
	default:
		return "unknown"
	}
}

type providerCooldowns struct {
	mu              sync.Mutex
	defaultDuration time.Duration
	untilByProvider map[string]time.Time
}
type agentSelector struct {
	mu         sync.Mutex
	nextByRole map[string]int
}
type worktreeTracker struct {
	mu    sync.Mutex
	busy  map[string]bool
	locks map[string]string
}

func newWorktreeTracker() *worktreeTracker {
	return &worktreeTracker{busy: map[string]bool{}, locks: map[string]string{}}
}
func (t *worktreeTracker) tryAcquire(path, bridgeID, agentName string) (bool, error) {
	t.mu.Lock()
	if t.busy[path] {
		t.mu.Unlock()
		return false, nil
	}
	t.busy[path] = true
	t.mu.Unlock()
	lockPath := filepath.Join(path, ".agent-bridge.lock")
	content := fmt.Sprintf("bridge_id=%s\nagent=%s\npid=%d\nstarted_at=%s\n", bridgeID, agentName, os.Getpid(), time.Now().Format(time.RFC3339))
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.mu.Lock()
		delete(t.busy, path)
		t.mu.Unlock()
		if os.IsExist(err) {
			return false, nil
		}
		return false, err
	}
	_, werr := f.WriteString(content)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(lockPath)
		t.mu.Lock()
		delete(t.busy, path)
		t.mu.Unlock()
		if werr != nil {
			return false, werr
		}
		return false, cerr
	}
	t.mu.Lock()
	t.locks[path] = lockPath
	t.mu.Unlock()
	return true, nil
}
func (t *worktreeTracker) release(path string) {
	t.mu.Lock()
	lock := t.locks[path]
	delete(t.locks, path)
	delete(t.busy, path)
	t.mu.Unlock()
	if lock != "" {
		_ = os.Remove(lock)
	}
}
func (t *worktreeTracker) isBusy(path string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.busy[path]
}

func main() {
	cfg := Config{
		BaseURL: envOr("ORCH_BASE_URL", "https://mcp.singularia.de"), BridgeID: strings.TrimSpace(os.Getenv("BRIDGE_ID")), Token: strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN")),
		AgentsFile: strings.TrimSpace(os.Getenv("AGENTS_CONFIG")), ModelProfile: strings.TrimSpace(os.Getenv("MODEL")), ModelPolicyFile: strings.TrimSpace(os.Getenv("MODEL_POLICY")),
		PollSeconds: envInt("POLL_SECONDS", 5), AgentFailureCooldown: time.Duration(envInt("AGENT_FAILURE_COOLDOWN_SECONDS", 300)) * time.Second, AgentTimeout: time.Duration(envInt("AGENT_TIMEOUT_SECONDS", 3600)) * time.Second,
		LogLevel: strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))), EnableLegacyLaunchTemplate: envBool("ENABLE_LEGACY_LAUNCH_TEMPLATE", false), WorkReleaseFallbackToFail: envBool("WORK_RELEASE_FALLBACK_TO_FAIL", true),
	}
	must(cfg.BridgeID != "", "BRIDGE_ID is required")
	must(cfg.Token != "", "AGENT_SHARED_TOKEN is required")
	must(cfg.AgentsFile != "", "AGENTS_CONFIG is required")
	roster, err := loadRoster(cfg.AgentsFile)
	fatalIf(err, "failed to load agents config")
	if len(roster.Agents) == 0 {
		log.Fatal("agents config contains no agents")
	}
	fatalIf(resolveRosterEnv(&roster), "agents config env resolution failed")
	fatalIf(validateRoster(roster, cfg.EnableLegacyLaunchTemplate), "invalid agents config")
	var policy *ModelPolicy
	if cfg.ModelPolicyFile != "" {
		p, err := loadModelPolicy(cfg.ModelPolicyFile)
		fatalIf(err, "failed to load model policy")
		policy = &p
		if cfg.ModelProfile == "" {
			cfg.ModelProfile = p.DefaultProfile
		}
	}
	fatalIf(validateModelPolicyForRoster(roster, policy, cfg.ModelProfile), "model policy validation failed")

	logger := log.New(os.Stderr, fmt.Sprintf("[bridge/%s] ", cfg.BridgeID), log.LstdFlags)
	debug := cfg.LogLevel == "DEBUG"
	roles := collectRoles(roster)
	logger.Printf("started: bridge_id=%s agents=%d roles=%s poll=%ds timeout=%s", cfg.BridgeID, len(roster.Agents), strings.Join(roles, ","), cfg.PollSeconds, cfg.AgentTimeout)
	if debug {
		logRosterDebug(logger, roster, policy, cfg.ModelProfile)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{Timeout: 30 * time.Second}
	cooldowns := newProviderCooldowns(cfg.AgentFailureCooldown)
	selector := newAgentSelector()
	wt := newWorktreeTracker()
	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			logger.Println("shutdown requested; waiting for running agents")
			wg.Wait()
			logger.Println("shutdown complete")
			return
		default:
		}
		if debug {
			logger.Printf("DEBUG poll roles=%s", strings.Join(roles, ","))
		}
		pkg, err := fetchNextWork(ctx, client, cfg, roles)
		if err != nil {
			logger.Println("poll error:", err)
			sleepOrDone(ctx, time.Duration(cfg.PollSeconds)*time.Second)
			continue
		}
		if pkg == nil {
			if debug {
				logger.Println("DEBUG no work available")
			}
			sleepOrDone(ctx, time.Duration(cfg.PollSeconds)*time.Second)
			continue
		}
		agent := selectAgent(roster, pkg, cooldowns, selector, time.Now(), wt)
		if agent == nil {
			detail := "bridge_capacity_unavailable: no available bridge agent for role/assignee; matching agent may be cooling down or worktree busy"
			retry := int(cooldowns.sleepDurationFor(pkg, roster, time.Now(), time.Duration(cfg.PollSeconds)*time.Second).Seconds())
			logger.Printf("capacity unavailable: role=%s assignee=%s — releasing task", pkg.Role, pkg.Assignee)
			if err := reportWorkRelease(ctx, client, cfg, *pkg, "no_available_agent", detail, retry); err != nil {
				logger.Printf("work/release error: %v", err)
				if cfg.WorkReleaseFallbackToFail {
					_ = reportWorkFailure(ctx, client, cfg, *pkg, Agent{}, AgentResult{-2, detail})
				}
			}
			sleepOrDone(ctx, time.Duration(retry)*time.Second)
			continue
		}
		worktree := strings.TrimSpace(agent.Worktrees[pkg.Repo])
		if worktree == "" {
			reportConfigFailure(ctx, client, cfg, *pkg, *agent, fmt.Sprintf("bridge_config_error: no worktree configured for agent=%s repo=%s", agent.Name, pkg.Repo), logger)
			continue
		}
		if _, err := os.Stat(worktree); err != nil {
			reportConfigFailure(ctx, client, cfg, *pkg, *agent, fmt.Sprintf("bridge_config_error: worktree path does not exist or is inaccessible: agent=%s path=%s err=%v", agent.Name, worktree, err), logger)
			continue
		}
		acquired, err := wt.tryAcquire(worktree, cfg.BridgeID, agent.Name)
		if err != nil {
			reportConfigFailure(ctx, client, cfg, *pkg, *agent, fmt.Sprintf("bridge_config_error: failed to acquire worktree lock: %v", err), logger)
			continue
		}
		if !acquired {
			detail := "bridge_capacity_unavailable: worktree is already locked by another running bridge/agent"
			_ = reportWorkRelease(ctx, client, cfg, *pkg, "worktree_busy", detail, cfg.PollSeconds)
			sleepOrDone(ctx, time.Duration(cfg.PollSeconds)*time.Second)
			continue
		}
		capturedPkg, capturedAgent, capturedWorktree := *pkg, *agent, worktree
		logger.Printf("agent started: agent=%s issue=%d task=%d worktree=%s", capturedAgent.Name, capturedPkg.IssueID, capturedPkg.ID, capturedWorktree)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer wt.release(capturedWorktree)
			result, runErr := runAgent(ctx, cfg, logger, &capturedAgent, capturedWorktree, capturedPkg, roster.AgentInstructions, policy, cfg.ModelProfile)
			if runErr != nil || result.ExitCode != 0 {
				if result.ErrorText == "" && runErr != nil {
					result.ErrorText = runErr.Error()
				}
				class := classifyAgentFailure(result, runErr)
				logger.Printf("agent failure classified: agent=%s provider=%s class=%s issue=%d", capturedAgent.Name, providerKey(capturedAgent.LLMProvider), class.String(), capturedPkg.IssueID)
				if class == transientFailure {
					until := cooldowns.mark(capturedAgent.LLMProvider, class, result.ErrorText, time.Now())
					logger.Printf("provider cooling down: provider=%s until=%s", providerKey(capturedAgent.LLMProvider), until.Format(time.RFC3339))
				}
				_ = reportWorkFailure(ctx, client, cfg, capturedPkg, capturedAgent, result)
			} else {
				logger.Printf("agent finished: agent=%s issue=%d task=%d", capturedAgent.Name, capturedPkg.IssueID, capturedPkg.ID)
			}
		}()
	}
}

func reportConfigFailure(ctx context.Context, client *http.Client, cfg Config, pkg WorkPackage, agent Agent, text string, logger *log.Logger) {
	logger.Println("error:", text)
	_ = reportWorkFailure(ctx, client, cfg, pkg, agent, AgentResult{-3, text})
	sleepOrDone(ctx, time.Duration(cfg.PollSeconds)*time.Second)
}
func sleepOrDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
func fatalIf(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}
func must(ok bool, msg string) {
	if !ok {
		log.Fatal(msg)
	}
}

func selectAgent(roster Roster, pkg *WorkPackage, cooldowns *providerCooldowns, selector *agentSelector, now time.Time, wt *worktreeTracker) *Agent {
	if pkg.Assignee != "" {
		for i := range roster.Agents {
			a := &roster.Agents[i]
			if a.Role == pkg.Role && a.Name == pkg.Assignee {
				if agentAvailable(cooldowns, a, now) && !worktreeBusy(wt, a, pkg.Repo) {
					return a
				}
				break
			}
		}
	}
	for i := len(pkg.PastWorkers) - 1; i >= 0; i-- {
		worker := strings.TrimSpace(pkg.PastWorkers[i])
		if worker == "" || worker == pkg.Assignee {
			continue
		}
		for j := range roster.Agents {
			a := &roster.Agents[j]
			if a.Role == pkg.Role && a.Name == worker && agentAvailable(cooldowns, a, now) && !worktreeBusy(wt, a, pkg.Repo) {
				return a
			}
		}
	}
	var candidates []*Agent
	for i := range roster.Agents {
		a := &roster.Agents[i]
		if a.Role == pkg.Role && agentAvailable(cooldowns, a, now) && !worktreeBusy(wt, a, pkg.Repo) {
			candidates = append(candidates, a)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	if pastProviders := pastWorkerProviders(roster, pkg.PastWorkers); len(pastProviders) > 0 {
		var cross []*Agent
		for _, a := range candidates {
			if !pastProviders[providerKey(a.LLMProvider)] {
				cross = append(cross, a)
			}
		}
		if len(cross) > 0 {
			candidates = cross
		}
	}
	return candidates[selector.next(pkg.Role, len(candidates))]
}
func pastWorkerProviders(roster Roster, workers []string) map[string]bool {
	if len(workers) == 0 {
		return nil
	}
	ws := map[string]bool{}
	for _, w := range workers {
		ws[strings.TrimSpace(w)] = true
	}
	out := map[string]bool{}
	for _, a := range roster.Agents {
		if ws[a.Name] {
			out[providerKey(a.LLMProvider)] = true
		}
	}
	return out
}
func newAgentSelector() *agentSelector { return &agentSelector{nextByRole: map[string]int{}} }
func (s *agentSelector) next(role string, n int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.nextByRole[role] % n
	s.nextByRole[role] = (idx + 1) % n
	return idx
}
func agentAvailable(c *providerCooldowns, a *Agent, now time.Time) bool {
	return c == nil || a == nil || !c.isCooling(a.LLMProvider, now)
}
func worktreeBusy(wt *worktreeTracker, a *Agent, repo string) bool {
	if wt == nil || a == nil {
		return false
	}
	p := strings.TrimSpace(a.Worktrees[repo])
	return p != "" && wt.isBusy(p)
}
func providerKey(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "<unknown>"
	}
	return p
}
func newProviderCooldowns(d time.Duration) *providerCooldowns {
	if d <= 0 {
		d = 5 * time.Minute
	}
	return &providerCooldowns{defaultDuration: d, untilByProvider: map[string]time.Time{}}
}
func (c *providerCooldowns) mark(provider string, class failureClass, reason string, now time.Time) time.Time {
	if c == nil || class != transientFailure {
		return time.Time{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	until := now.Add(c.defaultDuration)
	c.untilByProvider[providerKey(provider)] = until
	return until
}
func (c *providerCooldowns) isCooling(provider string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := providerKey(provider)
	until, ok := c.untilByProvider[key]
	if !ok {
		return false
	}
	if now.Before(until) {
		return true
	}
	delete(c.untilByProvider, key)
	return false
}
func (c *providerCooldowns) sleepDurationFor(pkg *WorkPackage, roster Roster, now time.Time, fallback time.Duration) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[string]bool{}
	var min time.Duration
	for _, a := range roster.Agents {
		if a.Role != pkg.Role {
			continue
		}
		key := providerKey(a.LLMProvider)
		if seen[key] {
			continue
		}
		seen[key] = true
		until, ok := c.untilByProvider[key]
		if !ok || !now.Before(until) {
			return fallback
		}
		d := until.Sub(now)
		if min == 0 || d < min {
			min = d
		}
	}
	if min > 0 {
		return min
	}
	return fallback
}

func buildAgentPackagePrompt(pkg WorkPackage, instructions []string) ([]byte, error) {
	if len(instructions) > 0 {
		pkg.AgentInstructions = instructions
	}
	raw, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf(`AUTHORITATIVE WORK PACKAGE

You are running in headless agent mode. The JSON block below is the authoritative work package from the orchestrator.

Rules:
- Treat current_status, workflow_key, type_labels, valid_transitions, next_assignee_roles, past_workers, and agent_instructions as higher priority than issue body text, issue comments, repository documentation, memory, or inferred workflow names.
- Before changing any status:* label, choose the target status only from valid_transitions.
- Do not use a status from issue text, comments, memory, or repository docs unless it appears in valid_transitions.
- Do not create or use workflow statuses that are not present in valid_transitions.
- If no valid transition fits the work you completed, post one Author-tagged blocker comment and do not change status labels or routing state.
- Choose the handoff footer role from next_assignee_roles, unless the chosen transition is terminal and no next role is needed.
- Do not choose or hardcode concrete GitHub usernames for the next step; the bridge selects the concrete agent from the role pool.
- If multiple valid transitions are possible, choose the one that best matches the work actually completed and explain the reason briefly in the final issue comment.
- If type_labels contains type:smoke-test, treat this as a no-code workflow-routing smoke test: do not create branches, commits, PRs, review artifacts, implementation changes, or source-file modifications unless the work package explicitly asks for them. Do not treat unrelated failing tests or missing PRs as blockers; mention them briefly and continue routing.

WORK PACKAGE JSON:
%s
`, string(raw))
	return []byte(prompt), nil
}

func runAgent(parent context.Context, cfg Config, logger *log.Logger, agent *Agent, worktree string, pkg WorkPackage, instructions []string, policy *ModelPolicy, modelProfile string) (AgentResult, error) {
	data, err := buildAgentPackagePrompt(pkg, instructions)
	if err != nil {
		return AgentResult{-1, err.Error()}, err
	}
	f, err := os.CreateTemp("", fmt.Sprintf("work-%d-*.prompt.txt", pkg.ID))
	if err != nil {
		return AgentResult{-1, err.Error()}, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return AgentResult{-1, err.Error()}, err
	}
	_ = f.Close()
	sel, err := resolveModelSelection(policy, agent.LLMProvider, effectiveModelProfile(policy, agent.Role, modelProfile))
	if err != nil {
		return AgentResult{-3, err.Error()}, err
	}
	logger.Printf("agent model selected: agent=%s role=%s provider=%s requested_profile=%s matched_profile=%s model=%s", agent.Name, agent.Role, providerKey(agent.LLMProvider), sel.RequestedProfile, sel.MatchedProfile, sel.Model)
	ctx, cancel := context.WithTimeout(parent, cfg.AgentTimeout)
	defer cancel()
	cmd, stdin, err := buildAgentCommand(ctx, agent, worktree, tmp, sel, cfg.EnableLegacyLaunchTemplate)
	if err != nil {
		return AgentResult{-3, err.Error()}, err
	}
	if stdin != nil {
		defer stdin.Close()
	}
	output := newTailBuffer(8000)
	cmd.Stdout = io.MultiWriter(os.Stdout, output)
	cmd.Stderr = io.MultiWriter(os.Stderr, output)
	cmd.Env = buildEnv(agent.Env)
	if err := cmd.Start(); err != nil {
		return AgentResult{-1, err.Error()}, err
	}
	logger.Printf("spawned agent=%s role=%s issue=%d pid=%d", agent.Name, pkg.Role, pkg.IssueID, cmd.Process.Pid)
	start := time.Now()
	waitErr := cmd.Wait()
	dur := time.Since(start).Round(time.Second)
	if ctx.Err() == context.DeadlineExceeded {
		text := fmt.Sprintf("agent timed out after %s\n%s", cfg.AgentTimeout, strings.TrimSpace(output.String()))
		return AgentResult{-4, text}, ctx.Err()
	}
	code := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	logger.Printf("agent exited: agent=%s issue=%d exit=%d duration=%s", agent.Name, pkg.IssueID, code, dur)
	res := AgentResult{ExitCode: code}
	if code != 0 {
		res.ErrorText = strings.TrimSpace(output.String())
		if res.ErrorText == "" && waitErr != nil {
			res.ErrorText = waitErr.Error()
		}
	}
	return res, nil
}
func buildAgentCommand(ctx context.Context, agent *Agent, worktree, packageFile string, sel ModelSelection, legacy bool) (*exec.Cmd, *os.File, error) {
	if strings.TrimSpace(agent.Command) != "" {
		var args []string
		for _, raw := range agent.Args {
			xs, err := expandArg(raw, worktree, packageFile, sel.Args)
			if err != nil {
				return nil, nil, err
			}
			args = append(args, xs...)
		}
		cmd := exec.CommandContext(ctx, agent.Command, args...)
		wd := agent.Workdir
		if wd == "" {
			wd = "{worktree}"
		}
		rwd, err := expandSingle(wd, worktree, packageFile)
		if err != nil {
			return nil, nil, err
		}
		cmd.Dir = rwd
		if agent.Stdin != "" {
			sp, err := expandSingle(agent.Stdin, worktree, packageFile)
			if err != nil {
				return nil, nil, err
			}
			f, err := os.Open(sp)
			if err != nil {
				return nil, nil, err
			}
			cmd.Stdin = f
			return cmd, f, nil
		}
		return cmd, nil, nil
	}
	if agent.LaunchTemplate != "" {
		if !legacy {
			return nil, nil, fmt.Errorf("agent %q uses legacy launch_template but ENABLE_LEGACY_LAUNCH_TEMPLATE is false", agent.Name)
		}
		line := strings.ReplaceAll(agent.LaunchTemplate, "{model_args}", strings.Join(shellQuoteArgs(sel.Args), " "))
		line = strings.ReplaceAll(line, "{worktree}", shellQuote(worktree))
		line = strings.ReplaceAll(line, "{package_file}", shellQuote(packageFile))
		return exec.CommandContext(ctx, "sh", "-c", line), nil, nil
	}
	return nil, nil, fmt.Errorf("agent %q has neither command nor launch_template", agent.Name)
}
func expandArg(raw, worktree, packageFile string, modelArgs []string) ([]string, error) {
	if raw == "{model_args}" {
		return append([]string(nil), modelArgs...), nil
	}
	s, err := expandSingle(raw, worktree, packageFile)
	if err != nil {
		return nil, err
	}
	return []string{s}, nil
}
func expandSingle(raw, worktree, packageFile string) (string, error) {
	s := strings.ReplaceAll(raw, "{worktree}", worktree)
	s = strings.ReplaceAll(s, "{package_file}", packageFile)
	if strings.Contains(s, "{model_args}") {
		return "", fmt.Errorf("{model_args} must be a standalone args entry")
	}
	return s, nil
}
func shellQuoteArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, shellQuote(a))
	}
	return out
}

func fetchNextWork(ctx context.Context, client *http.Client, cfg Config, roles []string) (*WorkPackage, error) {
	base := strings.TrimRight(cfg.BaseURL, "/") + "/api/v1/work/next"
	q := url.Values{}
	q.Set("roles", strings.Join(roles, ","))
	q.Set("bridge_id", cfg.BridgeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("work/next failed status=%d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out workNextResp
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&out); err != nil {
		return nil, err
	}
	return out.Task, nil
}
func reportWorkFailure(ctx context.Context, client *http.Client, cfg Config, pkg WorkPackage, agent Agent, result AgentResult) error {
	return postJSON(ctx, client, cfg, "/api/v1/work/fail", map[string]any{"task_id": pkg.ID, "issue_id": pkg.IssueID, "bridge_id": cfg.BridgeID, "agent": agent.Name, "exit_code": result.ExitCode, "error_text": result.ErrorText})
}
func reportWorkRelease(ctx context.Context, client *http.Client, cfg Config, pkg WorkPackage, reason, detail string, retry int) error {
	if retry <= 0 {
		retry = cfg.PollSeconds
	}
	return postJSON(ctx, client, cfg, "/api/v1/work/release", map[string]any{"task_id": pkg.ID, "issue_id": pkg.IssueID, "bridge_id": cfg.BridgeID, "reason": reason, "detail": detail, "retry_after_seconds": retry})
}
func postJSON(ctx context.Context, client *http.Client, cfg Config, path string, body map[string]any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s failed status=%d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
func classifyAgentFailure(result AgentResult, err error) failureClass {
	text := strings.ToLower(result.ErrorText + " ")
	if err != nil {
		text += strings.ToLower(err.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(text, "agent timed out") {
		return timeoutFailure
	}
	for _, m := range []string{"bridge_config_error", "permission denied", "bad credentials", "authentication failed", "has no model", "model policy", "legacy launch_template"} {
		if strings.Contains(text, m) {
			return configurationFailure
		}
	}
	for _, m := range []string{"quota", "rate limit", "too many requests", "temporarily unavailable", "overloaded", "network is unreachable", "connection refused", "connection reset", "timeout", "deadline exceeded", "out of extra usage", "session limit", "usage limit"} {
		if strings.Contains(text, m) {
			return transientFailure
		}
	}
	return unknownFailure
}

type tailBuffer struct {
	mu        sync.Mutex
	buf       []byte
	limit     int
	truncated bool
}

func newTailBuffer(limit int) *tailBuffer { return &tailBuffer{limit: limit} }
func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		drop := len(b.buf) - b.limit
		b.buf = append([]byte(nil), b.buf[drop:]...)
		b.truncated = true
	}
	return len(p), nil
}
func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := string(b.buf)
	if b.truncated {
		s = "[agent output truncated: showing tail]\n" + s
	}
	return s
}
func resolveRosterEnv(roster *Roster) error {
	for i := range roster.Agents {
		m, err := resolveEnv(roster.Agents[i].Env)
		if err != nil {
			return fmt.Errorf("agent %q: %w", roster.Agents[i].Name, err)
		}
		roster.Agents[i].Env = m
	}
	return nil
}
func resolveEnv(raw map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range raw {
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			name := v[2 : len(v)-1]
			rv, ok := os.LookupEnv(name)
			if !ok {
				return nil, fmt.Errorf("env var $%s is not set", name)
			}
			out[k] = rv
		} else if strings.HasPrefix(v, "$") {
			name := v[1:]
			rv, ok := os.LookupEnv(name)
			if !ok {
				return nil, fmt.Errorf("env var $%s is not set", name)
			}
			out[k] = rv
		} else {
			out[k] = v
		}
	}
	return out, nil
}
func buildEnv(agentEnv map[string]string) []string {
	env := map[string]string{"PATH": "/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8"}
	for _, k := range []string{"HOME", "USER", "SHELL", "TERM", "TMPDIR"} {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			env[k] = v
		}
	}
	for k, v := range agentEnv {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}
func loadModelPolicy(path string) (ModelPolicy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ModelPolicy{}, err
	}
	var p ModelPolicy
	err = json.Unmarshal(b, &p)
	return p, err
}
func validateModelPolicyForRoster(roster Roster, policy *ModelPolicy, global string) error {
	for _, a := range roster.Agents {
		uses := strings.Contains(a.LaunchTemplate, "{model_args}")
		for _, arg := range a.Args {
			if arg == "{model_args}" {
				uses = true
			}
		}
		if uses {
			if policy == nil {
				return fmt.Errorf("agent %q uses {model_args}, but MODEL_POLICY is not configured", a.Name)
			}
			if _, err := resolveModelSelection(policy, a.LLMProvider, effectiveModelProfile(policy, a.Role, global)); err != nil {
				return fmt.Errorf("agent %q: %w", a.Name, err)
			}
		}
	}
	return nil
}
func effectiveModelProfile(policy *ModelPolicy, role, global string) string {
	if policy != nil && policy.RoleProfiles != nil {
		if p := strings.TrimSpace(policy.RoleProfiles[role]); p != "" {
			return p
		}
	}
	return global
}
func resolveModelSelection(policy *ModelPolicy, provider, profile string) (ModelSelection, error) {
	if policy == nil {
		return ModelSelection{}, fmt.Errorf("MODEL_POLICY is required")
	}
	if profile == "" {
		profile = policy.DefaultProfile
	}
	models := policy.Providers[provider]
	if models == nil {
		models = policy.Providers[strings.ToLower(provider)]
	}
	if models == nil {
		return ModelSelection{}, fmt.Errorf("provider %q has no model policy", provider)
	}
	profiles := policy.Fallbacks[profile]
	if len(profiles) == 0 {
		profiles = []string{profile}
	}
	for _, cand := range profiles {
		if spec, ok := models[cand]; ok {
			return ModelSelection{RequestedProfile: profile, MatchedProfile: cand, Model: spec.Model, Args: append([]string(nil), spec.Args...)}, nil
		}
	}
	return ModelSelection{}, fmt.Errorf("provider %q has no model for profile %q or fallbacks %v", provider, profile, profiles)
}
func loadRoster(path string) (Roster, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Roster{}, err
	}
	var r Roster
	err = json.Unmarshal(b, &r)
	return r, err
}
func validateRoster(roster Roster, legacy bool) error {
	names := map[string]bool{}
	paths := map[string]string{}
	for _, a := range roster.Agents {
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("agent with empty name")
		}
		if names[a.Name] {
			return fmt.Errorf("duplicate agent name %q", a.Name)
		}
		names[a.Name] = true
		if a.Role == "" || a.LLMProvider == "" {
			return fmt.Errorf("agent %q has empty role or llm_provider", a.Name)
		}
		if len(a.Worktrees) == 0 {
			return fmt.Errorf("agent %q has no worktrees", a.Name)
		}
		if a.Command == "" && a.LaunchTemplate == "" {
			return fmt.Errorf("agent %q has neither command nor launch_template", a.Name)
		}
		if a.LaunchTemplate != "" && !legacy {
			return fmt.Errorf("agent %q uses legacy launch_template but ENABLE_LEGACY_LAUNCH_TEMPLATE is false", a.Name)
		}
		for repo, path := range a.Worktrees {
			if strings.TrimSpace(repo) == "" || strings.TrimSpace(path) == "" {
				return fmt.Errorf("agent %q has invalid worktree", a.Name)
			}
			if other := paths[path]; other != "" {
				return fmt.Errorf("worktree path shared by agents %q and %q: %s", other, a.Name, path)
			}
			paths[path] = a.Name
		}
	}
	return nil
}
func collectRoles(roster Roster) []string {
	seen := map[string]bool{}
	var roles []string
	for _, a := range roster.Agents {
		if !seen[a.Role] {
			seen[a.Role] = true
			roles = append(roles, a.Role)
		}
	}
	sort.Strings(roles)
	return roles
}
func logRosterDebug(logger *log.Logger, roster Roster, policy *ModelPolicy, global string) {
	for _, a := range roster.Agents {
		if len(a.Env) > 0 {
			var keys []string
			for k := range a.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			logger.Printf("DEBUG agent env configured: agent=%s keys=%s", a.Name, strings.Join(keys, ","))
		}
		if sel, err := resolveModelSelection(policy, a.LLMProvider, effectiveModelProfile(policy, a.Role, global)); err == nil {
			logger.Printf("DEBUG agent model configured: agent=%s role=%s provider=%s profile=%s matched_profile=%s model=%s", a.Name, a.Role, providerKey(a.LLMProvider), sel.RequestedProfile, sel.MatchedProfile, sel.Model)
		}
	}
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func envOr(k, f string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return f
	}
	return v
}
func envInt(k string, f int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return f
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return f
	}
	return n
}
func envBool(k string, f bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(k))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return f
	}
}
