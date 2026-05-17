Author: mud-rev

## Code Review Report

**PR:** #54 — fix: serialise per-issue store critical section to prevent duplicate tasks (#53)
**Issue:** #53
**Branch:** `fix/53-concurrent-webhook-duplicate-task`
**Reviewer:** mud-rev
**Date:** 2026-05-17

### Verdict
APPROVED

### Summary
The fix correctly identifies and addresses the TOCTOU race in `processIssueWith`: two concurrent webhook handlers could both read the same active task (in separate auto-commit transactions), both supersede it, and both enqueue a replacement. The per-issue `sync.Mutex` via `sync.Map` is the minimal and idiomatic Go solution — it serialises only the four-step store critical section per issue, leaving GitHub API calls and state computation outside the lock, so unrelated issues are never blocked. All four acceptance criteria are met. Tests pass under `go test -race ./... -count=1`.

### Documentation Check
- `README.md` — not needed (the existing Deduplication section describes the business-logic behaviour; the concurrency guarantee is documented in the source code comments as the acceptance criteria specified)
- API reference — not needed (no public API changes)
- `CHANGELOG.md` — not needed (project has no CHANGELOG file)
- `docs/architecture/` — not needed (no structural changes)
- Inline code docs — updated: `issueProcessLock` in `dispatch.go` and `issueMu` field comment in `main.go` both explain the concurrency invariant
- `.env.example` — not needed (no new env vars)

### Findings

#### MUST FIX — blocking (approval withheld until resolved)
None.

#### SHOULD FIX — non-blocking but important
1. `README.md:268-273` (Deduplication section) — The section describes the deduplication logic but does not mention that concurrent webhook events for the same issue are now serialised, so the at-most-one active task guarantee holds under concurrent load. Contributors reading this section have no signal that the property is concurrency-safe. Consider adding one sentence, e.g.: _"Concurrent webhook handlers for the same issue are serialised by a per-issue mutex, so at most one task is enqueued even when two events arrive simultaneously."_

#### NIT / SUGGESTION — optional
1. `dispatch_test.go:955` — The test asserts `queued != 1` but does not print the actual task statuses on failure, making it harder to diagnose a future regression. Consider adding a loop that prints each task's status and ID in the error message.
2. `dispatch.go:193-198` — `issueProcessLock` allocates a new `*sync.Mutex` on every call and discards it if the key already exists. This is correct behaviour for `sync.Map.LoadOrStore`, but a brief note in the comment that the discarded mutex is GC'd without issue would pre-empt a common reader concern.

### Test Review

**Test suite result:** All tests pass — `ok  github-issue-orchestrator  4.055s` (with `-race` flag, run 3 times, zero failures).

**Test files reviewed:**
- `dispatch_test.go` — covers the new `safeGitHub` goroutine-safe stub and `TestProcessIssueConcurrent_NoDuplicateTasks` concurrent race test; all pre-existing tests also pass

**Coverage assessment:**
- Well covered:
  - Primary TOCTOU race: 10 goroutines with starting-gun barrier all call `processIssueWith` for the same issue; asserts exactly 1 `queued` task remains
  - Deduplication when role and assignee are unchanged (pre-existing tests, still pass)
  - Legitimate supersede when assignee changes within same role (pre-existing test `TestProcessIssueWith_StaleTask_AssigneeChange`)
  - Legitimate supersede when role changes (pre-existing test at `dispatch_test.go:219`)
  - First-assignment mid-flight supersede (`dispatch_test.go:380`)
- NOT covered (missing tests):
  - Cross-issue non-interference: no test verifies that concurrent calls for *different* issue numbers do not block each other. Low risk given per-issue granularity of the mutex, but good-to-have.
  - Panic safety: no test that a panic inside the critical section still releases the lock (the `defer unlock()` handles this correctly, but it is untested).

**Test quality findings:**
1. NIT — `dispatch_test.go:967`: `s.logger` is set to `log.New(&bytes.Buffer{}, "", 0)` (discards output). This is fine for a concurrent test but means log lines that would expose subtle bugs are silently dropped. Consider using `log.New(os.Stderr, "", 0)` in tests to aid debugging — or at minimum capture and print on failure.
2. NIT — `dispatch_test.go:1001`: error message says "expected exactly 1 queued task … got %d (total tasks: %d)" but doesn't list the individual task statuses. Minor, as noted above.

**Overall test verdict:** Adequate — the race condition from the issue is reproduced deterministically and the fix is verified to prevent it. Pre-existing coverage of all legitimate supersede paths is intact.

### Security Assessment
No concerns identified. The change introduces no new I/O boundaries, no user-controlled input paths, and no authentication or secret handling. `sync.Map` and `sync.Mutex` are standard library types with well-understood safety properties.

### Performance Notes
The per-issue mutex is held only for the four-step store sequence (four SQLite statements against an in-process database), not for GitHub API calls. Under normal operation, each issue's critical section completes in sub-millisecond time. The `sync.Map` entries are never evicted; the memory footprint is one `*sync.Mutex` per tracked issue number, which is negligible for the bounded issue sets this server handles.

### Positive Observations
- The `safeGitHub` stub is a clean solution: it returns fixed values without mutating shared state, eliminating the data race that would occur if the existing `mockGitHub` (which appends to slices) were used in a concurrent test. The explicit comment explaining *why* a separate stub was needed is exactly the right level of documentation.
- Using `sync.Map.LoadOrStore` with `defer unlock()` is the idiomatic Go pattern for this class of problem. The discarded allocation on the non-store path is a known property of the pattern and correctly accepted here.
- GitHub API calls and state computation are intentionally left outside the lock — this is the right architectural choice and is explicitly documented in the code comment.
- The starting-gun barrier (`close(ready)`) is the standard pattern for maximising goroutine concurrency in Go race tests; using `goroutines=10` is sufficient to reliably expose the race on any multicore machine.

### Required Next Steps for Developer
None — this PR is approved as-is. The SHOULD FIX (README documentation) and NITs are optional and may be addressed in a follow-on PR if desired.
