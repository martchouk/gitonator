package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	BaseURL           string
	Token             string
	Assignee          string
	PollSeconds       int
	HeartbeatSeconds  int
	TmuxTarget        string
	TasksDir          string
	DispatchCommand   string
	ClaimLimit        int
}

type TaskRow struct {
	ID           int64           `json:"id"`
	IssueNumber  int             `json:"issue_number"`
	Role         string          `json:"role"`
	Assignee     string          `json:"assignee"`
	Action       string          `json:"action"`
	Status       string          `json:"status"`
	DedupKey     string          `json:"dedup_key"`
	Payload      json.RawMessage `json:"payload"`
	CreatedAt    string          `json:"created_at"`
}

type ListResp struct {
	OK    bool      `json:"ok"`
	Tasks []TaskRow `json:"tasks"`
}

type LocalTaskEnvelope struct {
	Task         TaskRow          `json:"task"`
	FetchedAtUTC string           `json:"fetched_at_utc"`
	ResultHint   LocalResultHint  `json:"result_hint"`
}

type LocalResultHint struct {
	DoneFile string `json:"done_file"`
	FailFile string `json:"fail_file"`
}

type LocalResult struct {
	Message string                 `json:"message"`
	Result  map[string]interface{} `json:"result"`
}

func main() {
	cfg := Config{
		BaseURL:          envOr("ORCH_BASE_URL", "https://mcp.singularia.de"),
		Token:            strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN")),
		Assignee:         envOr("AGENT_ASSIGNEE", "johnvolldepp"),
		PollSeconds:      envInt("POLL_SECONDS", 5),
		HeartbeatSeconds: envInt("HEARTBEAT_SECONDS", 20),
		TmuxTarget:       envOr("TMUX_TARGET", "johnvolldepp"),
		TasksDir:         envOr("TASKS_DIR", "./agent_tasks"),
		DispatchCommand:  strings.TrimSpace(os.Getenv("DISPATCH_COMMAND")),
		ClaimLimit:       envInt("CLAIM_LIMIT", 10),
	}

	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "AGENT_SHARED_TOKEN is required")
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.TasksDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "failed to create TASKS_DIR:", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	for {
		if err := pollOnce(client, cfg); err != nil {
			fmt.Fprintln(os.Stderr, "poll error:", err)
		}
		time.Sleep(time.Duration(cfg.PollSeconds) * time.Second)
	}
}

func pollOnce(client *http.Client, cfg Config) error {
	tasks, err := fetchTasks(client, cfg)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		if task.Status != "queued" {
			continue
		}
		if err := runTask(client, cfg, task); err != nil {
			fmt.Fprintf(os.Stderr, "task %d error: %v\n", task.ID, err)
		}
		// process one task per poll loop
		return nil
	}

	return nil
}

func fetchTasks(client *http.Client, cfg Config) ([]TaskRow, error) {
	u := fmt.Sprintf(
		"%s/api/v1/agent/tasks?assignee=%s&limit=%d",
		strings.TrimRight(cfg.BaseURL, "/"),
		cfg.Assignee,
		cfg.ClaimLimit,
	)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch tasks failed: %s", strings.TrimSpace(string(raw)))
	}

	var out ListResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Tasks, nil
}

func runTask(client *http.Client, cfg Config, task TaskRow) error {
	if err := postAction(client, cfg, task.ID, "claim", map[string]interface{}{
		"agent": cfg.Assignee,
	}); err != nil {
		return fmt.Errorf("claim failed: %w", err)
	}

	taskBase := fmt.Sprintf("task-%d-issue-%d", task.ID, task.IssueNumber)
	taskFile := filepath.Join(cfg.TasksDir, taskBase+".json")
	doneFile := filepath.Join(cfg.TasksDir, taskBase+".done.json")
	failFile := filepath.Join(cfg.TasksDir, taskBase+".fail.json")

	env := LocalTaskEnvelope{
		Task:         task,
		FetchedAtUTC: time.Now().UTC().Format(time.RFC3339),
		ResultHint: LocalResultHint{
			DoneFile: doneFile,
			FailFile: failFile,
		},
	}

	if err := writeJSONFile(taskFile, env); err != nil {
		_ = postAction(client, cfg, task.ID, "fail", map[string]interface{}{
			"agent":   cfg.Assignee,
			"message": fmt.Sprintf("failed to write task file: %v", err),
		})
		return err
	}

	if err := postAction(client, cfg, task.ID, "heartbeat", map[string]interface{}{
		"agent":   cfg.Assignee,
		"message": "task claimed; dispatching to local worker",
	}); err != nil {
		return fmt.Errorf("heartbeat after claim failed: %w", err)
	}

	if err := dispatchToLocalWorker(cfg, taskFile); err != nil {
		_ = postAction(client, cfg, task.ID, "fail", map[string]interface{}{
			"agent":   cfg.Assignee,
			"message": fmt.Sprintf("dispatch failed: %v", err),
		})
		return err
	}

	return waitForLocalCompletion(client, cfg, task, doneFile, failFile)
}

func dispatchToLocalWorker(cfg Config, taskFile string) error {
	var cmd *exec.Cmd

	if cfg.DispatchCommand != "" {
		line := strings.ReplaceAll(cfg.DispatchCommand, "{file}", shellQuote(taskFile))
		line = strings.ReplaceAll(line, "{assignee}", shellQuote(cfg.Assignee))
		line = strings.ReplaceAll(line, "{tmux_target}", shellQuote(cfg.TmuxTarget))
		cmd = exec.Command("sh", "-lc", line)
	} else {
		// default: send a visible notice into tmux
		line := fmt.Sprintf("echo 'NEW TASK FILE: %s'", taskFile)
		cmd = exec.Command("tmux", "send-keys", "-t", cfg.TmuxTarget, line, "C-m")
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func waitForLocalCompletion(client *http.Client, cfg Config, task TaskRow, doneFile, failFile string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	heartbeatTicker := time.NewTicker(time.Duration(cfg.HeartbeatSeconds) * time.Second)
	defer heartbeatTicker.Stop()

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()

	if err := postAction(client, cfg, task.ID, "heartbeat", map[string]interface{}{
		"agent":   cfg.Assignee,
		"message": "local worker started",
	}); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-heartbeatTicker.C:
			if err := postAction(client, cfg, task.ID, "heartbeat", map[string]interface{}{
				"agent":   cfg.Assignee,
				"message": "waiting for local completion file",
			}); err != nil {
				return fmt.Errorf("heartbeat failed: %w", err)
			}

		case <-pollTicker.C:
			if fileExists(doneFile) {
				res, err := readLocalResult(doneFile)
				if err != nil {
					return failTask(client, cfg, task.ID, fmt.Sprintf("invalid done file: %v", err), nil)
				}
				err = postAction(client, cfg, task.ID, "complete", map[string]interface{}{
					"agent":   cfg.Assignee,
					"message": defaultMessage(res.Message, "completed by local worker"),
					"result":  defaultResult(res.Result),
				})
				if err != nil {
					return err
				}
				_ = os.Remove(doneFile)
				return nil
			}

			if fileExists(failFile) {
				res, err := readLocalResult(failFile)
				if err != nil {
					return failTask(client, cfg, task.ID, fmt.Sprintf("invalid fail file: %v", err), nil)
				}
				err = postAction(client, cfg, task.ID, "fail", map[string]interface{}{
					"agent":   cfg.Assignee,
					"message": defaultMessage(res.Message, "failed by local worker"),
					"result":  defaultResult(res.Result),
				})
				if err != nil {
					return err
				}
				_ = os.Remove(failFile)
				return nil
			}
		}
	}
}

func failTask(client *http.Client, cfg Config, taskID int64, message string, result map[string]interface{}) error {
	return postAction(client, cfg, taskID, "fail", map[string]interface{}{
		"agent":   cfg.Assignee,
		"message": message,
		"result":  defaultResult(result),
	})
}

func postAction(client *http.Client, cfg Config, taskID int64, action string, payload map[string]interface{}) error {
	b, _ := json.Marshal(payload)
	u := fmt.Sprintf("%s/api/v1/agent/tasks/%d/%s", strings.TrimRight(cfg.BaseURL, "/"), taskID, action)

	req, _ := http.NewRequest(http.MethodPost, u, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s failed: %s", action, strings.TrimSpace(string(raw)))
	}
	return nil
}

func readLocalResult(path string) (LocalResult, error) {
	var out LocalResult
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

func writeJSONFile(path string, v interface{}) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func defaultMessage(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func defaultResult(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
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
	_, err := fmt.Sscanf(v, "%d", &n)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
