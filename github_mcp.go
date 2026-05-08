package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// First MVP of a GitHub Issue workflow MCP server.
//
// Transport:
//   - stdio JSON-RPC 2.0 compatible with MCP clients
//
// Configuration via environment variables:
//   GITHUB_TOKEN   - GitHub personal access token or GitHub App token
//   GITHUB_OWNER   - repository owner/org
//   GITHUB_REPO    - repository name
//   LOG_LEVEL      - optional, set to DEBUG for stderr debug logs
//
// Exposed MCP tools:
//   - get_issue_context
//   - list_issue_comments
//   - post_issue_comment
//   - assign_issue
//   - set_issue_labels
//   - add_issue_labels
//   - remove_issue_label
//   - transition_issue
//   - get_workflow_state
//   - find_stakeholder_approvals
//
// The workflow logic follows the earlier GitHub issue state-machine discussion:
// single active assignee + one status:* label at a time.

type Config struct {
	GitHubToken string
	Owner       string
	Repo        string
	Debug       bool
}

type Server struct {
	cfg    Config
	gh     *GitHubClient
	logger *log.Logger
}

type GitHubClient struct {
	baseURL    string
	httpClient *http.Client
	token      string
	owner      string
	repo       string
}

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type Issue struct {
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	State     string        `json:"state"`
	HTMLURL   string        `json:"html_url"`
	User      GitHubUser    `json:"user"`
	Assignees []GitHubUser  `json:"assignees"`
	Labels    []GitHubLabel `json:"labels"`
}

type GitHubUser struct {
	Login string `json:"login"`
}

type GitHubLabel struct {
	Name string `json:"name"`
}

type IssueComment struct {
	ID        int64      `json:"id"`
	Body      string     `json:"body"`
	HTMLURL   string     `json:"html_url"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	User      GitHubUser `json:"user"`
}

type WorkflowState struct {
	StatusLabel       string   `json:"statusLabel"`
	TypeLabels        []string `json:"typeLabels"`
	CurrentAssignees  []string `json:"currentAssignees"`
	SuggestedRole     string   `json:"suggestedRole"`
	Stakeholder       string   `json:"stakeholder"`
	NeedsPO           bool     `json:"needsPO"`
	NeedsReviewer     bool     `json:"needsReviewer"`
	NeedsDeveloper    bool     `json:"needsDeveloper"`
	NeedsStakeholder  bool     `json:"needsStakeholder"`
	RecognizedApprove bool     `json:"recognizedApprove"`
}

var (
	statusLabels = []string{
		"status:new",
		"status:po-analysis",
		"status:awaiting-stakeholder-approval",
		"status:approved-for-dev",
		"status:in-progress",
		"status:ready-for-review",
		"status:review-in-progress",
		"status:changes-requested",
		"status:ready-for-po-review",
		"status:po-review-in-progress",
		"status:awaiting-final-stakeholder-approval",
		"status:blocked",
		"status:done",
		"status:rejected",
	}
	poUser        = "thebesserwisser"
	developerUser = "johnvolldepp"
	reviewerUser  = "bobwurst"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	logger := log.New(os.Stderr, "[github-mcp] ", log.LstdFlags|log.LUTC)
	server := &Server{
		cfg: cfg,
		gh: &GitHubClient{
			baseURL: "https://api.github.com",
			httpClient: &http.Client{
				Timeout: 30 * time.Second,
			},
			token: cfg.GitHubToken,
			owner: cfg.Owner,
			repo: cfg.Repo,
		},
		logger: logger,
	}

	if err := server.run(context.Background(), os.Stdin, os.Stdout); err != nil {
		logger.Println("fatal:", err)
		os.Exit(1)
	}
}

func loadConfig() (Config, error) {
	cfg := Config{
		GitHubToken: strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		Owner:       strings.TrimSpace(os.Getenv("GITHUB_OWNER")),
		Repo:        strings.TrimSpace(os.Getenv("GITHUB_REPO")),
		Debug:       strings.EqualFold(strings.TrimSpace(os.Getenv("LOG_LEVEL")), "DEBUG"),
	}
	if cfg.GitHubToken == "" {
		return cfg, errors.New("GITHUB_TOKEN is required")
	}
	if cfg.Owner == "" || cfg.Repo == "" {
		return cfg, errors.New("GITHUB_OWNER and GITHUB_REPO are required")
	}
	return cfg, nil
}

func (s *Server) run(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if s.cfg.Debug {
			s.logger.Println("recv:", line)
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &RPCError{Code: -32700, Message: "parse error", Data: err.Error()},
			})
			continue
		}

		resp := s.handleRequest(ctx, req)
		if s.cfg.Debug {
			b, _ := json.Marshal(resp)
			s.logger.Println("send:", string(b))
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) handleRequest(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	id := decodeID(req.ID)
	respondErr := func(code int, msg string, data interface{}) JSONRPCResponse {
		return JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg, Data: data}}
	}

	switch req.Method {
	case "initialize":
		return JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "github-issue-orchestrator",
				"version": "0.1.0",
			},
		}}
	case "notifications/initialized":
		return JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: map[string]any{}}
	case "ping":
		return JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: map[string]string{"pong": "ok"}}
	case "tools/list":
		return JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{"tools": s.tools()}}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return respondErr(-32602, "invalid params", err.Error())
		}
		result, err := s.callTool(ctx, params.Name, params.Arguments)
		if err != nil {
			return respondErr(-32000, "tool call failed", err.Error())
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{
			"content": []map[string]string{{
				"type": "text",
				"text": prettyJSON(result),
			}},
			"structuredContent": result,
		}}
	default:
		return respondErr(-32601, "method not found", req.Method)
	}
}

func (s *Server) tools() []Tool {
	return []Tool{
		newTool("get_issue_context", "Fetch an issue plus comments and normalized workflow state", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"comment_limit": map[string]interface{}{"type": "integer", "default": 20},
			},
			"required": []string{"issue_number"},
		}),
		newTool("list_issue_comments", "List comments on an issue", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"limit":        map[string]interface{}{"type": "integer", "default": 50},
			},
			"required": []string{"issue_number"},
		}),
		newTool("post_issue_comment", "Post a comment to an issue", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"body":         map[string]interface{}{"type": "string"},
			},
			"required": []string{"issue_number", "body"},
		}),
		newTool("assign_issue", "Set assignees on an issue", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"assignees":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"issue_number", "assignees"},
		}),
		newTool("set_issue_labels", "Replace all labels on an issue", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"labels":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"issue_number", "labels"},
		}),
		newTool("add_issue_labels", "Add labels to an issue", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"labels":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"issue_number", "labels"},
		}),
		newTool("remove_issue_label", "Remove a label from an issue", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"label":        map[string]interface{}{"type": "string"},
			},
			"required": []string{"issue_number", "label"},
		}),
		newTool("get_workflow_state", "Compute normalized workflow state from assignee/labels/comments", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"issue_number"},
		}),
		newTool("find_stakeholder_approvals", "Detect /approve comments from the stakeholder", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"issue_number"},
		}),
		newTool("transition_issue", "Apply a workflow transition: set assignee, enforce single status:* label, optionally add a comment", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"status":       map[string]interface{}{"type": "string"},
				"assignee":     map[string]interface{}{"type": "string"},
				"comment":      map[string]interface{}{"type": "string"},
			},
			"required": []string{"issue_number", "status", "assignee"},
		}),
	}
}

func newTool(name, desc string, schema map[string]interface{}) Tool {
	return Tool{Name: name, Description: desc, InputSchema: schema}
}

func (s *Server) callTool(ctx context.Context, name string, raw json.RawMessage) (interface{}, error) {
	switch name {
	case "get_issue_context":
		var args struct {
			IssueNumber  int `json:"issue_number"`
			CommentLimit int `json:"comment_limit"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		if args.CommentLimit <= 0 {
			args.CommentLimit = 20
		}
		issue, err := s.gh.GetIssue(ctx, args.IssueNumber)
		if err != nil {
			return nil, err
		}
		comments, err := s.gh.ListIssueComments(ctx, args.IssueNumber, args.CommentLimit)
		if err != nil {
			return nil, err
		}
		state := computeWorkflowState(issue, comments)
		return map[string]interface{}{
			"issue":    issue,
			"comments": comments,
			"workflow": state,
		}, nil

	case "list_issue_comments":
		var args struct {
			IssueNumber int `json:"issue_number"`
			Limit       int `json:"limit"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		if args.Limit <= 0 {
			args.Limit = 50
		}
		comments, err := s.gh.ListIssueComments(ctx, args.IssueNumber, args.Limit)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"comments": comments}, nil

	case "post_issue_comment":
		var args struct {
			IssueNumber int    `json:"issue_number"`
			Body        string `json:"body"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		comment, err := s.gh.PostIssueComment(ctx, args.IssueNumber, args.Body)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"comment": comment}, nil

	case "assign_issue":
		var args struct {
			IssueNumber int      `json:"issue_number"`
			Assignees   []string `json:"assignees"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		issue, err := s.gh.AssignIssue(ctx, args.IssueNumber, args.Assignees)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"issue": issue}, nil

	case "set_issue_labels":
		var args struct {
			IssueNumber int      `json:"issue_number"`
			Labels      []string `json:"labels"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		labels, err := s.gh.SetIssueLabels(ctx, args.IssueNumber, args.Labels)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"labels": labels}, nil

	case "add_issue_labels":
		var args struct {
			IssueNumber int      `json:"issue_number"`
			Labels      []string `json:"labels"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		labels, err := s.gh.AddIssueLabels(ctx, args.IssueNumber, args.Labels)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"labels": labels}, nil

	case "remove_issue_label":
		var args struct {
			IssueNumber int    `json:"issue_number"`
			Label       string `json:"label"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		if err := s.gh.RemoveIssueLabel(ctx, args.IssueNumber, args.Label); err != nil {
			return nil, err
		}
		return map[string]interface{}{"removed": args.Label}, nil

	case "get_workflow_state":
		var args struct {
			IssueNumber int `json:"issue_number"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		issue, err := s.gh.GetIssue(ctx, args.IssueNumber)
		if err != nil {
			return nil, err
		}
		comments, err := s.gh.ListIssueComments(ctx, args.IssueNumber, 100)
		if err != nil {
			return nil, err
		}
		return computeWorkflowState(issue, comments), nil

	case "find_stakeholder_approvals":
		var args struct {
			IssueNumber int `json:"issue_number"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		issue, err := s.gh.GetIssue(ctx, args.IssueNumber)
		if err != nil {
			return nil, err
		}
		comments, err := s.gh.ListIssueComments(ctx, args.IssueNumber, 100)
		if err != nil {
			return nil, err
		}
		stakeholder := issue.User.Login
		var approvals []IssueComment
		for _, c := range comments {
			if c.User.Login == stakeholder && containsApprove(c.Body) {
				approvals = append(approvals, c)
			}
		}
		return map[string]interface{}{
			"stakeholder": stakeholder,
			"approved":    len(approvals) > 0,
			"comments":    approvals,
		}, nil

	case "transition_issue":
		var args struct {
			IssueNumber int    `json:"issue_number"`
			Status      string `json:"status"`
			Assignee    string `json:"assignee"`
			Comment     string `json:"comment"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		if !isAllowedStatus(args.Status) {
			return nil, fmt.Errorf("unsupported status label: %s", args.Status)
		}
		issue, err := s.gh.GetIssue(ctx, args.IssueNumber)
		if err != nil {
			return nil, err
		}
		currentLabels := labelsToStrings(issue.Labels)
		var nextLabels []string
		for _, l := range currentLabels {
			if !strings.HasPrefix(l, "status:") {
				nextLabels = append(nextLabels, l)
			}
		}
		nextLabels = append(nextLabels, args.Status)
		if _, err := s.gh.SetIssueLabels(ctx, args.IssueNumber, nextLabels); err != nil {
			return nil, err
		}
		if _, err := s.gh.AssignIssue(ctx, args.IssueNumber, []string{args.Assignee}); err != nil {
			return nil, err
		}
		var comment *IssueComment
		if strings.TrimSpace(args.Comment) != "" {
			c, err := s.gh.PostIssueComment(ctx, args.IssueNumber, args.Comment)
			if err != nil {
				return nil, err
			}
			comment = &c
		}
		updated, err := s.gh.GetIssue(ctx, args.IssueNumber)
		if err != nil {
			return nil, err
		}
		result := map[string]interface{}{"issue": updated}
		if comment != nil {
			result["comment"] = comment
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (gh *GitHubClient) GetIssue(ctx context.Context, issueNumber int) (Issue, error) {
	var issue Issue
	err := gh.doJSON(ctx, http.MethodGet, gh.issueURL(issueNumber), nil, &issue)
	return issue, err
}

func (gh *GitHubClient) ListIssueComments(ctx context.Context, issueNumber, limit int) ([]IssueComment, error) {
	if limit <= 0 {
		limit = 50
	}
	var comments []IssueComment
	url := gh.issueCommentsURL(issueNumber) + "?per_page=" + strconv.Itoa(limit)
	err := gh.doJSON(ctx, http.MethodGet, url, nil, &comments)
	return comments, err
}

func (gh *GitHubClient) PostIssueComment(ctx context.Context, issueNumber int, body string) (IssueComment, error) {
	var out IssueComment
	payload := map[string]string{"body": body}
	err := gh.doJSON(ctx, http.MethodPost, gh.issueCommentsURL(issueNumber), payload, &out)
	return out, err
}

func (gh *GitHubClient) AssignIssue(ctx context.Context, issueNumber int, assignees []string) (Issue, error) {
	var out Issue
	payload := map[string][]string{"assignees": assignees}
	err := gh.doJSON(ctx, http.MethodPatch, gh.issueURL(issueNumber), payload, &out)
	return out, err
}

func (gh *GitHubClient) SetIssueLabels(ctx context.Context, issueNumber int, labels []string) ([]GitHubLabel, error) {
	var out []GitHubLabel
	err := gh.doJSON(ctx, http.MethodPut, gh.issueLabelsURL(issueNumber), labels, &out)
	return out, err
}

func (gh *GitHubClient) AddIssueLabels(ctx context.Context, issueNumber int, labels []string) ([]GitHubLabel, error) {
	var out []GitHubLabel
	payload := map[string][]string{"labels": labels}
	err := gh.doJSON(ctx, http.MethodPost, gh.issueLabelsURL(issueNumber), payload, &out)
	return out, err
}

func (gh *GitHubClient) RemoveIssueLabel(ctx context.Context, issueNumber int, label string) error {
	return gh.doJSON(ctx, http.MethodDelete, gh.issueLabelsURL(issueNumber)+"/"+labelEscape(label), nil, nil)
}

func (gh *GitHubClient) doJSON(ctx context.Context, method, url string, payload interface{}, out interface{}) error {
	var body io.Reader
	if payload != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			return err
		}
		body = buf
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+gh.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := gh.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github API %s %s failed: status=%d body=%s", method, url, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (gh *GitHubClient) issueURL(issueNumber int) string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/%d", gh.baseURL, gh.owner, gh.repo, issueNumber)
}

func (gh *GitHubClient) issueCommentsURL(issueNumber int) string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", gh.baseURL, gh.owner, gh.repo, issueNumber)
}

func (gh *GitHubClient) issueLabelsURL(issueNumber int) string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels", gh.baseURL, gh.owner, gh.repo, issueNumber)
}

func computeWorkflowState(issue Issue, comments []IssueComment) WorkflowState {
	status := ""
	var types []string
	for _, l := range issue.Labels {
		if strings.HasPrefix(l.Name, "status:") {
			status = l.Name
		}
		if strings.HasPrefix(l.Name, "type:") {
			types = append(types, l.Name)
		}
	}
	assignees := make([]string, 0, len(issue.Assignees))
	for _, a := range issue.Assignees {
		assignees = append(assignees, a.Login)
	}

	stakeholder := issue.User.Login
	approved := false
	for _, c := range comments {
		if c.User.Login == stakeholder && containsApprove(c.Body) {
			approved = true
		}
	}

	ws := WorkflowState{
		StatusLabel:       status,
		TypeLabels:        types,
		CurrentAssignees:  assignees,
		Stakeholder:       stakeholder,
		RecognizedApprove: approved,
	}

	switch status {
	case "status:new", "status:po-analysis":
		ws.SuggestedRole = "po"
		ws.NeedsPO = true
	case "status:awaiting-stakeholder-approval", "status:awaiting-final-stakeholder-approval":
		ws.SuggestedRole = "stakeholder"
		ws.NeedsStakeholder = true
	case "status:approved-for-dev", "status:in-progress", "status:changes-requested":
		ws.SuggestedRole = "developer"
		ws.NeedsDeveloper = true
	case "status:ready-for-review", "status:review-in-progress":
		ws.SuggestedRole = "reviewer"
		ws.NeedsReviewer = true
	case "status:ready-for-po-review", "status:po-review-in-progress":
		ws.SuggestedRole = "po"
		ws.NeedsPO = true
	case "status:done":
		ws.SuggestedRole = "done"
	case "status:blocked", "status:rejected":
		ws.SuggestedRole = "manual"
	default:
		ws.SuggestedRole = "unknown"
	}
	return ws
}

func containsApprove(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "/approve" {
			return true
		}
	}
	return false
}

func labelsToStrings(labels []GitHubLabel) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		out = append(out, l.Name)
	}
	return out
}

func isAllowedStatus(status string) bool {
	for _, s := range statusLabels {
		if s == status {
			return true
		}
	}
	return false
}

func labelEscape(label string) string {
	// GitHub label names in URLs must be URL-escaped enough for spaces/colons.
	repl := strings.NewReplacer(" ", "%20", ":", "%3A", "/", "%2F")
	return repl.Replace(label)
}

func prettyJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func decodeID(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

