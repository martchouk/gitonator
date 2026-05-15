package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
)

// leanRegistry loads the lean WorkflowRegistry from the real YAML files.
func leanRegistry(t *testing.T) *WorkflowRegistry {
	t.Helper()
	reg, err := LoadWorkflowRegistry("workflows", "lean")
	if err != nil {
		t.Fatalf("load workflow registry: %v", err)
	}
	return reg
}

// mockGitHub implements GitHubAPI for dispatch tests.
type mockGitHub struct {
	issues          []Issue // returned in sequence by GetIssue
	getIssueIdx     int
	setLabelsCalled bool
	setLabelsArgs   []string
	setLabelsErr    error // if non-nil, returned by SetIssueLabels
	postedComments  []string
	postCommentErr  error          // if non-nil, returned by PostIssueComment
	comments        []IssueComment // returned by ListIssueComments
}

func (m *mockGitHub) GetIssue(_ context.Context, _ int) (Issue, error) {
	if m.getIssueIdx >= len(m.issues) {
		return Issue{}, fmt.Errorf("mockGitHub: GetIssue called %d times but only %d issues configured",
			m.getIssueIdx+1, len(m.issues))
	}
	i := m.getIssueIdx
	m.getIssueIdx++
	return m.issues[i], nil
}
func (m *mockGitHub) ListIssueComments(_ context.Context, _ int, _ int) ([]IssueComment, error) {
	return m.comments, nil
}
func (m *mockGitHub) PostIssueComment(_ context.Context, _ int, body string) (IssueComment, error) {
	m.postedComments = append(m.postedComments, body)
	if m.postCommentErr != nil {
		return IssueComment{}, m.postCommentErr
	}
	return IssueComment{}, nil
}
func (m *mockGitHub) AssignIssue(_ context.Context, _ int, _ []string) (Issue, error) {
	return Issue{}, nil
}
func (m *mockGitHub) SetIssueLabels(_ context.Context, _ int, labels []string) ([]GitHubLabel, error) {
	m.setLabelsCalled = true
	m.setLabelsArgs = labels
	if m.setLabelsErr != nil {
		return nil, m.setLabelsErr
	}
	out := make([]GitHubLabel, len(labels))
	for i, l := range labels {
		out[i] = GitHubLabel{Name: l}
	}
	return out, nil
}
func (m *mockGitHub) AddIssueLabels(_ context.Context, _ int, _ []string) ([]GitHubLabel, error) {
	return nil, nil
}
func (m *mockGitHub) RemoveIssueLabel(_ context.Context, _ int, _ string) error {
	return nil
}

// TestProcessIssueLabelBootstrap verifies that processIssue applies status:new to an
// unlabeled issue and queues a PO work package — reproducing the bug from issue #12.
func TestProcessIssueLabelBootstrap(t *testing.T) {
	unlabeled := Issue{
		Number:    11,
		User:      GitHubUser{Login: "martchouk"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
	}
	labeled := unlabeled
	labeled.Labels = []GitHubLabel{{Name: "status:new"}}

	mock := &mockGitHub{issues: []Issue{unlabeled, labeled}}
	store := tempStore(t)
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        mock,
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	result, err := s.processIssue(context.Background(), 11)
	if err != nil {
		t.Fatalf("processIssue returned error: %v", err)
	}

	if !mock.setLabelsCalled {
		t.Fatal("expected SetIssueLabels to be called for unlabeled issue, but it was not")
	}
	if !containsString(mock.setLabelsArgs, "status:new") {
		t.Errorf("SetIssueLabels args=%v, want to contain \"status:new\"", mock.setLabelsArgs)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Errorf("expected queued=true, got false — no work package was produced")
	}

	task, err := store.FindActiveTaskByIssue(11)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task in the store, got nil")
	}
	if task.Role != "po" {
		t.Errorf("task.Role=%q, want \"po\"", task.Role)
	}
	if task.CurrentStatus != "status:new" {
		t.Errorf("task.CurrentStatus=%q, want \"status:new\"", task.CurrentStatus)
	}
}

// TestProcessIssueLabelBootstrapSetLabelsError verifies that a GitHub API failure during
// bootstrap is propagated as an error and does not silently succeed.
func TestProcessIssueLabelBootstrapSetLabelsError(t *testing.T) {
	unlabeled := Issue{
		Number: 11,
		User:   GitHubUser{Login: "martchouk"},
	}
	mock := &mockGitHub{
		issues:       []Issue{unlabeled},
		setLabelsErr: fmt.Errorf("github: 403 forbidden"),
	}
	store := tempStore(t)
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        mock,
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	_, err := s.processIssue(context.Background(), 11)
	if err == nil {
		t.Fatal("expected an error when SetIssueLabels fails, got nil")
	}
}

// TestProcessIssueRoleTransitionSupersedes reproduces issue #21: when an issue transitions
// to a new status that requires a different role, any queued task for the old role must be
// superseded so the new role's task can be dispatched.
func TestProcessIssueRoleTransitionSupersedes(t *testing.T) {
	// Issue is now in-development (requires developer), but a stale PO task is queued.
	issue := Issue{
		Number:    20,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "zed-arc"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}

	mock := &mockGitHub{issues: []Issue{issue}}
	store := tempStore(t)
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        mock,
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	// Seed a stale queued PO task (as if a prior issue_comment event created it).
	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       20,
		Role:          "po",
		CurrentStatus: "status:new",
	}); err != nil {
		t.Fatalf("seed stale task: %v", err)
	}

	result, err := s.processIssue(context.Background(), 20)
	if err != nil {
		t.Fatalf("processIssue returned error: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}

	// A new task must have been queued (not deduplicated).
	if queued, _ := m["queued"].(bool); !queued {
		t.Error("expected queued=true after role transition, got false — stale PO task was not superseded")
	}

	// The active task must now be for the developer role.
	task, err := store.FindActiveTaskByIssue(20)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task after role transition, got nil")
	}
	if task.Role != "developer" {
		t.Errorf("task.Role=%q, want %q", task.Role, "developer")
	}
}

// TestProcessIssueSameRoleDeduplicates verifies that a second event for the same issue and
// same role does NOT create a duplicate task.
func TestProcessIssueSameRoleDeduplicates(t *testing.T) {
	issue := Issue{
		Number:    30,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:new"}},
	}

	store := tempStore(t)
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        &mockGitHub{issues: []Issue{issue, issue}},
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	if _, err := s.processIssue(context.Background(), 30); err != nil {
		t.Fatalf("first processIssue: %v", err)
	}

	result2, err := s.processIssue(context.Background(), 30)
	if err != nil {
		t.Fatalf("second processIssue: %v", err)
	}

	m, ok := result2.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result2)
	}
	if queued, _ := m["queued"].(bool); queued {
		t.Error("expected queued=false on duplicate same-role event, got true")
	}
	if deduped, _ := m["deduplicated"].(bool); !deduped {
		t.Error("expected deduplicated=true on same-role duplicate, got false")
	}
}

// TestProcessIssueSameRoleDifferentAssigneeSupersedes reproduces issue #24: when an issue
// keeps the same role but its assignee changes, the stale queued task must be superseded
// and a new task queued with the updated assignee so the right agent receives it.
func TestProcessIssueSameRoleDifferentAssigneeSupersedes(t *testing.T) {
	issueWithNewAssignee := Issue{
		Number:    50,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "mud-rev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}

	store := tempStore(t)
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        &mockGitHub{issues: []Issue{issueWithNewAssignee}},
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	// Seed a stale developer task with the old assignee (e.g. assigned mid-flight).
	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       50,
		Role:          "developer",
		Assignee:      "bud-dev",
		CurrentStatus: "status:in-development",
	}); err != nil {
		t.Fatalf("seed stale task: %v", err)
	}

	result, err := s.processIssue(context.Background(), 50)
	if err != nil {
		t.Fatalf("processIssue returned error: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}

	// A new task must have been queued (not deduplicated).
	if queued, _ := m["queued"].(bool); !queued {
		t.Error("expected queued=true after assignee change within same role, got false — stale task was not superseded")
	}

	// The active task must carry the new assignee.
	task, err := store.FindActiveTaskByIssue(50)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task after assignee change, got nil")
	}
	if task.Role != "developer" {
		t.Errorf("task.Role=%q, want \"developer\"", task.Role)
	}
	if task.Assignee != "mud-rev" {
		t.Errorf("task.Assignee=%q, want \"mud-rev\"", task.Assignee)
	}
}

// TestProcessIssueSameRoleEmptyToNonEmptyAssigneeSupersedes verifies that a first
// assignment mid-flight (existing task has Assignee=="") also triggers a supersede so
// the stale unassigned task is replaced by one carrying the new assignee.
func TestProcessIssueSameRoleEmptyToNonEmptyAssigneeSupersedes(t *testing.T) {
	issueWithAssignee := Issue{
		Number:    51,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}

	store := tempStore(t)
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        &mockGitHub{issues: []Issue{issueWithAssignee}},
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	// Seed a stale developer task with no assignee (queued before assignment).
	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       51,
		Role:          "developer",
		Assignee:      "",
		CurrentStatus: "status:in-development",
	}); err != nil {
		t.Fatalf("seed stale task: %v", err)
	}

	result, err := s.processIssue(context.Background(), 51)
	if err != nil {
		t.Fatalf("processIssue returned error: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}

	// A new task must have been queued (not deduplicated).
	if queued, _ := m["queued"].(bool); !queued {
		t.Error("expected queued=true on first assignment mid-flight, got false — stale unassigned task was not superseded")
	}

	// The active task must carry the new assignee.
	task, err := store.FindActiveTaskByIssue(51)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task after first assignment, got nil")
	}
	if task.Role != "developer" {
		t.Errorf("task.Role=%q, want \"developer\"", task.Role)
	}
	if task.Assignee != "bud-dev" {
		t.Errorf("task.Assignee=%q, want \"bud-dev\"", task.Assignee)
	}
}

// TestProcessIssueAlreadyLabeledSkipsBootstrap verifies that a labeled issue is not
// re-labeled — SetIssueLabels must not be called when a status label is already present.
func TestProcessIssueAlreadyLabeledSkipsBootstrap(t *testing.T) {
	issue := Issue{
		Number:    42,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:story-definition"}},
	}

	mock := &mockGitHub{issues: []Issue{issue}}
	store := tempStore(t)
	s := &Server{
		cfg:       Config{Owner: "owner", Repo: "repo"},
		gh:        mock,
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	_, err := s.processIssue(context.Background(), 42)
	if err != nil {
		t.Fatalf("processIssue returned error: %v", err)
	}

	if mock.setLabelsCalled {
		t.Errorf("SetIssueLabels must not be called for an already-labeled issue, but was called with %v", mock.setLabelsArgs)
	}
}

// TestProcessIssueWith_YAMLWorkflow_QueuesDevTask verifies that processIssueWith with a
// non-nil YAML WorkflowDef uses the YAML engine: an issue at status:in-development queues
// a developer task rather than falling back to the legacy engine's role mapping.
func TestProcessIssueWith_YAMLWorkflow_QueuesDevTask(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number:    55,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}
	mock := &mockGitHub{issues: []Issue{issue}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 55, wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Errorf("expected queued=true for status:in-development with YAML workflow")
	}

	task, err := store.FindActiveTaskByIssue(55)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task in the store, got nil")
	}
	if task.Role != "developer" {
		t.Errorf("task.Role=%q, want %q", task.Role, "developer")
	}
	if task.CurrentStatus != "status:in-development" {
		t.Errorf("task.CurrentStatus=%q, want %q", task.CurrentStatus, "status:in-development")
	}
}

// TestProcessIssueWith_UnknownStatusLabel_LogsWarning verifies that processIssueWith emits a
// WARN log when an issue carries a status label not in the active workflow with no comment
// footer fallback, does not queue any task, and does NOT post any GitHub comment.
func TestProcessIssueWith_UnknownStatusLabel_LogsWarning(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number:    99,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:approved-for-dev"}}, // legacy label, not in lean workflow
	}
	mock := &mockGitHub{issues: []Issue{issue}}
	store := tempStore(t)
	var logBuf bytes.Buffer
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&logBuf, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 99, wd)
	if err != nil {
		t.Fatalf("processIssueWith returned unexpected error: %v", err)
	}

	// No task must be queued.
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); queued {
		t.Error("expected queued=false for unknown status label with no footer, got true")
	}

	// A WARN line must appear in the server log.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "WARN") {
		t.Errorf("expected WARN in log output for unknown status label, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "status:approved-for-dev") {
		t.Errorf("expected log to mention the unknown label, got: %s", logOutput)
	}

	// The server must NOT post any comment to the issue.
	if len(mock.postedComments) != 0 {
		t.Errorf("expected no GitHub comment to be posted, but got %d comment(s): %v",
			len(mock.postedComments), mock.postedComments)
	}
}

// TestProcessIssueWith_CommentFooter_OverridesLabel verifies that a valid
// "[next assignee role -> <role>]" footer in the last comment takes priority over
// the status label when determining the role for the work package.
func TestProcessIssueWith_CommentFooter_OverridesLabel(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number:    101,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "mud-rev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}}, // label says developer
	}
	mock := &mockGitHub{
		issues: []Issue{issue},
		comments: []IssueComment{
			{ID: 1, Body: "Work complete.\n[next assignee role -> reviewer]"},
		},
	}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 101, wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Fatal("expected queued=true when footer overrides label, got false")
	}

	task, err := store.FindActiveTaskByIssue(101)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task, got nil")
	}
	if task.Role != "reviewer" {
		t.Errorf("task.Role=%q, want %q (footer should override label)", task.Role, "reviewer")
	}
}

// TestProcessIssueWith_CommentFooter_UnknownLabel_Routes verifies that when a status label
// is unknown but the last comment has a valid "[next assignee role -> <role>]" footer,
// the server routes the task via the footer instead of stalling.
func TestProcessIssueWith_CommentFooter_UnknownLabel_Routes(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number:    102,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:ready-for-review"}}, // unknown label
	}
	mock := &mockGitHub{
		issues: []Issue{issue},
		comments: []IssueComment{
			{ID: 10, Body: "PR is up.\n[next assignee role -> reviewer]"},
		},
	}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 102, wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Fatal("expected queued=true when footer rescues unknown label, got false")
	}

	task, err := store.FindActiveTaskByIssue(102)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task, got nil")
	}
	if task.Role != "reviewer" {
		t.Errorf("task.Role=%q, want %q", task.Role, "reviewer")
	}
	if task.Assignee != "bud-dev" {
		t.Errorf("task.Assignee=%q, want %q", task.Assignee, "bud-dev")
	}
}

// TestProcessIssueWith_CommentFooter_InvalidRole_FallsBackToLabel verifies that a footer
// containing an unrecognised role is silently ignored and routing falls back to the label.
func TestProcessIssueWith_CommentFooter_InvalidRole_FallsBackToLabel(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number:    103,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}
	mock := &mockGitHub{
		issues: []Issue{issue},
		comments: []IssueComment{
			{ID: 5, Body: "[next assignee role -> bogusrole]"},
		},
	}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 103, wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Fatal("expected queued=true via label fallback when footer role is invalid")
	}

	task, err := store.FindActiveTaskByIssue(103)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task, got nil")
	}
	if task.Role != "developer" {
		t.Errorf("task.Role=%q, want %q (should fall back to label role)", task.Role, "developer")
	}
}

// TestProcessIssueWith_CommentFooter_TerminalState verifies that a valid comment footer
// does NOT cause a task to be queued when the status label is a known terminal state.
func TestProcessIssueWith_CommentFooter_TerminalState(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number: 104,
		Labels: []GitHubLabel{{Name: "status:done"}},
	}
	mock := &mockGitHub{
		issues: []Issue{issue},
		comments: []IssueComment{
			{ID: 20, Body: "[next assignee role -> po]"},
		},
	}
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  tempStore(t),
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 104, wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); queued {
		t.Error("expected queued=false for terminal state even with a comment footer, got true")
	}
}

// TestProcessIssueWith_NextAssigneeRolesPopulated verifies that a queued WorkPackage
// carries the next_assignee_roles derived from outbound workflow transitions.
func TestProcessIssueWith_NextAssigneeRolesPopulated(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number:    105,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}
	mock := &mockGitHub{issues: []Issue{issue}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 105, wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Fatal("expected queued=true for status:in-development")
	}

	pkg, ok := m["task"].(WorkPackage)
	if !ok {
		t.Fatalf("expected task to be WorkPackage, got %T", m["task"])
	}
	if !containsString(pkg.NextAssigneeRoles, "reviewer") {
		t.Errorf("expected NextAssigneeRoles to contain %q, got %v", "reviewer", pkg.NextAssigneeRoles)
	}
}

// TestProcessIssueWith_PopulatesWorkflowContext verifies that a queued WorkPackage carries
// the active workflow key and the list of statically-reachable target statuses, so that
// the bridge can pass this context to spawned agents (fixes the root cause of issue #32).
func TestProcessIssueWith_PopulatesWorkflowContext(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number:    77,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}
	mock := &mockGitHub{issues: []Issue{issue}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 77, wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Fatal("expected queued=true for status:in-development")
	}

	pkg, ok := m["task"].(WorkPackage)
	if !ok {
		t.Fatalf("expected task to be WorkPackage, got %T", m["task"])
	}

	if pkg.WorkflowKey != "lean" {
		t.Errorf("WorkflowKey=%q, want %q", pkg.WorkflowKey, "lean")
	}
	if len(pkg.ValidTransitions) == 0 {
		t.Error("expected ValidTransitions to be non-empty for status:in-development")
	}
	// status:code-review must be reachable from status:in-development.
	found := false
	for _, tgt := range pkg.ValidTransitions {
		if tgt == "status:code-review" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected status:code-review in ValidTransitions, got %v", pkg.ValidTransitions)
	}
}
