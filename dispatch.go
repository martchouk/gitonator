package main

import (
	"context"
	"fmt"
)

// WorkPackage is the canonical work unit exchanged between orchestrator and bridge.
type WorkPackage struct {
	ID            int64  `json:"id"`
	Repo          string `json:"repo"`
	IssueID       int    `json:"issue_id"`
	Role          string `json:"role"`
	Assignee      string `json:"assignee"`
	LastCommentID int64  `json:"last_comment_id"`
	CurrentStatus string `json:"current_status"`
}

func (s *Server) processIssue(ctx context.Context, issueNumber int) (interface{}, error) {
	issue, comments, err := s.loadIssueAndComments(ctx, issueNumber, 100)
	if err != nil {
		return nil, err
	}

	state := computeWorkflowState(issue, comments)
	s.debugf("processIssue: issue=%d status=%s suggested_role=%s assignees=%v",
		issueNumber, state.StatusLabel, state.SuggestedRole, state.CurrentAssignees)

	// Bootstrap: a freshly created issue with no status label enters the workflow as status:new.
	if state.StatusLabel == "" {
		s.debugf("processIssue: issue=%d no status label — bootstrapping to status:new", issueNumber)
		bootstrapLabels := append(labelsToStrings(issue.Labels), "status:new")
		if _, err := s.gh.SetIssueLabels(ctx, issueNumber, bootstrapLabels); err != nil {
			return nil, fmt.Errorf("bootstrap status:new label: %w", err)
		}
		issue, comments, err = s.loadIssueAndComments(ctx, issueNumber, 100)
		if err != nil {
			return nil, err
		}
		state = computeWorkflowState(issue, comments)
	}

	pkg, ok := decideNextAction(s.cfg, issue, state, comments)
	if !ok {
		s.debugf("processIssue: issue=%d no action — terminal or wait state", issueNumber)
		return map[string]interface{}{
			"issue":    issue,
			"workflow": state,
			"queued":   false,
		}, nil
	}

	// Close out any dispatched task for this issue before queuing a new one.
	// This is a no-op if no dispatched task exists.
	if err := s.store.CompleteDispatchedTask(issueNumber); err != nil {
		s.logger.Printf("close-out dispatched task failed: issue=%d err=%v", issueNumber, err)
	}

	existing, err := s.store.FindActiveTaskByIssue(issueNumber)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		s.debugf("processIssue: issue=%d task deduplicated existing_task_id=%d role=%s",
			issueNumber, existing.ID, existing.Role)
		return map[string]interface{}{
			"issue":         issue,
			"workflow":      state,
			"queued":        false,
			"deduplicated":  true,
			"existing_task": existing,
		}, nil
	}

	taskID, err := s.store.QueueTask(pkg)
	if err != nil {
		return nil, err
	}
	pkg.ID = taskID
	s.logger.Printf("task queued: issue=%d role=%s assignee=%s task_id=%d status=%s",
		issueNumber, pkg.Role, pkg.Assignee, taskID, pkg.CurrentStatus)

	return map[string]interface{}{
		"issue":    issue,
		"workflow": state,
		"queued":   true,
		"task_id":  taskID,
		"task":     pkg,
	}, nil
}

// decideNextAction derives the next work package from the current workflow state.
// The orchestrator has no knowledge of GitHub usernames — role is derived from the status label.
func decideNextAction(cfg Config, issue Issue, state WorkflowState, comments []IssueComment) (WorkPackage, bool) {
	role := ""
	switch state.StatusLabel {
	// "" is only reachable from direct callers (MCP tool, tests) that bypass processIssue's bootstrap.
	case "", "status:new", "status:po-analysis",
		"status:ready-for-po-review", "status:po-review-in-progress",
		"status:blocked":
		role = "po"
	case "status:ready-for-requirements-review", "status:requirements-review-in-progress":
		role = "reviewer"
	case "status:architect-analysis":
		role = "architect"
	case "status:approved-for-dev", "status:in-progress", "status:changes-requested":
		role = "developer"
	case "status:ready-for-review", "status:review-in-progress":
		role = "reviewer"
	// Human-wait states: no task queued; Bridge waits for webhook.
	case "status:awaiting-stakeholder-approval", "status:awaiting-final-stakeholder-approval":
		return WorkPackage{}, false
	// Terminal states.
	case "status:done", "status:rejected":
		return WorkPackage{}, false
	default:
		return WorkPackage{}, false
	}

	var lastCommentID int64
	if len(comments) > 0 {
		lastCommentID = comments[len(comments)-1].ID
	}

	repo := fmt.Sprintf("%s/%s", cfg.Owner, cfg.Repo)

	return WorkPackage{
		Repo:          repo,
		IssueID:       issue.Number,
		Role:          role,
		Assignee:      currentAssigneeOfIssue(issue),
		LastCommentID: lastCommentID,
		CurrentStatus: state.StatusLabel,
	}, true
}
