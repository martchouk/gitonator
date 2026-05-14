package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadWorkflowRegistry_LoadsLeanAndFull verifies that both YAML workflow files are
// loaded and accessible by their declared key.
func TestLoadWorkflowRegistry_LoadsLeanAndFull(t *testing.T) {
	reg, err := LoadWorkflowRegistry("workflows", "lean")
	if err != nil {
		t.Fatalf("LoadWorkflowRegistry: %v", err)
	}

	lean := reg.Get("lean")
	if lean == nil {
		t.Fatal("expected 'lean' workflow, got nil")
	}
	if lean.Workflow.Key != "lean" {
		t.Errorf("lean key: got %q, want %q", lean.Workflow.Key, "lean")
	}

	full := reg.Get("full")
	if full == nil {
		t.Fatal("expected 'full' workflow, got nil")
	}
	if full.Workflow.Key != "full" {
		t.Errorf("full key: got %q, want %q", full.Workflow.Key, "full")
	}
}

// TestLoadWorkflowRegistry_DefaultFallback verifies that Get with an unknown key returns
// the default workflow.
func TestLoadWorkflowRegistry_DefaultFallback(t *testing.T) {
	reg, err := LoadWorkflowRegistry("workflows", "lean")
	if err != nil {
		t.Fatalf("LoadWorkflowRegistry: %v", err)
	}

	wd := reg.Get("unknown-key")
	if wd == nil {
		t.Fatal("expected default workflow for unknown key, got nil")
	}
	if wd.Workflow.Key != "lean" {
		t.Errorf("fallback workflow key: got %q, want %q", wd.Workflow.Key, "lean")
	}

	wd2 := reg.Get("")
	if wd2 == nil || wd2.Workflow.Key != "lean" {
		t.Errorf("fallback for empty key: got %v", wd2)
	}
}

// TestLoadWorkflowRegistry_MissingDefaultKey verifies that a missing default key causes
// an error rather than a silent fallback.
func TestLoadWorkflowRegistry_MissingDefaultKey(t *testing.T) {
	_, err := LoadWorkflowRegistry("workflows", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing default key, got nil")
	}
}

// TestLoadWorkflowRegistry_MissingDir verifies that a non-existent directory is an error.
func TestLoadWorkflowRegistry_MissingDir(t *testing.T) {
	_, err := LoadWorkflowRegistry("/tmp/no-such-dir-xyz", "lean")
	if err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
}

// TestLoadWorkflowRegistry_SkipsFilesWithoutKey verifies that YAML files without a
// workflow.key field (like the legacy extraction file) are ignored.
func TestLoadWorkflowRegistry_SkipsFilesWithoutKey(t *testing.T) {
	reg, err := LoadWorkflowRegistry("workflows", "lean")
	if err != nil {
		t.Fatalf("LoadWorkflowRegistry: %v", err)
	}
	for _, k := range reg.Keys() {
		if k == "" {
			t.Error("registry contains a workflow with an empty key")
		}
	}
}

// TestLoadWorkflowRegistry_InvalidYAML verifies that a syntactically invalid YAML file
// causes a load error.
func TestLoadWorkflowRegistry_InvalidYAML(t *testing.T) {
	dir := t.TempDir()

	// Write a broken YAML file with a key so it is not skipped.
	if err := os.WriteFile(
		filepath.Join(dir, "bad.yaml"),
		[]byte("workflow:\n  key: bad\nstatuses: [\n"), // unclosed bracket
		0644,
	); err != nil {
		t.Fatal(err)
	}
	_, err := LoadWorkflowRegistry(dir, "bad")
	if err == nil {
		t.Fatal("expected parse error for invalid YAML, got nil")
	}
}

// TestLoadWorkflowRegistry_ValidationUnknownToStatus verifies that a transition
// referencing an unknown to-status fails validation.
func TestLoadWorkflowRegistry_ValidationUnknownToStatus(t *testing.T) {
	dir := t.TempDir()
	yaml := `
workflow:
  id: test
  key: test
statuses:
  - id: status:new
    role: po
    queues_work: true
transitions:
  - id: go_nowhere
    from:
      - status:new
    to: status:does-not-exist
    allowed_roles: [po]
`
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadWorkflowRegistry(dir, "test")
	if err == nil {
		t.Fatal("expected validation error for unknown to_status, got nil")
	}
}

// TestLoadWorkflowRegistry_ValidationDeadEnd verifies that a non-terminal status with no
// outgoing transitions fails validation.
func TestLoadWorkflowRegistry_ValidationDeadEnd(t *testing.T) {
	dir := t.TempDir()
	yaml := `
workflow:
  id: test
  key: test
statuses:
  - id: status:new
    role: po
    queues_work: true
  - id: status:dead-end
    role: po
    queues_work: true
transitions:
  - id: go_to_dead_end
    from: [status:new]
    to: status:dead-end
    allowed_roles: [po]
`
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadWorkflowRegistry(dir, "test")
	if err == nil {
		t.Fatal("expected validation error for dead-end non-terminal status, got nil")
	}
}

// TestLoadWorkflowRegistry_ValidationUnknownGuard verifies that referencing an
// undeclared guard name fails validation.
func TestLoadWorkflowRegistry_ValidationUnknownGuard(t *testing.T) {
	dir := t.TempDir()
	yaml := `
workflow:
  id: test
  key: test
statuses:
  - id: status:new
    role: po
    queues_work: true
  - id: status:done
    role: null
    queues_work: false
    terminal: true
transitions:
  - id: finish
    from: [status:new]
    to: status:done
    allowed_roles: [po]
    guard: no_such_guard
`
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadWorkflowRegistry(dir, "test")
	if err == nil {
		t.Fatal("expected validation error for unknown guard reference, got nil")
	}
}

// TestLoadWorkflowRegistry_ValidationDuplicateStatusID verifies that two statuses sharing
// the same id cause a validation error.
func TestLoadWorkflowRegistry_ValidationDuplicateStatusID(t *testing.T) {
	dir := t.TempDir()
	content := `
workflow:
  id: test
  key: test
statuses:
  - id: status:new
    role: po
    queues_work: true
  - id: status:new
    role: developer
    queues_work: true
  - id: status:done
    terminal: true
transitions:
  - id: finish
    from: [status:new]
    to: status:done
    allowed_roles: [po]
`
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadWorkflowRegistry(dir, "test")
	if err == nil {
		t.Fatal("expected validation error for duplicate status id, got nil")
	}
}

// TestLoadWorkflowRegistry_ValidationDuplicateTransitionID verifies that two transitions
// sharing the same id cause a validation error.
func TestLoadWorkflowRegistry_ValidationDuplicateTransitionID(t *testing.T) {
	dir := t.TempDir()
	content := `
workflow:
  id: test
  key: test
statuses:
  - id: status:new
    role: po
    queues_work: true
  - id: status:done
    terminal: true
transitions:
  - id: finish
    from: [status:new]
    to: status:done
    allowed_roles: [po]
  - id: finish
    from: [status:new]
    to: status:done
    allowed_roles: [developer]
`
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadWorkflowRegistry(dir, "test")
	if err == nil {
		t.Fatal("expected validation error for duplicate transition id, got nil")
	}
}

// TestLeanWorkflowStatuses verifies that the lean workflow contains the expected statuses.
func TestLeanWorkflowStatuses(t *testing.T) {
	reg, err := LoadWorkflowRegistry("workflows", "lean")
	if err != nil {
		t.Fatalf("LoadWorkflowRegistry: %v", err)
	}
	wd := reg.Get("lean")

	for _, id := range []string{
		"status:new", "status:story-definition", "status:dev-planning",
		"status:plan-review", "status:ready-for-development", "status:in-development",
		"status:code-review", "status:po-approval", "status:blocked",
		"status:done", "status:rejected",
	} {
		if !wd.HasStatus(id) {
			t.Errorf("lean workflow missing expected status %q", id)
		}
	}
}

// TestFullWorkflowStatuses verifies that the full workflow contains its expected statuses
// and that the id was corrected from the original erroneous value.
func TestFullWorkflowStatuses(t *testing.T) {
	reg, err := LoadWorkflowRegistry("workflows", "lean")
	if err != nil {
		t.Fatalf("LoadWorkflowRegistry: %v", err)
	}
	wd := reg.Get("full")

	if wd.Workflow.ID == "lean_github_issue_workflow" {
		t.Error("full workflow still has the wrong id 'lean_github_issue_workflow'; expected 'full_6_roles_issue_workflow'")
	}
	if wd.Workflow.ID != "full_6_roles_issue_workflow" {
		t.Errorf("full workflow id: got %q, want %q", wd.Workflow.ID, "full_6_roles_issue_workflow")
	}

	for _, id := range []string{
		"status:new", "status:triage", "status:solution-design",
		"status:ready-for-dev", "status:in-development", "status:code-review",
		"status:testing", "status:po-acceptance", "status:blocked",
		"status:done", "status:rejected",
	} {
		if !wd.HasStatus(id) {
			t.Errorf("full workflow missing expected status %q", id)
		}
	}
}
