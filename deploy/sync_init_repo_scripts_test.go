package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateInitRepoScriptIncludesWorkflowLabels(t *testing.T) {
	got, err := generateInitRepoScript("../workflows/workflow-full-6-roles-issue.yaml")
	if err != nil {
		t.Fatalf("generateInitRepoScript: %v", err)
	}
	body := string(got)
	for _, want := range []string{
		`upsert_label "status:solution-design"`,
		`upsert_label "status:ui-design"`,
		`upsert_label "type:change-request"`,
		`upsert_label "type:smoke-test"`,
		`upsert_label "role:architect"`,
		`upsert_label "needs:architecture"`,
		`upsert_label "needs:ui-design"`,
		`upsert_label "risk:high"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("generated script missing %q", want)
		}
	}
}

func TestGeneratedInitRepoScriptsAreInSync(t *testing.T) {
	targets := []syncTarget{
		{WorkflowPath: "../workflows/workflow-lean-3-roles-issue.yaml", OutputPath: "init_repo_lean.sh"},
		{WorkflowPath: "../workflows/workflow-full-6-roles-issue.yaml", OutputPath: "init_repo_full.sh"},
	}
	for _, target := range targets {
		generated, err := generateInitRepoScript(target.WorkflowPath)
		if err != nil {
			t.Fatalf("generate %s: %v", target.WorkflowPath, err)
		}
		current, err := os.ReadFile(target.OutputPath)
		if err != nil {
			t.Fatalf("read %s: %v", target.OutputPath, err)
		}
		if !bytes.Equal(current, generated) {
			t.Fatalf("%s is out of sync; run go run ./deploy/sync_init_repo_scripts.go", target.OutputPath)
		}
	}
}

func TestSyncOneCheckDetectsDrift(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	outPath := filepath.Join(dir, "init.sh")
	workflow := []byte(`workflow:
  key: tiny
  roles: [po]
statuses:
  - id: status:new
issue_types:
  - id: type:bug
    name: bug
guards: {}
`)
	if err := os.WriteFile(workflowPath, workflow, 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	if err := os.WriteFile(outPath, []byte("# stale\n"), 0o755); err != nil {
		t.Fatalf("write output: %v", err)
	}

	err := syncOne(syncTarget{WorkflowPath: workflowPath, OutputPath: outPath}, true)
	if err == nil {
		t.Fatal("expected drift check to fail")
	}
	if !strings.Contains(err.Error(), "out of sync") {
		t.Fatalf("expected out-of-sync error, got %v", err)
	}
}
