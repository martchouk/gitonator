Author: mud-rev

## Code Review Report

**PR:** #29 — feat: YAML-driven workflow engine with lean/full workflow selection
**Issue:** #28
**Branch:** `feature/28-yaml-workflow-engine`
**Reviewer:** mud-rev
**Date:** 2026-05-14
**Commit reviewed:** `7bfd423` (legacy engine removal)

### Verdict
CHANGES REQUESTED

### Summary
The legacy engine removal is structurally clean: 795 lines deleted, 84 added, all 77 remaining tests pass, and every dispatch test was correctly ported to the lean workflow's status labels. The logic is correct. However, the README was not updated to match the deletion — three sections still describe the removed legacy engine as if it were the running implementation. This is a blocking documentation gap: an operator deploying from this branch would read a "Workflow model" section that describes statuses and transitions that no longer exist in any running engine.

### Documentation Check
- `README.md` — **MISSING updates** — three sections actively contradict the now-running engine (see MUST FIX findings below)
- API reference — not needed
- `CHANGELOG.md` — not needed (project does not keep one)
- `docs/architecture/` — not needed
- Inline code docs — one stale docstring (see SHOULD FIX)
- `.env.example` — not needed

### Findings

#### MUST FIX — blocking (approval withheld until resolved)

1. `README.md:36` — Core-features bullet "recognise `/approve` comment directives for stakeholder-wait states" is now false. `processApproveComment` was removed in this commit; the YAML workflows (lean and full) have no stakeholder-wait states; and the webhook handler no longer calls any approve-processing logic. The bullet must be removed or replaced with a description of what the server actually does with issue-comment events (re-runs `processIssueWith`).

2. `README.md:179-205` — The entire **"Workflow model / Main lifecycle"** section documents the deleted legacy 10-step flow with status labels that no longer exist in any engine (`status:po-analysis`, `status:ready-for-requirements-review`, `status:awaiting-stakeholder-approval`, `status:architect-analysis`, `status:approved-for-dev`, `status:ready-for-review`, `status:po-review-in-progress`, `status:awaiting-final-stakeholder-approval`). An operator deploying from `main` would read this section and configure issue labels that the running server ignores. This section must be replaced with the lean workflow's actual lifecycle (or a pointer to the YAML files, which already serve as authoritative documentation).

3. `README.md:89` — "The stakeholder for `/approve` transitions is resolved from the issue labels or the issue creator." implies the server automatically processes `/approve` comments as transition triggers. It no longer does. The `find_stakeholder_approvals` MCP tool can still locate approval comments, but the server engine itself does not act on them. The paragraph must be rewritten to reflect the current behaviour (manual approval lookup via MCP tool only).

#### SHOULD FIX — non-blocking but important

1. `workflow.go:452-454` — The `workflowDef` docstring says "Returns nil when no registry is loaded **(legacy hard-coded engine is used instead)**." The legacy engine no longer exists. A nil return from `workflowDef` now causes a nil-pointer panic in the majority of MCP tool handlers (`get_issue_context`, `get_workflow_state`, `validate_transition`, `get_transition_matrix`, `get_issue_timeline`, `transitionIssue`) — not a graceful fallback. The docstring misleads future developers into treating nil as a safe return value. Update it to state that nil is an error condition and should not occur in a correctly started server.

2. `workflow.go:349` — `validate_transition` calls `s.loadIssueAndComments(ctx, args.IssueNumber, 100)` and immediately discards the comments (`issue, _, err := ...`). `validateTransitionFromDef` never uses comments — it only uses the issue's labels. This is a network round-trip of up to 100 comments wasted on every `validate_transition` call. Replace with `s.gh.GetIssue(ctx, args.IssueNumber)` directly.

#### NIT / SUGGESTION — optional

1. `dispatch.go:117-119` — `computeState` is now a one-line pass-through: `return computeWorkflowStateFromDef(wd, issue, comments)`. The wrapper adds no logic; consider inlining or removing it to reduce indirection.

2. `webhook_test.go` — File now contains only `package main` (an empty skeleton). Either delete it or add a brief comment explaining it is reserved for future HTTP-level tests.

### Test Review

**Test suite result:** All 77 tests pass.

**Test files reviewed (this commit):**
- `dispatch_test.go` — All server constructions now include `workflows: leanRegistry(t)` (new helper that loads the real lean YAML). Status labels updated throughout: `status:in-progress` → `status:in-development`, `status:po-analysis` → `status:story-definition`. All assertions remain correct.
- `workflow_test.go` — 258 lines of legacy engine tests deleted (corresponding code deleted). No orphaned tests remain.
- `webhook_test.go` — Legacy `processApproveComment` tests removed (corresponding function removed). File is now an empty skeleton.

**Coverage assessment:**

Well covered:
- Bootstrap, deduplication, supersede, and assignee-change paths (via updated dispatch tests using lean workflow labels)
- YAML engine dispatch path (`TestProcessIssueWith_YAMLWorkflow_QueuesDevTask` retained)
- All previously reviewed engine and registry tests retained

NOT covered (new gaps introduced by this commit):
- **No test for `processIssue` returning an error when `s.workflows` is nil** — `processIssue` calls `s.workflowDef("")` which returns nil, then `processIssueWith(ctx, n, nil)` returns an error. This path is now the only protection against a misconfigured server. A test asserting the error message is returned would lock in this behaviour.
- **No test for `handleGitHubWebhook` when `s.workflows` is nil** — `wd` would be nil, passed to `processIssueWith`, which would return an HTTP 500. Not tested.

**Overall test verdict:** Adequate for the current scope, but two new nil-registry paths introduced by this commit have no test coverage.

### Security Assessment
No concerns. The removal of legacy engine code eliminates several dead code paths. No new input-handling code introduced.

### Performance Notes
`validate_transition` now loads 100 comments unnecessarily (see SHOULD FIX #2). For high-frequency validation calls this is a measurable regression against a GitHub API rate limit.

### Positive Observations
- The removal is thorough and consistent: every legacy function, its call sites, its tests, and its data types (`TransitionRule`, `statusLabels`, `transitionRules`, `WorkflowState.Stakeholder`, `WorkflowState.RecognizedApprove`) are all gone with no loose ends.
- `processIssueWith`'s nil guard (`if wd == nil { return nil, fmt.Errorf(...) }`) is the right defensive pattern — it turns a potential panic into a clear error message.
- The `leanRegistry(t)` test helper is a clean solution that makes all dispatch tests self-describing: any reader can see they're running against the real lean workflow.
- The `TransitionValidationResult` field `MatchedRuleDescription` is retained and meaningful: it now always reflects the YAML `description` field of the matched transition, which is more informative than the legacy engine provided.

### Required Next Steps for Developer

1. **MUST FIX** — Update `README.md`: remove or rewrite the "Core features" bullet about `/approve` directives (line 36), replace the "Workflow model / Main lifecycle" section (lines 179-205) with the lean workflow's actual lifecycle, and update the stakeholder-resolution paragraph (line 89) to reflect that `/approve` is now a manual lookup tool, not an automatic engine trigger.
2. **SHOULD FIX** — Update the `workflowDef` docstring to remove the "legacy hard-coded engine is used instead" claim.
3. **SHOULD FIX** — Replace `s.loadIssueAndComments` with `s.gh.GetIssue` in the `validate_transition` handler.
