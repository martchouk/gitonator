Author: mud-rev

## Code Review Report

**PR:** #44 — fix: rescue footer routing for out-of-workflow roles on unrecognised status
**Issue:** #42
**Branch:** `fix/42-footer-routing-and-agent-protocols`
**Reviewer:** mud-rev
**Date:** 2026-05-15

### Verdict
APPROVED

### Summary
The fix is correct, minimal, and precisely targeted at the #42 failure mode. A single boolean condition change in `dispatch.go` (2 lines changed) relaxes the `wd.HasRole()` guard when the current status is unrecognised, allowing footer routing to rescue issues stuck with foreign labels regardless of whether the footer role is in the active workflow. The regression test is well-constructed and directly reproduces the exact compounded failure. All 100+ tests pass.

### Documentation Check
- `README.md` — not needed (internal routing fix, no user-facing behaviour changed)
- API reference — not needed
- `CHANGELOG.md` — not present in the project; not needed
- `docs/architecture/` — not needed
- Inline code docs — updated; the new comment block at `dispatch.go:57-60` explains the relaxation clearly
- `.env.example` — not needed

### Findings

#### MUST FIX — blocking (approval withheld until resolved)
None.

#### SHOULD FIX — non-blocking but important
1. `dispatch.go:65` — The `state.StatusLabel == ""` arm of `statusKnown` is unreachable at that point in the function. The bootstrap block (lines 43-54) either sets the label to `status:new` and reloads state, or returns an error. By line 65, `state.StatusLabel` is always non-empty. The defensive guard is harmless but conflates "not yet labelled" with "labelled with a foreign value", which could confuse future readers. Consider dropping the `state.StatusLabel == ""` term and simplifying to `statusKnown := wd.HasStatus(state.StatusLabel)` with a brief note that the empty case is already handled above.

2. **CLAUDE.md agent-protocol fixes are gitignored and not in this PR.** The PR description lists updates to six agent `CLAUDE.md` files that prevent the two root-cause agent behaviours from recurring — but these files are gitignored. If `apply-role.sh` redeploys from templates that do not include these changes, the agent-level fixes will be silently lost and the same stall can recur despite the server-side fix. Consider either (a) committing the canonical CLAUDE.md content to a non-gitignored location in the repo or (b) opening a follow-up issue to track this deployment risk.

#### NIT / SUGGESTION — optional
1. `dispatch.go:68` — The inner terminal-state guard (`sd == nil || sd.QueuesWork`) implicitly allows footer routing for all unknown statuses because `wd.StatusByID` returns `nil` for them. This is correct and intentional, but a brief inline note (`// unknown status → sd is nil, treated as work-queuing state`) would make the interaction with the outer `statusKnown` check explicit and spare future readers from having to trace through `StatusByID`.

### Test Review

**Test suite result:** All tests pass (`go test ./...` — 1 package, 0 failures, 0.754s).

**Test files reviewed:**
- `dispatch_test.go` — new function `TestProcessIssueWith_CommentFooter_UnknownLabelOutOfWorkflowRole_Routes` added at line 618; updated comment on `TestProcessIssueWith_CommentFooter_InvalidRole_FallsBackToLabel` at line 666

**Coverage assessment:**
- Well covered:
  - Unknown status + out-of-workflow footer role → task queued for that role (new test, exact #42 scenario)
  - Unknown status + in-workflow footer role → task queued (pre-existing `TestProcessIssueWith_CommentFooter_UnknownLabel_Routes`)
  - Known status + invalid footer role → falls back to label routing (pre-existing `TestProcessIssueWith_CommentFooter_InvalidRole_FallsBackToLabel`)
  - Known terminal status + valid footer → task NOT queued (pre-existing `TestProcessIssueWith_CommentFooter_TerminalState`)
- NOT covered (missing, non-blocking):
  - Unknown status + out-of-workflow role + NO footer: should fall through to the WARN log and return `queued=false`. Indirectly covered by the general `TestProcessIssueWith_UnknownStatusLabel_LogsWarning` test, but a dedicated test for the out-of-workflow-role variant of that path would close the gap.

**Test quality findings:**
1. SHOULD FIX — No dedicated test for: unknown status + out-of-workflow footer role is absent but the issue is assigned to a role with no footer comment → must fall back to WARN + no-op. Existing `TestProcessIssueWith_UnknownStatusLabel_LogsWarning` covers the general case but not the combination with a non-lean role assignee. Low risk — the code path is simple — but the test would act as a guardrail against future regressions in the WARN log branch.
2. NIT — `TestProcessIssueWith_CommentFooter_UnknownLabelOutOfWorkflowRole_Routes` does not assert on the WARN log message (that the server logged the rescue). The server-side WARN at `dispatch.go:83` is the only operator-visible signal for this rescue; asserting it would make the test more complete. (Pattern: pass a `bytes.Buffer` logger and check its contents — already done in other tests in this file.)

**Overall test verdict:** Adequate. The new test directly covers the changed code path and the critical failure scenario. The missing cases are low-risk additions that would improve confidence, not corrections to errors.

### Security Assessment
No concerns identified. The change widens the set of conditions under which footer routing is accepted, but only when the current status is already unrecognised (i.e., the issue is in an invalid/stalled state). It does not lower any authentication or authorisation barriers.

### Performance Notes
No concerns identified. The change adds one `wd.HasStatus` call (a linear scan over `wd.Statuses`) per `processIssueWith` invocation. This is negligible.

### Positive Observations
- The fix is admirably minimal: 2 lines changed in production code, with the rest of the surrounding logic untouched. This is exactly the right scope.
- Extracting `statusKnown` as a named boolean makes the compound condition at line 66 readable and clearly communicates the intent.
- The WARN log at `dispatch.go:83` ("rescued by comment footer") is excellent operator-visible signal — it makes the rescue path auditable in server logs.
- The PR description is thorough: it explains both root causes, what changed, and what was explicitly left unchanged (known-status behaviour).
- The test comment correctly cross-references roundtrip test #42, making the regression history traceable from the code.

### Required Next Steps for Developer
No blocking items. The following are recommended before the next roundtrip test:

1. (Optional) Simplify `statusKnown` on `dispatch.go:65` by removing the unreachable `state.StatusLabel == ""` arm.
2. (Optional) Add a test asserting the WARN log message fires when the rescue path is taken.
3. (Recommended) Track the gitignored CLAUDE.md agent-protocol fixes in a follow-up issue so they are not silently lost on the next `apply-role.sh` run.
