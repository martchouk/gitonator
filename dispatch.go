package main

import (
	"context"
	"fmt"
	"strings"
)

// WorkPackage is the canonical work unit exchanged between orchestrator and bridge.
type WorkPackage struct {
	ID               int64    `json:"id"`
	Repo             string   `json:"repo"`
	IssueID          int      `json:"issue_id"`
	Role             string   `json:"role"`
	Assignee         string   `json:"assignee"`
	LastCommentID    int64    `json:"last_comment_id"`
	CurrentStatus    string   `json:"current_status"`
	WorkflowKey      string   `json:"workflow_key,omitempty"`
	ValidTransitions []string `json:"valid_transitions,omitempty"`
}

// processIssue processes an issue using the default (lean) workflow.
func (s *Server) processIssue(ctx context.Context, issueNumber int) (interface{}, error) {
	return s.processIssueWith(ctx, issueNumber, s.workflowDef(""))
}

// processIssueWith processes an issue using the supplied YAML WorkflowDef.
// wd must not be nil; use s.workflowDef("") to get the default (lean) workflow.
func (s *Server) processIssueWith(ctx context.Context, issueNumber int, wd *WorkflowDef) (interface{}, error) {
	if wd == nil {
		return nil, fmt.Errorf("processIssueWith: workflow definition required (registry not loaded)")
	}
	issue, comments, err := s.loadIssueAndComments(ctx, issueNumber, 100)
	if err != nil {
		return nil, err
	}

	state := s.computeState(wd, issue, comments)
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
		state = s.computeState(wd, issue, comments)
	}

	// Warn when the issue carries a status label that is not recognised by the active
	// workflow. decideNextActionFromDef will return ok=false for the same reason, so
	// no task will be queued — but a silent no-op is hard to diagnose from logs alone.
	// A comment is also posted to the issue so agents can self-correct without
	// requiring access to server logs.
	if state.StatusLabel != "" && !wd.HasStatus(state.StatusLabel) {
		s.logger.Printf("WARN processIssue: issue=%d unrecognized status label %q — not in workflow %q; no action will be queued",
			issueNumber, state.StatusLabel, wd.Workflow.Key)
		s.postUnknownStatusComment(ctx, issueNumber, state.StatusLabel, wd)
	}

	pkg, ok := decideNextActionFromDef(wd, s.cfg, issue, state, comments)
	if !ok {
		s.debugf("processIssue: issue=%d no action — terminal or wait state", issueNumber)
		return map[string]interface{}{
			"issue":    issue,
			"workflow": state,
			"queued":   false,
		}, nil
	}

	pkg.WorkflowKey = wd.Workflow.Key
	pkg.ValidTransitions = wd.ValidTransitionsFrom(state.StatusLabel)

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
		if existing.Role == pkg.Role && existing.Assignee == pkg.Assignee {
			// Fully deduplicated — same role and same assignee.
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
		// Role or assignee changed — supersede the stale task so the new one
		// reflects the current state and reaches the right agent.
		if existing.Role == pkg.Role {
			s.debugf("processIssue: issue=%d superseding stale task existing_task_id=%d role=%s old_assignee=%s new_assignee=%s",
				issueNumber, existing.ID, existing.Role, existing.Assignee, pkg.Assignee)
		} else {
			s.debugf("processIssue: issue=%d superseding stale task existing_task_id=%d old_role=%s new_role=%s",
				issueNumber, existing.ID, existing.Role, pkg.Role)
		}
		if err := s.store.SupersedeQueuedTask(issueNumber); err != nil {
			return nil, fmt.Errorf("supersede stale task: %w", err)
		}
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

func (s *Server) computeState(wd *WorkflowDef, issue Issue, comments []IssueComment) WorkflowState {
	return computeWorkflowStateFromDef(wd, issue, comments)
}

// postUnknownStatusComment posts a correction comment on the issue when an unrecognised
// status label is detected. The comment lists the valid labels for the active workflow
// so that agents reading the issue thread can self-correct without access to server logs.
func (s *Server) postUnknownStatusComment(ctx context.Context, issueNumber int, label string, wd *WorkflowDef) {
	var b strings.Builder
	fmt.Fprintf(&b, "[Orchestrator] Unrecognised status label detected: `%s`\n\n", label)
	fmt.Fprintf(&b, "This label is not part of the active workflow (`%s`). No task will be queued until a valid status label is applied.\n\n", wd.Workflow.Key)
	b.WriteString("Valid status labels:\n")
	for _, id := range wd.AllStatusIDs() {
		fmt.Fprintf(&b, "- `%s`\n", id)
	}
	b.WriteString("\nTo transition this issue to a valid status, use the `transition_issue` MCP tool instead of setting labels directly.")

	if _, err := s.gh.PostIssueComment(ctx, issueNumber, b.String()); err != nil {
		s.logger.Printf("WARN postUnknownStatusComment failed: issue=%d err=%v", issueNumber, err)
	}
}

