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
	"time"
)

type Config struct {
	BaseURL     string
	BridgeID    string
	Token       string
	AgentsFile  string
	PollSeconds int
	LogLevel    string
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

type WorkPackage struct {
	ID                int64    `json:"id"`
	Repo              string   `json:"repo"`
	IssueID           int      `json:"issue_id"`
	Role              string   `json:"role"`
	Assignee          string   `json:"assignee"`
	LastCommentID     int64    `json:"last_comment_id"`
	CurrentStatus     string   `json:"current_status"`
	WorkflowKey       string   `json:"workflow_key,omitempty"`
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

func main() {
	cfg := Config{
		BaseURL:     envOr("ORCH_BASE_URL", "https://mcp.singularia.de"),
		BridgeID:    strings.TrimSpace(os.Getenv("BRIDGE_ID")),
		Token:       strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN")),
		AgentsFile:  strings.TrimSpace(os.Getenv("AGENTS_CONFIG")),
		PollSeconds: envInt("POLL_SECONDS", 5),
		LogLevel:    strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))),
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

	logger := log.New(os.Stderr, fmt.Sprintf("[bridge/%s] ", cfg.BridgeID), log.LstdFlags|log.LUTC)
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
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}

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

		agent := selectAgent(roster, pkg)
		if agent == nil {
			logger.Printf("warning: no agent for role=%s assignee=%s — skipping", pkg.Role, pkg.Assignee)
			time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
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

		result, err := runAgent(logger, agent, worktree, *pkg, roster.AgentInstructions)
		if err != nil {
			logger.Printf("agent run error: agent=%s issue=%d err=%v", agent.Name, pkg.IssueID, err)
		}
		if err != nil || result.ExitCode != 0 {
			if result.ErrorText == "" && err != nil {
				result.ErrorText = err.Error()
			}
			if reportErr := reportWorkFailure(client, cfg, *pkg, *agent, result); reportErr != nil {
				logger.Printf("work/fail report error: agent=%s issue=%d err=%v", agent.Name, pkg.IssueID, reportErr)
			}
			time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
			continue
		}
		// No sleep — poll immediately after agent exits.
	}
}

// selectAgent finds the agent to use for the work package.
// Priority 1: match by role AND assignee name (preferred agent within the correct role).
// Priority 2: match by role only (any agent capable of the required role).
// Assignee-only matches across roles are intentionally ignored to prevent a stale
// assignee from routing work to the wrong agent type.
func selectAgent(roster Roster, pkg *WorkPackage) *Agent {
	if pkg.Assignee != "" {
		for i := range roster.Agents {
			if roster.Agents[i].Role == pkg.Role && roster.Agents[i].Name == pkg.Assignee {
				return &roster.Agents[i]
			}
		}
	}
	for i := range roster.Agents {
		if roster.Agents[i].Role == pkg.Role {
			return &roster.Agents[i]
		}
	}
	return nil
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
- Treat current_status, workflow_key, valid_transitions, next_assignee_roles, and agent_instructions as higher priority than issue body text, issue comments, repository documentation, memory, or inferred workflow names.
- Before changing any status:* label, choose the target status only from valid_transitions.
- Do not use a status from issue text, comments, memory, or repository docs unless it appears in valid_transitions.
- If no valid transition fits the work you completed, post an Author-tagged issue comment explaining the blocker and do not change status labels.
- Choose the handoff footer role from next_assignee_roles, unless the chosen transition is terminal and no next role is needed.

WORK PACKAGE JSON:
%s
`, string(raw))
	return []byte(prompt), nil
}

func runAgent(logger *log.Logger, agent *Agent, worktree string, pkg WorkPackage, instructions []string) (AgentResult, error) {
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

	cmdLine := agent.LaunchTemplate
	cmdLine = strings.ReplaceAll(cmdLine, "{worktree}", shellQuote(worktree))
	cmdLine = strings.ReplaceAll(cmdLine, "{package_file}", shellQuote(tmpPath))

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
// in the host environment; other values are used as literals.
func resolveEnv(raw map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if strings.HasPrefix(v, "$") {
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
