## Code Review Report

**PR:** #2 — feat: role-centric orchestrator + unified Bridge with autonomous agent spawning
**Issue:** #1
**Branch:** `feature/1-role-centric-orchestrator`
**Reviewer:** mud-rev
**Date:** 2026-05-12

---

### Verdict

**CHANGES REQUESTED**

All three modules build cleanly and all 25 tests pass. The core architectural rewrite is sound and correctly addresses the requirements from issue #1. However, three blocking issues — all relating to documentation that was left in the pre-rewrite state — must be resolved before this PR can merge. Additionally, two correctness issues in the Bridge and the store should be fixed.

---

### Summary

The role-centric refactor is well-executed. `decideNextAction` is now a 10-line pure function with no knowledge of agent names. The `WorkPackage` format is correct, the Bridge's assignee-priority selection is implemented cleanly, and the atomic dispatch transaction prevents double-dispatch races. The simplified task lifecycle (queued → dispatched → completed, no heartbeats) is a real improvement over the old claim/heartbeat/complete/fail protocol.

The blocking issues are entirely on the documentation side: `README.md`, `bridge/README.md`, `agent/README.md`, and `bridge/run.sh_TEMPLATE` all describe the previous architecture and must be replaced before a deployer can successfully operate this system.

---

### Documentation Check

| Document | Status |
|----------|--------|
| `README.md` (root) | **MISSING — must be updated** (documents old API, old roles, `[handoff]`/`[po-analysis]` blocks, stdio MCP, old dispatch config) |
| `bridge/README.md` | **MISSING — must be updated** (documents old `AGENT_ASSIGNEE`-based poll, heartbeat, done/fail files, per-agent bridge) |
| `agent/README.md` | **MISSING — must be updated** (documents removed commands: `handoff`, `complete`, `fail`; old orchestrator relay for comments) |
| `bridge/run.sh_TEMPLATE` | **MISSING — must be updated** (sets old env vars; runs old `bridge.go` invocation) |
| API reference | Not a separate file — covered by README and inline code docs |
| `CHANGELOG.md` | Not present in this project — not needed |
| `docs/architecture/` | Not present in this project — not needed |
| Inline code docs | Updated — exported types and key functions have adequate doc comments |
| `.env.example` | Not present in this project — not needed |

---

### Findings

#### MUST FIX — blocking (approval withheld until resolved)

**1. `README.md` (root) — entirely describes the old architecture**

`README.md:36–39`, `README.md:63–76`, `README.md:94–97`, and throughout the file:

The root README still documents:
- Hardcoded roles mapping to `thebesserwisser`, `johnvolldepp`, `bobwurst`
- `[po-analysis]` and `[handoff]` structured comment formats with examples
- Old HTTP API endpoints: `GET /api/v1/agent/tasks`, `POST /api/v1/agent/tasks/{id}/claim`, `/heartbeat`, `/complete`, `/fail`, `/api/v1/agent/comment`
- `DISPATCH_DIR`, `DISPATCH_COMMAND`, `DISPATCH_TMUX_TEMPLATE` env vars (removed)
- `STAKEHOLDER_OVERRIDE` env var (removed)
- MCP stdio JSON-RPC usage examples (`printf '...' | ./github-issue-orchestrator`)
- Old dedup key format: `issue:123|role:reviewer|action:static-review|status:ready-for-review`
- Old task file schema with `action`, `issue_url`, `context` fields

A deployer following this README would try to configure `DISPATCH_COMMAND`, look for `/api/v1/agent/tasks?assignee=...`, and pipe JSON-RPC to stdin — all of which will fail.

**Required:** Rewrite `README.md` to document the new architecture: role-based routing, `GET /api/v1/work/next?roles=...&bridge_id=...`, `/mcp/tools/call`, `/mcp/tools/list`, new env vars, new task lifecycle, agents.json roster format.

---

**2. `bridge/README.md` and `bridge/run.sh_TEMPLATE` — document the old bridge entirely**

`bridge/README.md` from line 1 to end describes:
- `AGENT_ASSIGNEE`, `HEARTBEAT_SECONDS`, `CLAIM_LIMIT`, `TASKS_DIR` (all removed)
- One-bridge-per-agent pattern (replaced by single multi-agent bridge)
- `GET /api/v1/agent/tasks`, claim, heartbeat, complete, fail endpoints (all removed)
- `.done.json`/`.fail.json` result file protocol (removed)
- curl examples for old endpoints

`bridge/run.sh_TEMPLATE:8–13` sets `AGENT`, `TMUX_TARGET`, `TASKS_DIR`, `DISPATCH_COMMAND` and runs `go run bridge.go` — none of which match the new interface.

A deployer following these files would start a bridge that immediately fails: `BRIDGE_ID` and `AGENTS_CONFIG` are missing, and the old poll endpoint is gone.

**Required:** Rewrite `bridge/README.md` to document `BRIDGE_ID`, `AGENTS_CONFIG`, `POLL_SECONDS`, `LOG_LEVEL`, the `agents.json` roster JSON format, the `{worktree}`/`{package_file}` launch template placeholders, the new work package format, and the new log output. Update `bridge/run.sh_TEMPLATE` to use the new env vars.

---

**3. `agent/README.md` — documents removed commands and the removed orchestrator relay**

`agent/README.md:69–79` lists `handoff`, `complete`, and `fail` as supported commands — all three have been removed from `agent-task.go`. The README also documents:
- `ORCH_BASE_URL` and `AGENT_SHARED_TOKEN` as required env vars for comment operations (`agent/README.md:248–254`) — in the new code, `comment` and `approve` use `GITHUB_TOKEN` directly
- `POST /api/v1/agent/comment` as the comment API — this endpoint is removed
- Old task envelope JSON format with `result_hint.done_file`/`fail_file` — replaced by the flat `WorkPackage` format

**Required:** Rewrite `agent/README.md` to document the new four-command interface (`show`, `open`, `comment`, `approve`), the `WorkPackage` JSON format, and `GITHUB_TOKEN` as the only required env var for comment/approve.

---

#### SHOULD FIX — non-blocking but important

**4. `bridge/bridge.go:175–177` — `{worktree}` and `{package_file}` not shell-quoted before `sh -c` injection**

```go
cmdLine = strings.ReplaceAll(cmdLine, "{worktree}", worktree)
cmdLine = strings.ReplaceAll(cmdLine, "{package_file}", tmpPath)
```

Both substitutions are injected into `sh -c cmdLine` as bare strings. A worktree path like `/home/john doe/agents/bud-dev/repo` will be split by the shell into two arguments, breaking the command. While `os.CreateTemp` returns safe paths, worktree paths are operator-configured and can contain spaces.

**Required:** Wrap substituted values in single quotes with embedded single-quote escaping, or provide a `shellQuote()` helper (the old bridge had one).

---

**5. `store.go:288–322` — `RecoverStaleTasksWithLog` SELECT then UPDATE is non-atomic**

The function first SELECTs bridge IDs into a slice, then runs the UPDATE as a separate statement outside any transaction. Between the SELECT and UPDATE, a webhook arriving for one of the selected tasks will call `CompleteDispatchedTask`, marking that task as `completed`. The subsequent UPDATE WHERE `status='dispatched'` will skip it — but the recovery log will claim it was "recovered" even though it was properly closed out. Over time this produces misleading logs and makes it harder to diagnose bridge failures.

**Required:** Wrap both statements in a transaction, or restructure to use a single atomic `UPDATE` and derive bridge IDs from `RowsAffected()` with a follow-up SELECT (simpler: just log `N stale tasks recovered` without bridge IDs, which the current `RecoverStaleTasks` already does correctly).

---

**6. `workflow.go:820` and `workflow.go:920` — dead code kept alive by a compiler hack**

```go
func normalizeWorkflowStatusLabel(raw string) string { ... }
// normalizeWorkflowStatusLabel is referenced here to avoid the compiler dropping it.
var _ = normalizeWorkflowStatusLabel
```

`normalizeWorkflowStatusLabel` was only called by the removed `detectCommentDrivenTransition`. Suppressing the "declared and not used" error with `var _ = ...` is misleading — it implies the function is referenced somewhere via reflection or a function table. It is not. Delete both the function (lines 820–858) and the dummy reference (line 920).

---

**7. `bridge/bridge.go:174` — `{package}` inline placeholder documented in the requirement but not implemented**

The requirement (issue #1, §2.1) lists `{package}` as a supported launch template placeholder for inline JSON. `runAgent` only replaces `{worktree}` and `{package_file}`. A user who writes:

```json
"launch_template": "claude --dangerously-skip-permissions --project-dir {worktree} --print \"{package}\""
```

will get a literal `{package}` in their command. If `{package}` is intentionally dropped, document this clearly in the updated bridge README (the `{package_file}` approach is safer anyway).

---

#### NIT / SUGGESTION — optional

**8. `webhook.go:237–241` — empty `if handled {}` block**

```go
if handled {
    // Approve transitions are applied inline; processIssue queues the next task.
}
_, err := s.processIssue(ctx, env.Issue.Number)
```

`processIssue` is called unconditionally. The empty `if` adds no behaviour and may confuse a reader into looking for the missing `else`. Move the comment to the `processIssue` call and remove the `if` block.

**9. `webhook.go:134` — `handleMCPToolsList` accepts any HTTP method**

`handleWorkNext` enforces `GET`, `handleMCPToolsCall` enforces `POST`. `handleMCPToolsList` has no method check. A `POST /mcp/tools/list` succeeds. Low risk, but inconsistent. Add a `GET`-only check.

**10. `workflow_test.go:134` — `TestValidateTransitionStakeholderApprove` tests a webhook-bypassed path**

The webhook `/approve` handling now goes through `processApproveComment` (inline, bypasses `validateTransition`). The `RequiresStakeholderApprove` check in `validateTransition` is only reachable via the `transition_issue` MCP tool. The test is still valid for that path, but a brief comment noting this would prevent future engineers from assuming the test covers the webhook flow.

---

### Test Coverage Assessment

**Well-covered:**
- All 14 status labels → correct `SuggestedRole` (table-driven, `len(cases) == len(statusLabels)` guard prevents silent mismatches when labels change)
- `decideNextAction` returns correct role and `ok=false` for all 14 states
- `validateTransition` role-based actor checks (allowed, blocked, empty role, unknown status)
- Stakeholder `/approve` enforcement in `validateTransition`
- `GetNextWorkPackage` atomicity and role filtering
- `CompleteDispatchedTask` (including no-op on empty store)
- `bridge_id` stored on dispatch (verified by direct DB query)
- Bridge `selectAgent` priority: assignee-over-role, role fallback, nil on no match
- `collectRoles` deduplication

**Missing / weak coverage:**
- `processApproveComment` — no test verifies that a non-stakeholder actor is silently ignored, or that the correct label is set for each stakeholder-wait state. This is the new primary webhook path for approval and warrants dedicated tests.
- `RecoverStaleTasksWithLog` — no test verifies that bridge IDs are returned correctly, or the race condition described in finding #5.
- `handleWorkNext` HTTP handler — no integration tests for missing `roles`/`bridge_id` params, auth rejection, role filtering.
- `handleMCPToolsCall` / `handleMCPToolsList` — no handler tests.

The missing handler and `processApproveComment` tests are not blocking for this PR but should be addressed in a follow-up.

---

### Security Assessment

- **Auth** (`authorizeAgent`): constant-time token comparison via string equality on a trimmed token. No timing-safe comparison (`hmac.Equal` is not used). For a shared token used across multiple bridges, this is low risk in practice but worth noting. The webhook HMAC verification uses `hmac.Equal` correctly.
- **SQL injection**: `GetNextWorkPackage` builds its IN clause using `?` placeholders correctly — no injection risk.
- **Shell injection** (`bridge/bridge.go:178`): `sh -c cmdLine` with unquoted `{worktree}` and `{package_file}` substitutions is a command injection risk if worktree paths contain shell metacharacters (spaces, backticks, `$`, etc.). See finding #4. The `AGENTS_CONFIG` file is operator-controlled, but defence in depth is appropriate.
- **Temp file**: `os.CreateTemp` with `0o600` default permissions — correct. Deleted after agent exits via `defer os.Remove(tmpPath)`.
- **No secrets committed**: confirmed.
- **Webhook HMAC**: unchanged from previous version, correctly uses `hmac.Equal`.

---

### Performance Notes

- `GetNextWorkPackage` uses a transaction with a single SELECT + UPDATE. Under concurrent Bridge instances (though the current design is one-at-a-time), SQLite's WAL mode handles this correctly.
- `loadIssueAndComments` is called multiple times per webhook event (once for `processApproveComment` with `limit=0`, once for `processIssue`). This is two GitHub API calls per event minimum. Acceptable for the current scale; worth noting if webhook volume grows.
- No obvious regressions introduced.

---

### Positive Observations

- `decideNextAction` is now a pure, 10-line role table — easy to read, easy to test, zero implicit dependencies.
- `WorkPackage.Assignee` correctly passes the current GitHub assignee to the Bridge for prio-1 name matching without the orchestrator needing to know any agent names.
- `selectAgent` is clean and obvious: two sequential scans, clear comments, no branching complexity.
- `GetNextWorkPackage` transaction is correct: SELECT inside the transaction prevents a race where two concurrent Bridge instances could both see the same queued task.
- `CompleteDispatchedTask` + dedup guard in `processIssue` elegantly handles the case where a webhook fires while an agent is still running — the task is closed out before a new one is queued.
- `processApproveComment` bypasses `validateTransition` for the stakeholder approval path: the comment in the code correctly explains why (GitHub API timing), and the stakeholder identity check is performed inline. Good practical decision.
- The 14-label regression test with `len(cases) == len(statusLabels)` guard is exactly the right way to prevent silent routing bugs when the label set changes.
- Backward-compat migrations (`ALTER TABLE ... ADD COLUMN`) are listed explicitly and swallow duplicate-column errors correctly — safe for deployments upgrading from the previous schema.
- Removal of `[handoff]`/`[po-analysis]` parsing (`parseTaggedBlock`, `detectCommentDrivenTransition`, `processIssueCommentDirective`) eliminates a significant class of fragile comment-parsing bugs. The codebase is meaningfully simpler.

---

### Required Next Steps for Developer

1. **Rewrite `README.md`** (root): remove all references to old agent usernames, `[handoff]`/`[po-analysis]` comment formats, stdio MCP, old dispatch config (`DISPATCH_DIR`/`DISPATCH_COMMAND`/`DISPATCH_TMUX_TEMPLATE`), and old API endpoints. Document the new architecture: `GET /api/v1/work/next`, `/mcp/tools/call`, `/mcp/tools/list`, `agents.json`, role-based routing, and new env vars.

2. **Rewrite `bridge/README.md`**: document `BRIDGE_ID`, `AGENTS_CONFIG`, the `agents.json` roster format, `{worktree}`/`{package_file}` placeholders, new work package format, log output, and the one-bridge-all-agents model.

3. **Update `bridge/run.sh_TEMPLATE`**: replace with a template using the new env vars (`BRIDGE_ID`, `AGENTS_CONFIG`, `AGENT_SHARED_TOKEN`, `ORCH_BASE_URL`, `POLL_SECONDS`, `LOG_LEVEL`).

4. **Rewrite `agent/README.md`**: document the new four-command interface, `WorkPackage` JSON format, `GITHUB_TOKEN` requirement for comment/approve, and removal of `handoff`/`complete`/`fail`.

5. **Shell-quote `{worktree}` and `{package_file}` in `runAgent`** (`bridge/bridge.go:175–177`): wrap substituted values to prevent word-splitting on paths with spaces.

6. **Delete `normalizeWorkflowStatusLabel` and its dummy reference** (`workflow.go:820` and `:920`).

7. **Fix `RecoverStaleTasksWithLog` atomicity** (`store.go:288–322`): wrap SELECT and UPDATE in a transaction.

Once items 1–6 are addressed (item 7 is strongly recommended), re-request review and I will approve.
