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
	"runtime"
	"strings"
	"time"
)

// WorkPackage is the work unit received from the orchestrator via AGENTS_CONFIG.
type WorkPackage struct {
	ID                int64    `json:"id"`
	Repo              string   `json:"repo"`
	IssueID           int      `json:"issue_id"`
	Role              string   `json:"role"`
	Assignee          string   `json:"assignee"`
	LastCommentID     int64    `json:"last_comment_id"`
	CurrentStatus     string   `json:"current_status"`
	WorkflowKey       string   `json:"workflow_key"`
	ValidTransitions  []string `json:"valid_transitions"`
	NextAssigneeRoles []string `json:"next_assignee_roles"`
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
		return errors.New("missing command or package file")
	}

	command := strings.TrimSpace(args[0])
	pkgFile := strings.TrimSpace(args[1])

	pkg, err := readWorkPackage(pkgFile)
	if err != nil {
		return err
	}

	switch command {
	case "show":
		return showPackage(pkgFile, pkg)

	case "open":
		return openIssue(pkg)

	case "comment":
		message, fields, err := parseCommentFlags(args[2:])
		if err != nil {
			return err
		}
		if strings.TrimSpace(message) == "" {
			return errors.New("comment requires --message")
		}
		return postGitHubComment(pkg, buildCommentBody(message, fields))

	case "approve":
		// Legacy low-level helper: posts "/approve" as a raw GitHub comment.
		// The workflow engine does NOT act on this comment automatically;
		// approval is driven by MCP tool calls, not by comment content.
		return postGitHubComment(pkg, "/approve")

	default:
		return fmt.Errorf("unsupported command: %s", command)
	}
}

func showPackage(pkgFile string, pkg WorkPackage) error {
	fmt.Printf("Package file:   %s\n", pkgFile)
	fmt.Printf("Task ID:        %d\n", pkg.ID)
	fmt.Printf("Repo:           %s\n", blankIfEmpty(pkg.Repo))
	fmt.Printf("Issue ID:       %d\n", pkg.IssueID)
	fmt.Printf("Issue URL:      https://github.com/%s/issues/%d\n", pkg.Repo, pkg.IssueID)
	fmt.Printf("Role:           %s\n", pkg.Role)
	fmt.Printf("Assignee:       %s\n", blankIfEmpty(pkg.Assignee))
	fmt.Printf("Last comment:   %d\n", pkg.LastCommentID)
	fmt.Printf("Current status: %s\n", blankIfEmpty(pkg.CurrentStatus))
	fmt.Printf("Workflow:       %s\n", blankIfEmpty(pkg.WorkflowKey))
	fmt.Printf("Valid next:     %s\n", strings.Join(pkg.ValidTransitions, ", "))
	fmt.Printf("Next roles:     %s\n", strings.Join(pkg.NextAssigneeRoles, ", "))
	return nil
}

func openIssue(pkg WorkPackage) error {
	if pkg.Repo == "" || pkg.IssueID <= 0 {
		return errors.New("work package has no repo or issue_id")
	}
	u := fmt.Sprintf("https://github.com/%s/issues/%d", pkg.Repo, pkg.IssueID)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		return fmt.Errorf("unsupported OS for open: %s", runtime.GOOS)
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Printf("Opened: %s\n", u)
	return nil
}

// postGitHubComment posts a comment to the issue using the agent's own GitHub token.
// Requires env vars: GITHUB_TOKEN, GITHUB_OWNER, GITHUB_REPO (or derives owner/repo from pkg.Repo).
func postGitHubComment(pkg WorkPackage, body string) error {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		return errors.New("GITHUB_TOKEN is required")
	}
	if pkg.IssueID <= 0 {
		return errors.New("work package has no issue_id")
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("empty comment body")
	}

	repo := pkg.Repo
	if repo == "" {
		owner := strings.TrimSpace(os.Getenv("GITHUB_OWNER"))
		repoName := strings.TrimSpace(os.Getenv("GITHUB_REPO"))
		if owner == "" || repoName == "" {
			return errors.New("pkg.Repo is empty and GITHUB_OWNER/GITHUB_REPO are not set")
		}
		repo = owner + "/" + repoName
	}

	u := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repo, pkg.IssueID)

	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub comment failed status=%d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(raw, &out); err == nil && out.HTMLURL != "" {
		fmt.Printf("Comment posted: %s\n", out.HTMLURL)
	} else {
		fmt.Printf("Comment posted to issue #%d\n", pkg.IssueID)
	}
	return nil
}

func buildCommentBody(message string, fields map[string]string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(message))
	if len(fields) > 0 {
		b.WriteString("\n\n")
		for _, k := range sortedKeys(fields) {
			b.WriteString("- ")
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(fields[k])
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
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

func parseStringKeyValue(s string) (string, string, error) {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid key=value: %s", s)
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", errors.New("key must not be empty")
	}
	return key, strings.TrimSpace(parts[1]), nil
}

func readWorkPackage(path string) (WorkPackage, error) {
	var pkg WorkPackage
	raw, err := os.ReadFile(path)
	if err != nil {
		return pkg, err
	}
	raw = extractWorkPackageJSON(raw)
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return pkg, err
	}
	return pkg, nil
}

func extractWorkPackageJSON(raw []byte) []byte {
	const marker = "WORK PACKAGE JSON:"
	text := string(raw)
	idx := strings.Index(text, marker)
	if idx < 0 {
		return raw
	}
	return []byte(strings.TrimSpace(text[idx+len(marker):]))
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

func printUsage() {
	fmt.Fprintln(os.Stderr, `
Usage:
  agent-task show    <package-file>
  agent-task open    <package-file>
  agent-task comment <package-file> --message "..." [--field key=value]...

Environment variables for comment:
  GITHUB_TOKEN   — required; agent's own GitHub token
  GITHUB_OWNER   — fallback if pkg.Repo is empty
  GITHUB_REPO    — fallback if pkg.Repo is empty

Examples:
  agent-task show ./work-17.json

  agent-task open ./work-17.json

  agent-task comment ./work-17.json \
    --message "Implementation finished, opening PR." \
    --field pr=https://github.com/org/repo/pull/42`)
}
