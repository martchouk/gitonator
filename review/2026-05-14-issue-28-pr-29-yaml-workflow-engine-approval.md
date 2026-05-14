Author: mud-rev

## Code Review Report — Approval

**PR:** #29 — feat: YAML-driven workflow engine with lean/full workflow selection
**Issue:** #28
**Branch:** `feature/28-yaml-workflow-engine`
**Reviewer:** mud-rev
**Date:** 2026-05-14
**Re-review of commit:** `68e2079`

### Verdict
APPROVED

### Summary
All five findings from the change-request review have been addressed correctly. The fix commit (`68e2079`) adds 5 new tests (85 total, all passing), closes the `applyTransitionMetadata` coverage gap, aligns MCP tool behaviour with the webhook default, adds duplicate-ID validation, and removes the misleading empty `if handled` block. The codebase is in good shape for merge.

### Finding Resolution

| Finding | Severity | Resolution |
|---------|----------|------------|
| `applyTransitionMetadata` untested | MUST FIX | ✅ Two direct tests added: `TestApplyTransitionMetadata_Set` (exercises `$from` substitution with two keys) and `TestApplyTransitionMetadata_Clear` (exercises selective key removal). Both call `s.applyTransitionMetadata` directly and make precise assertions against the store. |
| MCP/webhook engine inconsistency | SHOULD FIX | ✅ `workflowDef()` no longer returns `nil` for an empty key when a registry is loaded — the early-return guard removed; now delegates to `WorkflowRegistry.Get(key)` which already falls back to `lean`. Doc comment updated to explain the new behaviour. |
| Duplicate status/transition ID validation | SHOULD FIX | ✅ `validateWorkflowDef` checks each ID against a set before inserting. Two tests cover both paths: `TestLoadWorkflowRegistry_ValidationDuplicateStatusID` and `TestLoadWorkflowRegistry_ValidationDuplicateTransitionID`. |
| No dispatch-level YAML engine test | SHOULD FIX | ✅ `TestProcessIssueWith_YAMLWorkflow_QueuesDevTask` added: loads the real lean workflow, calls `processIssueWith(ctx, 55, wd)`, asserts `developer` task queued with correct `CurrentStatus`. |
| Empty `if handled { }` block | SHOULD FIX | ✅ Replaced with `if _, err := s.processApproveComment(...); err != nil { return err }` — clean one-liner, misleading body gone. |
| `// indirect` on `gopkg.in/yaml.v3` | NIT | ✅ Removed by `go mod tidy`. |

### Test Suite
**All 85 tests pass.** (+5 from fix commit: 2 × `applyTransitionMetadata`, 2 × duplicate-ID validation, 1 × dispatch YAML path)

### Notes
- The unaddressed NIT (comment on unparsed YAML fields in structs) is acceptable — the struct names and the YAML file content are self-documenting enough in context.
- The `workflowDef` alignment means MCP tool calls without a `workflow` argument now use the lean YAML engine rather than the legacy engine. This is the correct behaviour per issue #28 and is consistent with the webhook handler. Existing callers on the legacy path are unaffected because `processIssue` still passes `nil` explicitly.
