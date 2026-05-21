package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Config struct {
	BaseURL              string
	BridgeID             string
	Token                string
	AgentsFile           string
	ModelProfile         string
	ModelPolicyFile      string
	PollSeconds          int
	AgentFailureCooldown time.Duration
	LogLevel             string
}

type Agent struct {
	Name           string            `json:"name"`
	Role           string            `json:"role"`
	LLMProvider    string            `json:"llm_provider"`
	LaunchTemplate string            `json:"launch_template"`
	Env            map[string]string `json:"env,omitempty"`
	Worktrees      map[string]string `json:"worktrees"`
}

type Roster struct {
	Agents            []Agent  `json:"agents"`
	AgentInstructions []string `json:"agent_instructions,omitempty"`
}

type ModelPolicy struct {
	DefaultProfile string                          `json:"default_profile"`
	Fallbacks      map[string][]string             `json:"fallbacks"`
	Providers      map[string]map[string]ModelSpec `json:"providers"`
}

type ModelSpec struct {
	Model string `json:"model"`
	Args  string `json:"args"`
}

type ModelSelection struct {
	RequestedProfile string
	MatchedProfile   string
	Model            string
	Args             string
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
)

type providerCooldowns struct {
	mu              sync.Mutex
	defaultDuration time.Duration
	untilByProvider map[string]time.Time
}

type agentSelector struct {
	nextByRole map[string]int
}

// worktreeTracker records which worktree paths are currently occupied by a running agent.
// It is the authoritative concurrency gate: selectAgent checks isBusy, and the main loop
// calls tryAcquire (atomic) before spawning a goroutine and release when the agent exits.
type worktreeTracker struct {
	mu   sync.Mutex
	busy map[string]bool
}

func newWorktreeTracker() *worktreeTracker {
	return &worktreeTracker{busy: make(map[string]bool)}
}

// tryAcquire marks path as busy and returns true, or returns false if already busy.
func (t *worktreeTracker) tryAcquire(path string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.busy[path] {
		return false
	}
	t.busy[path] = true
	return true
}

func (t *worktreeTracker) release(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.busy, path)
}

func (t *worktreeTracker) isBusy(path string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.busy[path]
}

func main() {
	cfg := Config{
		BaseURL:              envOr("ORCH_BASE_URL", "https://mcp.singularia.de"),
		BridgeID:             strings.TrimSpace(os.Getenv("BRIDGE_ID")),
		Token:                strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN")),
		AgentsFile:           strings.TrimSpace(os.Getenv("AGENTS_CONFIG")),
		ModelProfile:         strings.TrimSpace(os.Getenv("MODEL")),
		ModelPolicyFile:      strings.TrimSpace(os.Getenv("MODEL_POLICY")),
		PollSeconds:          envInt("POLL_SECONDS", 5),
		AgentFailureCooldown: time.Duration(envInt("AGENT_FAILURE_COOLDOWN_SECONDS", 300)) * time.Second,
		LogLevel:             strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))),
	}

	if cfg.BridgeID == "" {
		fmt.Fprintln(os.Stderr, "BRIDGE_ID is required")
		os.Exit(1)
	}
	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "AGENT_SHARED_TOKEN is required")
		os.Exit(1)
	}
	if cfg.AgentsFile == "" {
		fmt.Fprintln(os.Stderr, "AGENTS_CONFIG is required")
		os.Exit(1)
	}

	roster, err := loadRoster(cfg.AgentsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to load agents config:", err)
		os.Exit(1)
	}
	if len(roster.Agents) == 0 {
		fmt.Fprintln(os.Stderr, "agents config contains no agents")
		os.Exit(1)
	}
	if err := resolveRosterEnv(&roster); err != nil {
		fmt.Fprintln(os.Stderr, "agents config env resolution failed:", err)
		os.Exit(1)
	}

	var modelPolicy *ModelPolicy
	if cfg.ModelPolicyFile != "" {
		policy, err := loadModelPolicy(cfg.ModelPolicyFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to load model policy:", err)
			os.Exit(1)
		}
		modelPolicy = &policy
		if cfg.ModelProfile == "" {
			cfg.ModelProfile = strings.TrimSpace(modelPolicy.DefaultProfile)
		}
	}
	if err := validateModelPolicyForRoster(roster, modelPolicy, cfg.ModelProfile); err != nil {
		fmt.Fprintln(os.Stderr, "model policy validation failed:", err)
		os.Exit(1)
	}

	logger := log.New(os.Stderr, fmt.Sprintf("[bridge/%s] ", cfg.BridgeID), log.LstdFlags)
	debug := cfg.LogLevel == "DEBUG"

	roles := collectRoles(roster)
	logger.Printf("started: bridge_id=%s agents=%d roles=%s poll=%ds",
		cfg.BridgeID, len(roster.Agents), strings.Join(roles, ","), cfg.PollSeconds)
	if debug {
		for _, a := range roster.Agents {
			if len(a.Env) > 0 {
				keys := make([]string, 0, len(a.Env))
				for k := range a.Env {
					keys = append(keys, k)
				}
				logger.Printf("DEBUG agent env configured: agent=%s keys=%s", a.Name, strings.Join(keys, ","))
			}
			if strings.Contains(a.LaunchTemplate, "{model_args}") {
				selection, err := resolveModelSelection(modelPolicy, a.LLMProvider, cfg.ModelProfile)
				if err == nil {
					logger.Printf("DEBUG agent model configured: agent=%s provider=%s profile=%s matched_profile=%s model=%s",
						a.Name, providerKey(a.LLMProvider), selection.RequestedProfile, selection.MatchedProfile, selection.Model)
				}
			}
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	cooldowns := newProviderCooldowns(cfg.AgentFailureCooldown)
	selector := newAgentSelector()
	wt := newWorktreeTracker()
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		if debug {
			logger.Printf("DEBUG poll: bridge=%s roles=%s", cfg.BridgeID, strings.Join(roles, ","))
		}
		pkg, err := fetchNextWork(client, cfg, roles)
		if err != nil {
			logger.Println("poll error:", err)
			time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
			continue
		}

		if pkg == nil {
			if debug {
				logger.Println("DEBUG no work available")
			}
			time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
			continue
		}
		if debug {
			logger.Printf("DEBUG work received: task=%d issue=%d role=%s assignee=%s status=%s",
				pkg.ID, pkg.IssueID, pkg.Role, pkg.Assignee, pkg.CurrentStatus)
		}

		now := time.Now()
		agent := selectAgent(roster, pkg, cooldowns, selector, now, wt)
		if agent == nil {
			errText := "no available bridge agent for role/assignee; matching agent may be cooling down or worktree busy"
			if debug {
				logger.Printf("DEBUG requeue: no agent available role=%s assignee=%s agents=[%s]",
					pkg.Role, pkg.Assignee, agentRoleSummary(roster, pkg.Role, cooldowns, wt, pkg.Repo, now))
				logger.Printf("DEBUG work/fail payload: task=%d issue=%d bridge=%s agent= exit=-1 error=%q",
					pkg.ID, pkg.IssueID, cfg.BridgeID, errText)
			}
			logger.Printf("warning: no available agent for role=%s assignee=%s — requeueing", pkg.Role, pkg.Assignee)
			result := AgentResult{ExitCode: -1, ErrorText: errText}
			if reportErr := reportWorkFailure(client, cfg, *pkg, Agent{}, result); reportErr != nil {
				logger.Printf("work/fail report error: issue=%d err=%v", pkg.IssueID, reportErr)
			}
			time.Sleep(cooldowns.sleepDurationFor(pkg, roster, now, time.Duration(cfg.PollSeconds)*time.Second))
			continue
		}

		worktree, ok := agent.Worktrees[pkg.Repo]
		if !ok || strings.TrimSpace(worktree) == "" {
			logger.Printf("error: no worktree configured for agent=%s repo=%s", agent.Name, pkg.Repo)
			time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
			continue
		}
		if _, err := os.Stat(worktree); err != nil {
			logger.Printf("error: worktree path does not exist: agent=%s path=%s", agent.Name, worktree)
			time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
			continue
		}

		// Atomically acquire the worktree before spawning. selectAgent already checked
		// isBusy, but tryAcquire is the authoritative gate that prevents the race between
		// check and acquire when multiple goroutines are running.
		if !wt.tryAcquire(worktree) {
			errText := "worktree acquired by concurrent agent between selection and dispatch"
			logger.Printf("warning: worktree race agent=%s worktree=%s — requeueing", agent.Name, worktree)
			result := AgentResult{ExitCode: -1, ErrorText: errText}
			if reportErr := reportWorkFailure(client, cfg, *pkg, *agent, result); reportErr != nil {
				logger.Printf("work/fail report error: agent=%s issue=%d err=%v", agent.Name, pkg.IssueID, reportErr)
			}
			time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
			continue
		}

		logger.Printf("agent started: agent=%s issue=%d task=%d worktree=%s", agent.Name, pkg.IssueID, pkg.ID, worktree)

		capturedPkg := *pkg
		capturedAgent := *agent
		capturedWorktree := worktree
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer wt.release(capturedWorktree)

			result, runErr := runAgent(logger, &capturedAgent, capturedWorktree, capturedPkg, roster.AgentInstructions, modelPolicy, cfg.ModelProfile)
			if runErr != nil {
				logger.Printf("agent run error: agent=%s issue=%d err=%v", capturedAgent.Name, capturedPkg.IssueID, runErr)
			}
			if runErr != nil || result.ExitCode != 0 {
				if result.ErrorText == "" && runErr != nil {
					result.ErrorText = runErr.Error()
				}
				class := classifyAgentFailure(result, runErr)
				if class == transientFailure {
					until := cooldowns.mark(capturedAgent.LLMProvider, class, result.ErrorText, time.Now())
					logger.Printf("provider cooling down: provider=%s agent=%s until=%s reason=%s",
						providerKey(capturedAgent.LLMProvider), capturedAgent.Name, until.Format(time.RFC3339), result.ErrorText)
				}
				if debug {
					logger.Printf("DEBUG requeue: agent failed agent=%s agents=[%s]",
						capturedAgent.Name, agentRoleSummary(roster, capturedPkg.Role, cooldowns, wt, capturedPkg.Repo, time.Now()))
					logger.Printf("DEBUG work/fail payload: task=%d issue=%d bridge=%s agent=%s exit=%d error=%q",
						capturedPkg.ID, capturedPkg.IssueID, cfg.BridgeID, capturedAgent.Name, result.ExitCode, result.ErrorText)
				}
				if reportErr := reportWorkFailure(client, cfg, capturedPkg, capturedAgent, result); reportErr != nil {
					logger.Printf("work/fail report error: agent=%s issue=%d err=%v", capturedAgent.Name, capturedPkg.IssueID, reportErr)
				}
			} else {
				logger.Printf("agent finished: agent=%s issue=%d task=%d", capturedAgent.Name, capturedPkg.IssueID, capturedPkg.ID)
			}
		}()
		// No sleep — poll immediately so other repos/roles can be picked up while this agent runs.
	}
}

// selectAgent finds the agent to use for the work package.
// Priority 1: match by role AND assignee name when that agent's provider is available and worktree is free.
// Priority 2: match by role and past worker, preferring the most recent matching past worker.
// Priority 3: round-robin over available agents for the required role, filtered by free worktree.
// Assignee-only matches across roles are intentionally ignored to prevent a stale
// assignee from routing work to the wrong agent type.
func selectAgent(roster Roster, pkg *WorkPackage, cooldowns *providerCooldowns, selector *agentSelector, now time.Time, wt *worktreeTracker) *Agent {
	if pkg.Assignee != "" {
		for i := range roster.Agents {
			if roster.Agents[i].Role == pkg.Role && roster.Agents[i].Name == pkg.Assignee {
				if agentAvailable(cooldowns, &roster.Agents[i], now) && !worktreeBusy(wt, &roster.Agents[i], pkg.Repo) {
					return &roster.Agents[i]
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
			if roster.Agents[j].Role == pkg.Role && roster.Agents[j].Name == worker &&
				agentAvailable(cooldowns, &roster.Agents[j], now) && !worktreeBusy(wt, &roster.Agents[j], pkg.Repo) {
				return &roster.Agents[j]
			}
		}
	}

	var candidates []*Agent
	for i := range roster.Agents {
		if roster.Agents[i].Role == pkg.Role && agentAvailable(cooldowns, &roster.Agents[i], now) && !worktreeBusy(wt, &roster.Agents[i], pkg.Repo) {
			candidates = append(candidates, &roster.Agents[i])
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// Prefer agents whose LLM provider differs from providers already used by past
	// workers — this gives reviewers a different model perspective than the developer.
	// Fall back to the full candidate list when no cross-provider agent is available.
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

	if selector == nil {
		return candidates[0]
	}
	idx := selector.next(pkg.Role, len(candidates))
	return candidates[idx]
}

// pastWorkerProviders returns the set of LLM provider keys used by the named workers.
func pastWorkerProviders(roster Roster, workers []string) map[string]bool {
	if len(workers) == 0 {
		return nil
	}
	workerSet := make(map[string]bool, len(workers))
	for _, w := range workers {
		workerSet[strings.TrimSpace(w)] = true
	}
	providers := make(map[string]bool)
	for i := range roster.Agents {
		if workerSet[roster.Agents[i].Name] {
			providers[providerKey(roster.Agents[i].LLMProvider)] = true
		}
	}
	return providers
}

func newAgentSelector() *agentSelector {
	return &agentSelector{nextByRole: map[string]int{}}
}

func (s *agentSelector) next(role string, n int) int {
	if s == nil || n <= 0 {
		return 0
	}
	idx := s.nextByRole[role] % n
	s.nextByRole[role] = (idx + 1) % n
	return idx
}

func agentAvailable(cooldowns *providerCooldowns, agent *Agent, now time.Time) bool {
	return cooldowns == nil || agent == nil || !cooldowns.isCooling(agent.LLMProvider, now)
}

// worktreeBusy returns true when the agent's worktree path for repo is currently occupied
// by a running agent goroutine.
func worktreeBusy(wt *worktreeTracker, agent *Agent, repo string) bool {
	if wt == nil || agent == nil {
		return false
	}
	path := strings.TrimSpace(agent.Worktrees[repo])
	if path == "" {
		return false
	}
	return wt.isBusy(path)
}

// agentRoleSummary returns a human-readable list of all agents for role,
// annotating each with "available", "cooling", or "busy" — used in debug log lines.
func agentRoleSummary(roster Roster, role string, cooldowns *providerCooldowns, wt *worktreeTracker, repo string, now time.Time) string {
	var parts []string
	for i := range roster.Agents {
		a := &roster.Agents[i]
		if a.Role != role {
			continue
		}
		var status string
		switch {
		case !agentAvailable(cooldowns, a, now):
			status = "cooling"
		case worktreeBusy(wt, a, repo):
			status = "busy"
		default:
			status = "available"
		}
		parts = append(parts, a.Name+"("+status+")")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}

func providerKey(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "<unknown>"
	}
	return provider
}

func newProviderCooldowns(defaultDuration time.Duration) *providerCooldowns {
	if defaultDuration <= 0 {
		defaultDuration = 5 * time.Minute
	}
	return &providerCooldowns{
		defaultDuration: defaultDuration,
		untilByProvider: map[string]time.Time{},
	}
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
	if c == nil {
		return false
	}
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
	if c == nil {
		return fallback
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[string]bool{}
	var providers []string
	for _, a := range roster.Agents {
		if a.Role != pkg.Role {
			continue
		}
		key := providerKey(a.LLMProvider)
		if !seen[key] {
			seen[key] = true
			providers = append(providers, key)
		}
	}
	var min time.Duration
	for _, provider := range providers {
		until, ok := c.untilByProvider[provider]
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

// buildAgentPackageJSON injects instructions into a copy of pkg and wraps the
// JSON in an authoritative prompt to be written to the agent's package file.
func buildAgentPackageJSON(pkg WorkPackage, instructions []string) ([]byte, error) {
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
- If no valid transition fits the work you completed, post an Author-tagged issue comment explaining the blocker and do not change status labels.
- Choose the handoff footer role from next_assignee_roles, unless the chosen transition is terminal and no next role is needed.
- Do not choose or hardcode concrete GitHub usernames for the next step; the bridge selects the concrete agent from the role pool.

WORK PACKAGE JSON:
%s
`, string(raw))
	return []byte(prompt), nil
}

func runAgent(logger *log.Logger, agent *Agent, worktree string, pkg WorkPackage, instructions []string, modelPolicy *ModelPolicy, modelProfile string) (AgentResult, error) {
	pkgData, err := buildAgentPackageJSON(pkg, instructions)
	if err != nil {
		return AgentResult{ExitCode: -1, ErrorText: err.Error()}, fmt.Errorf("marshal work package: %w", err)
	}

	tmpFile, err := os.CreateTemp("", fmt.Sprintf("work-%d-*.json", pkg.ID))
	if err != nil {
		return AgentResult{ExitCode: -1, ErrorText: err.Error()}, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(pkgData); err != nil {
		tmpFile.Close()
		return AgentResult{ExitCode: -1, ErrorText: err.Error()}, fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	cmdLine, err := buildAgentCommandLine(agent, worktree, tmpPath, modelPolicy, modelProfile)
	if err != nil {
		return AgentResult{ExitCode: -1, ErrorText: err.Error()}, err
	}

	output := boundedBuffer{limit: 4000}
	cmd := exec.Command("sh", "-c", cmdLine)
	cmd.Stdout = io.MultiWriter(os.Stdout, &output)
	cmd.Stderr = io.MultiWriter(os.Stderr, &output)
	cmd.Env = buildEnv(agent.Env)

	if err := cmd.Start(); err != nil {
		return AgentResult{ExitCode: -1, ErrorText: err.Error()}, fmt.Errorf("spawn agent: %w", err)
	}

	logger.Printf("spawned agent=%s role=%s issue=%d pid=%d",
		agent.Name, pkg.Role, pkg.IssueID, cmd.Process.Pid)

	start := time.Now()
	waitErr := cmd.Wait()
	duration := time.Since(start).Round(time.Second)

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	logger.Printf("agent exited: agent=%s issue=%d exit=%d duration=%s",
		agent.Name, pkg.IssueID, exitCode, duration)

	result := AgentResult{ExitCode: exitCode}
	if exitCode != 0 {
		result.ErrorText = strings.TrimSpace(output.String())
		if result.ErrorText == "" && waitErr != nil {
			result.ErrorText = waitErr.Error()
		}
	}
	return result, nil
}

func buildAgentCommandLine(agent *Agent, worktree, packageFile string, modelPolicy *ModelPolicy, modelProfile string) (string, error) {
	cmdLine := agent.LaunchTemplate
	if strings.Contains(cmdLine, "{model_args}") {
		selection, err := resolveModelSelection(modelPolicy, agent.LLMProvider, modelProfile)
		if err != nil {
			return "", fmt.Errorf("resolve model args for agent %q provider %q: %w", agent.Name, agent.LLMProvider, err)
		}
		cmdLine = strings.ReplaceAll(cmdLine, "{model_args}", strings.TrimSpace(selection.Args))
	}
	cmdLine = strings.ReplaceAll(cmdLine, "{worktree}", shellQuote(worktree))
	cmdLine = strings.ReplaceAll(cmdLine, "{package_file}", shellQuote(packageFile))
	return cmdLine, nil
}

func fetchNextWork(client *http.Client, cfg Config, roles []string) (*WorkPackage, error) {
	u := fmt.Sprintf(
		"%s/api/v1/work/next?roles=%s&bridge_id=%s",
		strings.TrimRight(cfg.BaseURL, "/"),
		strings.Join(roles, ","),
		cfg.BridgeID,
	)

	req, err := http.NewRequest(http.MethodGet, u, nil)
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
		return nil, fmt.Errorf("decode work/next response: %w", err)
	}
	return out.Task, nil
}

func reportWorkFailure(client *http.Client, cfg Config, pkg WorkPackage, agent Agent, result AgentResult) error {
	body, err := json.Marshal(map[string]any{
		"task_id":    pkg.ID,
		"issue_id":   pkg.IssueID,
		"bridge_id":  cfg.BridgeID,
		"agent":      agent.Name,
		"exit_code":  result.ExitCode,
		"error_text": result.ErrorText,
	})
	if err != nil {
		return err
	}

	u := strings.TrimRight(cfg.BaseURL, "/") + "/api/v1/work/fail"
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
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
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("work/fail failed status=%d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func classifyAgentFailure(result AgentResult, err error) failureClass {
	text := strings.ToLower(result.ErrorText)
	if err != nil {
		text += " " + strings.ToLower(err.Error())
	}
	for _, marker := range []string{
		"out of extra usage",
		"quota",
		"rate limit",
		"rate-limit",
		"session limit",
		"too many requests",
		"temporarily unavailable",
		"overloaded",
		"network is unreachable",
		"connection refused",
		"connection reset",
		"timeout",
	} {
		if strings.Contains(text, marker) {
			return transientFailure
		}
	}
	return unknownFailure
}

type boundedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buf.Write(p[:remaining])
			b.truncated = true
			return len(p), nil
		}
		_, _ = b.buf.Write(p)
		return len(p), nil
	}
	b.truncated = true
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	out := b.buf.String()
	if b.truncated {
		out += "\n[agent output truncated]"
	}
	return out
}

// resolveRosterEnv resolves $VAR references in each agent's Env map in-place.
// It returns an error naming the agent and variable if a referenced var is unset.
func resolveRosterEnv(roster *Roster) error {
	for i := range roster.Agents {
		resolved, err := resolveEnv(roster.Agents[i].Env)
		if err != nil {
			return fmt.Errorf("agent %q: %w", roster.Agents[i].Name, err)
		}
		roster.Agents[i].Env = resolved
	}
	return nil
}

// resolveEnv resolves a single env map: values starting with "$" are looked up
// in the host environment. Both $VAR and ${VAR} are supported; other values
// are used as literals.
func resolveEnv(raw map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") && len(v) >= 4 {
			varName := v[2 : len(v)-1]
			resolved, ok := os.LookupEnv(varName)
			if !ok {
				return nil, fmt.Errorf("env var $%s is not set in host environment", varName)
			}
			out[k] = resolved
		} else if strings.HasPrefix(v, "$") {
			varName := v[1:]
			resolved, ok := os.LookupEnv(varName)
			if !ok {
				return nil, fmt.Errorf("env var $%s is not set in host environment", varName)
			}
			out[k] = resolved
		} else {
			out[k] = v
		}
	}
	return out, nil
}

// buildEnv merges the host environment with agentEnv, with agentEnv taking precedence.
func buildEnv(agentEnv map[string]string) []string {
	envMap := make(map[string]string)
	for _, e := range os.Environ() {
		k, v, _ := strings.Cut(e, "=")
		envMap[k] = v
	}
	for k, v := range agentEnv {
		envMap[k] = v
	}
	result := make([]string, 0, len(envMap))
	for k, v := range envMap {
		result = append(result, k+"="+v)
	}
	return result
}

func loadModelPolicy(path string) (ModelPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ModelPolicy{}, err
	}
	var policy ModelPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return ModelPolicy{}, err
	}
	return policy, nil
}

func validateModelPolicyForRoster(roster Roster, policy *ModelPolicy, profile string) error {
	for _, agent := range roster.Agents {
		if !strings.Contains(agent.LaunchTemplate, "{model_args}") {
			continue
		}
		if policy == nil {
			return fmt.Errorf("agent %q uses {model_args}, but MODEL_POLICY is not configured", agent.Name)
		}
		if _, err := resolveModelSelection(policy, agent.LLMProvider, profile); err != nil {
			return fmt.Errorf("agent %q: %w", agent.Name, err)
		}
	}
	return nil
}

func resolveModelSelection(policy *ModelPolicy, provider, requestedProfile string) (ModelSelection, error) {
	if policy == nil {
		return ModelSelection{}, fmt.Errorf("MODEL_POLICY is required when launch_template uses {model_args}")
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return ModelSelection{}, fmt.Errorf("llm_provider is required")
	}
	profile := strings.TrimSpace(requestedProfile)
	if profile == "" {
		profile = strings.TrimSpace(policy.DefaultProfile)
	}
	if profile == "" {
		return ModelSelection{}, fmt.Errorf("MODEL or default_profile is required")
	}

	providerModels := policy.Providers[provider]
	if providerModels == nil {
		providerModels = policy.Providers[strings.ToLower(provider)]
	}
	if providerModels == nil {
		return ModelSelection{}, fmt.Errorf("provider %q has no model policy", provider)
	}

	profiles := policy.Fallbacks[profile]
	if len(profiles) == 0 {
		profiles = []string{profile}
	}
	for _, candidate := range profiles {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		spec, ok := providerModels[candidate]
		if !ok {
			continue
		}
		return ModelSelection{
			RequestedProfile: profile,
			MatchedProfile:   candidate,
			Model:            spec.Model,
			Args:             spec.Args,
		}, nil
	}
	return ModelSelection{}, fmt.Errorf("provider %q has no model for profile %q or fallbacks %v", provider, profile, profiles)
}

func loadRoster(path string) (Roster, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Roster{}, err
	}
	var r Roster
	if err := json.Unmarshal(data, &r); err != nil {
		return Roster{}, err
	}
	return r, nil
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
	return roles
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func envOr(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	return n
}
