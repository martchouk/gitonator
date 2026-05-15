package main

import (
	"os"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	f, err := os.CreateTemp("", "store-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s, err := OpenStore(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func testPkg(issueID int, role string) WorkPackage {
	return WorkPackage{
		Repo:          "owner/repo",
		IssueID:       issueID,
		Role:          role,
		Assignee:      "",
		LastCommentID: 0,
		CurrentStatus: "status:in-progress",
	}
}

func TestQueueAndFindActive(t *testing.T) {
	s := tempStore(t)
	pkg := testPkg(42, "developer")

	id, err := s.QueueTask(pkg)
	if err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	found, err := s.FindActiveTaskByIssue(42)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find active task, got nil")
	}
	if found.Role != "developer" {
		t.Errorf("role: got %q, want %q", found.Role, "developer")
	}
}

func TestDedupPreventsDoubleQueue(t *testing.T) {
	s := tempStore(t)
	pkg := testPkg(42, "developer")

	if _, err := s.QueueTask(pkg); err != nil {
		t.Fatalf("first QueueTask: %v", err)
	}

	found, err := s.FindActiveTaskByIssue(42)
	if err != nil {
		t.Fatalf("FindActiveTaskByIssue: %v", err)
	}
	if found == nil {
		t.Fatal("expected active task after first queue")
	}
	// Simulates processIssue dedup guard: if found != nil, we do not queue again.
}

func TestGetNextWorkPackageAtomicity(t *testing.T) {
	s := tempStore(t)

	// Queue two tasks for different issues, different roles.
	if _, err := s.QueueTask(testPkg(1, "developer")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueTask(testPkg(2, "po")); err != nil {
		t.Fatal(err)
	}

	// Fetch with only "developer" role — should get issue 1.
	pkg, err := s.GetNextWorkPackage("bridge-1", []string{"developer"})
	if err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}
	if pkg == nil {
		t.Fatal("expected a work package, got nil")
	}
	if pkg.IssueID != 1 {
		t.Errorf("issue_id: got %d, want 1", pkg.IssueID)
	}
	if pkg.Role != "developer" {
		t.Errorf("role: got %q, want %q", pkg.Role, "developer")
	}

	// After dispatch, issue 1 task should not appear again.
	pkg2, err := s.GetNextWorkPackage("bridge-1", []string{"developer"})
	if err != nil {
		t.Fatalf("second GetNextWorkPackage: %v", err)
	}
	if pkg2 != nil {
		t.Errorf("expected nil (no more developer tasks), got issue_id=%d", pkg2.IssueID)
	}
}

func TestGetNextWorkPackageRolesFilter(t *testing.T) {
	s := tempStore(t)

	if _, err := s.QueueTask(testPkg(10, "reviewer")); err != nil {
		t.Fatal(err)
	}

	// Bridge only knows about "po" role — should get nothing.
	pkg, err := s.GetNextWorkPackage("bridge-po", []string{"po"})
	if err != nil {
		t.Fatalf("GetNextWorkPackage: %v", err)
	}
	if pkg != nil {
		t.Errorf("expected nil for non-matching role, got issue_id=%d", pkg.IssueID)
	}
}

func TestCompleteDispatchedTask(t *testing.T) {
	s := tempStore(t)

	// Queue then dispatch.
	pkg := testPkg(99, "developer")
	if _, err := s.QueueTask(pkg); err != nil {
		t.Fatal(err)
	}
	dp, err := s.GetNextWorkPackage("bridge-1", []string{"developer"})
	if err != nil || dp == nil {
		t.Fatalf("GetNextWorkPackage: %v %v", dp, err)
	}

	// Active task exists (dispatched).
	found, err := s.FindActiveTaskByIssue(99)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil {
		t.Fatal("expected dispatched task to appear as active")
	}

	// Complete the dispatched task.
	if err := s.CompleteDispatchedTask(99); err != nil {
		t.Fatalf("CompleteDispatchedTask: %v", err)
	}

	// No more active task.
	found2, err := s.FindActiveTaskByIssue(99)
	if err != nil {
		t.Fatal(err)
	}
	if found2 != nil {
		t.Errorf("expected nil after CompleteDispatchedTask, got status=%s", found2.Status)
	}
}

func TestSupersedeQueuedTask(t *testing.T) {
	s := tempStore(t)
	pkg := testPkg(55, "po")

	if _, err := s.QueueTask(pkg); err != nil {
		t.Fatal(err)
	}

	// Task is active before superseding.
	found, err := s.FindActiveTaskByIssue(55)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil {
		t.Fatal("expected active task before supersede, got nil")
	}

	if err := s.SupersedeQueuedTask(55); err != nil {
		t.Fatalf("SupersedeQueuedTask: %v", err)
	}

	// No active task after superseding.
	found2, err := s.FindActiveTaskByIssue(55)
	if err != nil {
		t.Fatal(err)
	}
	if found2 != nil {
		t.Errorf("expected nil after SupersedeQueuedTask, got status=%s role=%s", found2.Status, found2.Role)
	}

	// Task row is in 'superseded' status.
	var status string
	if err := s.db.QueryRow(`SELECT status FROM tasks WHERE id=?`, found.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "superseded" {
		t.Errorf("task status: got %q, want %q", status, "superseded")
	}
}

func TestSupersedeQueuedTaskIsNoopWhenNone(t *testing.T) {
	s := tempStore(t)
	// No tasks — must not error.
	if err := s.SupersedeQueuedTask(404); err != nil {
		t.Errorf("SupersedeQueuedTask on empty store: %v", err)
	}
}

func TestSupersedeQueuedTaskDoesNotAffectDispatched(t *testing.T) {
	s := tempStore(t)
	pkg := testPkg(66, "developer")

	if _, err := s.QueueTask(pkg); err != nil {
		t.Fatal(err)
	}
	// Dispatch the task.
	dp, err := s.GetNextWorkPackage("bridge-1", []string{"developer"})
	if err != nil || dp == nil {
		t.Fatalf("GetNextWorkPackage: %v %v", dp, err)
	}

	// Supersede should not touch dispatched tasks.
	if err := s.SupersedeQueuedTask(66); err != nil {
		t.Fatalf("SupersedeQueuedTask: %v", err)
	}

	// Task should still be active (dispatched).
	found, err := s.FindActiveTaskByIssue(66)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil {
		t.Fatal("expected dispatched task to remain active after SupersedeQueuedTask")
	}
	if found.Status != "dispatched" {
		t.Errorf("task status: got %q, want %q", found.Status, "dispatched")
	}
}

func TestCompleteDispatchedTaskIsNoopWhenNone(t *testing.T) {
	s := tempStore(t)
	// No tasks at all — must not error.
	if err := s.CompleteDispatchedTask(404); err != nil {
		t.Errorf("CompleteDispatchedTask on empty store: %v", err)
	}
}

func TestHasAnyTask_FalseWhenNone(t *testing.T) {
	s := tempStore(t)
	seen, err := s.HasAnyTask(999)
	if err != nil {
		t.Fatalf("HasAnyTask: %v", err)
	}
	if seen {
		t.Error("expected false for issue with no tasks, got true")
	}
}

func TestHasAnyTask_TrueAfterQueue(t *testing.T) {
	s := tempStore(t)
	if _, err := s.QueueTask(WorkPackage{Repo: "o/r", IssueID: 50, Role: "po", CurrentStatus: "status:new"}); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	seen, err := s.HasAnyTask(50)
	if err != nil {
		t.Fatalf("HasAnyTask: %v", err)
	}
	if !seen {
		t.Error("expected true after QueueTask, got false")
	}
}

func TestHasAnyTask_TrueAfterTaskSuperseded(t *testing.T) {
	s := tempStore(t)
	if _, err := s.QueueTask(WorkPackage{Repo: "o/r", IssueID: 51, Role: "po", CurrentStatus: "status:new"}); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}
	if err := s.SupersedeQueuedTask(51); err != nil {
		t.Fatalf("SupersedeQueuedTask: %v", err)
	}
	// Task is superseded, not active — HasAnyTask must still return true.
	seen, err := s.HasAnyTask(51)
	if err != nil {
		t.Fatalf("HasAnyTask: %v", err)
	}
	if !seen {
		t.Error("expected true for superseded task (issue was previously processed), got false")
	}
}

func TestIssueMetadataSetAndGet(t *testing.T) {
	s := tempStore(t)

	if err := s.SetIssueMetadata(10, "blocked_from", "status:in-development"); err != nil {
		t.Fatalf("SetIssueMetadata: %v", err)
	}

	val, ok, err := s.GetIssueMetadata(10, "blocked_from")
	if err != nil {
		t.Fatalf("GetIssueMetadata: %v", err)
	}
	if !ok {
		t.Fatal("expected key to be found, got ok=false")
	}
	if val != "status:in-development" {
		t.Errorf("value: got %q, want %q", val, "status:in-development")
	}
}

func TestIssueMetadataAbsentKey(t *testing.T) {
	s := tempStore(t)
	_, ok, err := s.GetIssueMetadata(99, "missing")
	if err != nil {
		t.Fatalf("GetIssueMetadata: %v", err)
	}
	if ok {
		t.Error("expected ok=false for absent key, got true")
	}
}

func TestIssueMetadataUpsert(t *testing.T) {
	s := tempStore(t)

	if err := s.SetIssueMetadata(5, "k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetIssueMetadata(5, "k", "v2"); err != nil {
		t.Fatal(err)
	}

	val, ok, err := s.GetIssueMetadata(5, "k")
	if err != nil || !ok {
		t.Fatalf("GetIssueMetadata: ok=%v err=%v", ok, err)
	}
	if val != "v2" {
		t.Errorf("value after upsert: got %q, want %q", val, "v2")
	}
}

func TestIssueMetadataMap(t *testing.T) {
	s := tempStore(t)

	_ = s.SetIssueMetadata(7, "a", "1")
	_ = s.SetIssueMetadata(7, "b", "2")
	_ = s.SetIssueMetadata(8, "a", "other-issue") // different issue, must not appear

	m, err := s.GetIssueMetadataMap(7)
	if err != nil {
		t.Fatalf("GetIssueMetadataMap: %v", err)
	}
	if m["a"] != "1" {
		t.Errorf("a: got %q, want %q", m["a"], "1")
	}
	if m["b"] != "2" {
		t.Errorf("b: got %q, want %q", m["b"], "2")
	}
	if _, ok := m["a"]; len(m) != 2 || !ok {
		t.Errorf("expected 2 keys for issue 7, got %v", m)
	}
}

func TestIssueMetadataClear(t *testing.T) {
	s := tempStore(t)

	_ = s.SetIssueMetadata(3, "blocked_from", "status:in-development")
	_ = s.SetIssueMetadata(3, "other", "keep")

	if err := s.ClearIssueMetadata(3, []string{"blocked_from"}); err != nil {
		t.Fatalf("ClearIssueMetadata: %v", err)
	}

	_, ok, err := s.GetIssueMetadata(3, "blocked_from")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected blocked_from to be deleted, but it was found")
	}

	val, ok, err := s.GetIssueMetadata(3, "other")
	if err != nil || !ok || val != "keep" {
		t.Errorf("expected 'other' key to remain: ok=%v val=%q err=%v", ok, val, err)
	}
}

// TestGetNextWorkPackage_WorkflowContextRoundTrip verifies that WorkflowKey and
// ValidTransitions stored in payload_json are returned by GetNextWorkPackage.
func TestGetNextWorkPackage_WorkflowContextRoundTrip(t *testing.T) {
	s := tempStore(t)
	pkg := WorkPackage{
		Repo:             "owner/repo",
		IssueID:          88,
		Role:             "developer",
		CurrentStatus:    "status:in-development",
		WorkflowKey:      "lean",
		ValidTransitions: []string{"status:code-review", "status:blocked"},
	}
	if _, err := s.QueueTask(pkg); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}

	got, err := s.GetNextWorkPackage("bridge-1", []string{"developer"})
	if err != nil || got == nil {
		t.Fatalf("GetNextWorkPackage: %v %v", got, err)
	}
	if got.WorkflowKey != "lean" {
		t.Errorf("WorkflowKey: got %q, want %q", got.WorkflowKey, "lean")
	}
	found := map[string]bool{}
	for _, tgt := range got.ValidTransitions {
		found[tgt] = true
	}
	if !found["status:code-review"] || !found["status:blocked"] {
		t.Errorf("ValidTransitions: got %v, want [status:code-review status:blocked]", got.ValidTransitions)
	}
}

// TestGetNextWorkPackage_OldPayloadMissingWorkflowFields verifies backward-compatibility:
// a task row inserted before PR #33 (payload_json without workflow_key/valid_transitions)
// is returned with zero values for those fields rather than an error.
func TestGetNextWorkPackage_OldPayloadMissingWorkflowFields(t *testing.T) {
	s := tempStore(t)

	// Insert a task using the legacy (pre-PR-33) payload_json that has no workflow fields.
	legacyPayload := `{"id":0,"repo":"owner/repo","issue_id":89,"role":"po","assignee":"","last_comment_id":0,"current_status":"status:new"}`
	_, err := s.db.Exec(
		`INSERT INTO tasks (issue_number, repo, role, assignee, last_comment_id, current_status, status, dedup_key, payload_json, created_at)
		 VALUES (89, 'owner/repo', 'po', '', 0, 'status:new', 'queued', 'issue:89', ?, datetime('now'))`,
		legacyPayload,
	)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	got, err := s.GetNextWorkPackage("bridge-1", []string{"po"})
	if err != nil || got == nil {
		t.Fatalf("GetNextWorkPackage: %v %v", got, err)
	}
	if got.WorkflowKey != "" {
		t.Errorf("WorkflowKey: got %q, want empty for legacy row", got.WorkflowKey)
	}
	if len(got.ValidTransitions) != 0 {
		t.Errorf("ValidTransitions: got %v, want nil/empty for legacy row", got.ValidTransitions)
	}
}

// TestGetNextWorkPackage_NextAssigneeRolesRoundTrip verifies that NextAssigneeRoles
// stored in payload_json is restored by GetNextWorkPackage (fixes Finding 1 from issue #46).
func TestGetNextWorkPackage_NextAssigneeRolesRoundTrip(t *testing.T) {
	s := tempStore(t)
	pkg := WorkPackage{
		Repo:              "owner/repo",
		IssueID:           90,
		Role:              "developer",
		CurrentStatus:     "status:in-development",
		WorkflowKey:       "lean",
		ValidTransitions:  []string{"status:code-review"},
		NextAssigneeRoles: []string{"reviewer"},
	}
	if _, err := s.QueueTask(pkg); err != nil {
		t.Fatalf("QueueTask: %v", err)
	}

	got, err := s.GetNextWorkPackage("bridge-1", []string{"developer"})
	if err != nil || got == nil {
		t.Fatalf("GetNextWorkPackage: %v %v", got, err)
	}
	if len(got.NextAssigneeRoles) != 1 || got.NextAssigneeRoles[0] != "reviewer" {
		t.Errorf("NextAssigneeRoles: got %v, want [reviewer]", got.NextAssigneeRoles)
	}
}

// TestIssueWorkflowKeyPersistAndLookup verifies SetIssueWorkflowKey and
// GetIssueWorkflowKey round-trip (Finding 3 from issue #46).
func TestIssueWorkflowKeyPersistAndLookup(t *testing.T) {
	s := tempStore(t)

	if err := s.SetIssueWorkflowKey(42, "full"); err != nil {
		t.Fatalf("SetIssueWorkflowKey: %v", err)
	}

	key, ok, err := s.GetIssueWorkflowKey(42)
	if err != nil {
		t.Fatalf("GetIssueWorkflowKey: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if key != "full" {
		t.Errorf("key: got %q, want %q", key, "full")
	}
}

func TestIssueWorkflowKeyAbsent(t *testing.T) {
	s := tempStore(t)
	_, ok, err := s.GetIssueWorkflowKey(999)
	if err != nil {
		t.Fatalf("GetIssueWorkflowKey: %v", err)
	}
	if ok {
		t.Error("expected ok=false for unknown issue, got true")
	}
}

func TestIssueWorkflowKeyUpsert(t *testing.T) {
	s := tempStore(t)

	if err := s.SetIssueWorkflowKey(5, "lean"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetIssueWorkflowKey(5, "full"); err != nil {
		t.Fatal(err)
	}

	key, ok, err := s.GetIssueWorkflowKey(5)
	if err != nil || !ok {
		t.Fatalf("GetIssueWorkflowKey: ok=%v err=%v", ok, err)
	}
	if key != "full" {
		t.Errorf("key after upsert: got %q, want %q", key, "full")
	}
}

func TestBridgeIDStoredOnDispatch(t *testing.T) {
	s := tempStore(t)
	if _, err := s.QueueTask(testPkg(7, "po")); err != nil {
		t.Fatal(err)
	}
	pkg, err := s.GetNextWorkPackage("my-bridge", []string{"po"})
	if err != nil || pkg == nil {
		t.Fatalf("GetNextWorkPackage: %v %v", pkg, err)
	}

	// Read bridge_id from DB directly.
	var bridgeID string
	err = s.db.QueryRow(`SELECT COALESCE(bridge_id,'') FROM tasks WHERE id=?`, pkg.ID).Scan(&bridgeID)
	if err != nil {
		t.Fatal(err)
	}
	if bridgeID != "my-bridge" {
		t.Errorf("bridge_id: got %q, want %q", bridgeID, "my-bridge")
	}
}
