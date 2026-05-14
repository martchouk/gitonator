Author: mud-rev

## Code Review Report

**PR:** #29 — feat: YAML-driven workflow engine with lean/full workflow selection
**Issue:** #28
**Branch:** `feature/28-yaml-workflow-engine`
**Reviewer:** mud-rev
**Date:** 2026-05-14

### Verdict
CHANGES REQUESTED

### Summary
The PR delivers a complete YAML-driven workflow engine, adding five new Go files, two workflow YAML definitions, full SQLite-backed `issue_metadata` support, and a `?workflow=lean|full` webhook parameter. The design is sound, the new code is well-structured, and the existing tests all continue to pass (80 of 80). One blocking gap remains: `applyTransitionMetadata` — the function that performs the `$from` → actual-status substitution critical to blocked-resume — is never called by any test. All other findings are non-blocking.

### Documentation Check
- `README.md` — updated (new YAML engine section, `WORKFLOWS_DIR` env var, `issue_metadata` table docs, deduplication clarification, new debug log lines)
- API reference — not needed (no new HTTP endpoints; webhook `?workflow=` parameter documented in README)
- `CHANGELOG.md` — not needed (project does not keep one)
- `docs/architecture/` — not needed (no dedicated architecture docs directory)
- Inline code docs — updated (all public functions and new types have doc comments)
- `.env.example` — not needed (project documents env vars in README, no `.env.example` file)

### Findings

#### MUST FIX — blocking (approval withheld until resolved)

1. `workflow_engine.go:194` / `workflow_engine_test.go:395` — `applyTransitionMetadata` is a new code path that is never exercised by any test. `TestBlockedRoundTrip_MetadataSetAndResolved` simulates what the function does by calling `store.SetIssueMetadata` directly (bypassing `applyTransitionMetadata`). As a result, the `$from` substitution logic (`if val == "$from" { val = fromStatus }`) and the `clear_metadata` path inside `applyTransitionMetadata` are completely untested. A bug in either branch would go undetected. Add a test that creates a `Server` with a temp store, calls `applyTransitionMetadata` with a `TransitionDef` whose `SetMetadata` contains `"$from"`, and asserts that the correct resolved value is written to the store.

#### SHOULD FIX — non-blocking but important

1. `webhook.go:161` vs `workflow.go:605` — Behavioral inconsistency between the webhook handler and MCP tools: `handleGitHubWebhook` always selects a YAML workflow (defaulting to `lean` when `?workflow=` is absent or unknown), while `workflowDef()` returns `nil` for an empty key, causing MCP tools (`validate_transition`, `transition_issue`, `get_workflow_state`, `process_issue_event`) to fall back to the legacy hard-coded engine when called without a `workflow` argument. An operator processing a webhook event (YAML engine) and then auditing the same issue's state via an MCP tool (legacy engine) will see different status-to-role mappings and potentially different validation results. At minimum, document this asymmetry; ideally align MCP tools to also default to `lean` when `workflow` is empty.

2. `workflow_registry.go:97` — `validateWorkflowDef` does not check for duplicate status IDs or duplicate transition IDs. A YAML file with two `statuses` entries sharing the same `id` would silently let the second one overwrite the first in the `statusSet` map (and the first `StatusDef` would be returned by `StatusByID` because iteration stops at the first match). Add a duplicate-ID check for both statuses and transitions.

3. `dispatch_test.go` / `webhook_test.go` — No dispatch-level test exercises `processIssueWith` with a non-nil `WorkflowDef`. All dispatch tests (`TestProcessIssueLabelBootstrap`, `TestProcessIssueRoleTransitionSupersedes`, etc.) call `processIssue` which uses `wd=nil` (legacy path). The YAML dispatch path (YAML state computation → YAML next-action decision → queue task) is tested only at the unit level, not end-to-end. Add at least one test that creates a `Server` with a loaded `WorkflowDef` and calls `processIssueWith(ctx, n, wd)`.

4. `webhook.go:257-261` — The `if handled { ... }` block in `processWebhookPayload` has an empty body (comment only). The comment inside ("Approve transitions are applied inline; processIssue queues the next task.") describes intended behavior but there is no code. Either add the code this block was intended to contain, or remove the `if handled` check and let `processIssueWith` always run. In its current state this misleads readers about what happens after a handled approval comment.

#### NIT / SUGGESTION — optional

1. `workflow_definition.go:4` — `WorkflowMeta`, `StatusDef`, and `TransitionDef` each have fields in the YAML files that are not captured by the Go struct (e.g., `entity`, `version`, `purpose`, `roles`, `queues_next_role`, `required_outputs`, `terminal_after_transition`, `close_issue`). The Go YAML parser silently ignores unknown fields. A one-line comment on the struct — "only fields used by the engine are parsed; additional YAML fields serve as workflow documentation" — would prevent future engineers from adding these fields to the struct prematurely.

2. `go.mod:6` — `gopkg.in/yaml.v3` is marked `// indirect` but it is directly imported by `workflow_registry.go`. Running `go mod tidy` would remove the `// indirect` annotation. Cosmetic only.

3. `workflow_engine.go:109` — The comment on `findMatchingTransitionDef`'s bootstrap-skip branch says "bootstrap-only transition; skip" but does not explain _why_ (nil `From` is the bootstrap sentinel). This is a subtle invariant — add a brief note: `// nil From indicates a bootstrap transition (no source state); skip in runtime matching`.

### Test Review

**Test suite result:** All 80 tests pass (0 failing).

**Test files reviewed:**
- `workflow_registry_test.go` — covers load happy path, default fallback, missing default key, missing directory, files without key, invalid YAML, validation errors (unknown to-status, dead-end, unknown guard), and status-set assertions for both lean and full workflows.
- `workflow_engine_test.go` — covers `computeWorkflowStateFromDef` (known/terminal/unknown/no-status/role mapping), `validateTransitionFromDef` (allowed, wrong role, unknown target, empty role, no matching transition, specific transitions in lean workflow, guard pass/fail, block transitions, blocked resume with and without metadata, terminal reopen), `resolveTransitionTarget` (static, metadata present/absent/nil, unrecognized dynamic), `evaluateGuard` (any_label present/absent, all_absent satisfied/violated, empty guard), `decideNextActionFromDef` (po/developer/reviewer role statuses, terminal, unknown), full workflow guarded transitions, and blocked round-trip integration test.
- `store_test.go` — covers `SetIssueMetadata`, `GetIssueMetadata` (absent key), upsert, `GetIssueMetadataMap` (multi-key, cross-issue isolation), and `ClearIssueMetadata` (selective deletion, retains other keys).
- `dispatch_test.go` — covers label bootstrap, bootstrap error propagation, role-transition supersede, same-role deduplication, assignee change supersede, empty-to-non-empty assignee supersede, already-labeled skip.
- `webhook_test.go` — minor update to `processApproveComment` signature; existing test updated.

**Coverage assessment:**

Well covered:
- All YAML validation paths (invalid YAML, unknown to/from status, guard reference, dead-end detection)
- All engine functions: `computeWorkflowStateFromDef`, `validateTransitionFromDef`, `findMatchingTransitionDef`, `resolveTransitionTarget`, `evaluateGuard`, `decideNextActionFromDef`
- `issue_metadata` store CRUD operations (set, get, upsert, map, clear)
- Both lean and full workflow loaded and structurally validated by test
- Dispatch-level deduplication, supersede, and bootstrap logic (legacy engine path)

NOT covered (missing tests):
- **`applyTransitionMetadata`** — the `$from` substitution and `clear_metadata` execution paths are not tested (MUST FIX)
- **YAML dispatch path** — `processIssueWith(ctx, n, wd)` with a real `WorkflowDef` is never called from any test; all dispatch tests use the legacy `nil` wd path (SHOULD FIX)
- **Webhook `?workflow=` parameter** — `handleGitHubWebhook` parsing of the `workflow` query parameter is not tested at the HTTP handler level
- **`workflowDef()` helper** — not tested; specifically the `s.workflows == nil` guard and the `TrimSpace`-empty-key early return are untested

**Test quality findings:**
1. MUST FIX — `workflow_engine_test.go:395` (`TestBlockedRoundTrip_MetadataSetAndResolved`) bypasses `applyTransitionMetadata` entirely, leaving the function's core logic (`$from` substitution) untested by any test in the suite. The comment "Simulate applying set_metadata" makes this explicit but does not justify the omission.
2. SHOULD FIX — `workflow_engine_test.go:155` (`TestValidateTransitionFromDef_BlockIssue`) tests block transitions only from three states (`in-development`, `code-review`, `po-approval`), but the lean YAML allows blocking from five additional states (`new`, `story-definition`, `dev-planning`, `plan-review`, `ready-for-development`). The test name is therefore misleading about what "block" coverage exists.

**Overall test verdict:** Inadequate — the `applyTransitionMetadata` code path (critical to the blocked-resume feature) has no test coverage.

### Security Assessment

No concerns identified. The `?workflow=` parameter is passed through `strings.TrimSpace` and validated by `WorkflowRegistry.Get` which falls back to the default; there is no injection risk. YAML files are loaded from a server-controlled directory (`WORKFLOWS_DIR` env var), not from user input. The new `issue_metadata` table uses parameterized queries throughout.

### Performance Notes

No concerns identified. Workflow registry is loaded once at startup (no per-request file I/O). The `issue_metadata` table uses a compound primary key `(issue_id, key)` which serves as the implicit index for point lookups.

### Positive Observations

- The pre-check strategy in `LoadWorkflowRegistry` (parse only files that declare a `workflow.key`) is an elegant way to coexist with the legacy extraction YAML without requiring it to be moved or renamed.
- The `$metadata.<key>` dynamic target mechanism for `resume_from_blocked` cleanly solves the dead-end problem raised in the issue analysis, without requiring the engine to special-case any transition ID.
- `validateWorkflowDef`'s dead-end detection at load time means the `status:blocked` dead-end described in the issue analysis is structurally impossible for any loaded YAML workflow — a broken workflow is caught before the server accepts traffic.
- Keeping the legacy engine fully intact and running both paths in parallel (nil wd → legacy, non-nil wd → YAML) is a low-risk integration strategy.
- `GitHubAPI` interface extracted in `github.go` enables the clean `mockGitHub` in `dispatch_test.go`, which in turn enables the detailed dispatch-path unit tests — a good design improvement as a side-effect of this PR.

### Required Next Steps for Developer

1. Add a direct test for `applyTransitionMetadata`: construct a `Server` with a temp store, build a `TransitionDef` with `SetMetadata: map[string]string{"blocked_from": "$from"}` and `ClearMetadata: []string{"blocked_from"}`, call `s.applyTransitionMetadata(issueID, "status:in-development", td)`, and assert that `store.GetIssueMetadata(issueID, "blocked_from")` returns `("status:in-development", true, nil)`. Then call again for the clear path and assert the key is absent.
2. (SHOULD FIX) Document or resolve the MCP tools vs webhook default-engine inconsistency (empty `workflow` key → legacy via MCP, → lean YAML via webhook).
3. (SHOULD FIX) Add duplicate-ID validation to `validateWorkflowDef` for both statuses and transitions.
4. (SHOULD FIX) Add at least one dispatch-level test for `processIssueWith` with a non-nil YAML `WorkflowDef`.
5. (SHOULD FIX) Remove or populate the empty `if handled { ... }` block in `processWebhookPayload`.
