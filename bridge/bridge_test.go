package main

import (
	"testing"
)

func TestSelectAgentPriorityAssignee(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer"},
		{Name: "ada-pow", Role: "po"},
	}}

	// Priority 1: assignee match overrides role.
	pkg := &WorkPackage{Role: "developer", Assignee: "ada-pow"}
	agent := selectAgent(roster, pkg)
	if agent == nil {
		t.Fatal("expected agent, got nil")
	}
	if agent.Name != "ada-pow" {
		t.Errorf("expected ada-pow (assignee match), got %s", agent.Name)
	}
}

func TestSelectAgentFallsBackToRole(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer"},
		{Name: "ada-pow", Role: "po"},
	}}

	// No assignee — falls back to role match.
	pkg := &WorkPackage{Role: "developer", Assignee: ""}
	agent := selectAgent(roster, pkg)
	if agent == nil {
		t.Fatal("expected agent, got nil")
	}
	if agent.Name != "bud-dev" {
		t.Errorf("expected bud-dev (role match), got %s", agent.Name)
	}
}

func TestSelectAgentReturnsNilWhenNoMatch(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer"},
	}}

	pkg := &WorkPackage{Role: "reviewer", Assignee: "nobody"}
	agent := selectAgent(roster, pkg)
	if agent != nil {
		t.Errorf("expected nil, got %s", agent.Name)
	}
}

func TestCollectRoles(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer"},
		{Name: "ada-pow", Role: "po"},
		{Name: "mud-rev", Role: "reviewer"},
		{Name: "sam-tst", Role: "developer"}, // duplicate role
	}}

	roles := collectRoles(roster)
	if len(roles) != 3 {
		t.Errorf("expected 3 unique roles, got %d: %v", len(roles), roles)
	}
	seen := map[string]bool{}
	for _, r := range roles {
		if seen[r] {
			t.Errorf("duplicate role in collectRoles: %s", r)
		}
		seen[r] = true
	}
}
