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
	Issue    Issue                `json:"issue"`
	Workflow WorkflowState        `json:"workflow"`
	Timeline []IssueTimelineEntry `json:"timeline"`
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
				"workflow":     map[string]interface{}{"type": "string", "description": "Workflow key: lean (default) or full"},
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
				"workflow":     map[string]interface{}{"type": "string", "description": "Workflow key: lean (default) or full"},
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
				"workflow":     map[string]interface{}{"type": "string", "description": "Workflow key: lean (default) or full"},
			},
			"required": []string{"issue_number", "status", "actor_role"},
		}),
		newTool("get_transition_matrix", "Show allowed transition matrix", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"workflow": map[string]interface{}{"type": "string", "description": "Workflow key: lean (default) or full"},
			},
		}),
		newTool("process_issue_event", "Run webhook-style issue processing", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"issue_number": map[string]interface{}{"type": "integer"},
				"workflow":     map[string]interface{}{"type": "string", "description": "Workflow key: lean (default) or full"},
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
			IssueNumber  int    `json:"issue_number"`
			CommentLimit int    `json:"comment_limit"`
			Workflow     string `json:"workflow"`
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
		ws := s.computeState(s.workflowDef(args.Workflow), issue, comments)
		return map[string]interface{}{
			"issue":    issue,
			"comments": comments,
			"workflow": ws,
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
			IssueNumber int    `json:"issue_number"`
			Workflow    string `json:"workflow"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		issue, comments, err := s.loadIssueAndComments(ctx, args.IssueNumber, 100)
		if err != nil {
			return nil, err
		}
		return computeWorkflowStateFromDef(s.workflowDef(args.Workflow), issue, comments), nil

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
			Workflow    string `json:"workflow"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		issue, err := s.gh.GetIssue(ctx, args.IssueNumber)
		if err != nil {
			return nil, err
		}
		wd := s.workflowDef(args.Workflow)
		meta, _ := s.store.GetIssueMetadataMap(args.IssueNumber)
		return validateTransitionFromDef(wd, issue, meta, args.ActorRole, args.ToStatus), nil

	case "transition_issue":
		var args struct {
			IssueNumber int    `json:"issue_number"`
			Status      string `json:"status"`
			Assignee    string `json:"assignee"`
			Comment     string `json:"comment"`
			ActorRole   string `json:"actor_role"`
			Workflow    string `json:"workflow"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		wd := s.workflowDef(args.Workflow)
		if args.Workflow != "" && s.store != nil {
			_ = s.store.SetIssueWorkflowKey(args.IssueNumber, wd.Workflow.Key)
		}
		return s.transitionIssue(ctx, args.IssueNumber, args.Status, args.Assignee, args.Comment, args.ActorRole, "mcp_tool", nil, nil, wd)

	case "get_transition_matrix":
		var args struct {
			Workflow string `json:"workflow"`
		}
		_ = json.Unmarshal(raw, &args)
		wd := s.workflowDef(args.Workflow)
		return map[string]interface{}{"workflow": wd.Workflow.Key, "transitions": wd.Transitions}, nil

	case "process_issue_event":
		var args struct {
			IssueNumber int    `json:"issue_number"`
			Workflow    string `json:"workflow"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
		wd := s.workflowDef(args.Workflow)
		if args.Workflow != "" && s.store != nil {
			_ = s.store.SetIssueWorkflowKey(args.IssueNumber, wd.Workflow.Key)
		}
		return s.processIssueWith(ctx, args.IssueNumber, wd)

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
		workflow := computeWorkflowStateFromDef(s.workflowDef(""), issue, comments)
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

// workflowDef returns the WorkflowDef for the given key from the server's registry.
// An empty or unknown key falls back to the registry default (lean). Returns nil only
// when no registry is loaded — callers must treat nil as an error condition; main exits
// on registry load failure so this should not occur in a correctly started server.
func (s *Server) workflowDef(key string) *WorkflowDef {
	if s.workflows == nil {
		return nil
	}
	return s.workflows.Get(key)
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
	wd *WorkflowDef,
) (interface{}, error) {
	issue, _, err := s.loadIssueAndComments(ctx, issueNumber, 100)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(triggerType) == "" {
		triggerType = "mcp_tool"
	}

	var fromStatus string
	var validation TransitionValidationResult
	var matchedDef *TransitionDef

	meta, _ := s.store.GetIssueMetadataMap(issueNumber)
	fromStatus = computeWorkflowStateFromDef(wd, issue, nil).StatusLabel
	validation = validateTransitionFromDef(wd, issue, meta, actorRole, toStatus)
	if validation.Allowed {
		matchedDef = findMatchingTransitionDef(wd, fromStatus, toStatus, meta)
	}

	fromAssignee := currentAssigneeOfIssue(issue)

	s.debugf("transitionIssue: issue=%d from=%s to=%s actor=%s allowed=%v",
		issueNumber, fromStatus, toStatus, actorRole, validation.Allowed)
	if !validation.Allowed {
		s.debugf("transitionIssue: issue=%d rejected violations=%q", issueNumber, strings.Join(validation.Violations, "; "))
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

	// Apply YAML-workflow metadata side-effects (set_metadata / clear_metadata).
	var sideEffectErr error
	if matchedDef != nil {
		s.applyTransitionMetadata(issueNumber, fromStatus, matchedDef)
		if matchedDef.CloseIssue {
			if err := s.gh.CloseIssue(ctx, issueNumber); err != nil {
				sideEffectErr = fmt.Errorf("close_issue failed: %w", err)
			}
		}
		if matchedDef.ReopenIssue && sideEffectErr == nil {
			if err := s.gh.ReopenIssue(ctx, issueNumber); err != nil {
				sideEffectErr = fmt.Errorf("reopen_issue failed: %w", err)
			}
		}
	}

	auditResult := "applied"
	auditReason := ""
	if sideEffectErr != nil {
		auditResult = "partially_applied"
		auditReason = sideEffectErr.Error()
		s.logger.Printf("WARN transitionIssue: issue=%d %s (label already updated)", issueNumber, sideEffectErr)
	}
	_ = s.store.RecordTransitionAudit(
		issueNumber, fromStatus, toStatus, fromAssignee, assignee, actorRole,
		triggerType, triggerCommentID, auditResult, auditReason, validation,
		mergeAuditMetadata(triggerMetadata, map[string]interface{}{"comment_posted": posted != nil}),
	)
	s.debugf("transitionIssue: issue=%d %s from=%s to=%s assignee=%q comment_posted=%v",
		issueNumber, auditResult, fromStatus, toStatus, assignee, posted != nil)
	if sideEffectErr != nil {
		return nil, fmt.Errorf("transition partially applied (label updated, %s)", sideEffectErr)
	}

	result := map[string]interface{}{
		"issue":      updated,
		"validation": validation,
	}
	if posted != nil {
		result["comment"] = posted
	}
	return result, nil
}

func containsApprove(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "/approve" {
			return true
		}
	}
	return false
}

func resolveStakeholder(issue Issue) string {
	for _, l := range issue.Labels {
		if strings.HasPrefix(strings.ToLower(l.Name), "stakeholder:") {
			return strings.TrimSpace(strings.TrimPrefix(l.Name, "stakeholder:"))
		}
	}
	return issue.User.Login
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
