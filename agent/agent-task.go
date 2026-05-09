package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type LocalTaskEnvelope struct {
	Task struct {
		ID          int64           `json:"id"`
		IssueNumber int             `json:"issue_number"`
		IssueURL    string          `json:"issue_url"`
		Role        string          `json:"role"`
		Assignee    string          `json:"assignee"`
		Action      string          `json:"action"`
		Status      string          `json:"status"`
		DedupKey    string          `json:"dedup_key"`
		Payload     json.RawMessage `json:"payload"`
		CreatedAt   string          `json:"created_at"`
	} `json:"task"`
	FetchedAtUTC string `json:"fetched_at_utc"`
	ResultHint   struct {
		DoneFile string `json:"done_file"`
		FailFile string `json:"fail_file"`
	} `json:"result_hint"`
}

type LocalResult struct {
	Message string                 `json:"message"`
	Result  map[string]interface{} `json:"result"`
}

type CommentResponse struct {
	OK      bool   `json:"ok"`
	Comment struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	} `json:"comment"`
	Error string `json:"error"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		printUsage()
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return errors.New("missing command or taskfile")
	}

	command := strings.TrimSpace(args[0])
	taskFile := strings.TrimSpace(args[1])

	env, err := readTaskEnvelope(taskFile)
	if err != nil {
		return err
	}

	switch command {
	case "show":
		return showTask(taskFile, env)

	case "open":
		return openTask(env)

	case "comment":
		message, fields, err := parseCommentFlags(args[2:])
		if err != nil {
			return err
		}
		if strings.TrimSpace(message) == "" {
			return errors.New("comment requires --message")
		}
		return postComment(env, buildCommentBody(message, fields))

	case "handoff":
		to, state, summary, err := parseHandoffFlags(args[2:])
		if err != nil {
			return err
		}
		if to == "" || state == "" || summary == "" {
			return errors.New("handoff requires --to, --state and --summary")
		}
		body := buildHandoffBody(env, to, state, summary)
		return postComment(env, body)

	case "approve":
		return postComment(env, "/approve")

	case "complete", "fail":
		message, resultMap, err := parseResultFlags(args[2:])
		if err != nil {
			return err
		}

		if strings.TrimSpace(message) == "" {
			if command == "complete" {
				message = "completed by local worker"
			} else {
				message = "failed by local worker"
			}
		}

		out := LocalResult{
			Message: message,
			Result:  resultMap,
		}

		target := env.ResultHint.DoneFile
		if command == "fail" {
			target = env.ResultHint.FailFile
		}
		if strings.TrimSpace(target) == "" {
			return errors.New("target result file path is empty in task envelope")
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeJSONFile(target, out); err != nil {
			return err
		}

		fmt.Printf("%s task %d for issue #%d -> %s\n",
			strings.ToUpper(command),
			env.Task.ID,
			env.Task.IssueNumber,
			target,
		)
		return nil

	default:
		return fmt.Errorf("unsupported command: %s", command)
	}
}

func showTask(taskFile string, env LocalTaskEnvelope) error {
	fmt.Printf("Task file:      %s\n", taskFile)
	fmt.Printf("Task ID:        %d\n", env.Task.ID)
	fmt.Printf("Issue number:   %d\n", env.Task.IssueNumber)
	fmt.Printf("Issue URL:      %s\n", blankIfEmpty(env.Task.IssueURL))
	fmt.Printf("Role:           %s\n", env.Task.Role)
	fmt.Printf("Assignee:       %s\n", env.Task.Assignee)
	fmt.Printf("Action:         %s\n", env.Task.Action)
	fmt.Printf("Status:         %s\n", env.Task.Status)
	fmt.Printf("Dedup key:      %s\n", env.Task.DedupKey)
	fmt.Printf("Created at:     %s\n", blankIfEmpty(env.Task.CreatedAt))
	fmt.Printf("Fetched at:     %s\n", blankIfEmpty(env.FetchedAtUTC))
	fmt.Printf("Done file:      %s\n", blankIfEmpty(env.ResultHint.DoneFile))
	fmt.Printf("Fail file:      %s\n", blankIfEmpty(env.ResultHint.FailFile))

	fmt.Println("\nPayload:")
	if len(env.Task.Payload) == 0 {
		fmt.Println("  <empty>")
	} else {
		var pretty bytesHolder
		if err := prettyJSON(env.Task.Payload, &pretty); err != nil {
			fmt.Printf("  %s\n", string(env.Task.Payload))
		} else {
			fmt.Println(pretty.String())
		}
	}

	return nil
}

func openTask(env LocalTaskEnvelope) error {
	url := strings.TrimSpace(env.Task.IssueURL)
	if url == "" {
		return errors.New("task has no issue_url")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported OS for open: %s", runtime.GOOS)
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Printf("Opened issue URL: %s\n", url)
	return nil
}

func postComment(env LocalTaskEnvelope, body string) error {
	baseURL := strings.TrimSpace(os.Getenv("ORCH_BASE_URL"))
	token := strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN"))
	agent := strings.TrimSpace(os.Getenv("AGENT_ASSIGNEE"))

	if baseURL == "" {
		return errors.New("ORCH_BASE_URL is required")
	}
	if token == "" {
		return errors.New("AGENT_SHARED_TOKEN is required")
	}
	if env.Task.IssueNumber <= 0 {
		return errors.New("task has no issue_number")
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("empty comment body")
	}

	payload := map[string]interface{}{
		"issue_number": env.Task.IssueNumber,
		"body":         body,
		"agent":        agent,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := strings.TrimRight(baseURL, "/") + "/api/v1/agent/comment"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respRaw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("comment failed: %s", strings.TrimSpace(string(respRaw)))
	}

	var out CommentResponse
	if err := json.Unmarshal(respRaw, &out); err != nil {
		fmt.Printf("Comment posted for issue #%d\n", env.Task.IssueNumber)
		return nil
	}

	if !out.OK {
		if out.Error != "" {
			return fmt.Errorf("comment failed: %s", out.Error)
		}
		return errors.New("comment failed")
	}

	if out.Comment.HTMLURL != "" {
		fmt.Printf("Comment posted: %s\n", out.Comment.HTMLURL)
	} else {
		fmt.Printf("Comment posted for issue #%d\n", env.Task.IssueNumber)
	}
	return nil
}

func buildCommentBody(message string, fields map[string]string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(message))

	if len(fields) > 0 {
		b.WriteString("\n\n")
		keys := sortedKeys(fields)
		for _, k := range keys {
			b.WriteString("- ")
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(fields[k])
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func buildHandoffBody(env LocalTaskEnvelope, to, state, summary string) string {
	from := strings.TrimSpace(os.Getenv("AGENT_ASSIGNEE"))
	if from == "" {
		from = env.Task.Assignee
	}
	return strings.TrimSpace(fmt.Sprintf(
		"[handoff]\nfrom: %s\nto: %s\nstate: %s\nsummary: %s\n[/handoff]",
		from,
		to,
		state,
		summary,
	))
}

func parseResultFlags(args []string) (string, map[string]interface{}, error) {
	message := ""
	result := map[string]interface{}{}

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--message":
			if i+1 >= len(args) {
				return "", nil, errors.New("--message requires a value")
			}
			message = args[i+1]
			i += 2

		case "--result":
			if i+1 >= len(args) {
				return "", nil, errors.New("--result requires key=value")
			}
			k, v, err := parseKeyValue(args[i+1])
			if err != nil {
				return "", nil, err
			}
			result[k] = v
			i += 2

		default:
			return "", nil, fmt.Errorf("unknown argument: %s", args[i])
		}
	}

	return message, result, nil
}

func parseCommentFlags(args []string) (string, map[string]string, error) {
	message := ""
	fields := map[string]string{}

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--message":
			if i+1 >= len(args) {
				return "", nil, errors.New("--message requires a value")
			}
			message = args[i+1]
			i += 2

		case "--field":
			if i+1 >= len(args) {
				return "", nil, errors.New("--field requires key=value")
			}
			k, v, err := parseStringKeyValue(args[i+1])
			if err != nil {
				return "", nil, err
			}
			fields[k] = v
			i += 2

		default:
			return "", nil, fmt.Errorf("unknown argument: %s", args[i])
		}
	}

	return message, fields, nil
}

func parseHandoffFlags(args []string) (string, string, string, error) {
	var to, state, summary string

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--to":
			if i+1 >= len(args) {
				return "", "", "", errors.New("--to requires a value")
			}
			to = strings.TrimSpace(args[i+1])
			i += 2
		case "--state":
			if i+1 >= len(args) {
				return "", "", "", errors.New("--state requires a value")
			}
			state = strings.TrimSpace(args[i+1])
			i += 2
		case "--summary":
			if i+1 >= len(args) {
				return "", "", "", errors.New("--summary requires a value")
			}
			summary = strings.TrimSpace(args[i+1])
			i += 2
		default:
			return "", "", "", fmt.Errorf("unknown argument: %s", args[i])
		}
	}

	return to, state, summary, nil
}

func parseKeyValue(s string) (string, interface{}, error) {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid --result value: %s (want key=value)", s)
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	if key == "" {
		return "", nil, errors.New("result key must not be empty")
	}

	var anyVal interface{}
	if json.Unmarshal([]byte(val), &anyVal) == nil {
		return key, anyVal, nil
	}
	return key, val, nil
}

func parseStringKeyValue(s string) (string, string, error) {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid key=value: %s", s)
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", errors.New("key must not be empty")
	}
	return key, val, nil
}

func readTaskEnvelope(path string) (LocalTaskEnvelope, error) {
	var env LocalTaskEnvelope
	raw, err := os.ReadFile(path)
	if err != nil {
		return env, err
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, err
	}
	return env, nil
}

func writeJSONFile(path string, v interface{}) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func blankIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "<empty>"
	}
	return s
}

type bytesHolder struct {
	b []byte
}

func (h *bytesHolder) Write(p []byte) (int, error) {
	h.b = append(h.b, p...)
	return len(p), nil
}

func (h *bytesHolder) String() string {
	return string(h.b)
}

func prettyJSON(raw json.RawMessage, out *bytesHolder) error {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `
Usage:
  agent-task show     <taskfile>
  agent-task open     <taskfile>
  agent-task comment  <taskfile> --message "..." [--field key=value]...
  agent-task handoff  <taskfile> --to bobwurst --state ready-for-review --summary "..."
  agent-task approve  <taskfile>
  agent-task complete <taskfile> [--message "..."] [--result key=value]...
  agent-task fail     <taskfile> [--message "..."] [--result key=value]...

Examples:
  agent-task show ./agent_tasks/task-17-issue-42.json

  agent-task open ./agent_tasks/task-17-issue-42.json

  agent-task comment ./agent_tasks/task-17-issue-42.json \
    --message "Review finished and posted to the issue." \
    --field outcome=accepted

  agent-task handoff ./agent_tasks/task-17-issue-42.json \
    --to bobwurst \
    --state ready-for-review \
    --summary "Implementation finished, ready for static review."

  agent-task approve ./agent_tasks/task-17-issue-42.json

  agent-task complete ./agent_tasks/task-17-issue-42.json \
    --message "Review finished and posted to GitHub" \
    --result outcome=accepted

  agent-task fail ./agent_tasks/task-17-issue-42.json \
    --message "Repo checkout missing" \
    --result reason=missing_checkout
`)
}
