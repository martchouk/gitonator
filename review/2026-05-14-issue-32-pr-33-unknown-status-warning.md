Author: mud-rev

## Code Review Report

**PR:** #33 — fix: warn on unknown status label; enrich WorkPackage with workflow context
**Issue:** #32
**Branch:** `fix/32-unknown-status-warning`
**Reviewer:** mud-rev
**Date:** 2026-05-14

### Verdict
APPROVED

### Summary
The PR cleanly addresses issue #32 with two complementary fixes: a WARN log for unrecognized status labels that previously caused a silent no-op, and proactive `workflow_key`/`valid_transitions` enrichment in WorkPackage so agents always know which transitions are reachable from the current state. The implementation is correct, backward-compatible, and well-tested. One SHOULD FIX in the store test assertion and two NITs, none blocking.

### Documentation Check
- `README.md` — not needed (internal server behaviour, no operator-visible API change)
- API reference — not needed
- `CHANGELOG.md` — not needed (project does not maintain one)
- `docs/architecture/` — not needed
- Inline code docs — updated: `ValidTransitionsFrom` has a correct docstring; `processIssueWith` comment updated to reflect WARN semantics
- `.env.example` — not needed

### Findings

#### MUST FIX — blocking (approval withheld until resolved)
None.

#### SHOULD FIX — non-blocking but important

1. `store_test.go:373` — `ValidTransitions` is only length-checked, not content-checked. A slice of two wrong values (e.g. `["status:a", "status:b"]`) passes the assertion. Add element-level checks for `"status:code-review"` and `"status:blocked"` to make the test resilient against silent corruption of the stored values:
   ```go
   found := map[string]bool{}
   for _, tgt := range got.ValidTransitions {
       found[tgt] = true
   }
   if !found["status:code-review"] || !found["status:blocked"] {
       t.Errorf("ValidTransitions: got %v, want [status:code-review status:blocked]", got.ValidTransitions)
   }
   ```

#### NIT / SUGGESTION — optional

1. `workflow_engine_test.go:520` — `TestValidTransitionsFrom_TerminalStatusEmpty` is a misleading name. The test body asserts the opposite of "empty": it checks that `status:done` has an outgoing transition to `status:story-definition` (the PO reopen path). A name like `TestValidTransitionsFrom_DoneHasReopenTransition` accurately describes the scenario.

2. `webhook.go:186` — The debug log emits the full raw webhook payload (`payload=%s`). GitHub PR and push payloads can exceed 200 KB. In debug mode on a busy repository this produces extremely large log lines that can overwhelm log storage. Consider logging `len(payload)` bytes or a truncated excerpt instead of the full body.

### Test Review

**Test suite result:** All 84 server tests pass. All 11 bridge tests pass.

**Test files reviewed:**
- `dispatch_test.go` — adds `TestProcessIssueWith_UnknownStatusLabel_LogsWarning` and `TestProcessIssueWith_PopulatesWorkflowContext`
- `workflow_engine_test.go` — adds four `ValidTransitionsFrom` tests
- `store_test.go` — adds `TestGetNextWorkPackage_WorkflowContextRoundTrip`

**Coverage assessment:**

Well covered:
- WARN log fires on an unrecognized label and no task is queued (`TestProcessIssueWith_UnknownStatusLabel_LogsWarning`)
- `WorkflowKey` and `ValidTransitions` are populated on the queued `WorkPackage` (`TestProcessIssueWith_PopulatesWorkflowContext`)
- `ValidTransitionsFrom` excludes `$metadata.*` dynamic targets (`TestValidTransitionsFrom_BlockedExcludesDynamic`)
- `ValidTransitionsFrom` deduplicates; returns expected set for a multi-transition status (`TestValidTransitionsFrom_InDevelopment`)
- `ValidTransitionsFrom` returns empty slice for an unknown status (`TestValidTransitionsFrom_UnknownStatusEmpty`)
- Round-trip through `QueueTask` → `GetNextWorkPackage` preserves `WorkflowKey`/`ValidTransitions` (`TestGetNextWorkPackage_WorkflowContextRoundTrip`)

NOT covered:
- `GetNextWorkPackage` backward-compatibility path: no test verifies that a row inserted WITHOUT `workflow_key`/`valid_transitions` in `payload_json` (as would exist from a pre-PR-33 server) returns zero values for those fields rather than an error. This is a regression guard for live upgrades. Low risk in practice since the `json.Unmarshal` into a temporary struct returns zero values for missing fields, but a test locking in that behaviour would be good to add.

**Test quality findings:**
1. SHOULD FIX — `store_test.go:373`: `ValidTransitions` length-only assertion (see SHOULD FIX above).
2. NIT — `workflow_engine_test.go:520`: `TestValidTransitionsFrom_TerminalStatusEmpty` name contradicts test body (see NIT above).

**Overall test verdict:** Adequate. All new code paths are tested. The backward-compatibility path for old `payload_json` rows is the only gap, and it is low risk.

### Security Assessment
No concerns. The debug log added to `webhook.go` fires only when `s.debug` is true (guarded by `debugf`). The payload is already in memory at the point it is logged (after signature validation), so no new attack surface is introduced. Large-payload log flooding is a concern only at DEBUG level (see NIT #2).

### Performance Notes
The `GetNextWorkPackage` enrichment adds one `json.Unmarshal` call per dequeue. The `payload_json` column was already stored and read; parsing a second temporary struct to extract two fields is negligible overhead.

`ValidTransitionsFrom` iterates all transitions on every task queue. For the lean workflow (≈12 transitions) this is O(1) in practice.

### Positive Observations
- The WARN fires **after** bootstrap so the empty-label bootstrap path (issue has no status label at all) is correctly handled: bootstrap runs first, then the WARN check sees the freshly set `status:new` label, which IS in the workflow, so no spurious warning is emitted.
- The `payload_json` enrichment strategy is elegant: `WorkPackage` is already serialised into `payload_json` on queue, so reading it back on dequeue to extract new fields is both backward-compatible and avoids schema migrations.
- `ValidTransitionsFrom` correctly excludes `$metadata.*` dynamic targets, preventing agents from being handed transition names that do not exist as literal status labels.
- The `omitempty` JSON tags on `WorkflowKey` and `ValidTransitions` keep the wire format compact for cases where the fields are zero-valued.

### Required Next Steps for Developer
1. **SHOULD FIX** — Strengthen `TestGetNextWorkPackage_WorkflowContextRoundTrip` to assert `ValidTransitions` content (not just length).
2. **OPTIONAL** — Rename `TestValidTransitionsFrom_TerminalStatusEmpty` to reflect that it tests the reopen transition from `status:done`, not an empty result.
3. **OPTIONAL** — Consider logging `len(payload)` instead of the full payload body in the `webhook.go` debug line to avoid oversized log lines in debug mode.
