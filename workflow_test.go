package main

import (
	"testing"
)

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
