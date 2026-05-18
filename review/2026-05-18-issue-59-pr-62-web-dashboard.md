Author: mud-rev

## Code Review Report

**PR:** #62 — feat: web dashboard — REST API, SSE stream, React frontend (#59)
**Issue:** #59
**Branch:** `feature/59-web-dashboard`
**Reviewer:** mud-rev
**Date:** 2026-05-18

### Verdict
CHANGES REQUESTED

### Summary
The implementation is substantial and well-executed: a clean Go HTTP server with SSE fan-out, seven REST endpoints, and a complete React 18 + TypeScript + Material Design 3 frontend. All 157 tests pass and the TypeScript build is clean. However, one blocking deployment bug — a hardcoded `127.0.0.1` backend URL baked into the production bundle — would make the dashboard non-functional for any user accessing singularia.de from a machine other than the server. Two non-blocking but important concerns are also noted below.

### Documentation Check
- `README.md` — updated (Dashboard section added with env var, API table, and frontend build steps)
- API reference — updated (inline in README)
- `CHANGELOG.md` — not needed (project does not maintain a CHANGELOG)
- `docs/architecture/` — not needed (ADRs already committed in PR #60)
- Inline code docs — updated (all exported types and public functions have doc comments)
- `.env.example` — not needed (no `.env.example` exists in this repo)

### Findings

#### MUST FIX — blocking (approval withheld until resolved)

1. `dashboard/src/api/client.ts:1` — `BASE_URL` is hardcoded to `'http://127.0.0.1:6666'` and baked into the production bundle at build time. When the compiled frontend is deployed to `https://singularia.de` and a user visits from their own machine, every fetch call hits their localhost rather than the server, so the entire dashboard is broken. The README acknowledges this ("This works because both run on the same machine") but that assumption is false for anyone accessing the deployed public URL. Fix: use a Vite build-time environment variable so the backend address can be overridden at build time.

   ```ts
   // dashboard/src/api/client.ts
   const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? 'http://127.0.0.1:6666';
   export { BASE_URL, get };
   ```

   Document `VITE_API_BASE_URL` in the README's frontend build section.

#### SHOULD FIX — non-blocking but important

2. `main.go:105` — `defer cancel()` was removed without a substitute. `cancel()` is now only called in the SIGINT/SIGTERM branch. In the clean-exit branch (`case err := <-errCh:` with a nil or `context.Canceled` error) the context is never cancelled, so the dashboard goroutine and recovery loop do not receive a shutdown signal before `main()` returns. While not fatal (the OS reclaims goroutines on process exit), this is a regression from standard Go context hygiene and departs from the code's prior behaviour. Restore `defer cancel()` immediately after `ctx, cancel := context.WithCancel(...)`.

3. `webhook.go:23, store.go:607` — `handleWorkFail` (`POST /api/v1/work/fail`) and `RequeueDispatchedTask` are out of scope for issue #59, which is a web dashboard feature. These changes introduce a new bridge API endpoint and modify the task-recovery model — both are meaningful production changes that warrant their own issue, PR, design context, and changelog entry. Bundling them here makes this PR harder to reason about and harder to revert independently. Extract to a dedicated PR referencing a new issue.

#### NIT / SUGGESTION — optional

4. `dashboard_handler.go:333,373` — `setCORSHeaders(w)` is called twice in `handleWorkflowGet`: once at the top of the function (line 334) and again just before the final write (line 373). The duplicate call is harmless but noisy. Remove the second call and switch to `writeJSON` for consistency with the other handlers.

5. `dashboard_handler.go:21-24` — `DashboardServer.issueCache` (the struct field that holds `issues []GitHubIssueSummary` and `fetchedAt time.Time`) is never read or written. `handleDashboardIssues` always queries the database directly. Remove the dead field or implement the cache if latency is a concern.

6. `dashboard_handler_test.go` — `handleDashboardStream` HTTP handler is not tested. The `SSEHub` fan-out is tested in isolation (good), but the handler-level concerns — `200 OK`, `Content-Type: text/event-stream`, `X-Accel-Buffering: no`, and the initial `event: connected` payload — are untested. Add a test using `httptest.NewRecorder` (or a custom `http.ResponseWriter` that implements `http.Flusher`).

7. `webhook_test.go` — `handleWorkFail` error paths have no tests: wrong HTTP method, missing `task_id`, missing `bridge_id`, and invalid JSON body. One happy-path test is not sufficient for a new production API endpoint.

### Test Review

**Test suite result:** All 157 tests pass (`go test ./...` — 0 failures)

**Test files reviewed:**
- `dashboard_handler_test.go` — 30 tests covering all seven dashboard handlers (happy paths, empty state, deduplication, CORS, method-not-allowed), `buildWorkflowGraph`, SSEHub, and new store methods
- `webhook_test.go` — one new test for `handleWorkFail` (happy-path requeue only)
- `store_test.go` — tests for `RequeueDispatchedTask`, `ListActiveTasksAllIssues`, `ListRecentTasks`, `ListRecentAudit`

**Coverage assessment:**
- Well covered: all dashboard handler happy paths, SSE hub fan-out and non-blocking behaviour, store query methods (`ListActiveTasksAllIssues`, `ListRecentTasks`, `ListRecentAudit`, `RequeueDispatchedTask`), `buildWorkflowGraph` node/edge counts and source/target correctness
- NOT covered: `handleDashboardStream` HTTP handler (status, headers, initial event body); `handleWorkFail` error paths (wrong method, missing fields, invalid JSON); `handleDashboardAudit` with data (only empty-state tested); `handleDashboardTasks` limit query parameter

**Test quality findings:**
1. SHOULD FIX — `dashboard_handler_test.go:270` — `TestHandleDashboardAudit_Empty` is the only audit test; there is no test for the populated case, so a regression in `ListRecentAudit` result marshalling would go undetected.
2. NIT — `dashboard_handler_test.go:232` — `TestHandleDashboardTasks_Empty` casts `body["tasks"]` to `[]interface{}` without a nil check; if the key is absent the test panics rather than fails cleanly.

**Overall test verdict:** Adequate for the dashboard feature core, inadequate for the out-of-scope `handleWorkFail` endpoint and for the SSE stream handler. Address before merge.

### Security Assessment

- The dashboard API is intentionally unauthenticated (v1 scope, trusted internal network per ADR-001). This is an accepted risk documented in the README.
- `Access-Control-Allow-Origin: *` on all dashboard endpoints allows any website to make cross-origin requests to the loopback address if the server is reachable. Acceptable given the trusted-network assumption, but should be revisited if the `DASHBOARD_ADDR` ever binds to a non-loopback interface.
- `req.ErrorText` in `handleWorkFail` is truncated via `truncateForLog` (max 4000 bytes) before storage — good; prevents oversized payloads from bloating the SQLite `tasks` table.
- No secrets, credentials, or tokens are committed.

### Performance Notes

No concerns. The SSE hub uses non-blocking sends (slow clients are skipped, not back-pressured), the 30-second heartbeat keeps connections alive through typical proxies, and all database queries are bounded with explicit `LIMIT` clauses.

### Positive Observations

- The `scanTaskRows` helper is a clean refactor that eliminates duplicate row-scanning boilerplate across three new query methods and the existing `ListTasksByIssue`.
- `SSEHub.Broadcast` using `select { case ch <- b: default: }` is exactly the right pattern for fan-out to unreliable consumers: zero allocations per skip, no risk of blocking the broadcast caller.
- `handleDashboardIssueOrList` routing is handled explicitly in `dashboard_server.go` rather than in the handlers themselves — clean separation.
- The Vite dev-server proxy (`/api → http://127.0.0.1:6666`) means local development needs no cross-origin configuration changes.
- React SSE hook correctly cleans up the `EventSource` on unmount and implements exponential reconnect — no memory leaks.
- Frontend TypeScript compiles clean (`tsc --noEmit` exits 0) and the production bundle builds successfully.

### Required Next Steps for Developer

1. Fix `dashboard/src/api/client.ts`: replace `const BASE_URL = 'http://127.0.0.1:6666'` with `const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? 'http://127.0.0.1:6666'`. Update the README frontend build section to document `VITE_API_BASE_URL`.
2. Restore `defer cancel()` in `main.go` immediately after `ctx, cancel := context.WithCancel(context.Background())`.
3. Either (a) remove `handleWorkFail`/`RequeueDispatchedTask`/`truncateForLog` from this PR and open a dedicated issue+PR for that feature, or (b) add tests for all `handleWorkFail` error paths (wrong method, missing `task_id`, missing `bridge_id`, invalid JSON) so the endpoint meets the same test quality standard as the rest of the codebase.

[next assignee role -> developer]
