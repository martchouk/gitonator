package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSelectAgentRoleAndAssigneeMatch verifies that when an agent matches both role and
// assignee name it is preferred over an agent that matches only role.
func TestSelectAgentRoleAndAssigneeMatch(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer"},
		{Name: "bud-dev-2", Role: "developer"},
	}}

	pkg := &WorkPackage{Role: "developer", Assignee: "bud-dev-2"}
	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), nil)
	if agent == nil {
		t.Fatal("expected agent, got nil")
	}
	if agent.Name != "bud-dev-2" {
		t.Errorf("expected bud-dev-2 (role+assignee match), got %s", agent.Name)
	}
}

// TestSelectAgentCrossRoleAssigneeIgnored verifies that a stale assignee whose role differs
// from the work package role does not cause the wrong agent to be selected.
func TestSelectAgentCrossRoleAssigneeIgnored(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer"},
		{Name: "ada-pow", Role: "po"},
	}}

	// ada-pow is in the roster but as "po", not "developer" — must not be selected.
	pkg := &WorkPackage{Role: "developer", Assignee: "ada-pow"}
	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), nil)
	if agent == nil {
		t.Fatal("expected agent, got nil")
	}
	if agent.Name != "bud-dev" {
		t.Errorf("expected bud-dev (role match), got %s (cross-role assignee should be ignored)", agent.Name)
	}
}

func TestSelectAgentFallsBackToRole(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer"},
		{Name: "ada-pow", Role: "po"},
	}}

	// No assignee — falls back to role match.
	pkg := &WorkPackage{Role: "developer", Assignee: ""}
	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), nil)
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
	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), nil)
	if agent != nil {
		t.Errorf("expected nil, got %s", agent.Name)
	}
}

func TestSelectAgentSkipsCoolingAgent(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev-1", Role: "developer", LLMProvider: "anthropic"},
		{Name: "bud-dev-2", Role: "developer", LLMProvider: "openai"},
	}}
	cooldowns := newProviderCooldowns(5 * time.Minute)
	cooldowns.mark("anthropic", transientFailure, "quota exhausted", time.Unix(100, 0))

	pkg := &WorkPackage{Role: "developer"}
	agent := selectAgent(roster, pkg, cooldowns, newAgentSelector(), time.Unix(101, 0), nil)
	if agent == nil {
		t.Fatal("expected fallback agent, got nil")
	}
	if agent.Name != "bud-dev-2" {
		t.Errorf("expected non-cooling bud-dev-2, got %s", agent.Name)
	}
}

func TestSelectAgentFallsBackWhenAssignedAgentProviderCooling(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "ada-pow", Role: "po", LLMProvider: "anthropic"},
		{Name: "paula-po", Role: "po", LLMProvider: "openai"},
	}}
	cooldowns := newProviderCooldowns(5 * time.Minute)
	cooldowns.mark("anthropic", transientFailure, "quota exhausted", time.Unix(100, 0))

	pkg := &WorkPackage{Role: "po", Assignee: "ada-pow"}
	agent := selectAgent(roster, pkg, cooldowns, newAgentSelector(), time.Unix(101, 0), nil)
	if agent == nil {
		t.Fatal("expected fallback agent, got nil")
	}
	if agent.Name != "paula-po" {
		t.Fatalf("expected paula-po fallback, got %s", agent.Name)
	}
}

func TestSelectAgentPrefersPastWorkerWhenAvailable(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer", LLMProvider: "anthropic"},
		{Name: "elza-dev", Role: "developer", LLMProvider: "openai"},
	}}
	pkg := &WorkPackage{Role: "developer", PastWorkers: []string{"bud-dev", "elza-dev"}}

	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), nil)
	if agent == nil {
		t.Fatal("expected agent, got nil")
	}
	if agent.Name != "elza-dev" {
		t.Fatalf("expected most recent past worker elza-dev, got %s", agent.Name)
	}
}

// TestSelectAgentPrefersCrossProviderForReviewer verifies that when a past worker used
// provider A, the reviewer is selected from provider B if one is available.
func TestSelectAgentPrefersCrossProviderForReviewer(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "mud-rev-anthropic", Role: "reviewer", LLMProvider: "anthropic"},
		{Name: "mud-rev-openai", Role: "reviewer", LLMProvider: "openai"},
	}}
	// bud-dev (anthropic) was the developer; reviewer should prefer openai.
	pkg := &WorkPackage{Role: "reviewer", PastWorkers: []string{"bud-dev"}}
	// bud-dev is registered as a developer so the roster lookup resolves its provider.
	roster.Agents = append(roster.Agents, Agent{Name: "bud-dev", Role: "developer", LLMProvider: "anthropic"})

	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), nil)
	if agent == nil {
		t.Fatal("expected agent, got nil")
	}
	if agent.Name != "mud-rev-openai" {
		t.Errorf("expected cross-provider mud-rev-openai, got %s", agent.Name)
	}
}

// TestSelectAgentCrossProviderFallsBackWhenAllSameProvider verifies that when every
// available reviewer uses the same provider as the past workers, the best available
// reviewer is still returned rather than nil.
func TestSelectAgentCrossProviderFallsBackWhenAllSameProvider(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer", LLMProvider: "anthropic"},
		{Name: "mud-rev", Role: "reviewer", LLMProvider: "anthropic"},
	}}
	pkg := &WorkPackage{Role: "reviewer", PastWorkers: []string{"bud-dev"}}

	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), nil)
	if agent == nil {
		t.Fatal("expected fallback agent, got nil")
	}
	if agent.Name != "mud-rev" {
		t.Errorf("expected mud-rev as fallback, got %s", agent.Name)
	}
}

// TestSelectAgentNoPastWorkersUnchanged verifies that cross-provider logic is skipped
// entirely when there are no past workers, leaving round-robin behaviour intact.
func TestSelectAgentNoPastWorkersUnchanged(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "mud-rev-a", Role: "reviewer", LLMProvider: "anthropic"},
		{Name: "mud-rev-b", Role: "reviewer", LLMProvider: "openai"},
	}}
	pkg := &WorkPackage{Role: "reviewer"} // no PastWorkers

	selector := newAgentSelector()
	first := selectAgent(roster, pkg, nil, selector, time.Now(), nil)
	second := selectAgent(roster, pkg, nil, selector, time.Now(), nil)
	if first == nil || second == nil {
		t.Fatal("expected two selections")
	}
	if first.Name == second.Name {
		t.Errorf("expected round-robin to alternate, got %s twice", first.Name)
	}
}

func TestSelectAgentRoundRobinRolePool(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer", LLMProvider: "anthropic"},
		{Name: "elza-dev", Role: "developer", LLMProvider: "openai"},
		{Name: "mud-rev", Role: "reviewer", LLMProvider: "anthropic"},
	}}
	selector := newAgentSelector()
	pkg := &WorkPackage{Role: "developer"}

	first := selectAgent(roster, pkg, nil, selector, time.Now(), nil)
	second := selectAgent(roster, pkg, nil, selector, time.Now(), nil)
	third := selectAgent(roster, pkg, nil, selector, time.Now(), nil)
	if first == nil || second == nil || third == nil {
		t.Fatalf("expected three selections, got first=%v second=%v third=%v", first, second, third)
	}
	if first.Name != "bud-dev" || second.Name != "elza-dev" || third.Name != "bud-dev" {
		t.Fatalf("round robin sequence got %s, %s, %s", first.Name, second.Name, third.Name)
	}
}

// TestWorktreeTracker_TryAcquireAndRelease verifies the basic acquire/release cycle.
func TestWorktreeTracker_TryAcquireAndRelease(t *testing.T) {
	wt := newWorktreeTracker()
	path := t.TempDir()

	if wt.isBusy(path) {
		t.Fatal("expected path to be free initially")
	}
	ok, err := wt.tryAcquire(path, "test-bridge", "test-agent")
	if err != nil {
		t.Fatalf("tryAcquire: %v", err)
	}
	if !ok {
		t.Fatal("expected first acquire to succeed")
	}
	if !wt.isBusy(path) {
		t.Fatal("expected path to be busy after acquire")
	}
	ok2, err2 := wt.tryAcquire(path, "test-bridge", "test-agent")
	if err2 != nil {
		t.Fatalf("second tryAcquire: %v", err2)
	}
	if ok2 {
		t.Fatal("expected second acquire on same path to fail")
	}
	wt.release(path)
	if wt.isBusy(path) {
		t.Fatal("expected path to be free after release")
	}
	ok3, err3 := wt.tryAcquire(path, "test-bridge", "test-agent")
	if err3 != nil {
		t.Fatalf("re-acquire: %v", err3)
	}
	if !ok3 {
		t.Fatal("expected re-acquire after release to succeed")
	}
}

// TestSelectAgent_BusyWorktreeSkipped verifies that an agent whose worktree is already occupied
// is excluded from selection; an alternative agent with a free worktree is chosen instead.
func TestSelectAgent_BusyWorktreeSkipped(t *testing.T) {
	wt := newWorktreeTracker()
	budPath := t.TempDir()
	elzaPath := t.TempDir()

	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer", Worktrees: map[string]string{"owner/repo": budPath}},
		{Name: "elza-dev", Role: "developer", Worktrees: map[string]string{"owner/repo": elzaPath}},
	}}
	pkg := &WorkPackage{Role: "developer", Repo: "owner/repo"}

	// Mark bud-dev's worktree as busy.
	ok, err := wt.tryAcquire(budPath, "test-bridge", "bud-dev")
	if err != nil || !ok {
		t.Fatalf("tryAcquire should succeed: ok=%v err=%v", ok, err)
	}

	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), wt)
	if agent == nil {
		t.Fatal("expected agent, got nil")
	}
	if agent.Name != "elza-dev" {
		t.Errorf("expected elza-dev (free worktree), got %s", agent.Name)
	}
}

// TestSelectAgent_AllWorktreesBusyReturnsNil verifies that when every candidate agent has
// a busy worktree, nil is returned — causing the bridge to requeue the work package.
func TestSelectAgent_AllWorktreesBusyReturnsNil(t *testing.T) {
	wt := newWorktreeTracker()
	budPath := t.TempDir()
	elzaPath := t.TempDir()

	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer", Worktrees: map[string]string{"owner/repo": budPath}},
		{Name: "elza-dev", Role: "developer", Worktrees: map[string]string{"owner/repo": elzaPath}},
	}}
	pkg := &WorkPackage{Role: "developer", Repo: "owner/repo"}

	_, _ = wt.tryAcquire(budPath, "test-bridge", "bud-dev")
	_, _ = wt.tryAcquire(elzaPath, "test-bridge", "elza-dev")

	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), wt)
	if agent != nil {
		t.Errorf("expected nil when all worktrees are busy, got %s", agent.Name)
	}
}

// TestSelectAgent_AssigneeWithBusyWorktreeFallsToPool verifies that when the exact-match
// assignee's worktree is busy, selection falls through to the round-robin pool.
func TestSelectAgent_AssigneeWithBusyWorktreeFallsToPool(t *testing.T) {
	wt := newWorktreeTracker()
	budPath := t.TempDir()
	elzaPath := t.TempDir()

	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer", Worktrees: map[string]string{"owner/repo": budPath}},
		{Name: "elza-dev", Role: "developer", Worktrees: map[string]string{"owner/repo": elzaPath}},
	}}
	pkg := &WorkPackage{Role: "developer", Repo: "owner/repo", Assignee: "bud-dev"}

	_, _ = wt.tryAcquire(budPath, "test-bridge", "bud-dev")

	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), wt)
	if agent == nil {
		t.Fatal("expected fallback agent, got nil")
	}
	if agent.Name != "elza-dev" {
		t.Errorf("expected elza-dev as fallback when bud-dev worktree is busy, got %s", agent.Name)
	}
}

// TestSelectAgent_DifferentRepoDoesNotBlock verifies that a busy worktree for repo A does
// not block agent selection for repo B on the same agent.
func TestSelectAgent_DifferentRepoDoesNotBlock(t *testing.T) {
	wt := newWorktreeTracker()
	pathA := t.TempDir()
	pathB := t.TempDir()

	roster := Roster{Agents: []Agent{
		{Name: "bud-dev", Role: "developer", Worktrees: map[string]string{
			"owner/repo-a": pathA,
			"owner/repo-b": pathB,
		}},
	}}

	ok, err := wt.tryAcquire(pathA, "test-bridge", "bud-dev")
	if err != nil || !ok {
		t.Fatalf("tryAcquire for repo-a should succeed: ok=%v err=%v", ok, err)
	}

	pkg := &WorkPackage{Role: "developer", Repo: "owner/repo-b"}
	agent := selectAgent(roster, pkg, nil, newAgentSelector(), time.Now(), wt)
	if agent == nil {
		t.Fatal("expected agent for repo-b, got nil")
	}
	if agent.Name != "bud-dev" {
		t.Errorf("expected bud-dev for repo-b, got %s", agent.Name)
	}
}

func TestClassifyAgentFailureDetectsQuota(t *testing.T) {
	result := AgentResult{ExitCode: 1, ErrorText: "You're out of extra usage · resets 2:30pm (Europe/Berlin)"}
	class := classifyAgentFailure(result, nil)
	if class != transientFailure {
		t.Fatalf("class=%v, want transientFailure", class)
	}
}

func TestClassifyAgentFailureDetectsSessionLimit(t *testing.T) {
	result := AgentResult{ExitCode: 1, ErrorText: "You've hit your session limit · resets 2:30pm (Europe/Berlin)"}
	class := classifyAgentFailure(result, nil)
	if class != transientFailure {
		t.Fatalf("class=%v, want transientFailure", class)
	}
}

func TestAgentCooldownUsesConfiguredDuration(t *testing.T) {
	cooldowns := newProviderCooldowns(5 * time.Minute)
	now := time.Unix(100, 0)
	until := cooldowns.mark("anthropic", transientFailure, "quota exhausted", now)
	if !until.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("until=%s, want %s", until, now.Add(5*time.Minute))
	}
	if !cooldowns.isCooling("anthropic", now.Add(4*time.Minute)) {
		t.Fatal("expected provider to be cooling before cooldown expires")
	}
	if cooldowns.isCooling("anthropic", now.Add(5*time.Minute+time.Second)) {
		t.Fatal("expected cooldown to expire")
	}
}

func TestProviderCooldownSleepDurationUsesMatchingProviderCooldown(t *testing.T) {
	roster := Roster{Agents: []Agent{
		{Name: "ada-pow", Role: "po", LLMProvider: "anthropic"},
		{Name: "bud-dev", Role: "developer", LLMProvider: "openai"},
	}}
	cooldowns := newProviderCooldowns(5 * time.Minute)
	now := time.Unix(100, 0)
	cooldowns.mark("anthropic", transientFailure, "quota exhausted", now)

	got := cooldowns.sleepDurationFor(&WorkPackage{Role: "po"}, roster, now.Add(time.Minute), 10*time.Second)
	want := 4 * time.Minute
	if got != want {
		t.Fatalf("sleep duration=%s, want %s", got, want)
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

func TestResolveEnvBraceVarInterpolatesFromHost(t *testing.T) {
	t.Setenv("TEST_TOKEN_XYZ", "tok-abc123")
	out, err := resolveEnv(map[string]string{"GH_TOKEN": "${TEST_TOKEN_XYZ}"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["GH_TOKEN"] != "tok-abc123" {
		t.Errorf("expected tok-abc123, got %q", out["GH_TOKEN"])
	}
}

func TestResolveEnvBraceVarMissingReturnsError(t *testing.T) {
	_, err := resolveEnv(map[string]string{"GH_TOKEN": "${DEFINITELY_NOT_SET_VAR_XYZ}"})
	if err == nil {
		t.Fatal("expected error for unset var, got nil")
	}
	if !strings.Contains(err.Error(), "DEFINITELY_NOT_SET_VAR_XYZ") {
		t.Errorf("error should name the missing var, got: %v", err)
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

func TestResolveEnvMalformedBraceVarReturnsError(t *testing.T) {
	t.Setenv("TEST_TOKEN_XYZ", "tok-abc123")

	_, err := resolveEnv(map[string]string{"GH_TOKEN": "$TEST_TOKEN_XYZ}"})
	if err == nil {
		t.Fatal("expected error for malformed var, got nil")
	}
	if !strings.Contains(err.Error(), "TEST_TOKEN_XYZ}") {
		t.Errorf("error should preserve malformed var name, got: %v", err)
	}

	_, err = resolveEnv(map[string]string{"GH_TOKEN": "${TEST_TOKEN_XYZ"})
	if err == nil {
		t.Fatal("expected error for malformed var, got nil")
	}
	if !strings.Contains(err.Error(), "${TEST_TOKEN_XYZ") {
		t.Errorf("error should preserve malformed var name, got: %v", err)
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

func TestBuildEnvAgentVarOverridesMinimalEnv(t *testing.T) {
	env := buildEnv(map[string]string{"GH_TOKEN": "agent-token"})
	var found string
	for _, e := range env {
		if strings.HasPrefix(e, "GH_TOKEN=") {
			found = strings.TrimPrefix(e, "GH_TOKEN=")
		}
	}
	if found != "agent-token" {
		t.Errorf("expected agent-token in env, got %q", found)
	}
}

// TestBuildEnvMinimalEnvReturnedWhenAgentEnvNil verifies that the agent subprocess
// receives a minimal, controlled environment rather than the full parent process env.
// This prevents accidental PAT and credential leakage into agent processes.
func TestBuildEnvMinimalEnvReturnedWhenAgentEnvNil(t *testing.T) {
	t.Setenv("TEST_SENTINEL_VAR", "should-not-leak")
	env := buildEnv(nil)
	envMap := make(map[string]string)
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		envMap[k] = v
	}
	if envMap["PATH"] == "" {
		t.Error("expected PATH to be present in minimal env")
	}
	if envMap["LANG"] == "" {
		t.Error("expected LANG to be present in minimal env")
	}
	// Arbitrary host vars must not be inherited.
	if _, found := envMap["TEST_SENTINEL_VAR"]; found {
		t.Error("host TEST_SENTINEL_VAR must not leak into agent env")
	}
}

func TestResolveModelSelectionUsesRequestedProfile(t *testing.T) {
	policy := testModelPolicy()
	selection, err := resolveModelSelection(&policy, "openai", "standard")
	if err != nil {
		t.Fatalf("resolveModelSelection: %v", err)
	}
	if selection.Model != "gpt-5.3-codex" {
		t.Fatalf("model=%q, want gpt-5.3-codex", selection.Model)
	}
	if got := strings.Join(selection.Args, " "); got != "--model gpt-5.3-codex" {
		t.Fatalf("args=%q, want %q", got, "--model gpt-5.3-codex")
	}
	if selection.MatchedProfile != "standard" {
		t.Fatalf("matched profile=%q, want standard", selection.MatchedProfile)
	}
}

func TestResolveModelSelectionFallsBackWhenProviderLacksProfile(t *testing.T) {
	policy := testModelPolicy()
	selection, err := resolveModelSelection(&policy, "anthropic", "premium")
	if err != nil {
		t.Fatalf("resolveModelSelection: %v", err)
	}
	if selection.Model != "opus" {
		t.Fatalf("model=%q, want opus", selection.Model)
	}
	if selection.MatchedProfile != "advanced" {
		t.Fatalf("matched profile=%q, want advanced", selection.MatchedProfile)
	}
}

func TestResolveModelSelectionUsesDefaultProfile(t *testing.T) {
	policy := testModelPolicy()
	selection, err := resolveModelSelection(&policy, "anthropic", "")
	if err != nil {
		t.Fatalf("resolveModelSelection: %v", err)
	}
	if selection.Model != "sonnet" {
		t.Fatalf("model=%q, want sonnet", selection.Model)
	}
}

func TestResolveModelSelectionMissingProviderErrors(t *testing.T) {
	policy := testModelPolicy()
	_, err := resolveModelSelection(&policy, "unknown-provider", "standard")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown-provider") {
		t.Fatalf("error should name provider, got %v", err)
	}
}

// TestBuildAgentCommandInjectsModelArgs verifies that model args are correctly expanded
// in a legacy shell launch template via buildAgentCommand with legacy=true.
func TestBuildAgentCommandInjectsModelArgs(t *testing.T) {
	policy := testModelPolicy()
	agent := &Agent{
		Name:           "elza-dev",
		LLMProvider:    "openai",
		LaunchTemplate: "codex exec {model_args} -C {worktree} - < {package_file}",
	}
	sel, err := resolveModelSelection(&policy, "openai", "advanced")
	if err != nil {
		t.Fatalf("resolveModelSelection: %v", err)
	}
	cmd, _, err := buildAgentCommand(context.Background(), agent, "/tmp/work tree", "/tmp/pkg file", sel, true)
	if err != nil {
		t.Fatalf("buildAgentCommand: %v", err)
	}
	if len(cmd.Args) < 3 {
		t.Fatalf("expected at least 3 cmd args (sh -c <line>), got %v", cmd.Args)
	}
	line := cmd.Args[2]
	for _, want := range []string{"--model", "gpt-5.4", "-C '/tmp/work tree'", "< '/tmp/pkg file'"} {
		if !strings.Contains(line, want) {
			t.Fatalf("command missing %q: %s", want, line)
		}
	}
	if strings.Contains(line, "{model_args}") {
		t.Fatalf("command still has placeholder: %s", line)
	}
}

// TestResolveModelSelectionErrorsWhenPolicyNil verifies that resolveModelSelection returns
// an error when no policy is provided, which prevents agents with {model_args} from running.
func TestResolveModelSelectionErrorsWhenPolicyNil(t *testing.T) {
	_, err := resolveModelSelection(nil, "openai", "standard")
	if err == nil {
		t.Fatal("expected error when policy is nil, got nil")
	}
}

func TestValidateModelPolicyForRosterRequiresPolicyWhenPlaceholderUsed(t *testing.T) {
	roster := Roster{Agents: []Agent{{
		Name:           "elza-dev",
		LLMProvider:    "openai",
		LaunchTemplate: "codex exec {model_args} -C {worktree} - < {package_file}",
	}}}
	err := validateModelPolicyForRoster(roster, nil, "standard")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "MODEL_POLICY") {
		t.Fatalf("error should mention MODEL_POLICY, got %v", err)
	}
}

func TestBuildAgentPackagePrompt_WithInstructions(t *testing.T) {
	pkg := WorkPackage{IssueID: 42, Role: "developer"}
	instructions := []string{"Step 1: do X", "Step 2: do Y"}
	data, err := buildAgentPackagePrompt(pkg, instructions)
	if err != nil {
		t.Fatalf("buildAgentPackagePrompt: %v", err)
	}

	var out WorkPackage
	if err := json.Unmarshal(workPackageJSONFromPrompt(t, data), &out); err != nil {
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

func TestBuildAgentPackagePrompt_WrapsPromptAroundWorkPackage(t *testing.T) {
	pkg := WorkPackage{
		IssueID:           42,
		Role:              "developer",
		CurrentStatus:     "status:dev-planning",
		ValidTransitions:  []string{"status:plan-review", "status:blocked", "status:rejected"},
		NextAssigneeRoles: []string{"reviewer"},
	}
	data, err := buildAgentPackagePrompt(pkg, []string{"Use valid_transitions only."})
	if err != nil {
		t.Fatalf("buildAgentPackagePrompt: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		"AUTHORITATIVE WORK PACKAGE",
		"valid_transitions",
		"Use valid_transitions only.",
		"Do not use a status from issue text, comments, memory, or repository docs unless it appears in valid_transitions.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected prompt to contain %q, got:\n%s", want, body)
		}
	}
}

func TestBuildAgentPackagePrompt_NoInstructions(t *testing.T) {
	pkg := WorkPackage{IssueID: 7, Role: "reviewer"}
	data, err := buildAgentPackagePrompt(pkg, nil)
	if err != nil {
		t.Fatalf("buildAgentPackagePrompt: %v", err)
	}
	// agent_instructions must be absent from the JSON (omitempty).
	raw := workPackageJSONFromPrompt(t, data)
	if strings.Contains(string(raw), "agent_instructions") {
		t.Errorf("expected agent_instructions to be absent from JSON when empty, got: %s", string(raw))
	}
}

func TestBuildAgentPackagePrompt_DoesNotMutateOriginal(t *testing.T) {
	pkg := WorkPackage{IssueID: 99, Role: "po"}
	instructions := []string{"do something"}
	_, err := buildAgentPackagePrompt(pkg, instructions)
	if err != nil {
		t.Fatalf("buildAgentPackagePrompt: %v", err)
	}
	if len(pkg.AgentInstructions) != 0 {
		t.Errorf("expected original pkg.AgentInstructions to be unmodified, got %v", pkg.AgentInstructions)
	}
}

func TestReportWorkFailurePostsToServer(t *testing.T) {
	var method, path, auth string
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"requeued":true}`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, BridgeID: "bigmac", Token: "secret"}
	pkg := WorkPackage{ID: 6, IssueID: 57, Role: "reviewer"}
	err := reportWorkFailure(context.Background(), srv.Client(), cfg, pkg, Agent{Name: "mud-rev"}, AgentResult{
		ExitCode:  1,
		ErrorText: "You're out of extra usage",
	})
	if err != nil {
		t.Fatalf("reportWorkFailure: %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("method=%s, want POST", method)
	}
	if path != "/api/v1/work/fail" {
		t.Errorf("path=%s, want /api/v1/work/fail", path)
	}
	if auth != "Bearer secret" {
		t.Errorf("auth=%q", auth)
	}
	for _, want := range []string{`"task_id":6`, `"issue_id":57`, `"bridge_id":"bigmac"`, `"agent":"mud-rev"`, `"exit_code":1`, "extra usage"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

func TestReportWorkReleasePostsToServer(t *testing.T) {
	var method, path, auth string
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"released":true,"retry_after_seconds":30}`))
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, BridgeID: "bigmac", Token: "secret"}
	pkg := WorkPackage{ID: 7, IssueID: 58, Role: "developer"}
	err := reportWorkRelease(context.Background(), srv.Client(), cfg, pkg, "no_available_agent", "all agents cooling down", 30)
	if err != nil {
		t.Fatalf("reportWorkRelease: %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("method=%s, want POST", method)
	}
	if path != "/api/v1/work/release" {
		t.Errorf("path=%s, want /api/v1/work/release", path)
	}
	if auth != "Bearer secret" {
		t.Errorf("auth=%q", auth)
	}
	for _, want := range []string{`"task_id":7`, `"issue_id":58`, `"bridge_id":"bigmac"`, `"reason":"no_available_agent"`, `"retry_after_seconds":30`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

func workPackageJSONFromPrompt(t *testing.T, data []byte) []byte {
	t.Helper()
	const marker = "WORK PACKAGE JSON:\n"
	body := string(data)
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("prompt missing %q marker:\n%s", marker, body)
	}
	return []byte(strings.TrimSpace(body[idx+len(marker):]))
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

func testModelPolicy() ModelPolicy {
	return ModelPolicy{
		DefaultProfile: "standard",
		Fallbacks: map[string][]string{
			"basic":    {"basic"},
			"standard": {"standard", "basic"},
			"advanced": {"advanced", "standard", "basic"},
			"premium":  {"premium", "advanced", "standard", "basic"},
		},
		Providers: map[string]map[string]ModelSpec{
			"anthropic": {
				"basic":    {Model: "haiku", Args: ArgList{"--model", "haiku"}},
				"standard": {Model: "sonnet", Args: ArgList{"--model", "sonnet"}},
				"advanced": {Model: "opus", Args: ArgList{"--model", "opus"}},
			},
			"openai": {
				"basic":    {Model: "gpt-5.4-mini", Args: ArgList{"--model", "gpt-5.4-mini"}},
				"standard": {Model: "gpt-5.3-codex", Args: ArgList{"--model", "gpt-5.3-codex"}},
				"advanced": {Model: "gpt-5.4", Args: ArgList{"--model", "gpt-5.4"}},
				"premium":  {Model: "gpt-5.5", Args: ArgList{"--model", "gpt-5.5"}},
			},
		},
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
