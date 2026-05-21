package main

import (
	"bytes"
	"log"
	"testing"
)

// leanWorkflowForTest loads the lean workflow from the real YAML files.
func leanWorkflowForTest(t *testing.T) *WorkflowDef {
	t.Helper()
	reg, err := LoadWorkflowRegistry("workflows", "lean")
	if err != nil {
		t.Fatalf("load lean workflow: %v", err)
	}
	return reg.Get("lean")
}

func fullWorkflowForTest(t *testing.T) *WorkflowDef {
	t.Helper()
	reg, err := LoadWorkflowRegistry("workflows", "lean")
	if err != nil {
		t.Fatalf("load full workflow: %v", err)
	}
	return reg.Get("full")
}

// ---------------------------------------------------------------------------
// computeWorkflowStateFromDef
// ---------------------------------------------------------------------------

func TestComputeWorkflowStateFromDef_KnownStatus(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{
		Number: 1,
		User:   GitHubUser{Login: "creator"},
		Labels: []GitHubLabel{{Name: "status:in-development"}},
	}
	ws := computeWorkflowStateFromDef(wd, issue, nil)
	if ws.StatusLabel != "status:in-development" {
		t.Errorf("StatusLabel: got %q, want %q", ws.StatusLabel, "status:in-development")
	}
	if ws.SuggestedRole != "developer" {
		t.Errorf("SuggestedRole: got %q, want %q", ws.SuggestedRole, "developer")
	}
}

func TestComputeWorkflowStateFromDef_TerminalStatus(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:done"}}}
	ws := computeWorkflowStateFromDef(wd, issue, nil)
	if ws.SuggestedRole != "" {
		t.Errorf("SuggestedRole for terminal: got %q, want empty", ws.SuggestedRole)
	}
}

func TestComputeWorkflowStateFromDef_UnknownStatus(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:some-legacy-status"}}}
	ws := computeWorkflowStateFromDef(wd, issue, nil)
	if ws.SuggestedRole != "unknown" {
		t.Errorf("SuggestedRole for unknown status: got %q, want %q", ws.SuggestedRole, "unknown")
	}
}

func TestComputeWorkflowStateFromDef_NoStatus(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1}
	ws := computeWorkflowStateFromDef(wd, issue, nil)
	if ws.StatusLabel != "" {
		t.Errorf("StatusLabel for no label: got %q, want empty", ws.StatusLabel)
	}
	if ws.SuggestedRole != "unknown" {
		t.Errorf("SuggestedRole for empty status: got %q, want %q", ws.SuggestedRole, "unknown")
	}
}

func TestComputeWorkflowStateFromDef_POStatusesUsePORole(t *testing.T) {
	wd := leanWorkflowForTest(t)
	for _, label := range []string{"status:new", "status:story-definition", "status:po-approval", "status:blocked"} {
		issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: label}}}
		ws := computeWorkflowStateFromDef(wd, issue, nil)
		if ws.SuggestedRole != "po" {
			t.Errorf("label=%s: SuggestedRole=%q, want %q", label, ws.SuggestedRole, "po")
		}
	}
}

// ---------------------------------------------------------------------------
// validateTransitionFromDef
// ---------------------------------------------------------------------------

func TestValidateTransitionFromDef_AllowedTransition(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:new"}}}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:story-definition")
	if !res.Allowed {
		t.Errorf("expected allowed, violations: %v", res.Violations)
	}
}

func TestValidateTransitionFromDef_NewDoesNotSkipStoryDefinition(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:new"}}}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:dev-planning")
	if res.Allowed {
		t.Error("expected status:new -> status:dev-planning to be disallowed; PO must pass through story-definition")
	}
}

func TestValidateTransitionFromDef_WrongRole(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:new"}}}
	res := validateTransitionFromDef(wd, issue, nil, "developer", "status:story-definition")
	if res.Allowed {
		t.Error("expected not allowed: developer cannot drive story-definition from status:new")
	}
}

func TestValidateTransitionFromDef_UnknownTargetStatus(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:new"}}}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:nonexistent")
	if res.Allowed {
		t.Error("expected not allowed for unknown target status")
	}
}

func TestValidateTransitionFromDef_EmptyActorRole(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:new"}}}
	res := validateTransitionFromDef(wd, issue, nil, "", "status:story-definition")
	if res.Allowed {
		t.Error("expected not allowed for empty actor_role")
	}
}

func TestValidateTransitionFromDef_NoMatchingTransition(t *testing.T) {
	wd := leanWorkflowForTest(t)
	// story-definition → code-review is not a valid direct transition in lean workflow
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:story-definition"}}}
	res := validateTransitionFromDef(wd, issue, nil, "developer", "status:code-review")
	if res.Allowed {
		t.Error("expected not allowed: no direct transition from story-definition to code-review")
	}
}

func TestValidateTransitionFromDef_DeveloperCanStartImplementation(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:ready-for-development"}}}
	res := validateTransitionFromDef(wd, issue, nil, "developer", "status:in-development")
	if !res.Allowed {
		t.Errorf("expected allowed, violations: %v", res.Violations)
	}
}

func TestValidateTransitionFromDef_ReviewerCanApproveCode(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:code-review"}}}
	res := validateTransitionFromDef(wd, issue, nil, "reviewer", "status:po-approval")
	if !res.Allowed {
		t.Errorf("expected allowed, violations: %v", res.Violations)
	}
}

func TestValidateTransitionFromDef_BlockIssue(t *testing.T) {
	wd := leanWorkflowForTest(t)
	for _, from := range []string{"status:in-development", "status:code-review", "status:po-approval"} {
		issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: from}}}
		res := validateTransitionFromDef(wd, issue, nil, "developer", "status:blocked")
		if !res.Allowed {
			t.Errorf("label=%s: developer block should be allowed, violations: %v", from, res.Violations)
		}
	}
}

func TestValidateTransitionFromDef_ResumeFromBlockedWithMetadata(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:blocked"}}}
	meta := map[string]string{"blocked_from": "status:in-development"}
	res := validateTransitionFromDef(wd, issue, meta, "po", "status:in-development")
	if !res.Allowed {
		t.Errorf("expected resume_from_blocked to be allowed, violations: %v", res.Violations)
	}
}

func TestValidateTransitionFromDef_ResumeFromBlockedMissingMetadata(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:blocked"}}}
	// No meta — can still go to story-definition via blocked_back_to_definition
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:story-definition")
	if !res.Allowed {
		t.Errorf("expected blocked_back_to_definition to be allowed, violations: %v", res.Violations)
	}
}

func TestValidateTransitionFromDef_TerminalCanBeReopened(t *testing.T) {
	wd := leanWorkflowForTest(t)
	issue := Issue{Number: 1, Labels: []GitHubLabel{{Name: "status:done"}}}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:story-definition")
	if !res.Allowed {
		t.Errorf("expected po_reopen_done to be allowed, violations: %v", res.Violations)
	}
}

// ---------------------------------------------------------------------------
// resolveTransitionTarget
// ---------------------------------------------------------------------------

func TestResolveTransitionTarget_Static(t *testing.T) {
	val, err := resolveTransitionTarget("status:in-development", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "status:in-development" {
		t.Errorf("got %q, want %q", val, "status:in-development")
	}
}

func TestResolveTransitionTarget_MetadataPresent(t *testing.T) {
	meta := map[string]string{"blocked_from": "status:code-review"}
	val, err := resolveTransitionTarget("$metadata.blocked_from", meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "status:code-review" {
		t.Errorf("got %q, want %q", val, "status:code-review")
	}
}

func TestResolveTransitionTarget_MetadataMissing(t *testing.T) {
	_, err := resolveTransitionTarget("$metadata.blocked_from", map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing metadata key, got nil")
	}
}

func TestResolveTransitionTarget_NilMeta(t *testing.T) {
	_, err := resolveTransitionTarget("$metadata.blocked_from", nil)
	if err == nil {
		t.Fatal("expected error for nil meta, got nil")
	}
}

func TestResolveTransitionTarget_UnrecognizedDynamic(t *testing.T) {
	_, err := resolveTransitionTarget("$unknown.foo", nil)
	if err == nil {
		t.Fatal("expected error for unrecognized dynamic target, got nil")
	}
}

// ---------------------------------------------------------------------------
// evaluateGuard
// ---------------------------------------------------------------------------

func TestEvaluateGuard_AnyLabelPresent(t *testing.T) {
	gd := GuardDef{AnyLabel: []string{"area:ui", "needs:ui-design"}}
	issue := Issue{Labels: []GitHubLabel{{Name: "area:ui"}, {Name: "type:feature"}}}
	if !evaluateGuard(gd, issue) {
		t.Error("expected guard to pass when any_label is present")
	}
}

func TestEvaluateGuard_AnyLabelAbsent(t *testing.T) {
	gd := GuardDef{AnyLabel: []string{"area:ui", "needs:ui-design"}}
	issue := Issue{Labels: []GitHubLabel{{Name: "type:feature"}}}
	if evaluateGuard(gd, issue) {
		t.Error("expected guard to fail when none of any_label are present")
	}
}

func TestEvaluateGuard_AllAbsentSatisfied(t *testing.T) {
	gd := GuardDef{AllAbsent: []string{"needs:architecture", "risk:high"}}
	issue := Issue{Labels: []GitHubLabel{{Name: "type:bug"}}}
	if !evaluateGuard(gd, issue) {
		t.Error("expected guard to pass when all_absent labels are absent")
	}
}

func TestEvaluateGuard_AllAbsentViolated(t *testing.T) {
	gd := GuardDef{AllAbsent: []string{"needs:architecture", "risk:high"}}
	issue := Issue{Labels: []GitHubLabel{{Name: "risk:high"}}}
	if evaluateGuard(gd, issue) {
		t.Error("expected guard to fail when an all_absent label is present")
	}
}

func TestEvaluateGuard_EmptyGuardAlwaysPasses(t *testing.T) {
	gd := GuardDef{}
	if !evaluateGuard(gd, Issue{}) {
		t.Error("expected empty guard to always pass")
	}
}

// ---------------------------------------------------------------------------
// decideNextActionFromDef
// ---------------------------------------------------------------------------

func TestDecideNextActionFromDef_QueuesWorkForPO(t *testing.T) {
	wd := leanWorkflowForTest(t)
	repo := "owner/repo"
	issue := Issue{Number: 5, Labels: []GitHubLabel{{Name: "status:story-definition"}}}
	state := computeWorkflowStateFromDef(wd, issue, nil)
	pkg, ok := decideNextActionFromDef(wd, repo, issue, state, nil)
	if !ok {
		t.Fatal("expected ok=true for queues_work status, got false")
	}
	if pkg.Role != "po" {
		t.Errorf("role: got %q, want %q", pkg.Role, "po")
	}
	if pkg.CurrentStatus != "status:story-definition" {
		t.Errorf("CurrentStatus: got %q, want %q", pkg.CurrentStatus, "status:story-definition")
	}
}

func TestDecideNextActionFromDef_TerminalStatusNoWork(t *testing.T) {
	wd := leanWorkflowForTest(t)
	repo := "owner/repo"
	for _, label := range []string{"status:done", "status:rejected"} {
		issue := Issue{Number: 5, Labels: []GitHubLabel{{Name: label}}}
		state := computeWorkflowStateFromDef(wd, issue, nil)
		_, ok := decideNextActionFromDef(wd, repo, issue, state, nil)
		if ok {
			t.Errorf("label=%s: expected ok=false for terminal status, got true", label)
		}
	}
}

func TestDecideNextActionFromDef_DeveloperRoleForImplStatuses(t *testing.T) {
	wd := leanWorkflowForTest(t)
	repo := "owner/repo"
	for _, label := range []string{"status:dev-planning", "status:ready-for-development", "status:in-development"} {
		issue := Issue{Number: 5, Labels: []GitHubLabel{{Name: label}}}
		state := computeWorkflowStateFromDef(wd, issue, nil)
		pkg, ok := decideNextActionFromDef(wd, repo, issue, state, nil)
		if !ok {
			t.Errorf("label=%s: expected ok=true, got false", label)
		}
		if pkg.Role != "developer" {
			t.Errorf("label=%s: role=%q, want %q", label, pkg.Role, "developer")
		}
	}
}

func TestDecideNextActionFromDef_ReviewerRoleForReviewStatuses(t *testing.T) {
	wd := leanWorkflowForTest(t)
	repo := "owner/repo"
	for _, label := range []string{"status:plan-review", "status:code-review"} {
		issue := Issue{Number: 5, Labels: []GitHubLabel{{Name: label}}}
		state := computeWorkflowStateFromDef(wd, issue, nil)
		pkg, ok := decideNextActionFromDef(wd, repo, issue, state, nil)
		if !ok {
			t.Errorf("label=%s: expected ok=true, got false", label)
		}
		if pkg.Role != "reviewer" {
			t.Errorf("label=%s: role=%q, want %q", label, pkg.Role, "reviewer")
		}
	}
}

func TestDecideNextActionFromDef_UnknownStatusNoWork(t *testing.T) {
	wd := leanWorkflowForTest(t)
	repo := "owner/repo"
	issue := Issue{Number: 5, Labels: []GitHubLabel{{Name: "status:some-legacy-status"}}}
	state := computeWorkflowStateFromDef(wd, issue, nil)
	_, ok := decideNextActionFromDef(wd, repo, issue, state, nil)
	if ok {
		t.Error("expected ok=false for status not in workflow, got true")
	}
}

// ---------------------------------------------------------------------------
// full workflow guards
// ---------------------------------------------------------------------------

func TestFullWorkflow_GuardedTransitionPasses(t *testing.T) {
	wd := fullWorkflowForTest(t)
	// po_request_architecture requires needs_architecture guard
	issue := Issue{
		Number: 1,
		Labels: []GitHubLabel{{Name: "status:triage"}, {Name: "needs:architecture"}},
	}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:solution-design")
	if !res.Allowed {
		t.Errorf("expected guarded transition to pass, violations: %v", res.Violations)
	}
}

func TestFullWorkflow_GuardedTransitionFails(t *testing.T) {
	wd := fullWorkflowForTest(t)
	// po_request_architecture requires needs_architecture guard; issue has no such label
	issue := Issue{
		Number: 1,
		Labels: []GitHubLabel{{Name: "status:triage"}},
	}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:solution-design")
	if res.Allowed {
		t.Error("expected guarded transition to fail when guard label is absent")
	}
}

func TestFullWorkflow_NewIssueRoutesDirectlyToArchitecture(t *testing.T) {
	wd := fullWorkflowForTest(t)
	issue := Issue{
		Number: 1,
		Labels: []GitHubLabel{{Name: "status:new"}, {Name: "needs:architecture"}},
	}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:solution-design")
	if !res.Allowed {
		t.Errorf("expected status:new -> solution-design to pass, violations: %v", res.Violations)
	}
}

func TestFullWorkflow_NewIssueRoutesDirectlyToUIDesign(t *testing.T) {
	wd := fullWorkflowForTest(t)
	issue := Issue{
		Number: 1,
		Labels: []GitHubLabel{{Name: "status:new"}, {Name: "area:ui"}},
	}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:ui-design")
	if !res.Allowed {
		t.Errorf("expected status:new -> ui-design to pass, violations: %v", res.Violations)
	}
}

func TestFullWorkflow_NewIssueRoutesDirectlyToDeveloper(t *testing.T) {
	wd := fullWorkflowForTest(t)
	issue := Issue{
		Number: 1,
		Labels: []GitHubLabel{{Name: "status:new"}, {Name: "type:bug"}},
	}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:ready-for-dev")
	if !res.Allowed {
		t.Errorf("expected status:new -> ready-for-dev to pass, violations: %v", res.Violations)
	}
}

func TestFullWorkflow_NewIssueDoesNotSelfHandoffToTriage(t *testing.T) {
	wd := fullWorkflowForTest(t)
	issue := Issue{
		Number: 1,
		Labels: []GitHubLabel{{Name: "status:new"}},
	}
	res := validateTransitionFromDef(wd, issue, nil, "po", "status:triage")
	if res.Allowed {
		t.Error("expected status:new -> triage to be disallowed; PO intake should hand off directly")
	}
}

func TestFullWorkflow_NextRolesFromNewExcludesPO(t *testing.T) {
	wd := fullWorkflowForTest(t)
	roles := wd.NextRolesFrom("status:new")
	for _, want := range []string{"architect", "uidesigner", "developer"} {
		if !containsString(roles, want) {
			t.Errorf("expected NextRolesFrom(status:new) to include %q, got %v", want, roles)
		}
	}
	if containsString(roles, "po") {
		t.Errorf("expected NextRolesFrom(status:new) not to include po self-handoff, got %v", roles)
	}
}

// ---------------------------------------------------------------------------
// applyTransitionMetadata
// ---------------------------------------------------------------------------

// TestApplyTransitionMetadata_Set verifies that set_metadata entries are written to the store,
// and that the "$from" special value is resolved to the actual fromStatus.
func TestApplyTransitionMetadata_Set(t *testing.T) {
	store := tempStore(t)
	s := &Server{store: store, logger: log.New(&bytes.Buffer{}, "", 0)}

	td := &TransitionDef{
		SetMetadata: map[string]string{
			"blocked_from": "$from",
			"reason":       "manual",
		},
	}
	s.applyTransitionMetadata(42, "status:in-development", td)

	val, ok, err := store.GetIssueMetadata(42, "blocked_from")
	if err != nil {
		t.Fatalf("GetIssueMetadata blocked_from: %v", err)
	}
	if !ok {
		t.Fatal("expected blocked_from metadata to be set, got absent")
	}
	if val != "status:in-development" {
		t.Errorf("blocked_from: got %q, want %q", val, "status:in-development")
	}

	val2, ok2, err := store.GetIssueMetadata(42, "reason")
	if err != nil {
		t.Fatalf("GetIssueMetadata reason: %v", err)
	}
	if !ok2 {
		t.Fatal("expected reason metadata to be set, got absent")
	}
	if val2 != "manual" {
		t.Errorf("reason: got %q, want %q", val2, "manual")
	}
}

// TestApplyTransitionMetadata_Clear verifies that clear_metadata removes the specified keys
// from the store while leaving other keys intact.
func TestApplyTransitionMetadata_Clear(t *testing.T) {
	store := tempStore(t)
	s := &Server{store: store, logger: log.New(&bytes.Buffer{}, "", 0)}

	// Seed two metadata keys.
	if err := store.SetIssueMetadata(7, "blocked_from", "status:in-development"); err != nil {
		t.Fatalf("seed SetIssueMetadata: %v", err)
	}
	if err := store.SetIssueMetadata(7, "other", "keep"); err != nil {
		t.Fatalf("seed SetIssueMetadata other: %v", err)
	}

	td := &TransitionDef{ClearMetadata: []string{"blocked_from"}}
	s.applyTransitionMetadata(7, "status:blocked", td)

	_, ok, err := store.GetIssueMetadata(7, "blocked_from")
	if err != nil {
		t.Fatalf("GetIssueMetadata blocked_from: %v", err)
	}
	if ok {
		t.Error("expected blocked_from to be cleared, but it is still present")
	}

	val, ok2, err := store.GetIssueMetadata(7, "other")
	if err != nil {
		t.Fatalf("GetIssueMetadata other: %v", err)
	}
	if !ok2 || val != "keep" {
		t.Errorf("expected 'other' key to be retained with value %q, got ok=%v val=%q", "keep", ok2, val)
	}
}

// ---------------------------------------------------------------------------
// ValidTransitionsFrom
// ---------------------------------------------------------------------------

func TestValidTransitionsFrom_InDevelopment(t *testing.T) {
	wd := leanWorkflowForTest(t)
	targets := wd.ValidTransitionsFrom("status:in-development")
	// Expected static targets from status:in-development:
	// developer_start_or_continue_implementation → status:in-development (self)
	// developer_submit_code_review → status:code-review
	// block_issue → status:blocked
	// po_reject_active_issue → status:rejected
	// resume_from_blocked target is dynamic ($metadata.*) and must NOT appear.
	want := map[string]bool{
		"status:in-development": true,
		"status:code-review":    true,
		"status:blocked":        true,
		"status:rejected":       true,
	}
	if len(targets) != len(want) {
		t.Errorf("len(targets)=%d, want %d; targets=%v", len(targets), len(want), targets)
	}
	for _, tgt := range targets {
		if !want[tgt] {
			t.Errorf("unexpected target %q in ValidTransitionsFrom(status:in-development)", tgt)
		}
	}
}

func TestValidTransitionsFrom_ReadyForDevelopmentCanSubmitCodeReview(t *testing.T) {
	wd := leanWorkflowForTest(t)
	targets := wd.ValidTransitionsFrom("status:ready-for-development")
	if !containsString(targets, "status:code-review") {
		t.Errorf("expected status:code-review in ValidTransitionsFrom(status:ready-for-development), got %v", targets)
	}
}

func TestValidTransitionsFrom_FullReadyForDevCanSubmitCodeReview(t *testing.T) {
	wd := fullWorkflowForTest(t)
	targets := wd.ValidTransitionsFrom("status:ready-for-dev")
	if !containsString(targets, "status:code-review") {
		t.Errorf("expected status:code-review in ValidTransitionsFrom(status:ready-for-dev), got %v", targets)
	}
}

func TestValidTransitionsFrom_BlockedExcludesDynamic(t *testing.T) {
	wd := leanWorkflowForTest(t)
	targets := wd.ValidTransitionsFrom("status:blocked")
	// resume_from_blocked has to: "$metadata.blocked_from" — must NOT appear.
	// blocked_back_to_definition → status:story-definition
	// po_reject_active_issue → status:rejected
	for _, tgt := range targets {
		if len(tgt) > 0 && tgt[0] == '$' {
			t.Errorf("dynamic target %q must not appear in ValidTransitionsFrom results", tgt)
		}
	}
	found := map[string]bool{}
	for _, tgt := range targets {
		found[tgt] = true
	}
	if !found["status:story-definition"] {
		t.Errorf("expected status:story-definition in ValidTransitionsFrom(status:blocked); got %v", targets)
	}
	if !found["status:rejected"] {
		t.Errorf("expected status:rejected in ValidTransitionsFrom(status:blocked); got %v", targets)
	}
}

func TestValidTransitionsFrom_DoneHasReopenTransition(t *testing.T) {
	wd := leanWorkflowForTest(t)
	// status:done has po_reopen_done → status:story-definition
	targets := wd.ValidTransitionsFrom("status:done")
	found := false
	for _, tgt := range targets {
		if tgt == "status:story-definition" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected status:story-definition (reopen) from status:done; got %v", targets)
	}
}

func TestValidTransitionsFrom_UnknownStatusEmpty(t *testing.T) {
	wd := leanWorkflowForTest(t)
	targets := wd.ValidTransitionsFrom("status:nonexistent")
	if len(targets) != 0 {
		t.Errorf("expected empty slice for unknown status, got %v", targets)
	}
}

// ---------------------------------------------------------------------------
// parseNextAssigneeRole
// ---------------------------------------------------------------------------

func TestParseNextAssigneeRole_ValidFooter(t *testing.T) {
	comments := []IssueComment{
		{ID: 1, Body: "Work complete.\n[next assignee role -> reviewer]"},
	}
	role, ok := parseNextAssigneeRole(comments)
	if !ok {
		t.Fatal("expected ok=true for valid footer, got false")
	}
	if role != "reviewer" {
		t.Errorf("role=%q, want %q", role, "reviewer")
	}
}

func TestParseNextAssigneeRole_FooterWithSpaces(t *testing.T) {
	comments := []IssueComment{
		{ID: 1, Body: "[next assignee role ->  developer ]"},
	}
	role, ok := parseNextAssigneeRole(comments)
	if !ok {
		t.Fatal("expected ok=true for footer with extra spaces, got false")
	}
	if role != "developer" {
		t.Errorf("role=%q, want %q", role, "developer")
	}
}

func TestParseNextAssigneeRole_NoFooter(t *testing.T) {
	comments := []IssueComment{
		{ID: 1, Body: "Implementation complete, PR is up."},
	}
	_, ok := parseNextAssigneeRole(comments)
	if ok {
		t.Error("expected ok=false when no footer present, got true")
	}
}

func TestParseNextAssigneeRole_EmptyComments(t *testing.T) {
	_, ok := parseNextAssigneeRole(nil)
	if ok {
		t.Error("expected ok=false for empty comments, got true")
	}
}

func TestParseNextAssigneeRole_FooterOnlyInOlderComment(t *testing.T) {
	// Footer is in the first comment, not the last — must not match.
	comments := []IssueComment{
		{ID: 1, Body: "[next assignee role -> developer]"},
		{ID: 2, Body: "Updated implementation."},
	}
	_, ok := parseNextAssigneeRole(comments)
	if ok {
		t.Error("expected ok=false when footer is only in an older comment, got true")
	}
}

func TestParseNextAssigneeRole_EmptyRoleValue(t *testing.T) {
	comments := []IssueComment{
		{ID: 1, Body: "[next assignee role ->  ]"},
	}
	_, ok := parseNextAssigneeRole(comments)
	if ok {
		t.Error("expected ok=false for empty role value in footer, got true")
	}
}

func TestPastWorkersFromComments(t *testing.T) {
	comments := []IssueComment{
		{ID: 1, Body: "Author: bud-dev\n\nImplementation report."},
		{ID: 2, Body: "No author header here."},
		{ID: 3, Body: "Author: mud-rev\n\nReview report."},
		{ID: 4, Body: "Author: bud-dev\n\nFollow-up."},
	}
	got := pastWorkersFromComments(comments)
	want := []string{"bud-dev", "mud-rev"}
	if len(got) != len(want) {
		t.Fatalf("pastWorkersFromComments=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pastWorkersFromComments=%v, want %v", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// HasRole
// ---------------------------------------------------------------------------

func TestHasRole_KnownRole(t *testing.T) {
	wd := leanWorkflowForTest(t)
	for _, role := range []string{"po", "developer", "reviewer"} {
		if !wd.HasRole(role) {
			t.Errorf("HasRole(%q) = false, want true", role)
		}
	}
}

func TestHasRole_UnknownRole(t *testing.T) {
	wd := leanWorkflowForTest(t)
	if wd.HasRole("bogusrole") {
		t.Error("HasRole(\"bogusrole\") = true, want false")
	}
}

// ---------------------------------------------------------------------------
// NextRolesFrom
// ---------------------------------------------------------------------------

func TestNextRolesFrom_InDevelopment(t *testing.T) {
	wd := leanWorkflowForTest(t)
	// Normal forward path from status:in-development is to reviewer via code-review.
	// Self-loop (continue implementation) and exception paths (block, reject) must be excluded.
	roles := wd.NextRolesFrom("status:in-development")
	if !containsString(roles, "reviewer") {
		t.Errorf("expected NextRolesFrom(status:in-development) to include %q, got %v", "reviewer", roles)
	}
	if containsString(roles, "developer") {
		t.Errorf("expected NextRolesFrom(status:in-development) NOT to include %q (self-loop), got %v", "developer", roles)
	}
	if containsString(roles, "po") {
		t.Errorf("expected NextRolesFrom(status:in-development) NOT to include %q (exception path), got %v", "po", roles)
	}
}

func TestNextRolesFrom_ReadyForDevelopmentIncludesReviewer(t *testing.T) {
	wd := leanWorkflowForTest(t)
	roles := wd.NextRolesFrom("status:ready-for-development")
	if !containsString(roles, "reviewer") {
		t.Errorf("expected NextRolesFrom(status:ready-for-development) to include %q, got %v", "reviewer", roles)
	}
}

func TestNextRolesFrom_FullReadyForDevIncludesReviewer(t *testing.T) {
	wd := fullWorkflowForTest(t)
	roles := wd.NextRolesFrom("status:ready-for-dev")
	if !containsString(roles, "reviewer") {
		t.Errorf("expected NextRolesFrom(status:ready-for-dev) to include %q, got %v", "reviewer", roles)
	}
}

func TestNextRolesFrom_New(t *testing.T) {
	wd := leanWorkflowForTest(t)
	// status:new → po_start_definition → status:story-definition, so PO remains next.
	roles := wd.NextRolesFrom("status:new")
	if !containsString(roles, "po") {
		t.Errorf("expected NextRolesFrom(status:new) to include %q, got %v", "po", roles)
	}
	if containsString(roles, "developer") {
		t.Errorf("expected NextRolesFrom(status:new) not to include %q before story definition, got %v", "developer", roles)
	}
}

func TestNextRolesFrom_UnknownStatus(t *testing.T) {
	wd := leanWorkflowForTest(t)
	roles := wd.NextRolesFrom("status:nonexistent")
	if len(roles) != 0 {
		t.Errorf("expected empty slice for unknown status, got %v", roles)
	}
}

func TestNextRolesFrom_Terminal(t *testing.T) {
	wd := leanWorkflowForTest(t)
	// status:done has po_reopen_done → status:story-definition (role: po),
	// so the reachable role is "po", not empty.
	roles := wd.NextRolesFrom("status:done")
	if !containsString(roles, "po") {
		t.Errorf("expected NextRolesFrom(status:done) to include %q via reopen transition, got %v", "po", roles)
	}
}

// ---------------------------------------------------------------------------
// blocked round-trip (integration-style)
// ---------------------------------------------------------------------------

func TestBlockedRoundTrip_MetadataSetAndResolved(t *testing.T) {
	wd := leanWorkflowForTest(t)
	store := tempStore(t)

	issueID := 42
	fromStatus := "status:in-development"

	// Find the block_issue transition to simulate what transitionIssue does.
	meta := map[string]string{}
	matched := findMatchingTransitionDef(wd, fromStatus, "status:blocked", meta)
	if matched == nil {
		t.Fatal("expected block_issue transition to be found")
	}
	if matched.SetMetadata["blocked_from"] != "$from" {
		t.Fatalf("expected set_metadata.blocked_from=$from, got %q", matched.SetMetadata["blocked_from"])
	}

	// Simulate applying set_metadata (resolved $from → actual fromStatus).
	if err := store.SetIssueMetadata(issueID, "blocked_from", fromStatus); err != nil {
		t.Fatalf("SetIssueMetadata: %v", err)
	}

	// Now try to resume; the target should resolve to fromStatus.
	meta2, _ := store.GetIssueMetadataMap(issueID)
	resumeMatch := findMatchingTransitionDef(wd, "status:blocked", fromStatus, meta2)
	if resumeMatch == nil {
		t.Fatal("expected resume_from_blocked transition to be found after metadata is set")
	}
	if resumeMatch.ID != "resume_from_blocked" {
		t.Errorf("matched transition: got %q, want %q", resumeMatch.ID, "resume_from_blocked")
	}

	// Simulate applying clear_metadata.
	if err := store.ClearIssueMetadata(issueID, resumeMatch.ClearMetadata); err != nil {
		t.Fatalf("ClearIssueMetadata: %v", err)
	}
	_, ok, _ := store.GetIssueMetadata(issueID, "blocked_from")
	if ok {
		t.Error("expected blocked_from metadata to be cleared after resume")
	}
}
