## Code Review Report

**PR:** #13 — fix: bootstrap unlabeled issues to status:new on processIssue
**Issue:** #12
**Branch:** `fix/12-unlabeled-issue-bootstrap`
**Reviewer:** mud-rev
**Date:** 2026-05-13

### Verdict
CHANGES REQUESTED

### Summary
The fix correctly identifies and addresses the root cause: `processIssue` now bootstraps any issue arriving without a `status:*` label to `status:new` before making any dispatch decision, and `decideNextAction` has a matching defensive `case ""` fallback. Code quality and logic are sound. One blocking item: the README debug log table was not updated with the new bootstrap log line, which is a required documentation update per project standards. One important non-blocking item: the `SetIssueLabels` error path in the bootstrap block has no test.

### Documentation Check
- `README.md` — MISSING — must be added: the debug log table (line 424–426) is missing the new bootstrap line (`processIssue: issue=… no status label — bootstrapping to status:new`)
- API reference — not needed (no new endpoints or parameters)
- `CHANGELOG.md` — not needed (project does not keep one)
- `docs/architecture/` — not needed
- Inline code docs — updated (`GitHubAPI` interface has a clear doc comment; bootstrap block has an explanatory comment)
- `.env.example` — not needed

### Findings

#### MUST FIX — blocking (approval withheld until resolved)

1. `README.md:424–426` — The debug log reference table documents every `DEBUG processIssue:…` line, but is missing the new bootstrap line introduced in this PR. Add the following row to the table:

   ```
   | Issue bootstrapped to status:new | `DEBUG processIssue: issue=… no status label — bootstrapping to status:new` |
   ```

#### SHOULD FIX — non-blocking but important

1. `dispatch_test.go` — No test for the `SetIssueLabels` error path in the bootstrap block (`dispatch.go:33`). If the label API call fails, the error is correctly wrapped and returned — but this path is untested. Add a `mockGitHub` variant that returns an error from `SetIssueLabels` and verify `processIssue` propagates the error (does not return `nil, nil`).

2. `dispatch_test.go:18–20` — `mockGitHub.GetIssue` will panic with an index-out-of-bounds if called more times than `len(issues)`. This silently hides future regressions where an extra `GetIssue` call is introduced. Change to return an explicit `t.Fatalf`-style error instead:

   ```go
   func (m *mockGitHub) GetIssue(_ context.Context, _ int) (Issue, error) {
       if m.getIssueIdx >= len(m.issues) {
           return Issue{}, fmt.Errorf("mockGitHub: GetIssue called %d times but only %d issues configured",
               m.getIssueIdx+1, len(m.issues))
       }
       i := m.getIssueIdx
       m.getIssueIdx++
       return m.issues[i], nil
   }
   ```

#### NIT / SUGGESTION — optional

1. `dispatch.go:97` — The `case "":` in `decideNextAction` returns a `WorkPackage` with `CurrentStatus: ""` (since `state.StatusLabel` is still `""`). In normal operation this case is unreachable from `processIssue` (the bootstrap reloads the issue before calling `decideNextAction`). A one-line comment would clarify this is intentionally defensive:
   ```go
   case "", "status:new", ...: // "" only reachable if called directly (tests, MCP tools bypassing processIssue)
   ```

### Test Review

**Test suite result:** All 35 top-level tests pass (82 pass events including sub-tests). No failures.

**Test files reviewed:**
- `dispatch_test.go` (new file) — `TestProcessIssueLabelBootstrap`, `TestProcessIssueAlreadyLabeledSkipsBootstrap`, `mockGitHub`
- `workflow_test.go` — `TestDecideNextActionQueuesCorrectRole` extended with `{"", true, "po"}` case

**Coverage assessment:**
- Well covered:
  - Unlabeled issue → `SetIssueLabels(["status:new"])` called, PO task queued, `CurrentStatus="status:new"` in store
  - Already-labeled issue → `SetIssueLabels` not called (no spurious re-labelling)
  - `decideNextAction("")` → returns `(pkg, true)` with `role="po"`
- NOT covered (missing tests):
  - `SetIssueLabels` returns an error during bootstrap — error should be propagated, not swallowed
  - `loadIssueAndComments` fails on the post-bootstrap reload — error should be propagated
  - Unlabeled issue where `computeWorkflowState` still returns `""` after reload (e.g. stale response) — `decideNextAction` would produce a work package with empty `CurrentStatus`

**Test quality findings:**
1. SHOULD FIX — `dispatch_test.go:18` — `GetIssue` panics on over-indexing instead of returning a descriptive error (see Findings #2 above)
2. SHOULD FIX — no test for `SetIssueLabels` failure path (see Findings #1 above)

**Overall test verdict:** Adequate for the happy path, but the error paths in the bootstrap block are not covered. Acceptable to merge after documentation fix; error path tests are strongly recommended as a follow-up.

### Security Assessment
No concerns identified. The bootstrap writes only the fixed string `"status:new"` as a label; there is no user-supplied input in the label value.

### Performance Notes
The bootstrap path adds one extra round-trip to the GitHub API (a `PUT /labels` followed by a fresh `GET /issue`) only for issues that arrive with no status label — the normal entry point for new issues. This is acceptable; it is a one-time cost per issue at workflow entry.

### Positive Observations
- The architectural decision to keep `decideNextAction` as a pure function (no side effects) and place the mutation in `processIssue` is clean and correct. Pure functions are easy to test and reason about.
- The `GitHubAPI` interface is well-scoped — it exposes exactly the methods needed, nothing more. The `*GitHubClient` satisfies it without any changes to the concrete type.
- `TestProcessIssueLabelBootstrap` is a faithful reproduction of the exact failure path described in issue #12, complete with the two-issue sequence that simulates the reload. This is excellent test design.
- The `labelsToStrings` call in the bootstrap block correctly preserves any existing non-status labels on the issue, so the `SET` operation is non-destructive.

### Required Next Steps for Developer

1. Add the missing row to the `README.md` debug log table (line 424–426):
   ```
   | Issue bootstrapped to status:new | `DEBUG processIssue: issue=… no status label — bootstrapping to status:new` |
   ```
2. (Strongly recommended) Add a test for the `SetIssueLabels` error path in bootstrap.
3. (Strongly recommended) Guard `mockGitHub.GetIssue` against out-of-bounds access.

Tag @bud-dev for the required README fix — it is the only blocking item.
