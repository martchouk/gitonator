package main

import (
	"testing"
)

// TestStatusLabelToRole verifies every status label maps to the correct suggested role.
// This is a regression guard: any future change to statusLabels or computeWorkflowState
// must update this table.
func TestStatusLabelToRole(t *testing.T) {
	type tc struct {
		label string
		want  string // expected SuggestedRole
	}
	cases := []tc{
		{"status:new", "po"},
		{"status:po-analysis", "po"},
		{"status:awaiting-stakeholder-approval", "stakeholder"},
		{"status:approved-for-dev", "developer"},
		{"status:in-progress", "developer"},
		{"status:ready-for-review", "reviewer"},
		{"status:review-in-progress", "reviewer"},
		{"status:changes-requested", "developer"},
		{"status:ready-for-po-review", "po"},
		{"status:po-review-in-progress", "po"},
		{"status:awaiting-final-stakeholder-approval", "stakeholder"},
		{"status:blocked", "po"},
		{"status:done", "done"},
		{"status:rejected", "rejected"},
	}

	if len(cases) != len(statusLabels) {
		t.Fatalf("test cases count=%d does not match statusLabels count=%d; update this test", len(cases), len(statusLabels))
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			issue := Issue{
				Number: 1,
				User:   GitHubUser{Login: "creator"},
				Labels: []GitHubLabel{{Name: c.label}},
			}
			ws := computeWorkflowState(issue, nil)
			if ws.SuggestedRole != c.want {
				t.Errorf("label=%s: got SuggestedRole=%q, want %q", c.label, ws.SuggestedRole, c.want)
			}
		})
	}
}

// TestDecideNextActionQueuesCorrectRole verifies decideNextAction returns the right role
// and returns false for terminal and human-wait states.
func TestDecideNextActionQueuesCorrectRole(t *testing.T) {
	cfg := Config{Owner: "owner", Repo: "repo"}
	type tc struct {
		label   string
		wantOK  bool
		wantRole string
	}
	cases := []tc{
		{"status:new", true, "po"},
		{"status:po-analysis", true, "po"},
		{"status:awaiting-stakeholder-approval", false, ""},
		{"status:approved-for-dev", true, "developer"},
		{"status:in-progress", true, "developer"},
		{"status:ready-for-review", true, "reviewer"},
		{"status:review-in-progress", true, "reviewer"},
		{"status:changes-requested", true, "developer"},
		{"status:ready-for-po-review", true, "po"},
		{"status:po-review-in-progress", true, "po"},
		{"status:awaiting-final-stakeholder-approval", false, ""},
		{"status:blocked", true, "po"},
		{"status:done", false, ""},
		{"status:rejected", false, ""},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			issue := Issue{
				Number: 42,
				User:   GitHubUser{Login: "creator"},
				Labels: []GitHubLabel{{Name: c.label}},
			}
			state := computeWorkflowState(issue, nil)
			pkg, ok := decideNextAction(cfg, issue, state, nil)
			if ok != c.wantOK {
				t.Errorf("label=%s: ok=%v, want %v", c.label, ok, c.wantOK)
			}
			if ok && pkg.Role != c.wantRole {
				t.Errorf("label=%s: role=%q, want %q", c.label, pkg.Role, c.wantRole)
			}
		})
	}
}

// TestValidateTransitionAllowedRoles verifies the role-based actor check.
func TestValidateTransitionAllowedRoles(t *testing.T) {
	issue := Issue{
		Number: 1,
		User:   GitHubUser{Login: "creator"},
		Labels: []GitHubLabel{{Name: "status:in-progress"}},
	}

	t.Run("developer can move to ready-for-review", func(t *testing.T) {
		res := validateTransition(issue, nil, "developer", "status:ready-for-review")
		if !res.Allowed {
			t.Errorf("expected allowed, violations: %v", res.Violations)
		}
	})

	t.Run("po cannot move in-progress to ready-for-review", func(t *testing.T) {
		res := validateTransition(issue, nil, "po", "status:ready-for-review")
		if res.Allowed {
			t.Error("expected not allowed")
		}
	})

	t.Run("empty actor_role is rejected", func(t *testing.T) {
		res := validateTransition(issue, nil, "", "status:ready-for-review")
		if res.Allowed {
			t.Error("expected not allowed for empty role")
		}
	})

	t.Run("unknown target status is rejected", func(t *testing.T) {
		res := validateTransition(issue, nil, "developer", "status:nonexistent")
		if res.Allowed {
			t.Error("expected not allowed for unknown status")
		}
	})
}

// TestValidateTransitionStakeholderApprove verifies RequiresStakeholderApprove enforcement.
func TestValidateTransitionStakeholderApprove(t *testing.T) {
	creator := "alice"
	issue := Issue{
		Number: 1,
		User:   GitHubUser{Login: creator},
		Labels: []GitHubLabel{{Name: "status:awaiting-stakeholder-approval"}},
	}

	t.Run("stakeholder can approve when approve comment exists", func(t *testing.T) {
		comments := []IssueComment{
			{ID: 1, User: GitHubUser{Login: creator}, Body: "/approve"},
		}
		res := validateTransition(issue, comments, "stakeholder", "status:approved-for-dev")
		if !res.Allowed {
			t.Errorf("expected allowed, violations: %v", res.Violations)
		}
	})

	t.Run("stakeholder cannot approve without approve comment", func(t *testing.T) {
		res := validateTransition(issue, nil, "stakeholder", "status:approved-for-dev")
		if res.Allowed {
			t.Error("expected not allowed without /approve comment")
		}
	})
}

// TestResolveStakeholder verifies fallback logic.
func TestResolveStakeholder(t *testing.T) {
	t.Run("falls back to issue creator", func(t *testing.T) {
		issue := Issue{User: GitHubUser{Login: "creator"}}
		if got := resolveStakeholder(issue); got != "creator" {
			t.Errorf("got %q, want %q", got, "creator")
		}
	})

	t.Run("prefers stakeholder label", func(t *testing.T) {
		issue := Issue{
			User:   GitHubUser{Login: "creator"},
			Labels: []GitHubLabel{{Name: "stakeholder:alice"}},
		}
		if got := resolveStakeholder(issue); got != "alice" {
			t.Errorf("got %q, want %q", got, "alice")
		}
	})
}

// TestContainsApprove verifies the /approve detection.
func TestContainsApprove(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"/approve", true},
		{"  /approve  ", true},
		{"/approve\nextra text", true},
		{"some text\n/approve\nmore text", true},
		{"/approved", false},
		{"approve", false},
		{"", false},
	}
	for _, c := range cases {
		if got := containsApprove(c.body); got != c.want {
			t.Errorf("containsApprove(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}
