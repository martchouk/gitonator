package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

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

type WorkflowState struct {
	StatusLabel           string          `json:"statusLabel"`
	TypeLabels            []string        `json:"typeLabels"`
	CurrentAssignees      []string        `json:"currentAssignees"`
	SuggestedRole         string          `json:"suggestedRole"`
	Stakeholder           string          `json:"stakeholder"`
	RecognizedApprove     bool            `json:"recognizedApprove"`
	ParsedDirectives      []CommentIntent `json:"parsedDirectives"`
	NeedsPO               bool            `json:"needsPO"`
	NeedsReviewer         bool            `json:"needsReviewer"`
	NeedsDeveloper        bool            `json:"needsDeveloper"`
	NeedsStakeholder      bool            `json:"needsStakeholder"`
	NeedsFinalStakeholder bool            `json:"needsFinalStakeholder"`
}

type CommentIntent struct {
	Kind        string            `json:"kind"`
	Actor       string            `json:"actor"`
	Fields      map[string]string `json:"fields,omitempty"`
	CommentID   int64             `json:"commentId"`
	CreatedAt   time.Time         `json:"createdAt"`
	BodyPreview string            `json:"bodyPreview"`
}

type TransitionRule struct {
	FromStatuses               []string `json:"fromStatuses"`
	ToStatus                   string   `json:"toStatus"`
	AllowedActors              []string `json:"allowedActors"`
	RequiredAssigneeBefore     []string `json:"requiredAssigneeBefore"`
	RequiredAssigneeAfter      string   `json:"requiredAssigneeAfter"`
	RequiresStakeholderApprove bool     `json:"requiresStakeholderApprove"`
	Description                string   `json:"description"`
}

type TransitionValidationResult struct {
	Allowed                bool     `json:"allowed"`
	Actor                  string   `json:"actor"`
	FromStatus             string   `json:"fromStatus"`
	ToStatus               string   `json:"toStatus"`
	RequiredAssigneeAfter  string   `json:"requiredAssigneeAfter"`
	Violations             []string `json:"violations"`
	MatchedRuleDescription string   `json:"matchedRuleDescription,omitempty"`
}

type CommentTransitionDecision struct {
	Matched     bool   `json:"matched"`
	Reason      string `json:"reason"`
	ToStatus    string `json:"toStatus"`
	ToAssignee  string `json:"toAssignee"`
	Actor       string `json:"actor"`
	SourceKind  string `json:"sourceKind"`
	CommentID   int64  `json:"commentId"`
	BodyPreview string `json:"bodyPreview"`
}

type IssueTimelineEntry struct {
	Kind     string      `json:"kind"`
	SortTime string      `json:"sort_time"`
	Comment  interface{} `json:"comment,omitempty"`
	Audit    interface{} `json:"audit,omitempty"`
	Task     interface{} `json:"task,omitempty"`
}

type IssueTimelineResult struct {
	Issue    Issue                `json:"issue"`
	Workflow WorkflowState        `json:"workflow"`
	Timeline []IssueTimelineEntry `json:"timeline"`
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

	transitionRules = []TransitionRule{
		{[]string{"status:new", "status:po-analysis"}, "status:po-analysis", []string{poUser}, []string{"", poUser}, poUser, false, "PO can move a new issue into PO analysis."},
		{[]string{"status:new", "status:po-analysis"}, "status:awaiting-stakeholder-approval", []string{poUser}, []string{"", poUser}, "$stakeholder", false, "PO can request stakeholder approval."},
		{[]string{"status:awaiting-stakeholder-approval"}, "status:approved-for-dev", []string{"$stakeholder", poUser}, []string{"$stakeholder", poUser}, developerUser, true, "Stakeholder-approved scope can move to developer."},
		{[]string{"status:approved-for-dev", "status:changes-requested", "status:in-progress"}, "status:in-progress", []string{developerUser}, []string{developerUser}, developerUser, false, "Developer can start or continue implementation."},
		{[]string{"status:in-progress", "status:changes-requested"}, "status:ready-for-review", []string{developerUser}, []string{developerUser}, reviewerUser, false, "Developer can hand work to reviewer."},
		{[]string{"status:ready-for-review", "status:review-in-progress"}, "status:review-in-progress", []string{reviewerUser}, []string{reviewerUser}, reviewerUser, false, "Reviewer can begin or continue review."},
		{[]string{"status:ready-for-review", "status:review-in-progress"}, "status:changes-requested", []string{reviewerUser}, []string{reviewerUser}, developerUser, false, "Reviewer can reject and send back to developer."},
		{[]string{"status:ready-for-review", "status:review-in-progress"}, "status:ready-for-po-review", []string{reviewerUser}, []string{reviewerUser}, poUser, false, "Reviewer can accept and hand to PO."},
		{[]string{"status:ready-for-po-review", "status:po-review-in-progress"}, "status:po-review-in-progress", []string{poUser}, []string{poUser}, poUser, false, "PO can begin or continue PO review."},
		{[]string{"status:ready-for-po-review", "status:po-review-in-progress"}, "status:changes-requested", []string{poUser}, []string{poUser}, developerUser, false, "PO can reject and send back to developer."},
		{[]string{"status:ready-for-po-review", "status:po-review-in-progress"}, "status:awaiting-final-stakeholder-approval", []string{poUser}, []string{poUser}, "$stakeholder", false, "PO can request final stakeholder approval."},
		{[]string{"status:awaiting-final-stakeholder-approval"}, "status:done", []string{"$stakeholder", poUser}, []string{"$stakeholder", poUser}, "$stakeholder", true, "Final stakeholder-approved delivery can be completed."},
		{statusLabels, "status:blocked", []string{poUser, developerUser, reviewerUser, "$stakeholder"}, []string{"", poUser, developerUser, reviewerUser, "$stakeholder"}, poUser, false, "Any active actor may block the issue and return ownership to PO."},
		{statusLabels, "status:rejected", []string{poUser, "$stakeholder"}, []string{"", poUser, "$stakeholder"}, poUser, false, "PO or stakeholder may reject an issue."},
	}
)

func (s *Server) runStdio(ctx context.Context, r io.Reader, w io.Writer) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	for {
		var req JSONRPCRequest
		err := dec.Decode(&req)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &RPCError{
					Code:    -32700,
					Message: "parse error",
					Data:    err.Error(),
				},
			}
			if encErr := enc.Encode(resp); encErr != nil {
				return encErr
			}
			continue
		}

		if s.cfg.Debug {
			b, _ := json.Marshal(req)
			s.logger.Println("recv:", string(b))
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
}

func (s *Server) handleRequest(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	id := decodeID(req.ID)
	respondErr := func(code int, msg string, data interface{}) JSONRPCResponse {
		return JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg, Data: data}}
	}

	switch req.Method {
	case "initialize":
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "github-issue-orchestrator", "version": "0.5.0"},
			},
		}
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
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]interface{}{
				"content":           []map[string]string{{"type": "text", "text": prettyJSON(result)}},
				"structuredContent": result,
			},
		}
	default:
		return respondErr(-32601, "method not found", req.Method)
	}
}

func (s *Server) tools() []Tool {
	return []Tool{
		newTool("get_issue_context", "Fetch issue, comments, workflow state", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number":  map[string]interface{}{"type": "integer"},
				"comment_limit": map[string]interface{}{"type": "integer", "default": 20},
			},
			"required": []string{"issue_number"},
		}),
		newTool("list_issue_comments", "List issue comments", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"limit":        map[string]interface{}{"type": "integer", "default": 50},
			},
			"required": []string{"issue_number"},
		}),
		newTool("post_issue_comment", "Post a comment", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"body":         map[string]interface{}{"type": "string"},
			},
			"required": []string{"issue_number", "body"},
		}),
		newTool("assign_issue", "Assign issue", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"assignees":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"issue_number", "assignees"},
		}),
		newTool("set_issue_labels", "Replace labels", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"labels":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"issue_number", "labels"},
		}),
		newTool("add_issue_labels", "Add labels", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"labels":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"issue_number", "labels"},
		}),
		newTool("remove_issue_label", "Remove one label", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"label":        map[string]interface{}{"type": "string"},
			},
			"required": []string{"issue_number", "label"},
		}),
		newTool("get_workflow_state", "Compute workflow state", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"issue_number"},
		}),
		newTool("find_stakeholder_approvals", "Find stakeholder /approve comments", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"issue_number"},
		}),
		newTool("parse_comments", "Parse [handoff], [po-analysis], /approve", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"issue_number"},
		}),
		newTool("validate_transition", "Validate transition", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"to_status":    map[string]interface{}{"type": "string"},
				"actor":        map[string]interface{}{"type": "string"},
				"assignee":     map[string]interface{}{"type": "string"},
			},
			"required": []string{"issue_number", "to_status", "actor"},
		}),
		newTool("transition_issue", "Validate and apply transition", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"status":       map[string]interface{}{"type": "string"},
				"assignee":     map[string]interface{}{"type": "string"},
				"comment":      map[string]interface{}{"type": "string"},
				"actor":        map[string]interface{}{"type": "string"},
			},
			"required": []string{"issue_number", "status", "actor"},
		}),
		newTool("get_transition_matrix", "Show allowed transition matrix", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
		newTool("process_issue_event", "Run webhook-style issue processing", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"issue_number"},
		}),
		newTool("get_transition_audit", "List transition audit entries", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"limit":        map[string]interface{}{"type": "integer", "default": 50},
			},
			"required": []string{"issue_number"},
		}),
		newTool("get_issue_timeline", "Return a merged chronological timeline of comments, transition audit, and tasks", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"limit":        map[string]interface{}{"type": "integer", "default": 100},
			},
			"required": []string{"issue_number"},
		}),
		newTool("get_issue_tasks", "List task rows for an issue", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"limit":        map[string]interface{}{"type": "integer", "default": 50},
			},
			"required": []string{"issue_number"},
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
		issue, comments, err := s.loadIssueAndComments(ctx, args.IssueNumber, args.CommentLimit)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"issue":    issue,
			"comments": comments,
			"workflow": computeWorkflowState(s.cfg, issue, comments),
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
		issue, comments, err := s.loadIssueAndComments(ctx, args.IssueNumber, 100)
		if err != nil {
			return nil, err
		}
		return computeWorkflowState(s.cfg, issue, comments), nil

	case "find_stakeholder_approvals":
		var args struct {
			IssueNumber int `json:"issue_number"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		issue, comments, err := s.loadIssueAndComments(ctx, args.IssueNumber, 100)
		if err != nil {
			return nil, err
		}
		stakeholder := resolveStakeholder(s.cfg, issue, comments)
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

	case "parse_comments":
		var args struct {
			IssueNumber int `json:"issue_number"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		_, comments, err := s.loadIssueAndComments(ctx, args.IssueNumber, 100)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"directives": parseComments(comments)}, nil

	case "validate_transition":
		var args struct {
			IssueNumber int    `json:"issue_number"`
			ToStatus    string `json:"to_status"`
			Actor       string `json:"actor"`
			Assignee    string `json:"assignee"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		issue, comments, err := s.loadIssueAndComments(ctx, args.IssueNumber, 100)
		if err != nil {
			return nil, err
		}
		return validateTransition(s.cfg, issue, comments, args.Actor, args.ToStatus, args.Assignee), nil

	case "transition_issue":
		var args struct {
			IssueNumber int    `json:"issue_number"`
			Status      string `json:"status"`
			Assignee    string `json:"assignee"`
			Comment     string `json:"comment"`
			Actor       string `json:"actor"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return s.transitionIssue(ctx, args.IssueNumber, args.Status, args.Assignee, args.Comment, args.Actor, "mcp_tool", nil, nil)

	case "get_transition_matrix":
		return map[string]interface{}{
			"roles": map[string]string{
				"po":        poUser,
				"developer": developerUser,
				"reviewer":  reviewerUser,
			},
			"rules": transitionRules,
		}, nil

	case "process_issue_event":
		var args struct {
			IssueNumber int `json:"issue_number"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return s.processIssue(ctx, args.IssueNumber)

	case "get_transition_audit":
		var args struct {
			IssueNumber int `json:"issue_number"`
			Limit       int `json:"limit"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		rows, err := s.store.ListTransitionAudit(args.IssueNumber, args.Limit)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"audit": rows}, nil
	case "get_issue_timeline":
		var args struct {
			IssueNumber int `json:"issue_number"`
			Limit       int `json:"limit"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		if args.Limit <= 0 {
			args.Limit = 100
		}

		issue, comments, err := s.loadIssueAndComments(ctx, args.IssueNumber, args.Limit)
		if err != nil {
			return nil, err
		}
		workflow := computeWorkflowState(s.cfg, issue, comments)

		auditRows, err := s.store.ListTransitionAudit(args.IssueNumber, args.Limit)
		if err != nil {
			return nil, err
		}
		taskRows, err := s.store.ListTasksByIssue(args.IssueNumber, args.Limit)
		if err != nil {
			return nil, err
		}

		timeline := buildIssueTimeline(comments, auditRows, taskRows)

		return IssueTimelineResult{
			Issue:    issue,
			Workflow: workflow,
			Timeline: timeline,
		}, nil
	case "get_issue_tasks":
		var args struct {
			IssueNumber int `json:"issue_number"`
			Limit       int `json:"limit"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		rows, err := s.store.ListTasksByIssue(args.IssueNumber, args.Limit)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"tasks": rows}, nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Server) loadIssueAndComments(ctx context.Context, issueNumber, limit int) (Issue, []IssueComment, error) {
	issue, err := s.gh.GetIssue(ctx, issueNumber)
	if err != nil {
		return Issue{}, nil, err
	}
	comments, err := s.gh.ListIssueComments(ctx, issueNumber, limit)
	if err != nil {
		return Issue{}, nil, err
	}
	return issue, comments, nil
}

func currentAssigneeOfIssue(issue Issue) string {
	if len(issue.Assignees) == 0 {
		return ""
	}
	return issue.Assignees[0].Login
}

func (s *Server) transitionIssue(
	ctx context.Context,
	issueNumber int,
	toStatus string,
	assignee string,
	comment string,
	actor string,
	triggerType string,
	triggerCommentID *int64,
	triggerMetadata interface{},
) (interface{}, error) {
	issue, comments, err := s.loadIssueAndComments(ctx, issueNumber, 100)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(triggerType) == "" {
		triggerType = "mcp_tool"
	}

	fromStatus := computeWorkflowState(s.cfg, issue, comments).StatusLabel
	fromAssignee := currentAssigneeOfIssue(issue)

	validation := validateTransition(s.cfg, issue, comments, actor, toStatus, assignee)
	if !validation.Allowed {
		_ = s.store.RecordTransitionAudit(
			issueNumber,
			fromStatus,
			toStatus,
			fromAssignee,
			assignee,
			actor,
			triggerType,
			triggerCommentID,
			"rejected",
			strings.Join(validation.Violations, "; "),
			validation,
			mergeAuditMetadata(
				triggerMetadata,
				map[string]interface{}{
					"comment_body_present": strings.TrimSpace(comment) != "",
				},
			),
		)
		return nil, fmt.Errorf("transition rejected: %s", strings.Join(validation.Violations, "; "))
	}

	if strings.TrimSpace(assignee) == "" {
		assignee = validation.RequiredAssigneeAfter
	}

	currentLabels := labelsToStrings(issue.Labels)
	var nextLabels []string
	for _, l := range currentLabels {
		if !strings.HasPrefix(l, "status:") {
			nextLabels = append(nextLabels, l)
		}
	}
	nextLabels = append(nextLabels, toStatus)

	if _, err := s.gh.SetIssueLabels(ctx, issueNumber, nextLabels); err != nil {
		_ = s.store.RecordTransitionAudit(
			issueNumber,
			fromStatus,
			toStatus,
			fromAssignee,
			assignee,
			actor,
			triggerType,
			triggerCommentID,
			"failed",
			"set labels failed: "+err.Error(),
			validation,
			triggerMetadata,
		)
		return nil, err
	}

	if strings.TrimSpace(assignee) != "" {
		if _, err := s.gh.AssignIssue(ctx, issueNumber, []string{assignee}); err != nil {
			_ = s.store.RecordTransitionAudit(
				issueNumber,
				fromStatus,
				toStatus,
				fromAssignee,
				assignee,
				actor,
				triggerType,
				triggerCommentID,
				"failed",
				"assign issue failed: "+err.Error(),
				validation,
				triggerMetadata,
			)
			return nil, err
		}
	}

	var posted *IssueComment
	if strings.TrimSpace(comment) != "" {
		c, err := s.gh.PostIssueComment(ctx, issueNumber, comment)
		if err != nil {
			_ = s.store.RecordTransitionAudit(
				issueNumber,
				fromStatus,
				toStatus,
				fromAssignee,
				assignee,
				actor,
				triggerType,
				triggerCommentID,
				"failed",
				"post comment failed: "+err.Error(),
				validation,
				triggerMetadata,
			)
			return nil, err
		}
		posted = &c
	}

	updated, err := s.gh.GetIssue(ctx, issueNumber)
	if err != nil {
		_ = s.store.RecordTransitionAudit(
			issueNumber,
			fromStatus,
			toStatus,
			fromAssignee,
			assignee,
			actor,
			triggerType,
			triggerCommentID,
			"failed",
			"reload issue failed: "+err.Error(),
			validation,
			triggerMetadata,
		)
		return nil, err
	}

	_ = s.store.RecordTransitionAudit(
		issueNumber,
		fromStatus,
		toStatus,
		fromAssignee,
		assignee,
		actor,
		triggerType,
		triggerCommentID,
		"applied",
		"",
		validation,
		mergeAuditMetadata(
			triggerMetadata,
			map[string]interface{}{
				"comment_posted": posted != nil,
			},
		),
	)

	result := map[string]interface{}{
		"issue":      updated,
		"validation": validation,
	}
	if posted != nil {
		result["comment"] = posted
	}
	return result, nil
}

func computeWorkflowState(cfg Config, issue Issue, comments []IssueComment) WorkflowState {
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

	var assignees []string
	for _, a := range issue.Assignees {
		assignees = append(assignees, a.Login)
	}

	stakeholder := resolveStakeholder(cfg, issue, comments)
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
		ParsedDirectives:  parseComments(comments),
	}

	switch status {
	case "status:new", "status:po-analysis":
		ws.SuggestedRole = "po"
		ws.NeedsPO = true
	case "status:awaiting-stakeholder-approval":
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
	case "status:awaiting-final-stakeholder-approval":
		ws.SuggestedRole = "stakeholder"
		ws.NeedsFinalStakeholder = true
	case "status:done":
		ws.SuggestedRole = "done"
	default:
		ws.SuggestedRole = "unknown"
	}

	return ws
}

func parseComments(comments []IssueComment) []CommentIntent {
	var out []CommentIntent
	for _, c := range comments {
		body := strings.TrimSpace(c.Body)
		if body == "" {
			continue
		}
		if containsApprove(body) {
			out = append(out, CommentIntent{
				Kind:        "approve",
				Actor:       c.User.Login,
				CommentID:   c.ID,
				CreatedAt:   c.CreatedAt,
				BodyPreview: preview(body),
			})
		}
		if fields, ok := parseTaggedBlock(body, "handoff"); ok {
			out = append(out, CommentIntent{
				Kind:        "handoff",
				Actor:       c.User.Login,
				Fields:      fields,
				CommentID:   c.ID,
				CreatedAt:   c.CreatedAt,
				BodyPreview: preview(body),
			})
		}
		if fields, ok := parseTaggedBlock(body, "po-analysis"); ok {
			out = append(out, CommentIntent{
				Kind:        "po-analysis",
				Actor:       c.User.Login,
				Fields:      fields,
				CommentID:   c.ID,
				CreatedAt:   c.CreatedAt,
				BodyPreview: preview(body),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func parseTaggedBlock(body, tag string) (map[string]string, bool) {
	startTag := "[" + tag + "]"
	endTag := "[/" + tag + "]"

	lower := strings.ToLower(body)
	start := strings.Index(lower, strings.ToLower(startTag))
	end := strings.Index(lower, strings.ToLower(endTag))
	if start < 0 || end < 0 || end <= start {
		return nil, false
	}

	content := body[start+len(startTag) : end]
	fields := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		fields[strings.TrimSpace(strings.ToLower(parts[0]))] = strings.TrimSpace(parts[1])
	}
	return fields, true
}

func preview(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}

func resolveStakeholder(cfg Config, issue Issue, comments []IssueComment) string {
	if strings.TrimSpace(cfg.StakeholderOverride) != "" {
		return cfg.StakeholderOverride
	}
	for _, c := range comments {
		if fields, ok := parseTaggedBlock(c.Body, "po-analysis"); ok {
			if v := strings.TrimSpace(fields["stakeholder"]); v != "" {
				return v
			}
		}
	}
	for _, l := range issue.Labels {
		if strings.HasPrefix(strings.ToLower(l.Name), "stakeholder:") {
			return strings.TrimSpace(strings.TrimPrefix(l.Name, "stakeholder:"))
		}
	}
	return issue.User.Login
}

func validateTransition(cfg Config, issue Issue, comments []IssueComment, actor, toStatus, requestedAssignee string) TransitionValidationResult {
	state := computeWorkflowState(cfg, issue, comments)
	actor = strings.TrimSpace(actor)
	requestedAssignee = strings.TrimSpace(requestedAssignee)

	currentAssignee := ""
	if len(issue.Assignees) > 0 {
		currentAssignee = issue.Assignees[0].Login
	}

	res := TransitionValidationResult{
		Allowed:    false,
		Actor:      actor,
		FromStatus: state.StatusLabel,
		ToStatus:   toStatus,
	}

	if actor == "" {
		res.Violations = append(res.Violations, "actor is required")
		return res
	}
	if !isAllowedStatus(toStatus) {
		res.Violations = append(res.Violations, "target status is not recognized")
		return res
	}

	var matched *TransitionRule
	for i := range transitionRules {
		r := &transitionRules[i]
		if r.ToStatus == toStatus && containsString(r.FromStatuses, state.StatusLabel) {
			matched = r
			break
		}
	}
	if matched == nil {
		res.Violations = append(res.Violations, fmt.Sprintf("no transition rule from %s to %s", state.StatusLabel, toStatus))
		return res
	}

	requiredAfter := resolveDynamicActor(cfg, matched.RequiredAssigneeAfter, issue, comments)
	res.RequiredAssigneeAfter = requiredAfter
	res.MatchedRuleDescription = matched.Description

	if !containsString(resolveActorList(cfg, matched.AllowedActors, issue, comments), actor) {
		res.Violations = append(res.Violations, fmt.Sprintf("actor %s is not allowed to perform this transition", actor))
	}

	before := resolveActorList(cfg, matched.RequiredAssigneeBefore, issue, comments)
	if len(before) > 0 && !containsString(before, currentAssignee) {
		res.Violations = append(res.Violations, fmt.Sprintf("current assignee %q does not satisfy rule", currentAssignee))
	}

	if matched.RequiresStakeholderApprove && !state.RecognizedApprove {
		res.Violations = append(res.Violations, "required stakeholder /approve comment not found")
	}

	if requestedAssignee != "" && requiredAfter != "" && requestedAssignee != requiredAfter {
		res.Violations = append(res.Violations, fmt.Sprintf("requested assignee %s does not match required assignee %s", requestedAssignee, requiredAfter))
	}

	res.Allowed = len(res.Violations) == 0
	return res
}

func resolveDynamicActor(cfg Config, token string, issue Issue, comments []IssueComment) string {
	token = strings.TrimSpace(token)
	if token == "$stakeholder" {
		return resolveStakeholder(cfg, issue, comments)
	}
	return token
}

func resolveActorList(cfg Config, values []string, issue Issue, comments []IssueComment) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, resolveDynamicActor(cfg, v, issue, comments))
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func containsApprove(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "/approve" {
			return true
		}
	}
	return false
}

func isAllowedStatus(status string) bool {
	for _, s := range statusLabels {
		if s == status {
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

func normalizeWorkflowStatusLabel(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	s = strings.TrimPrefix(s, "status:")
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")

	switch s {
	case "new":
		return "status:new"
	case "po-analysis":
		return "status:po-analysis"
	case "awaiting-stakeholder-approval", "stakeholder-approval":
		return "status:awaiting-stakeholder-approval"
	case "approved-for-dev":
		return "status:approved-for-dev"
	case "in-progress":
		return "status:in-progress"
	case "ready-for-review":
		return "status:ready-for-review"
	case "review-in-progress":
		return "status:review-in-progress"
	case "changes-requested":
		return "status:changes-requested"
	case "ready-for-po-review":
		return "status:ready-for-po-review"
	case "po-review-in-progress":
		return "status:po-review-in-progress"
	case "awaiting-final-stakeholder-approval", "final-approval":
		return "status:awaiting-final-stakeholder-approval"
	case "blocked":
		return "status:blocked"
	case "done":
		return "status:done"
	case "rejected":
		return "status:rejected"
	default:
		return ""
	}
}

func detectCommentDrivenTransition(cfg Config, issue Issue, comments []IssueComment, actor string, commentID int64, body string) CommentTransitionDecision {
	body = strings.TrimSpace(body)
	if body == "" {
		return CommentTransitionDecision{}
	}

	ws := computeWorkflowState(cfg, issue, comments)

	if fields, ok := parseTaggedBlock(body, "handoff"); ok {
		toStatus := normalizeWorkflowStatusLabel(fields["state"])
		toAssignee := strings.TrimSpace(fields["to"])
		if toStatus == "" {
			return CommentTransitionDecision{
				Matched:     true,
				Reason:      "handoff comment had unrecognized state",
				Actor:       actor,
				CommentID:   commentID,
				SourceKind:  "handoff",
				BodyPreview: preview(body),
			}
		}
		return CommentTransitionDecision{
			Matched:     true,
			Reason:      "handoff comment",
			ToStatus:    toStatus,
			ToAssignee:  toAssignee,
			Actor:       actor,
			CommentID:   commentID,
			SourceKind:  "handoff",
			BodyPreview: preview(body),
		}
	}

	if fields, ok := parseTaggedBlock(body, "po-analysis"); ok {
		if stakeholder := strings.TrimSpace(fields["stakeholder"]); stakeholder != "" {
			return CommentTransitionDecision{
				Matched:     true,
				Reason:      "po-analysis comment requested stakeholder approval",
				ToStatus:    "status:awaiting-stakeholder-approval",
				ToAssignee:  stakeholder,
				Actor:       actor,
				CommentID:   commentID,
				SourceKind:  "po-analysis",
				BodyPreview: preview(body),
			}
		}
		return CommentTransitionDecision{
			Matched:     true,
			Reason:      "po-analysis comment",
			ToStatus:    "status:po-analysis",
			ToAssignee:  poUser,
			Actor:       actor,
			CommentID:   commentID,
			SourceKind:  "po-analysis",
			BodyPreview: preview(body),
		}
	}

	if containsApprove(body) {
		switch ws.StatusLabel {
		case "status:awaiting-stakeholder-approval":
			return CommentTransitionDecision{
				Matched:     true,
				Reason:      "stakeholder approval comment",
				ToStatus:    "status:approved-for-dev",
				ToAssignee:  developerUser,
				Actor:       actor,
				CommentID:   commentID,
				SourceKind:  "approve",
				BodyPreview: preview(body),
			}
		case "status:awaiting-final-stakeholder-approval":
			return CommentTransitionDecision{
				Matched:     true,
				Reason:      "final stakeholder approval comment",
				ToStatus:    "status:done",
				ToAssignee:  ws.Stakeholder,
				Actor:       actor,
				CommentID:   commentID,
				SourceKind:  "approve",
				BodyPreview: preview(body),
			}
		default:
			return CommentTransitionDecision{
				Matched:     true,
				Reason:      "approve comment does not apply in current status",
				Actor:       actor,
				CommentID:   commentID,
				SourceKind:  "approve",
				BodyPreview: preview(body),
			}
		}
	}

	return CommentTransitionDecision{}
}

func (s *Server) processIssueCommentDirective(ctx context.Context, issueNumber int, commentID int64, actor, body string) (bool, error) {
	issue, comments, err := s.loadIssueAndComments(ctx, issueNumber, 100)
	if err != nil {
		return false, err
	}

	fromStatus := computeWorkflowState(s.cfg, issue, comments).StatusLabel
	fromAssignee := currentAssigneeOfIssue(issue)

	decision := detectCommentDrivenTransition(s.cfg, issue, comments, actor, commentID, body)
	if !decision.Matched {
		return false, nil
	}
	if decision.ToStatus == "" {
		_ = s.store.RecordTransitionAudit(
			issueNumber,
			fromStatus,
			"",
			fromAssignee,
			"",
			actor,
			"webhook_comment",
			&commentID,
			"ignored",
			decision.Reason,
			nil,
			decision,
		)
		_ = s.store.RecordFailure(issueNumber, "comment-transition-ignored", decision.Reason, decision)
		s.logger.Printf("comment transition ignored: issue=%d actor=%s reason=%s", issueNumber, actor, decision.Reason)
		return true, nil
	}

	s.logger.Printf(
		"comment transition candidate: issue=%d actor=%s to=%s assignee=%s source=%s",
		issueNumber, actor, decision.ToStatus, decision.ToAssignee, decision.SourceKind,
	)

	_, err = s.transitionIssue(
		ctx,
		issueNumber,
		decision.ToStatus,
		decision.ToAssignee,
		"",
		actor,
		"webhook_comment",
		&commentID,
		map[string]interface{}{
			"decision": decision,
		},
	)
	if err != nil {
		_ = s.store.RecordFailure(issueNumber, "comment-transition-failed", err.Error(), map[string]interface{}{
			"decision": decision,
		})
		return true, err
	}

	_, err = s.processIssue(ctx, issueNumber)
	return true, err
}

func mergeAuditMetadata(base interface{}, extra map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}

	if baseMap, ok := base.(map[string]interface{}); ok {
		for k, v := range baseMap {
			out[k] = v
		}
	}

	for k, v := range extra {
		out[k] = v
	}

	return out
}

func buildIssueTimeline(
	comments []IssueComment,
	auditRows []TransitionAuditRow,
	taskRows []TaskRow,
) []IssueTimelineEntry {
	var out []IssueTimelineEntry

	for _, c := range comments {
		out = append(out, IssueTimelineEntry{
			Kind:     "comment",
			SortTime: c.CreatedAt.UTC().Format(time.RFC3339),
			Comment:  c,
		})
	}

	for _, a := range auditRows {
		out = append(out, IssueTimelineEntry{
			Kind:     "transition_audit",
			SortTime: a.CreatedAt,
			Audit:    a,
		})
	}

	for _, t := range taskRows {
		sortTime := t.CreatedAt
		if t.FinishedAt.Valid && t.FinishedAt.String != "" {
			sortTime = t.FinishedAt.String
		} else if t.HeartbeatAt.Valid && t.HeartbeatAt.String != "" {
			sortTime = t.HeartbeatAt.String
		} else if t.ClaimedAt.Valid && t.ClaimedAt.String != "" {
			sortTime = t.ClaimedAt.String
		}

		out = append(out, IssueTimelineEntry{
			Kind:     "task",
			SortTime: sortTime,
			Task:     t,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].SortTime == out[j].SortTime {
			return out[i].Kind < out[j].Kind
		}
		return out[i].SortTime < out[j].SortTime
	})

	return out
}
