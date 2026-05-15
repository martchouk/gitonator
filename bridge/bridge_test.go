package main

import (
	"encoding/json"
	"os"
	"strings"
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

func TestResolveEnvNilMapReturnsEmpty(t *testing.T) {
	out, err := resolveEnv(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty map, got %v", out)
	}
}

func TestResolveEnvLiteralValuePassesThrough(t *testing.T) {
	out, err := resolveEnv(map[string]string{"KEY": "literal-value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["KEY"] != "literal-value" {
		t.Errorf("expected literal-value, got %q", out["KEY"])
	}
}

func TestResolveEnvDollarVarInterpolatesFromHost(t *testing.T) {
	t.Setenv("TEST_TOKEN_XYZ", "tok-abc123")
	out, err := resolveEnv(map[string]string{"GH_TOKEN": "$TEST_TOKEN_XYZ"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["GH_TOKEN"] != "tok-abc123" {
		t.Errorf("expected tok-abc123, got %q", out["GH_TOKEN"])
	}
}

func TestResolveEnvMissingVarReturnsError(t *testing.T) {
	_, err := resolveEnv(map[string]string{"GH_TOKEN": "$DEFINITELY_NOT_SET_VAR_XYZ"})
	if err == nil {
		t.Fatal("expected error for unset var, got nil")
	}
	if !strings.Contains(err.Error(), "DEFINITELY_NOT_SET_VAR_XYZ") {
		t.Errorf("error should name the missing var, got: %v", err)
	}
}

func TestResolveRosterEnvPropagatesAgentName(t *testing.T) {
	roster := &Roster{Agents: []Agent{
		{Name: "bud-dev", Env: map[string]string{"GH_TOKEN": "$MISSING_VAR_FOR_AGENT"}},
	}}
	err := resolveRosterEnv(roster)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bud-dev") {
		t.Errorf("error should name the agent, got: %v", err)
	}
}

func TestBuildEnvAgentVarOverridesHost(t *testing.T) {
	t.Setenv("GH_TOKEN", "host-token")
	env := buildEnv(map[string]string{"GH_TOKEN": "agent-token"})
	var found string
	for _, e := range env {
		if strings.HasPrefix(e, "GH_TOKEN=") {
			found = strings.TrimPrefix(e, "GH_TOKEN=")
		}
	}
	if found != "agent-token" {
		t.Errorf("expected agent-token to override host-token, got %q", found)
	}
}

func TestBuildEnvEmptyAgentEnvReturnsHostEnv(t *testing.T) {
	t.Setenv("TEST_SENTINEL_VAR", "present")
	env := buildEnv(nil)
	var found bool
	for _, e := range env {
		if e == "TEST_SENTINEL_VAR=present" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected host env to be present when agent env is nil")
	}
}

func TestBuildAgentPackageJSON_WithInstructions(t *testing.T) {
	pkg := WorkPackage{IssueID: 42, Role: "developer"}
	instructions := []string{"Step 1: do X", "Step 2: do Y"}
	data, err := buildAgentPackageJSON(pkg, instructions)
	if err != nil {
		t.Fatalf("buildAgentPackageJSON: %v", err)
	}

	var out WorkPackage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(out.AgentInstructions) != 2 {
		t.Fatalf("expected 2 instructions, got %d: %v", len(out.AgentInstructions), out.AgentInstructions)
	}
	if out.AgentInstructions[0] != "Step 1: do X" {
		t.Errorf("instruction[0]=%q, want %q", out.AgentInstructions[0], "Step 1: do X")
	}
	if out.AgentInstructions[1] != "Step 2: do Y" {
		t.Errorf("instruction[1]=%q, want %q", out.AgentInstructions[1], "Step 2: do Y")
	}
}

func TestBuildAgentPackageJSON_NoInstructions(t *testing.T) {
	pkg := WorkPackage{IssueID: 7, Role: "reviewer"}
	data, err := buildAgentPackageJSON(pkg, nil)
	if err != nil {
		t.Fatalf("buildAgentPackageJSON: %v", err)
	}
	// agent_instructions must be absent from the JSON (omitempty).
	if strings.Contains(string(data), "agent_instructions") {
		t.Errorf("expected agent_instructions to be absent from JSON when empty, got: %s", string(data))
	}
}

func TestBuildAgentPackageJSON_DoesNotMutateOriginal(t *testing.T) {
	pkg := WorkPackage{IssueID: 99, Role: "po"}
	instructions := []string{"do something"}
	_, err := buildAgentPackageJSON(pkg, instructions)
	if err != nil {
		t.Fatalf("buildAgentPackageJSON: %v", err)
	}
	if len(pkg.AgentInstructions) != 0 {
		t.Errorf("expected original pkg.AgentInstructions to be unmodified, got %v", pkg.AgentInstructions)
	}
}

func TestLoadRosterParsesAgentInstructions(t *testing.T) {
	const cfg = `{
		"agent_instructions": ["Step A", "Step B"],
		"agents": [{"name": "bud-dev", "role": "developer", "launch_template": "echo", "worktrees": {}}]
	}`
	f, err := os.CreateTemp("", "roster-*.json")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(cfg); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	roster, err := loadRoster(f.Name())
	if err != nil {
		t.Fatalf("loadRoster: %v", err)
	}
	if len(roster.AgentInstructions) != 2 {
		t.Fatalf("expected 2 agent_instructions, got %d", len(roster.AgentInstructions))
	}
	if roster.AgentInstructions[0] != "Step A" {
		t.Errorf("instruction[0]=%q, want %q", roster.AgentInstructions[0], "Step A")
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
