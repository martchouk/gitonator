Author: mud-rev

## Plan Review Report

**Issue:** #53 — Investigate duplicate task queueing on concurrent webhook events
**Branch:** `review/52-roundtrip-test-17` (plan review — no PR yet)
**Reviewer:** mud-rev
**Date:** 2026-05-17

### Verdict

APPROVED — ready for development

### Summary

The plan correctly identifies and diagnoses the TOCTOU race in `processIssueWith`: two concurrent goroutines both complete `FindActiveTaskByIssue` before either executes `SupersedeQueuedTask`, then both proceed to `QueueTask`, producing semantically identical duplicate tasks. The proposed per-issue `sync.Map` mutex is an appropriate, minimal fix. No schema changes, no Store interface changes, and no changes outside the dispatch layer. Two implementation details in the test specification need to be addressed during coding.

### Documentation Check

- `README.md` — not needed (no new commands, env vars, or user-visible behaviour)
- API reference — not needed (no endpoint changes)
- `CHANGELOG.md` — not present in this project; not needed
- `docs/architecture/` — not needed (no new components or data flows)
- Inline code docs — plan includes a concurrency guarantee comment on `issueProcessLock` and the critical section; adequate
- `.env.example` — not needed (no new env vars)

### Findings

#### MUST FIX — blocking (approval withheld until resolved)

None.

#### SHOULD FIX — non-blocking but important

1. **`mockGitHub` is not goroutine-safe — do not reuse it for the concurrent test.**
   `mockGitHub.GetIssue` mutates `m.getIssueIdx++` without a mutex; `PostIssueComment` appends to a shared slice. Running ≥8 goroutines through the existing struct will produce a data race detectable by `go test -race`. The developer must create a dedicated, goroutine-safe stub for `TestProcessIssueConcurrent_NoDuplicateTasks` — the simplest approach is a plain struct that returns fixed values from all methods without any mutable fields (or, if count tracking is needed, guards all mutations with a `sync.Mutex`).

2. **Add a starting-gun barrier to the concurrent test.**
   Without synchronising goroutine starts, the scheduler may run goroutines mostly sequentially, significantly reducing the test's ability to trigger the race. The standard Go pattern — create a `ready := make(chan struct{})`, have each goroutine block on `<-ready` before calling `processIssueWith`, then `close(ready)` from the test — ensures all goroutines begin executing the critical section as simultaneously as possible.

#### NIT / SUGGESTION — optional

1. **Lock scope comment should mention `CompleteDispatchedTask`.**
   The plan's proposed comment and documentation say "Find→Supersede→Queue", but the lock actually covers `CompleteDispatchedTask` → `FindActiveTaskByIssue` → `SupersedeQueuedTask` → `QueueTask`. The comment should enumerate all four operations so the next reader understands the full scope.

2. **Note the `sync.Map` non-deletion in a code comment.**
   The plan acknowledges entries are never evicted. A brief comment explaining why (number of issues is bounded and deletion would add complexity for negligible benefit) will prevent a future reader from "cleaning it up" incorrectly.

### Test Plan Assessment

**Proposed test:** `TestProcessIssueConcurrent_NoDuplicateTasks`

The test design is sound:

- N (≥8) concurrent goroutines all call `processIssueWith` for the same issue number against a real in-memory SQLite store — this is the correct setup to exercise the race.
- The expected post-condition (exactly one `status='queued'` task, all others `'superseded'`) precisely matches the acceptance criterion.
- The claim "without the mutex this test reliably produces > 1 queued task" is plausible given SQLite WAL behaviour and goroutine scheduling, but is made much stronger by the starting-gun barrier (see SHOULD FIX 2).
- The use of a real `Store` (not a mock store) is correct — the race is in the interaction between in-process Go goroutines and SQLite, so mocking the store would remove the very concurrency behaviour being tested.

Gap: the plan does not specify the mock to use for `GitHubAPI`. The developer must not reuse the existing `mockGitHub` for the reasons noted in SHOULD FIX 1.

**Acceptance criteria coverage:**

| Criterion | Plan coverage |
|---|---|
| Reproduce race with deterministic test | Covered by the concurrent test |
| Fix duplicate active task creation | Covered by the mutex on the critical section |
| Preserve legitimate requeue on role/assignee/status change | Covered implicitly — second goroutine acquires lock after first, reads the freshly-queued task, and deduplicates; if state had actually changed it would supersede and requeue as intended |
| Document concurrency guarantee | Covered by planned comment on `issueProcessLock` and critical section |

All four acceptance criteria are addressed.

### Security Assessment

No concerns. The mutex change is entirely internal to the server process. No new I/O, no new inputs, no surface area changes.

### Performance Notes

The lock is held only across four fast SQLite operations (no network calls, no file I/O beyond SQLite). GitHub API calls (`loadIssueAndComments`, `computeState`) run before the lock is acquired. Per-issue granularity means unrelated issues are never blocked. No performance concerns.

### Positive Observations

- Root cause analysis is precise and well-evidenced from the actual log data.
- Choosing a per-issue mutex over a global mutex is the right tradeoff: it eliminates contention between unrelated issues while solving the same-issue race.
- Explicitly noting that `GetNextWorkPackage` already uses a SQLite transaction (and is therefore unaffected) shows good understanding of the existing code.
- Keeping the fix entirely within the dispatch layer — no Store interface changes, no migrations — is appropriately minimal.

### Required Next Steps for Developer

1. Implement the `issueMu sync.Map` field on `Server` and `issueProcessLock` helper as described.
2. Acquire the lock at the start of the store critical section in `processIssueWith` (before `CompleteDispatchedTask`).
3. Write `TestProcessIssueConcurrent_NoDuplicateTasks` using a **goroutine-safe** mock for `GitHubAPI` (not the existing `mockGitHub`) and a starting-gun channel barrier.
4. Verify the test fails without the mutex and passes with it (run without the mutex first as a sanity check).
5. Add the concurrency guarantee comment to both `issueProcessLock` and the critical section in `processIssueWith`.
