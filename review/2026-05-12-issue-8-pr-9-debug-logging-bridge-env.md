## Code Review Report

**PR:** #9 — feat: debug logging for all server and bridge lifecycle steps
**Issue:** #8
**Branch:** `feature/8-debug-logging`
**Reviewer:** mud-rev
**Date:** 2026-05-12

### Verdict
CHANGES REQUESTED

### Summary
The debug logging implementation itself is clean, well-structured, and fully meets the acceptance criteria in issue #8. However, the PR bundles two additional, substantial changes — a reworked workflow state machine (new requirements-review and architect-analysis states, four new transitions, a routing change in `processApproveComment`) and a new per-agent environment-injection feature in the bridge — that are not referenced by any GitHub issue. The workflow routing change in `processApproveComment` is untested. These must be addressed before approval.

### Documentation Check
- `README.md` — updated: new "Debug logging" section, updated status-label list and workflow flow diagram
- `bridge/README.md` — updated: `env` field documented, `agents.json.example` reference, per-agent env section
- `CHANGELOG.md` — not needed (project does not maintain one)
- `docs/architecture/` — not needed (no architecture docs directory)
- API reference — not needed (no separate API reference doc)
- Inline code docs — `resolveRosterEnv` and `resolveEnv` have one-line doc comments; adequate
- `.env.example` — not needed (project uses env var descriptions in README)

### Findings

#### MUST FIX — blocking (approval withheld until resolved)

1. **`workflow.go`, `dispatch.go`, `webhook.go` — workflow state-machine changes have no linked issue.**  
   The PR introduces three new status labels (`status:ready-for-requirements-review`, `status:requirements-review-in-progress`, `status:architect-analysis`), four new workflow transitions, changes the routing in `processApproveComment` (`status:approved-for-dev` → `status:architect-analysis`), and updates `computeWorkflowState`/`decideNextAction` for the new roles. None of this was scoped in issue #8, which asks only for debug logging. These are non-trivial behavioral changes to the core state machine that affect every issue moving through the workflow.  
   **Required fix:** Open a new GitHub issue describing the workflow expansion (or identify the existing issue that authorised it). Update the PR description to reference that issue. This is a process requirement — the review checklist requires every PR to reference a GitHub issue for every concern it addresses.

2. **`webhook.go:278` — `processApproveComment` routing change is untested.**  
   `processApproveComment` now routes `status:awaiting-stakeholder-approval` → `status:architect-analysis` instead of `status:approved-for-dev`. This function has zero unit-test coverage — no test in the codebase calls or exercises it. The new workflow tests (`TestArchitectTransitions`, `TestValidateTransitionStakeholderApprove`) call `validateTransition` directly, not `processApproveComment`. A regression in this function's routing logic would go completely undetected.  
   **Required fix:** Add a unit test (or table-driven tests) for `processApproveComment` covering: (a) routes to `status:architect-analysis` when current status is `awaiting-stakeholder-approval`, (b) routes to `status:done` when current status is `awaiting-final-stakeholder-approval`, (c) returns `false, nil` for non-approve comment bodies, (d) returns `false, nil` when actor is not the stakeholder.

#### SHOULD FIX — non-blocking but important

3. **`bridge/bridge.go:284` — `resolveEnv` silently accepts `${VAR}` as a literal string.**  
   Only bare `$VAR` is resolved; `${VAR}` (curly-brace form, common in shell contexts) is treated as a literal because `strings.HasPrefix(v, "$")` matches but `v[1:]` yields `{VAR}` which `os.LookupEnv` fails to find — actually it returns an error in that case, which is correct. But `$VAR_WITH_UNDERSCORE` would work while `${VAR_WITH_UNDERSCORE}` would fail with a confusing error message referencing `{VAR_WITH_UNDERSCORE}`. The bridge README documents the `$VAR` form but does not explicitly state that `${...}` is not supported.  
   **Recommended fix:** Either support `${VAR}` (strip `{` and `}` when present) or add an explicit note to `bridge/README.md` under the "Per-agent env variables" section that `${...}` is not supported — only `$VAR`.

4. **`bridge/agents.json.example` — absolute paths are developer-machine-specific.**  
   Both worktree entries use `/home/john/git/agents/…`. A new user copying this file will silently use a non-existent path.  
   **Recommended fix:** Replace with a generic placeholder such as `/path/to/your/worktree` so the intent is immediately clear.

#### NIT / SUGGESTION — optional

5. **`bridge_test.go` — test function name typo:** `TestBuildEnvEmptyAgentEnvReturnHostEnv` should be `…ReturnsHostEnv` (missing `s`) for consistency with other test names in the file.

6. **Implementation report understates test additions:** The report mentions only 3 new tests (the `debugf` tests) but the PR actually adds ~17 new tests across `logging_test.go`, `bridge/bridge_test.go`, and `workflow_test.go`. Minor, but worth correcting so reviewers have an accurate count.

### Test Review

**Test suite result:** All tests pass — orchestrator: `ok github-issue-orchestrator 0.483s`; bridge: `ok agent-bridge 0.372s`.

**Test files reviewed:**
- `logging_test.go` (new) — covers `debugf`: output when enabled, silence when disabled, format-arg substitution
- `bridge/bridge_test.go` (modified) — adds 7 new tests: `resolveEnv` (nil map, literal passthrough, dollar-var interpolation, missing-var error), `resolveRosterEnv` (agent name propagation), `buildEnv` (agent override wins, nil agent env passes host env through)
- `workflow_test.go` (modified) — adds 11 new sub-tests: `TestRequirementsReviewTransitions` (7 cases covering the PO→reviewer path), `TestArchitectTransitions` (4 cases covering stakeholder→architect→developer path); updates `TestValidateTransitionStakeholderApprove` and `TestDecideNextActionQueuesCorrectRole` for the new states

**Coverage assessment:**
- Well covered: `debugf` on/off/format; `resolveEnv` happy path, error, nil; `buildEnv` override and passthrough; all new workflow transition validation paths; `decideNextAction` for all new statuses
- NOT covered:
  - `processApproveComment` function (see MUST FIX #2)
  - Bridge `resolveEnv` with `${VAR}` form (see SHOULD FIX #3)
  - Bridge env injection integration: no test verifies that `buildEnv` output actually reaches the spawned subprocess via `cmd.Env`
  - `runAgent` with a non-nil `agent.Env` (the `cmd.Env = buildEnv(agent.Env)` line at `bridge/bridge.go:204` is uncovered)

**Test quality findings:**
1. MUST FIX — `processApproveComment` has no test; the routing change at `webhook.go:278` is a behavioural regression risk with zero coverage.
2. SHOULD FIX — `bridge/bridge.go:204` (`cmd.Env = buildEnv(agent.Env)`) is not exercised by any test; the env injection feature is tested at the unit level but not at the point of actual subprocess launch.

**Overall test verdict:** Partially adequate — logging and workflow validation tests are thorough; the `processApproveComment` gap is a meaningful regression risk given the routing change.

### Security Assessment

The per-agent env injection feature (`resolveEnv`, `buildEnv`) handles secrets (GitHub tokens) and deserves scrutiny:

- **Values are never logged** — the bridge only logs env *key names* (not values) at debug level. Good.
- **Fail-fast on missing vars** — `resolveRosterEnv` exits the bridge at startup if a referenced var is unset, preventing agents from running with empty tokens. Good.
- **`buildEnv` inheritance** — agent subprocesses inherit the full bridge environment plus the per-agent overrides. This means any secrets in the bridge process's environment (e.g., `AGENT_SHARED_TOKEN`) are also passed to agent subprocesses. This is the existing behaviour for all subprocesses and is unchanged; no new risk introduced.
- **`agents.json` gitignored** — the root `.gitignore` now ignores `agents.json`, reducing accidental token commits. Good.

No additional security concerns identified.

### Performance Notes

No concerns identified. Debug lines are gated by `s.debug`/`debug` boolean — zero allocation path when disabled.

### Positive Observations

- The `debugf` helper design is clean: single method, single check, single prefix — no scattered `if debug { ... }` blocks in callers.
- `resolveRosterEnv` errors name the offending agent and variable — excellent error ergonomics for operators debugging startup failures.
- `buildEnv` correctly merges host environment so agents inherit all necessary PATH, locale, and other ambient vars.
- `agents.json.example` + gitignore entry is good operational hygiene — secrets are much harder to accidentally commit.
- The new workflow transition tests are well-named and cover both the allowed and forbidden directions of each transition.
- Always-on INFO lines for `task queued` and `work claimed` are a sound choice — operational events that belong in production logs regardless of debug level.

### Required Next Steps for Developer

1. Open a new GitHub issue (or identify the existing one) covering the workflow state machine expansion (requirements-review, architect-analysis, and the four new transitions). Update the PR description to reference it.
2. Add unit tests for `processApproveComment` covering the four scenarios described in MUST FIX #2.
3. _(Recommended)_ Update `bridge/agents.json.example` to replace `/home/john/git/agents/…` paths with generic placeholders.
4. _(Recommended)_ Add an explicit note to `bridge/README.md` that `${VAR}` syntax is not supported — only bare `$VAR`.
