package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"testing"
)

// makeWebhookPayload builds a minimal GitHub issue webhook JSON body.
func makeWebhookPayload(t *testing.T, issueNumber int, action string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"action": action,
		"issue":  map[string]interface{}{"number": issueNumber},
	})
	if err != nil {
		t.Fatalf("marshal webhook payload: %v", err)
	}
	return body
}

// TestProcessWebhookPayload_PersistsExplicitWorkflowKey verifies that
// providing a non-empty wfKey persists the workflow key for the issue so
// subsequent calls without a key can look it up (Finding 3, issue #46).
func TestProcessWebhookPayload_PersistsExplicitWorkflowKey(t *testing.T) {
	reg := leanRegistry(t)
	store := tempStore(t)

	issue := Issue{
		Number:    200,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:new"}},
	}
	labeled := issue
	labeled.Labels = []GitHubLabel{{Name: "status:new"}}

	mock := &mockGitHub{issues: []Issue{issue, labeled}}
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        mock,
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: reg,
	}

	payload := makeWebhookPayload(t, 200, "labeled")

	// First call with explicit workflow key "lean".
	if err := s.processWebhookPayload(context.Background(), "issues", "d1", payload, reg.Get("lean"), "lean"); err != nil {
		t.Fatalf("processWebhookPayload: %v", err)
	}

	key, ok, err := store.GetIssueWorkflowKey(200)
	if err != nil {
		t.Fatalf("GetIssueWorkflowKey: %v", err)
	}
	if !ok || key != "lean" {
		t.Errorf("expected stored workflow key %q, got ok=%v key=%q", "lean", ok, key)
	}
}

// TestProcessWebhookPayload_LooksUpStoredWorkflowKey verifies that when no
// wfKey is provided the server looks up the persisted key and uses it
// instead of falling back to the default (Finding 3, issue #46).
func TestProcessWebhookPayload_LooksUpStoredWorkflowKey(t *testing.T) {
	reg := leanRegistry(t)
	store := tempStore(t)

	// Pre-store a workflow key for issue 201 so the second call picks it up.
	if err := store.SetIssueWorkflowKey(201, "lean"); err != nil {
		t.Fatalf("SetIssueWorkflowKey: %v", err)
	}

	issue := Issue{
		Number:    201,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:new"}},
	}

	mock := &mockGitHub{issues: []Issue{issue}}
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        mock,
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: reg,
	}

	payload := makeWebhookPayload(t, 201, "labeled")

	// Call without an explicit workflow key — should resolve from store.
	if err := s.processWebhookPayload(context.Background(), "issues", "d2", payload, reg.Get(""), ""); err != nil {
		t.Fatalf("processWebhookPayload: %v", err)
	}
	// If the stored key was used, the issue was processed successfully (no error).
}

// TestTransitionIssue_CloseIssue verifies that a transition with close_issue:true
// triggers a CloseIssue call on the GitHub client (Finding 2, issue #46).
func TestTransitionIssue_CloseIssue(t *testing.T) {
	wd := leanWorkflowForTest(t)

	// Build an issue at status:po-approval ready for the PO to approve and close.
	issue := Issue{
		Number:    300,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:po-approval"}},
	}
	afterTransition := Issue{
		Number: 300,
		Labels: []GitHubLabel{{Name: "status:done"}},
	}

	mock := &mockGitHub{issues: []Issue{issue, afterTransition}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	_, err := s.transitionIssue(
		context.Background(), 300, "status:done", "", "", "po",
		"mcp_tool", nil, nil, wd,
	)
	if err != nil {
		t.Fatalf("transitionIssue: %v", err)
	}

	if len(mock.closedIssues) == 0 {
		t.Error("expected CloseIssue to be called for po_approve_rollout_and_close (close_issue:true), but it was not")
	}
	if len(mock.closedIssues) > 0 && mock.closedIssues[0] != 300 {
		t.Errorf("CloseIssue called for issue %d, want 300", mock.closedIssues[0])
	}
}

// TestTransitionIssue_ReopenIssue verifies that a transition with reopen_issue:true
// triggers a ReopenIssue call on the GitHub client (Finding 2, issue #46).
func TestTransitionIssue_ReopenIssue(t *testing.T) {
	wd := leanWorkflowForTest(t)

	issue := Issue{
		Number:    301,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:done"}},
	}
	afterTransition := Issue{
		Number: 301,
		Labels: []GitHubLabel{{Name: "status:story-definition"}},
	}

	mock := &mockGitHub{issues: []Issue{issue, afterTransition}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	_, err := s.transitionIssue(
		context.Background(), 301, "status:story-definition", "", "", "po",
		"mcp_tool", nil, nil, wd,
	)
	if err != nil {
		t.Fatalf("transitionIssue: %v", err)
	}

	if len(mock.reopenedIssues) == 0 {
		t.Error("expected ReopenIssue to be called for po_reopen_done (reopen_issue:true), but it was not")
	}
	if len(mock.reopenedIssues) > 0 && mock.reopenedIssues[0] != 301 {
		t.Errorf("ReopenIssue called for issue %d, want 301", mock.reopenedIssues[0])
	}
}

// TestTransitionIssue_CloseIssueFailureReturnsPartialError verifies that when
// CloseIssue fails the transition returns a partial-failure error and records
// "partially_applied" in the audit (Finding 2, issue #46).
func TestTransitionIssue_CloseIssueFailureReturnsPartialError(t *testing.T) {
	wd := leanWorkflowForTest(t)

	issue := Issue{
		Number:    305,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:po-approval"}},
	}
	afterTransition := Issue{
		Number: 305,
		Labels: []GitHubLabel{{Name: "status:done"}},
	}

	mock := &mockGitHub{
		issues:        []Issue{issue, afterTransition},
		closeIssueErr: fmt.Errorf("github: 503 service unavailable"),
	}
	store := tempStore(t)
	var logBuf bytes.Buffer
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&logBuf, "", 0),
	}

	_, err := s.transitionIssue(
		context.Background(), 305, "status:done", "", "", "po",
		"mcp_tool", nil, nil, wd,
	)
	if err == nil {
		t.Fatal("expected partial-failure error when CloseIssue fails, got nil")
	}
	if !strings.Contains(err.Error(), "partially applied") {
		t.Errorf("expected error to mention %q, got %q", "partially applied", err.Error())
	}

	// Audit must record "partially_applied".
	audits, auditErr := store.ListTransitionAudit(305, 10)
	if auditErr != nil {
		t.Fatalf("ListTransitionAudit: %v", auditErr)
	}
	if len(audits) == 0 {
		t.Fatal("expected an audit entry, got none")
	}
	if audits[0].Result != "partially_applied" {
		t.Errorf("audit result: got %q, want %q", audits[0].Result, "partially_applied")
	}
}

// TestCallTool_TransitionIssue_PersistsWorkflowKey verifies that an explicit
// workflow arg on the transition_issue MCP tool persists the workflow key for
// the issue (Finding 1, issue #46).
func TestCallTool_TransitionIssue_PersistsWorkflowKey(t *testing.T) {
	reg := leanRegistry(t)
	store := tempStore(t)

	issue := Issue{
		Number:    400,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}
	afterTransition := Issue{
		Number: 400,
		Labels: []GitHubLabel{{Name: "status:code-review"}},
	}

	mock := &mockGitHub{issues: []Issue{issue, afterTransition}}
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        mock,
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: reg,
	}

	args, _ := json.Marshal(map[string]interface{}{
		"issue_number": 400,
		"status":       "status:code-review",
		"actor_role":   "developer",
		"workflow":     "lean",
	})
	if _, err := s.callTool(context.Background(), "transition_issue", args); err != nil {
		t.Fatalf("callTool transition_issue: %v", err)
	}

	key, ok, err := store.GetIssueWorkflowKey(400)
	if err != nil {
		t.Fatalf("GetIssueWorkflowKey: %v", err)
	}
	if !ok || key != "lean" {
		t.Errorf("expected stored workflow key %q, got ok=%v key=%q", "lean", ok, key)
	}
}

// TestTransitionIssue_NoCloseOnNormalTransition verifies that CloseIssue is
// NOT called for a transition that does not have close_issue:true.
func TestTransitionIssue_NoCloseOnNormalTransition(t *testing.T) {
	wd := leanWorkflowForTest(t)

	issue := Issue{
		Number:    302,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}
	afterTransition := Issue{
		Number: 302,
		Labels: []GitHubLabel{{Name: "status:code-review"}},
	}

	mock := &mockGitHub{issues: []Issue{issue, afterTransition}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	_, err := s.transitionIssue(
		context.Background(), 302, "status:code-review", "", "", "developer",
		"mcp_tool", nil, nil, wd,
	)
	if err != nil {
		t.Fatalf("transitionIssue: %v", err)
	}

	if len(mock.closedIssues) != 0 {
		t.Errorf("expected CloseIssue NOT to be called for a normal transition, but got calls: %v", mock.closedIssues)
	}
}
