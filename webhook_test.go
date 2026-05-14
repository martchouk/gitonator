package main

import (
	"bytes"
	"context"
	"log"
	"testing"
)

func TestApproveTransitionTarget(t *testing.T) {
	cases := []struct {
		fromStatus string
		wantTo     string
		wantOK     bool
	}{
		{"status:awaiting-stakeholder-approval", "status:architect-analysis", true},
		{"status:awaiting-final-stakeholder-approval", "status:done", true},
		{"status:in-progress", "", false},
		{"status:approved-for-dev", "", false},
		{"status:done", "", false},
		{"", "", false},
	}

	for _, c := range cases {
		t.Run(c.fromStatus, func(t *testing.T) {
			got, ok := approveTransitionTarget(c.fromStatus)
			if ok != c.wantOK {
				t.Errorf("approveTransitionTarget(%q) ok=%v, want %v", c.fromStatus, ok, c.wantOK)
			}
			if got != c.wantTo {
				t.Errorf("approveTransitionTarget(%q) toStatus=%q, want %q", c.fromStatus, got, c.wantTo)
			}
		})
	}
}

func TestProcessApproveCommentNonApproveBodyReturnsFalse(t *testing.T) {
	s := &Server{logger: log.New(&bytes.Buffer{}, "", 0)}
	handled, err := s.processApproveComment(context.Background(), 1, 0, "bud-dev", "just a regular comment", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Error("expected handled=false for non-approve body, got true")
	}
}
