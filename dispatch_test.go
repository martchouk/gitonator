package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
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
	closedIssues    []int
	reopenedIssues  []int
	closeIssueErr   error // if non-nil, returned by CloseIssue
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
func (m *mockGitHub) CloseIssue(_ context.Context, issueNumber int) error {
	m.closedIssues = append(m.closedIssues, issueNumber)
	return m.closeIssueErr
}
func (m *mockGitHub) ReopenIssue(_ context.Context, issueNumber int) error {
	m.reopenedIssues = append(m.reopenedIssues, issueNumber)
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
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return mock },
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	result, err := s.processIssue(context.Background(), 11, "owner/repo")
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
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return mock },
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	_, err := s.processIssue(context.Background(), 11, "owner/repo")
	if err == nil {
		t.Fatal("expected an error when SetIssueLabels fails, got nil")
	}
}

// TestProcessIssueBootstrap_SkipsWhenTaskHistoryExists verifies that a transient
// no-status-label webhook event for a mid-workflow issue does not reset it back to
// status:new. The guard uses HasAnyTask, which covers direct-label workflows where
// transition_audit may be empty but the orchestrator has already queued at least one task.
func TestProcessIssueBootstrap_SkipsWhenTaskHistoryExists(t *testing.T) {
	wd := leanWorkflowForTest(t)
	// Issue arrives with no status label — as if GitHub emitted a transient event
	// during a label replacement mid-workflow.
	unlabeled := Issue{
		Number:    12,
		User:      GitHubUser{Login: "martchouk"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
	}
	mock := &mockGitHub{issues: []Issue{unlabeled}}
	store := tempStore(t)
	// Simulate prior orchestrator processing by queuing a task for the issue.
	// This mirrors the real path: the issue was opened, processIssueWith bootstrapped it
	// to status:new, and QueueTask wrote the initial PO task. No transitionIssue calls
	// are needed — direct label edits leave audit empty but tasks are always written.
	if _, err := store.QueueTask(WorkPackage{
		Repo: "owner/repo", IssueID: 12, Role: "po", CurrentStatus: "status:new",
	}); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 12, "owner/repo", wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); queued {
		t.Error("expected queued=false when bootstrap skipped due to task history, got true")
	}
	if mock.setLabelsCalled {
		t.Error("expected SetIssueLabels NOT to be called when bootstrap is skipped, but it was")
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
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return mock },
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

	result, err := s.processIssue(context.Background(), 20, "owner/repo")
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
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return &mockGitHub{issues: []Issue{issue, issue}} },
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	if _, err := s.processIssue(context.Background(), 30, "owner/repo"); err != nil {
		t.Fatalf("first processIssue: %v", err)
	}

	result2, err := s.processIssue(context.Background(), 30, "owner/repo")
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
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return &mockGitHub{issues: []Issue{issueWithNewAssignee}} },
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

	result, err := s.processIssue(context.Background(), 50, "owner/repo")
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
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return &mockGitHub{issues: []Issue{issueWithAssignee}} },
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

	result, err := s.processIssue(context.Background(), 51, "owner/repo")
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
		cfg:       Config{},
		ghFor:     func(_ string) GitHubAPI { return mock },
		store:     store,
		logger:    log.New(&bytes.Buffer{}, "", 0),
		workflows: leanRegistry(t),
	}

	_, err := s.processIssue(context.Background(), 42, "owner/repo")
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
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 55, "owner/repo", wd)
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
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&logBuf, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 99, "owner/repo", wd)
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

// TestProcessIssueWith_CommentFooter_KnownStatus_IgnoresFooter verifies that for a
// recognised workflow status the YAML state machine takes precedence and the comment
// footer is ignored. Footer routing is a rescue mechanism for unrecognised statuses only.
func TestProcessIssueWith_CommentFooter_KnownStatus_IgnoresFooter(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number:    101,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}}, // known status → role=developer
	}
	mock := &mockGitHub{
		issues: []Issue{issue},
		comments: []IssueComment{
			{ID: 1, Body: "Work complete.\n[next assignee role -> reviewer]"},
		},
	}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 101, "owner/repo", wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Fatal("expected queued=true via YAML routing, got false")
	}

	task, err := store.FindActiveTaskByIssue(101)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task, got nil")
	}
	// YAML says status:in-development → role developer; footer must not override this.
	if task.Role != "developer" {
		t.Errorf("task.Role=%q, want %q — YAML routing must win over comment footer for known status", task.Role, "developer")
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
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 102, "owner/repo", wd)
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

// TestProcessIssueWith_CommentFooter_UnknownLabelOutOfWorkflowRole_Routes verifies that
// when the status label is unknown AND the footer role is not defined in the workflow
// (e.g. "architect" in the 3-role lean workflow), the footer still routes the task.
// This covers the rescue scenario from roundtrip test #42 where mud-rev applied a
// foreign status label and bud-dev handed off to architect via footer.
func TestProcessIssueWith_CommentFooter_UnknownLabelOutOfWorkflowRole_Routes(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number:    106,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "zed-arc"}},
		Labels:    []GitHubLabel{{Name: "status:changes-requested"}}, // foreign label
	}
	mock := &mockGitHub{
		issues: []Issue{issue},
		comments: []IssueComment{
			{ID: 99, Body: "Plan posted.\n[next assignee role -> architect]"},
		},
	}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 106, "owner/repo", wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Fatal("expected queued=true: unknown status + out-of-workflow footer role should still route via footer")
	}
	task, err := store.FindActiveTaskByIssue(106)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if task == nil {
		t.Fatal("expected an active task, got nil")
	}
	if task.Role != "architect" {
		t.Errorf("task.Role=%q, want %q", task.Role, "architect")
	}
	if task.Assignee != "zed-arc" {
		t.Errorf("task.Assignee=%q, want %q", task.Assignee, "zed-arc")
	}
}

// TestProcessIssueWith_CommentFooter_InvalidRole_FallsBackToLabel verifies that a footer
// containing an unrecognised role is silently ignored when the status IS known, and
// routing falls back to the label.
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
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 103, "owner/repo", wd)
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
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  tempStore(t),
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 104, "owner/repo", wd)
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
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 105, "owner/repo", wd)
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
	// Self-loop (developer) and exception paths (po via block/reject) must be absent.
	if containsString(pkg.NextAssigneeRoles, "developer") {
		t.Errorf("expected NextAssigneeRoles NOT to contain %q (self-loop), got %v", "developer", pkg.NextAssigneeRoles)
	}
	if containsString(pkg.NextAssigneeRoles, "po") {
		t.Errorf("expected NextAssigneeRoles NOT to contain %q (exception path), got %v", "po", pkg.NextAssigneeRoles)
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
		Labels:    []GitHubLabel{{Name: "status:in-development"}, {Name: "type:smoke-test"}},
	}
	mock := &mockGitHub{issues: []Issue{issue}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return mock },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), 77, "owner/repo", wd)
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
	if len(pkg.TypeLabels) != 1 || pkg.TypeLabels[0] != "type:smoke-test" {
		t.Errorf("TypeLabels=%v, want [type:smoke-test]", pkg.TypeLabels)
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

// safeGitHub is a goroutine-safe GitHubAPI stub for concurrent tests.
// All methods return fixed values without mutating shared state.
type safeGitHub struct {
	issue    Issue
	comments []IssueComment
}

func (s *safeGitHub) GetIssue(_ context.Context, _ int) (Issue, error) {
	return s.issue, nil
}
func (s *safeGitHub) ListIssueComments(_ context.Context, _ int, _ int) ([]IssueComment, error) {
	return s.comments, nil
}
func (s *safeGitHub) PostIssueComment(_ context.Context, _ int, _ string) (IssueComment, error) {
	return IssueComment{}, nil
}
func (s *safeGitHub) AssignIssue(_ context.Context, _ int, _ []string) (Issue, error) {
	return s.issue, nil
}
func (s *safeGitHub) SetIssueLabels(_ context.Context, _ int, labels []string) ([]GitHubLabel, error) {
	out := make([]GitHubLabel, len(labels))
	for i, l := range labels {
		out[i] = GitHubLabel{Name: l}
	}
	return out, nil
}
func (s *safeGitHub) AddIssueLabels(_ context.Context, _ int, _ []string) ([]GitHubLabel, error) {
	return nil, nil
}
func (s *safeGitHub) RemoveIssueLabel(_ context.Context, _ int, _ string) error { return nil }
func (s *safeGitHub) CloseIssue(_ context.Context, _ int) error                 { return nil }
func (s *safeGitHub) ReopenIssue(_ context.Context, _ int) error                { return nil }

// TestProcessIssueConcurrent_NoDuplicateTasks reproduces the TOCTOU race from issue #53:
// concurrent webhook handlers for the same issue both read the same active task, both
// supersede it, and both enqueue a replacement — producing duplicate active tasks.
// The test runs N goroutines simultaneously (via a starting-gun barrier) and asserts that
// exactly one task remains in 'queued' status after all goroutines complete.
// Without the per-issue mutex in processIssueWith this test reliably produces > 1 queued task.
func TestProcessIssueConcurrent_NoDuplicateTasks(t *testing.T) {
	wd := leanWorkflowForTest(t)
	const issueNumber = 200
	const goroutines = 10

	issue := Issue{
		Number:    issueNumber,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}

	store := tempStore(t)
	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return &safeGitHub{issue: issue} },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	// Starting-gun barrier: all goroutines block until the channel is closed,
	// maximising the chance they enter the critical section simultaneously.
	ready := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-ready
			_, _ = s.processIssueWith(context.Background(), issueNumber, "owner/repo", wd)
		}()
	}
	close(ready)
	wg.Wait()

	// Exactly one task must remain in 'queued' status.
	tasks, err := store.ListTasksByIssue(issueNumber, 100)
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	var queued int
	for _, tsk := range tasks {
		if tsk.Status == "queued" {
			queued++
		}
	}
	if queued != 1 {
		t.Errorf("expected exactly 1 queued task after %d concurrent processIssueWith calls, got %d (total tasks: %d)",
			goroutines, queued, len(tasks))
	}
}

func TestProcessIssueWith_DispatchedSameStateDeduplicates(t *testing.T) {
	wd := leanWorkflowForTest(t)
	const issueNumber = 201
	issue := Issue{
		Number:    issueNumber,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:po-approval"}},
	}
	store := tempStore(t)
	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       issueNumber,
		Role:          "po",
		Assignee:      "ada-pow",
		CurrentStatus: "status:po-approval",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	dispatched, err := store.GetNextWorkPackage("bridge-test", []string{"po"})
	if err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}
	if dispatched == nil {
		t.Fatal("expected seeded task to be dispatched")
	}

	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return &safeGitHub{issue: issue} },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), issueNumber, "owner/repo", wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); queued {
		t.Fatal("expected queued=false for same-state dispatched task")
	}
	if deduplicated, _ := m["deduplicated"].(bool); !deduplicated {
		t.Fatal("expected deduplicated=true for same-state dispatched task")
	}

	tasks, err := store.ListTasksByIssue(issueNumber, 100)
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	var queued, dispatchedCount, completed int
	for _, task := range tasks {
		switch task.Status {
		case "queued":
			queued++
		case "dispatched":
			dispatchedCount++
		case "completed":
			completed++
		}
	}
	if queued != 0 || dispatchedCount != 1 || completed != 0 || len(tasks) != 1 {
		t.Fatalf("expected original dispatched task to remain active without replacement; queued=%d dispatched=%d completed=%d total=%d",
			queued, dispatchedCount, completed, len(tasks))
	}
}

func TestProcessIssueWith_TerminalStateCompletesDispatchedTask(t *testing.T) {
	wd := leanWorkflowForTest(t)
	const issueNumber = 202
	issue := Issue{
		Number: issueNumber,
		User:   GitHubUser{Login: "creator"},
		Labels: []GitHubLabel{{Name: "status:done"}},
	}
	store := tempStore(t)
	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       issueNumber,
		Role:          "po",
		Assignee:      "ada-pow",
		CurrentStatus: "status:po-approval",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if _, err := store.GetNextWorkPackage("bridge-test", []string{"po"}); err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}

	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return &safeGitHub{issue: issue} },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), issueNumber, "owner/repo", wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); queued {
		t.Fatal("expected queued=false for terminal state")
	}

	tasks, err := store.ListTasksByIssue(issueNumber, 100)
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	var queued, dispatchedCount, completed int
	for _, task := range tasks {
		switch task.Status {
		case "queued":
			queued++
		case "dispatched":
			dispatchedCount++
		case "completed":
			completed++
		}
	}
	if queued != 0 || dispatchedCount != 0 || completed != 1 || len(tasks) != 1 {
		t.Fatalf("expected terminal state to complete in-flight task without replacement; queued=%d dispatched=%d completed=%d total=%d",
			queued, dispatchedCount, completed, len(tasks))
	}
}

func TestProcessIssueWith_DispatchedStatusChangeQueuesReplacement(t *testing.T) {
	wd := leanWorkflowForTest(t)
	const issueNumber = 203
	issue := Issue{
		Number:    issueNumber,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "bud-dev"}},
		Labels:    []GitHubLabel{{Name: "status:in-development"}},
	}
	store := tempStore(t)
	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       issueNumber,
		Role:          "developer",
		Assignee:      "bud-dev",
		CurrentStatus: "status:ready-for-development",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if _, err := store.GetNextWorkPackage("bridge-test", []string{"developer"}); err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}

	s := &Server{
		cfg:    Config{},
		ghFor:  func(_ string) GitHubAPI { return &safeGitHub{issue: issue} },
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	result, err := s.processIssueWith(context.Background(), issueNumber, "owner/repo", wd)
	if err != nil {
		t.Fatalf("processIssueWith: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if queued, _ := m["queued"].(bool); !queued {
		t.Fatal("expected queued=true after dispatched task status changed")
	}

	tasks, err := store.ListTasksByIssue(issueNumber, 100)
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	var queued, completed int
	var queuedStatus string
	for _, task := range tasks {
		switch task.Status {
		case "queued":
			queued++
			queuedStatus = task.CurrentStatus
		case "completed":
			completed++
		}
	}
	if queued != 1 || completed != 1 || queuedStatus != "status:in-development" {
		t.Fatalf("expected old task completed and one replacement queued for status:in-development; queued=%d completed=%d queuedStatus=%q total=%d",
			queued, completed, queuedStatus, len(tasks))
	}
}
