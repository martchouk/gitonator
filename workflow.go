package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Tool is used by the /mcp/tools/call HTTP endpoint.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type WorkflowState struct {
	StatusLabel      string   `json:"statusLabel"`
	TypeLabels       []string `json:"typeLabels"`
	CurrentAssignees []string `json:"currentAssignees"`
	SuggestedRole    string   `json:"suggestedRole"`
	Stakeholder      string   `json:"stakeholder"`
	RecognizedApprove bool    `json:"recognizedApprove"`
}

type TransitionRule struct {
	FromStatuses               []string `json:"fromStatuses"`
	ToStatus                   string   `json:"toStatus"`
	AllowedRoles               []string `json:"allowedRoles"`
	RequiresStakeholderApprove bool     `json:"requiresStakeholderApprove"`
	Description                string   `json:"description"`
}

type TransitionValidationResult struct {
	Allowed                bool     `json:"allowed"`
	ActorRole              string   `json:"actorRole"`
	FromStatus             string   `json:"fromStatus"`
	ToStatus               string   `json:"toStatus"`
	Violations             []string `json:"violations"`
	MatchedRuleDescription string   `json:"matchedRuleDescription,omitempty"`
}

type IssueTimelineEntry struct {
	Kind     string      `json:"kind"`
	SortTime string      `json:"sort_time"`
	Comment  interface{} `json:"comment,omitempty"`
	Audit    interface{} `json:"audit,omitempty"`
	Task     interface{} `json:"task,omitempty"`
}

type IssueTimelineResult struct {
	Issue    Issue               `json:"issue"`
	Workflow WorkflowState       `json:"workflow"`
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

	transitionRules = []TransitionRule{
		{
			FromStatuses: []string{"status:new", "status:po-analysis"},
			ToStatus:     "status:po-analysis",
			AllowedRoles: []string{"po"},
			Description:  "PO can move a new issue into PO analysis.",
		},
		{
			FromStatuses: []string{"status:new", "status:po-analysis"},
			ToStatus:     "status:awaiting-stakeholder-approval",
			AllowedRoles: []string{"po"},
			Description:  "PO can request stakeholder approval.",
		},
		{
			FromStatuses:               []string{"status:awaiting-stakeholder-approval"},
			ToStatus:                   "status:approved-for-dev",
			AllowedRoles:               []string{"stakeholder", "po"},
			RequiresStakeholderApprove: true,
			Description:                "Stakeholder-approved scope can move to developer.",
		},
		{
			FromStatuses: []string{"status:approved-for-dev", "status:changes-requested", "status:in-progress"},
			ToStatus:     "status:in-progress",
			AllowedRoles: []string{"developer"},
			Description:  "Developer can start or continue implementation.",
		},
		{
			FromStatuses: []string{"status:in-progress", "status:changes-requested"},
			ToStatus:     "status:ready-for-review",
			AllowedRoles: []string{"developer"},
			Description:  "Developer can hand work to reviewer.",
		},
		{
			FromStatuses: []string{"status:ready-for-review", "status:review-in-progress"},
			ToStatus:     "status:review-in-progress",
			AllowedRoles: []string{"reviewer"},
			Description:  "Reviewer can begin or continue review.",
		},
		{
			FromStatuses: []string{"status:ready-for-review", "status:review-in-progress"},
			ToStatus:     "status:changes-requested",
			AllowedRoles: []string{"reviewer"},
			Description:  "Reviewer can reject and send back to developer.",
		},
		{
			FromStatuses: []string{"status:ready-for-review", "status:review-in-progress"},
			ToStatus:     "status:ready-for-po-review",
			AllowedRoles: []string{"reviewer"},
			Description:  "Reviewer can accept and hand to PO.",
		},
		{
			FromStatuses: []string{"status:ready-for-po-review", "status:po-review-in-progress"},
			ToStatus:     "status:po-review-in-progress",
			AllowedRoles: []string{"po"},
			Description:  "PO can begin or continue PO review.",
		},
		{
			FromStatuses: []string{"status:ready-for-po-review", "status:po-review-in-progress"},
			ToStatus:     "status:changes-requested",
			AllowedRoles: []string{"po"},
			Description:  "PO can reject and send back to developer.",
		},
		{
			FromStatuses: []string{"status:ready-for-po-review", "status:po-review-in-progress"},
			ToStatus:     "status:awaiting-final-stakeholder-approval",
			AllowedRoles: []string{"po"},
			Description:  "PO can request final stakeholder approval.",
		},
		{
			FromStatuses:               []string{"status:awaiting-final-stakeholder-approval"},
			ToStatus:                   "status:done",
			AllowedRoles:               []string{"stakeholder", "po"},
			RequiresStakeholderApprove: true,
			Description:                "Final stakeholder-approved delivery can be completed.",
		},
		{
			FromStatuses: statusLabels,
			ToStatus:     "status:blocked",
			AllowedRoles: []string{"po", "developer", "reviewer", "stakeholder"},
			Description:  "Any active actor may block the issue; PO unblocks.",
		},
		{
			FromStatuses: statusLabels,
			ToStatus:     "status:rejected",
			AllowedRoles: []string{"po", "stakeholder"},
			Description:  "PO or stakeholder may reject an issue.",
		},
	}
)

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
		newTool("validate_transition", "Validate a transition using role-based actor", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"to_status":    map[string]interface{}{"type": "string"},
				"actor_role":   map[string]interface{}{"type": "string", "description": "Role name: po, developer, reviewer, architect, tester, designer, stakeholder"},
			},
			"required": []string{"issue_number", "to_status", "actor_role"},
		}),
		newTool("transition_issue", "Validate and apply a transition using role-based actor", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"status":       map[string]interface{}{"type": "string"},
				"assignee":     map[string]interface{}{"type": "string", "description": "Optional GitHub username to assign after transition"},
				"comment":      map[string]interface{}{"type": "string"},
				"actor_role":   map[string]interface{}{"type": "string", "description": "Role name: po, developer, reviewer, architect, tester, designer, stakeholder"},
			},
			"required": []string{"issue_number", "status", "actor_role"},
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
		newTool("get_issue_timeline", "Return merged chronological timeline of comments, transitions, and tasks", map[string]interface{}{
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
			"workflow": computeWorkflowState(issue, comments),
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
		return computeWorkflowState(issue, comments), nil

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
		stakeholder := resolveStakeholder(issue)
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

	case "validate_transition":
		var args struct {
			IssueNumber int    `json:"issue_number"`
			ToStatus    string `json:"to_status"`
			ActorRole   string `json:"actor_role"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		issue, comments, err := s.loadIssueAndComments(ctx, args.IssueNumber, 100)
		if err != nil {
			return nil, err
		}
		return validateTransition(issue, comments, args.ActorRole, args.ToStatus), nil

	case "transition_issue":
		var args struct {
			IssueNumber int    `json:"issue_number"`
			Status      string `json:"status"`
			Assignee    string `json:"assignee"`
			Comment     string `json:"comment"`
			ActorRole   string `json:"actor_role"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return s.transitionIssue(ctx, args.IssueNumber, args.Status, args.Assignee, args.Comment, args.ActorRole, "mcp_tool", nil, nil)

	case "get_transition_matrix":
		return map[string]interface{}{"rules": transitionRules}, nil

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
		workflow := computeWorkflowState(issue, comments)
		auditRows, err := s.store.ListTransitionAudit(args.IssueNumber, args.Limit)
		if err != nil {
			return nil, err
		}
		taskRows, err := s.store.ListTasksByIssue(args.IssueNumber, args.Limit)
		if err != nil {
			return nil, err
		}
		return IssueTimelineResult{
			Issue:    issue,
			Workflow: workflow,
			Timeline: buildIssueTimeline(comments, auditRows, taskRows),
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
	if limit <= 0 {
		return issue, nil, nil
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
	actorRole string,
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

	fromStatus := computeWorkflowState(issue, comments).StatusLabel
	fromAssignee := currentAssigneeOfIssue(issue)

	validation := validateTransition(issue, comments, actorRole, toStatus)
	if !validation.Allowed {
		_ = s.store.RecordTransitionAudit(
			issueNumber, fromStatus, toStatus, fromAssignee, assignee, actorRole,
			triggerType, triggerCommentID, "rejected",
			strings.Join(validation.Violations, "; "), validation, triggerMetadata,
		)
		return nil, fmt.Errorf("transition rejected: %s", strings.Join(validation.Violations, "; "))
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
			issueNumber, fromStatus, toStatus, fromAssignee, assignee, actorRole,
			triggerType, triggerCommentID, "failed", "set labels failed: "+err.Error(), validation, triggerMetadata,
		)
		return nil, err
	}

	if strings.TrimSpace(assignee) != "" {
		if _, err := s.gh.AssignIssue(ctx, issueNumber, []string{assignee}); err != nil {
			_ = s.store.RecordTransitionAudit(
				issueNumber, fromStatus, toStatus, fromAssignee, assignee, actorRole,
				triggerType, triggerCommentID, "failed", "assign issue failed: "+err.Error(), validation, triggerMetadata,
			)
			return nil, err
		}
	}

	var posted *IssueComment
	if strings.TrimSpace(comment) != "" {
		c, err := s.gh.PostIssueComment(ctx, issueNumber, comment)
		if err != nil {
			_ = s.store.RecordTransitionAudit(
				issueNumber, fromStatus, toStatus, fromAssignee, assignee, actorRole,
				triggerType, triggerCommentID, "failed", "post comment failed: "+err.Error(), validation, triggerMetadata,
			)
			return nil, err
		}
		posted = &c
	}

	updated, err := s.gh.GetIssue(ctx, issueNumber)
	if err != nil {
		_ = s.store.RecordTransitionAudit(
			issueNumber, fromStatus, toStatus, fromAssignee, assignee, actorRole,
			triggerType, triggerCommentID, "failed", "reload issue failed: "+err.Error(), validation, triggerMetadata,
		)
		return nil, err
	}

	_ = s.store.RecordTransitionAudit(
		issueNumber, fromStatus, toStatus, fromAssignee, assignee, actorRole,
		triggerType, triggerCommentID, "applied", "", validation,
		mergeAuditMetadata(triggerMetadata, map[string]interface{}{"comment_posted": posted != nil}),
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

	var assignees []string
	for _, a := range issue.Assignees {
		assignees = append(assignees, a.Login)
	}

	stakeholder := resolveStakeholder(issue)
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
	case "status:awaiting-stakeholder-approval":
		ws.SuggestedRole = "stakeholder"
	case "status:approved-for-dev", "status:in-progress", "status:changes-requested":
		ws.SuggestedRole = "developer"
	case "status:ready-for-review", "status:review-in-progress":
		ws.SuggestedRole = "reviewer"
	case "status:ready-for-po-review", "status:po-review-in-progress":
		ws.SuggestedRole = "po"
	case "status:awaiting-final-stakeholder-approval":
		ws.SuggestedRole = "stakeholder"
	case "status:blocked":
		ws.SuggestedRole = "po"
	case "status:done":
		ws.SuggestedRole = "done"
	case "status:rejected":
		ws.SuggestedRole = "rejected"
	default:
		ws.SuggestedRole = "unknown"
	}

	return ws
}

func resolveStakeholder(issue Issue) string {
	for _, l := range issue.Labels {
		if strings.HasPrefix(strings.ToLower(l.Name), "stakeholder:") {
			return strings.TrimSpace(strings.TrimPrefix(l.Name, "stakeholder:"))
		}
	}
	return issue.User.Login
}

func validateTransition(issue Issue, comments []IssueComment, actorRole, toStatus string) TransitionValidationResult {
	state := computeWorkflowState(issue, comments)
	actorRole = strings.TrimSpace(actorRole)

	res := TransitionValidationResult{
		Allowed:    false,
		ActorRole:  actorRole,
		FromStatus: state.StatusLabel,
		ToStatus:   toStatus,
	}

	if actorRole == "" {
		res.Violations = append(res.Violations, "actor_role is required")
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

	res.MatchedRuleDescription = matched.Description

	if !containsString(matched.AllowedRoles, actorRole) {
		res.Violations = append(res.Violations, fmt.Sprintf("role %q is not allowed to perform this transition", actorRole))
	}

	if matched.RequiresStakeholderApprove && !state.RecognizedApprove {
		res.Violations = append(res.Violations, "required stakeholder /approve comment not found")
	}

	res.Allowed = len(res.Violations) == 0
	return res
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

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
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

// normalizeWorkflowStatusLabel is referenced here to avoid the compiler dropping it.
var _ = normalizeWorkflowStatusLabel
