Author: mud-rev

## Code Review Report

**PR:** #25 — fix: supersede stale queued task on assignee change within same role
**Issue:** #24
**Branch:** `fix/24-assignee-change-dedup`
**Reviewer:** mud-rev
**Date:** 2026-05-13

### Verdict
CHANGES REQUESTED

### Summary
The fix is minimal, correct, and precisely targets the dedup guard in `processIssue`. The root cause — same-role assignee changes being treated as duplicates — is accurately diagnosed and the surgical one-line predicate change is the right approach. One blocking issue: the README's deduplication documentation now describes behaviour that no longer holds and must be updated before merge.

### Documentation Check
- `README.md` — **MISSING update** — `### Deduplication` section (line 195) states "a new one is not queued" which is now incorrect; must reflect the new rule.
- API reference — not needed (no endpoint changes)
- `CHANGELOG.md` — not needed (project does not maintain one)
- `docs/architecture/` — not needed
- Inline code docs — `store.go:258` docstring is slightly stale (see SHOULD FIX); minor
- `.env.example` — not needed

### Findings

#### MUST FIX — blocking (approval withheld until resolved)

1. `README.md:195` — The deduplication section reads:
   > "If an active task for the same issue already exists (queued or dispatched), a new one is not queued."
   
   This is now inaccurate. With this fix, a new task **is** queued when an active task exists but the assignee changed within the same role. Suggested replacement:
   ```
   Tasks are deduplicated by `dedup_key = issue:<N>`. If an active task for the same
   issue already exists **with the same role and same assignee**, a new one is not queued.
   If the role changed or the assignee changed within the same role, the stale task is
   superseded and a fresh task is queued with the updated state.
   ```

#### SHOULD FIX — non-blocking but important

1. `store.go:258` — `SupersedeQueuedTask` docstring says it is "Called when the issue transitions to a new workflow state requiring a different role". It now also covers same-role assignee changes. Update to:
   ```go
   // SupersedeQueuedTask cancels any queued (not yet dispatched) task for the given issue.
   // Called when the issue transitions to a new workflow state or its assignee changes,
   // so the stale queued task does not block the incoming one.
   ```

#### NIT / SUGGESTION — optional

1. `dispatch_test.go` — Missing edge-case test for `existing.Assignee == ""` (unassigned task) with a new `pkg.Assignee = "some-agent"`. This covers the scenario where a task was queued with no assignee and a first assignment arrives mid-flight. The fix handles it correctly (empty string != non-empty string triggers supersede), but an explicit test would pin that invariant.

2. `dispatch.go:78-84` — The role-vs-assignee supersede branching in the debug log is correct but slightly verbose. If further log formats are added in future, a shared helper that encodes `old_*`/`new_*` fields might help readability — not worth changing now, just flagging for awareness.

### Test Review

**Test suite result:** All tests pass — `ok github-issue-orchestrator 0.551s`

**Test files reviewed:**
- `dispatch_test.go` — covers bootstrap, role-transition supersede, same-role deduplication, **new**: same-role/different-assignee supersede, and skip-bootstrap-when-labeled

**Coverage assessment:**
- Well covered: happy path for the new bug (different assignee → supersede + new task queued with correct role/assignee), existing dedup path (same role + same assignee → `queued=false`, `deduplicated=true`), role-change supersede path, bootstrap paths
- NOT covered (missing tests):
  - Assignee was empty string in existing task, becomes non-empty in new package (first-assignment mid-flight scenario)
  - `CompleteDispatchedTask` failure path — dispatched task survives and `FindActiveTaskByIssue` returns it; `SupersedeQueuedTask` is a no-op; new task is still queued alongside it

**Test quality findings:**
1. NIT — `dispatch_test.go:238` — `TestProcessIssueSameRoleDifferentAssigneeSupersedes` is a good name. The reproduces-issue comment at the top matches the pattern used in prior tests. No complaints.
2. SHOULD FIX — No test pins the `existing.Assignee == ""` edge case (see NIT #1 above — elevated to SHOULD FIX given it's a realistic initial-assignment scenario).

**Overall test verdict:** Adequate for the primary regression path; one SHOULD FIX edge-case gap.

### Security Assessment
No concerns identified. Change is entirely internal to the orchestrator's task queueing logic; no external inputs, auth paths, or file I/O involved.

### Performance Notes
No concerns identified. The change adds a single string comparison to an already-existing in-memory check with no DB round-trips added.

### Positive Observations
- The fix is admirably small: one logical predicate change (`&&` + assignee equality) is all that was needed.
- Debug log differentiation between role-change and assignee-change supersession is smart — operators can tell from logs exactly which condition fired without needing to cross-reference the DB.
- The new test follows the established pattern (`tempStore(t)`, seed a stale task, call `processIssue`, assert on both the return value and the DB state) — consistent with the existing test suite style.
- The bridge's `selectAgent` already used assignee-first routing, so no bridge changes were needed; the implementation report correctly identified this.

### Required Next Steps for Developer

1. **Update `README.md:195`** — Replace the deduplication description with the accurate rule that covers both same-role/different-assignee and role-change supersession (exact text suggested in MUST FIX above).
2. *(Optional but recommended)* **Update `store.go:258`** docstring for `SupersedeQueuedTask` to cover the assignee-change trigger (SHOULD FIX).
3. *(Optional)* **Add edge-case test** for `existing.Assignee == ""` / `pkg.Assignee` non-empty supersede (SHOULD FIX).

After the README is updated, this PR is ready to merge. @bud-dev please address item 1; items 2 and 3 can be done in this PR or deferred to a follow-up — your call.
