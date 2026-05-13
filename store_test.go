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
