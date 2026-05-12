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
	Worktrees      map[string]string `json:"worktrees"`
}

type Roster struct {
	Agents []Agent `json:"agents"`
}

type WorkPackage struct {
	ID            int64  `json:"id"`
	Repo          string `json:"repo"`
	IssueID       int    `json:"issue_id"`
	Role          string `json:"role"`
	Assignee      string `json:"assignee"`
	LastCommentID int64  `json:"last_comment_id"`
	CurrentStatus string `json:"current_status"`
}

type workNextResp struct {
	OK   bool         `json:"ok"`
	Task *WorkPackage `json:"task"`
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

	logger := log.New(os.Stderr, fmt.Sprintf("[bridge/%s] ", cfg.BridgeID), log.LstdFlags|log.LUTC)
	debug := cfg.LogLevel == "DEBUG"

	roles := collectRoles(roster)
	logger.Printf("started: bridge_id=%s agents=%d roles=%s poll=%ds",
		cfg.BridgeID, len(roster.Agents), strings.Join(roles, ","), cfg.PollSeconds)

	client := &http.Client{Timeout: 30 * time.Second}

	for {
		pkg, err := fetchNextWork(client, cfg, roles)
		if err != nil {
			logger.Println("poll error:", err)
			time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
			continue
		}

		if pkg == nil {
			if debug {
				logger.Println("no work available")
			}
			time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
			continue
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

		if err := runAgent(logger, agent, worktree, pkg); err != nil {
			logger.Printf("agent run error: agent=%s issue=%d err=%v", agent.Name, pkg.IssueID, err)
		}
		// No sleep — poll immediately after agent exits.
	}
}

// selectAgent finds the agent to use for the work package.
// Priority 1: match by assignee name.
// Priority 2: match by role.
func selectAgent(roster Roster, pkg *WorkPackage) *Agent {
	if pkg.Assignee != "" {
		for i := range roster.Agents {
			if roster.Agents[i].Name == pkg.Assignee {
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

func runAgent(logger *log.Logger, agent *Agent, worktree string, pkg *WorkPackage) error {
	pkgData, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal work package: %w", err)
	}

	tmpFile, err := os.CreateTemp("", fmt.Sprintf("work-%d-*.json", pkg.ID))
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(pkgData); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	cmdLine := agent.LaunchTemplate
	cmdLine = strings.ReplaceAll(cmdLine, "{worktree}", worktree)
	cmdLine = strings.ReplaceAll(cmdLine, "{package_file}", tmpPath)

	cmd := exec.Command("sh", "-c", cmdLine)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn agent: %w", err)
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

	return nil
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
