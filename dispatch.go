package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type AgentTask struct {
	IssueNumber  int               `json:"issue_number"`
	IssueURL     string            `json:"issue_url"`
	Role         string            `json:"role"`
	Assignee     string            `json:"assignee"`
	Action       string            `json:"action"`
	Status       string            `json:"status"`
	Context      map[string]string `json:"context,omitempty"`
	CreatedAtUTC string            `json:"created_at_utc"`
}

func (s *Server) processIssue(ctx context.Context, issueNumber int) (interface{}, error) {
	issue, comments, err := s.loadIssueAndComments(ctx, issueNumber, 100)
	if err != nil {
		return nil, err
	}

	state := computeWorkflowState(s.cfg, issue, comments)
	next, ok := decideNextAction(issue, state)
	if !ok {
		return map[string]interface{}{
			"issue":    issue,
			"workflow": state,
			"queued":   false,
		}, nil
	}

	taskID, err := s.store.QueueTask(next)
	if err != nil {
		return nil, err
	}

	if err := s.dispatchTask(next); err != nil {
		_ = s.store.RecordFailure(issueNumber, "dispatch", err.Error(), next)
		return map[string]interface{}{
			"issue":          issue,
			"workflow":       state,
			"queued":         true,
			"task_id":        taskID,
			"dispatch_error": err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"issue":    issue,
		"workflow": state,
		"queued":   true,
		"task_id":  taskID,
		"task":     next,
	}, nil
}

func decideNextAction(issue Issue, state WorkflowState) (AgentTask, bool) {
	mk := func(role, assignee, action string) AgentTask {
		return AgentTask{
			IssueNumber:  issue.Number,
			IssueURL:     issue.HTMLURL,
			Role:         role,
			Assignee:     assignee,
			Action:       action,
			Status:       state.StatusLabel,
			CreatedAtUTC: nowUTC(),
			Context: map[string]string{
				"title":       issue.Title,
				"stakeholder": state.Stakeholder,
			},
		}
	}

	switch state.StatusLabel {
	case "status:new", "status:po-analysis":
		return mk("po", poUser, "prepare-analysis-or-user-story"), true
	case "status:awaiting-stakeholder-approval":
		return mk("stakeholder", state.Stakeholder, "review-and-approve-scope"), true
	case "status:approved-for-dev", "status:in-progress", "status:changes-requested":
		return mk("developer", developerUser, "implement-or-refine"), true
	case "status:ready-for-review", "status:review-in-progress":
		return mk("reviewer", reviewerUser, "static-review"), true
	case "status:ready-for-po-review", "status:po-review-in-progress":
		return mk("po", poUser, "po-review"), true
	case "status:awaiting-final-stakeholder-approval":
		return mk("stakeholder", state.Stakeholder, "final-approval"), true
	default:
		return AgentTask{}, false
	}
}

func (s *Server) dispatchTask(task AgentTask) error {
	if err := os.MkdirAll(s.cfg.DispatchDir, 0o755); err != nil {
		return err
	}

	filename := filepath.Join(
		s.cfg.DispatchDir,
		fmt.Sprintf("issue-%d-%s-%d.json", task.IssueNumber, task.Role, time.Now().UnixNano()),
	)

	if err := os.WriteFile(filename, []byte(prettyJSON(task)), 0o644); err != nil {
		return err
	}

	if cmdTemplate := strings.TrimSpace(s.cfg.DispatchCommand); cmdTemplate != "" {
		cmdLine := strings.ReplaceAll(cmdTemplate, "{file}", filename)
		cmdLine = strings.ReplaceAll(cmdLine, "{assignee}", task.Assignee)
		cmdLine = strings.ReplaceAll(cmdLine, "{issue}", strconv.Itoa(task.IssueNumber))
		cmd := exec.Command("sh", "-lc", cmdLine)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("dispatch command failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	if tmuxTemplate := strings.TrimSpace(s.cfg.DispatchTmuxTemplate); tmuxTemplate != "" {
		cmdLine := strings.ReplaceAll(tmuxTemplate, "{file}", filename)
		cmdLine = strings.ReplaceAll(cmdLine, "{assignee}", task.Assignee)
		cmdLine = strings.ReplaceAll(cmdLine, "{issue}", strconv.Itoa(task.IssueNumber))
		cmd := exec.Command("sh", "-lc", cmdLine)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("dispatch tmux failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	return nil
}
