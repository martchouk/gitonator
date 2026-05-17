package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadWorkPackageReadsRawJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work.json")
	if err := os.WriteFile(path, []byte(`{"id":42,"repo":"martchouk/github.mcp","issue_id":8,"role":"developer","current_status":"status:dev-planning"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	pkg, err := readWorkPackage(path)
	if err != nil {
		t.Fatalf("readWorkPackage: %v", err)
	}
	if pkg.ID != 42 || pkg.Role != "developer" || pkg.CurrentStatus != "status:dev-planning" {
		t.Fatalf("unexpected package: %+v", pkg)
	}
}

func TestReadWorkPackageReadsPromptWrappedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work-prompt.txt")
	raw := `AUTHORITATIVE WORK PACKAGE

Rules:
- Use valid_transitions only.

WORK PACKAGE JSON:
{
  "id": 51,
  "repo": "martchouk/github.mcp",
  "issue_id": 52,
  "role": "reviewer",
  "current_status": "status:code-review",
  "workflow_key": "lean",
  "valid_transitions": ["status:po-approval"],
  "next_assignee_roles": ["po"]
}
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	pkg, err := readWorkPackage(path)
	if err != nil {
		t.Fatalf("readWorkPackage: %v", err)
	}
	if pkg.ID != 51 || pkg.IssueID != 52 || pkg.WorkflowKey != "lean" {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	if len(pkg.ValidTransitions) != 1 || pkg.ValidTransitions[0] != "status:po-approval" {
		t.Fatalf("unexpected valid transitions: %+v", pkg.ValidTransitions)
	}
	if len(pkg.NextAssigneeRoles) != 1 || pkg.NextAssigneeRoles[0] != "po" {
		t.Fatalf("unexpected next roles: %+v", pkg.NextAssigneeRoles)
	}
}
