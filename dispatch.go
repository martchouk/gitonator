package main

import (
	"context"
	"fmt"
	"sync"
)

// WorkPackage is the canonical work unit exchanged between orchestrator and bridge.
type WorkPackage struct {
	ID                int64    `json:"id"`
	Repo              string   `json:"repo"`
	IssueID           int      `json:"issue_id"`
	Role              string   `json:"role"`
	Assignee          string   `json:"assignee"`
	LastCommentID     int64    `json:"last_comment_id"`
	CurrentStatus     string   `json:"current_status"`
	WorkflowKey       string   `json:"workflow_key,omitempty"`
	ValidTransitions  []string `json:"valid_transitions,omitempty"`
	NextAssigneeRoles []string `json:"next_assignee_roles,omitempty"`
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
	// Guard: a transient webhook during label replacement can arrive with no status label;
	// skip bootstrap if this issue already has task history to avoid resetting mid-workflow issues.
	// HasAnyTask covers all workflow paths (direct label edits, webhook-only) because the
	// orchestrator always calls QueueTask when first processing an issue.
	if state.StatusLabel == "" {
		seen, err := s.store.HasAnyTask(issueNumber)
		if err != nil {
			return nil, fmt.Errorf("check task history before bootstrap: %w", err)
		}
		if seen {
			s.debugf("processIssue: issue=%d no status label but has task history — skipping bootstrap", issueNumber)
			return map[string]interface{}{"issue": issue, "workflow": state, "queued": false}, nil
		}
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

	// Determine the role for this work package.
	// Comment footer [next assignee role -> <role>] is a rescue mechanism only:
	// it fires exclusively when the current status label is absent from the workflow YAML.
	// For recognised statuses the YAML state machine is authoritative (Priority 1 below).
	// Priority 1: status label → workflow role (via decideNextActionFromDef).
	// Priority 2 (rescue): unrecognised status label + footer → route via footer role.
	var pkg WorkPackage
	var routed bool

	if footerRole, ok := parseNextAssigneeRole(comments); ok && !wd.HasStatus(state.StatusLabel) && footerRole != "" {
		// Rescue: status label is not in the YAML workflow. Route via footer regardless of
		// whether footerRole itself is a defined workflow role (supports cross-workflow handoffs).
		var lastCommentID int64
		if len(comments) > 0 {
			lastCommentID = comments[len(comments)-1].ID
		}
		pkg = WorkPackage{
			Repo:          fmt.Sprintf("%s/%s", s.cfg.Owner, s.cfg.Repo),
			IssueID:       issue.Number,
			Role:          footerRole,
			Assignee:      currentAssigneeOfIssue(issue),
			LastCommentID: lastCommentID,
			CurrentStatus: state.StatusLabel,
		}
		routed = true
		s.logger.Printf("WARN processIssue: issue=%d unrecognized status label %q — rescued by comment footer, routing to role=%s",
			issueNumber, state.StatusLabel, footerRole)
	}

	if !routed {
		var ok bool
		pkg, ok = decideNextActionFromDef(wd, s.cfg, issue, state, comments)
		if !ok {
			if state.StatusLabel != "" && !wd.HasStatus(state.StatusLabel) {
				s.logger.Printf("WARN processIssue: issue=%d unrecognized status label %q — not in workflow %q; no action will be queued",
					issueNumber, state.StatusLabel, wd.Workflow.Key)
			} else {
				s.debugf("processIssue: issue=%d no action — terminal or wait state", issueNumber)
				if sd := wd.StatusByID(state.StatusLabel); sd != nil && !sd.QueuesWork {
					if err := s.clearActiveTasksForIssue(issueNumber); err != nil {
						return nil, err
					}
				}
			}
			return map[string]interface{}{
				"issue":    issue,
				"workflow": state,
				"queued":   false,
			}, nil
		}
	}

	pkg.WorkflowKey = wd.Workflow.Key
	pkg.ValidTransitions = wd.ValidTransitionsFrom(state.StatusLabel)
	pkg.NextAssigneeRoles = wd.NextRolesFrom(state.StatusLabel)

	// Serialise the store critical section per issue to prevent the TOCTOU race where
	// two concurrent webhook handlers both read the same active task, both supersede it,
	// and both queue a replacement — producing duplicate active tasks. GitHub API calls
	// and state computation above are intentionally outside this lock so unrelated I/O
	// does not block concurrent processing of different issues.
	// Critical section: CompleteDispatchedTask → FindActiveTaskByIssue → SupersedeQueuedTask → QueueTask.
	unlock := s.issueProcessLock(issueNumber)
	defer unlock()

	existing, err := s.store.FindActiveTaskByIssue(issueNumber)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.Role == pkg.Role && existing.Assignee == pkg.Assignee && existing.CurrentStatus == pkg.CurrentStatus {
			// Fully deduplicated — same status, role, and assignee.
			s.debugf("processIssue: issue=%d task deduplicated existing_task_id=%d role=%s status=%s",
				issueNumber, existing.ID, existing.Role, existing.CurrentStatus)
			return map[string]interface{}{
				"issue":         issue,
				"workflow":      state,
				"queued":        false,
				"deduplicated":  true,
				"existing_task": existing,
			}, nil
		}
		// Status, role, or assignee changed — clear stale active tasks so the
		// new one reflects the current state and reaches the right agent.
		if existing.Role == pkg.Role {
			s.debugf("processIssue: issue=%d superseding stale task existing_task_id=%d role=%s old_status=%s new_status=%s old_assignee=%s new_assignee=%s",
				issueNumber, existing.ID, existing.Role, existing.CurrentStatus, pkg.CurrentStatus, existing.Assignee, pkg.Assignee)
		} else {
			s.debugf("processIssue: issue=%d superseding stale task existing_task_id=%d old_role=%s new_role=%s",
				issueNumber, existing.ID, existing.Role, pkg.Role)
		}
		if err := s.store.CompleteDispatchedTask(issueNumber); err != nil {
			return nil, fmt.Errorf("complete stale dispatched task: %w", err)
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

// issueProcessLock acquires the per-issue mutex for issueNumber and returns an unlock func.
// It serialises the four-step store critical section
// (CompleteDispatchedTask → FindActiveTaskByIssue → SupersedeQueuedTask → QueueTask)
// so that concurrent webhook handlers for the same issue cannot both read a stale task,
// both supersede it, and both enqueue a replacement. Per-issue granularity means
// unrelated issues are never blocked by each other.
func (s *Server) issueProcessLock(issueNumber int) func() {
	v, _ := s.issueMu.LoadOrStore(issueNumber, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// clearActiveTasksForIssue clears any in-flight or queued work when an issue reaches
// a known workflow state that no longer queues work, such as a terminal status.
func (s *Server) clearActiveTasksForIssue(issueNumber int) error {
	unlock := s.issueProcessLock(issueNumber)
	defer unlock()
	if err := s.store.CompleteDispatchedTask(issueNumber); err != nil {
		return fmt.Errorf("complete dispatched task for non-queueing state: %w", err)
	}
	if err := s.store.SupersedeQueuedTask(issueNumber); err != nil {
		return fmt.Errorf("supersede queued task for non-queueing state: %w", err)
	}
	return nil
}
