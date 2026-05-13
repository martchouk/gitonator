package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"testing"
)

// mockGitHub implements GitHubAPI for dispatch tests.
type mockGitHub struct {
	issues          []Issue // returned in sequence by GetIssue
	getIssueIdx     int
	setLabelsCalled bool
	setLabelsArgs   []string
	setLabelsErr    error // if non-nil, returned by SetIssueLabels
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
	return nil, nil
}
func (m *mockGitHub) PostIssueComment(_ context.Context, _ int, _ string) (IssueComment, error) {
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
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
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
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
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
	// Issue is now in-progress (requires developer), but a stale PO task is queued.
	issue := Issue{
		Number:    20,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "zed-arc"}},
		Labels:    []GitHubLabel{{Name: "status:in-progress"}},
	}

	mock := &mockGitHub{issues: []Issue{issue}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
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
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     &mockGitHub{issues: []Issue{issue, issue}},
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
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
		Labels:    []GitHubLabel{{Name: "status:in-progress"}},
	}

	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     &mockGitHub{issues: []Issue{issueWithNewAssignee}},
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	// Seed a stale developer task with the old assignee (e.g. assigned mid-flight).
	if _, err := store.QueueTask(WorkPackage{
		Repo:          "owner/repo",
		IssueID:       50,
		Role:          "developer",
		Assignee:      "bud-dev",
		CurrentStatus: "status:in-progress",
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

// TestProcessIssueAlreadyLabeledSkipsBootstrap verifies that a labeled issue is not
// re-labeled — SetIssueLabels must not be called when a status label is already present.
func TestProcessIssueAlreadyLabeledSkipsBootstrap(t *testing.T) {
	issue := Issue{
		Number:    42,
		User:      GitHubUser{Login: "creator"},
		Assignees: []GitHubUser{{Login: "ada-pow"}},
		Labels:    []GitHubLabel{{Name: "status:po-analysis"}},
	}

	mock := &mockGitHub{issues: []Issue{issue}}
	store := tempStore(t)
	s := &Server{
		cfg:    Config{Owner: "owner", Repo: "repo"},
		gh:     mock,
		store:  store,
		logger: log.New(&bytes.Buffer{}, "", 0),
	}

	_, err := s.processIssue(context.Background(), 42)
	if err != nil {
		t.Fatalf("processIssue returned error: %v", err)
	}

	if mock.setLabelsCalled {
		t.Errorf("SetIssueLabels must not be called for an already-labeled issue, but was called with %v", mock.setLabelsArgs)
	}
}
