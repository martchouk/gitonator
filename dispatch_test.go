package main

import (
	"bytes"
	"context"
	"log"
	"testing"
)

// mockGitHub implements GitHubAPI for dispatch tests.
type mockGitHub struct {
	issues          []Issue // returned in sequence by GetIssue
	getIssueIdx     int
	setLabelsCalled bool
	setLabelsArgs   []string
}

func (m *mockGitHub) GetIssue(_ context.Context, _ int) (Issue, error) {
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
