Author: mud-rev

## Code Review Report

**PR:** #41 — fix: stable workflow routing via comment footer and agent handoff protocol
**Issue:** #37
**Branch:** `fix/37-stable-workflow-routing`
**Reviewer:** mud-rev
**Date:** 2026-05-15

### Verdict

CHANGES REQUESTED

### Summary

The PR delivers a well-structured, four-layer improvement to workflow routing resilience and is functionally correct. Code quality is high, Go idioms are followed, and test coverage is thorough. However, two documentation files (`bridge/README.md` and `README.md`) are not updated to reflect the new `next_assignee_roles` and `agent_instructions` work package fields or the `agent_instructions` roster key — these are user-facing additions that operators and agents need documented to adopt the handoff protocol. One test case that was explicitly called out in the implementation plan (`TestNextRolesFrom_Terminal`) is also missing. Changes are required before approval.

### Documentation Check

- `README.md` — **MISSING**: the Task Queue section documents the work package fields but omits `next_assignee_roles` (new in this PR); `valid_transitions` and `workflow_key` were also absent pre-PR
- `bridge/README.md` — **MISSING**: (1) Work Package Format section omits `next_assignee_roles` and `agent_instructions`; (2) `agents.json` format section omits the top-level `agent_instructions` array
- API reference — not needed (no HTTP endpoint shape change)
- `docs/architecture/` — not needed
- Inline code docs — updated (new functions have doc comments)
- `CHANGELOG.md` — not applicable (project has none)
- `.env.example` — not needed (no new env vars)
- `bridge/agents.json.example` — **updated** (new `agent_instructions` array added)

### Findings

#### MUST FIX — blocking (approval withheld until resolved)

1. `bridge/README.md:157–181` — **Work Package Format section is stale.** The JSON example and field table do not include `next_assignee_roles` or `agent_instructions`. Agents read the work package file to decide what to do next; if `next_assignee_roles` is undocumented, agents won't know the field exists or how to use it for the handoff footer. Add both fields to the example JSON and the table. `agent_instructions` should be noted as bridge-injected (not returned by the server API).

2. `bridge/README.md:68–130` — **`agents.json` format section does not document `agent_instructions`.** The roster format description and field table have no entry for the top-level `agent_instructions` array. This is the primary configuration knob for enabling the footer-based handoff protocol. Operators copying from the field table will not know it exists. Add the field and a brief description.

3. `README.md:231–247` — **Task Queue work package field list omits `next_assignee_roles`.** The PR introduces this field specifically so agents know which roles can take the next step. It should be documented here alongside the other fields.

#### SHOULD FIX — non-blocking but important

1. `workflow_engine_test.go` — **Missing `TestNextRolesFrom_Terminal` test.** The implementation report plan explicitly called for "TestNextRolesFrom_Terminal: `status:done` → assert `[]`." The actual diff includes `TestNextRolesFrom_UnknownStatus` (empty slice for an unknown status) but not a test for a *known* terminal status. The distinction matters: an unknown status returns `[]` because no transitions have it as a source; `status:done` returns `[]` because it has no outbound transitions at all. Both should be tested. Add:
   ```go
   func TestNextRolesFrom_Terminal(t *testing.T) {
       wd := leanWorkflowForTest(t)
       roles := wd.NextRolesFrom("status:done")
       if len(roles) != 0 {
           t.Errorf("expected empty slice for terminal status:done, got %v", roles)
       }
   }
   ```

2. `dispatch.go:62–79` — **No log when an unknown label is rescued by the footer.** When a valid footer routes around an unrecognised label, the server logs at `DEBUG` level only (`s.debugf`). In production (non-DEBUG), an unknown label silently succeeds — there is no operator-visible signal that the label is wrong and the footer is carrying the workflow. A `WARN` or at minimum an `INFO` log such as `"WARN processIssue: issue=%d unrecognized label %q rescued via comment footer, routing to role=%s"` would preserve the observability intent of the original WARN from PR #38.

#### NIT / SUGGESTION — optional

1. `workflow_engine.go:183` — The comment reads `// nextAssigneeFooterRe matches lines of the form "[next assignee role -> <role>]"` but the regex allows optional whitespace around `->` (`\s*->\s*`). Minor: the comment could say `"[next assignee role -> <role>]"` (whitespace around `->` is flexible).

2. `dispatch_test.go:431–474` — `TestProcessIssueWith_CommentFooter_UnknownLabel_Routes` sets `Assignees: []GitHubUser{{Login: "bud-dev"}}` but doesn't assert `task.Assignee == "bud-dev"`. Low priority but would complete the invariant check for footer-routed packages.

### Test Review

**Test suite result:** All 101 tests pass in the main package; all 15 tests pass in `bridge/`. (Developer report says 100 — possibly counted just before the final commit; my run got 101.)

**Test files reviewed:**
- `dispatch_test.go` — covers the new footer routing paths, terminal state guard, `NextAssigneeRoles` population, and the updated unknown-label no-comment assertion
- `workflow_engine_test.go` — covers `parseNextAssigneeRole` (6 cases: valid, spaces, no footer, empty comments, older comment, empty role), `HasRole` (known/unknown), `NextRolesFrom` (in-development, new, unknown status)
- `bridge/bridge_test.go` — covers `buildAgentPackageJSON` (with instructions, without, no mutation), `loadRoster` parsing of `agent_instructions`

**Coverage assessment:**
- Well covered: footer-overrides-label path; unknown-label-rescued-by-footer path; invalid-footer-falls-back-to-label path; terminal-state blocks footer; `parseNextAssigneeRole` edge cases; `HasRole`; `buildAgentPackageJSON` mutation safety; roster parsing; `NextRolesFrom` for active statuses
- NOT covered: `TestNextRolesFrom_Terminal` — known terminal status (e.g. `status:done`) returning empty roles (see SHOULD FIX #1)
- NOT covered: `TestProcessIssueWith_CommentFooter_NoComments` — footer routing when issue has zero comments and an active label (minor; `parseNextAssigneeRole` handles it, but no dispatch-level integration test)

**Test quality findings:**
1. SHOULD FIX — missing `TestNextRolesFrom_Terminal` (explicitly planned in the implementation report but not implemented)
2. NIT — `TestProcessIssueWith_CommentFooter_UnknownLabel_Routes` does not assert `task.Assignee`

**Overall test verdict:** Adequate for approval once the missing terminal-status test is added. The new routing logic is well covered; gaps are minor.

### Security Assessment

No concerns identified. The footer parsing uses a strict compiled regexp with anchors (`^...$`), making injection of unintended role values via whitespace tricks impossible. Role values are validated against `wd.HasRole()` before use — an attacker-controlled footer comment cannot route to a role that doesn't exist in the workflow. The `buildAgentPackageJSON` copy-on-write pattern prevents cross-agent contamination of `AgentInstructions`.

### Performance Notes

No concerns identified. The `nextAssigneeFooterRe` regexp is compiled once at package init (package-level `var`), not per call. `NextRolesFrom` and `HasRole` are O(n) on workflow size, which is small (< 20 statuses in any real workflow).

### Positive Observations

- **Elegant priority chain**: the three-priority routing design (footer → label → decideNextActionFromDef) is cleanly expressed and easy to follow. The `routed` boolean flag avoids nesting and makes the fallback logic explicit.
- **`buildAgentPackageJSON` extraction**: pulling the injection logic into a testable helper was the right call — it satisfies the no-mutation requirement while keeping `runAgent` simple.
- **Terminal state guard using `QueuesWork`**: reusing the existing semantic field (`queues_work`) rather than adding new YAML vocabulary is good economy. Verified against the lean workflow YAML — only the two true terminal statuses (`status:done`, `status:rejected`) have `queues_work: false`, so the guard is precise.
- **`parseNextAssigneeRole` scans ALL lines in last comment**: the line-by-line scan means the footer can appear anywhere in the last comment body, not just the very last line. This is more robust for agents that append context after the footer.
- **Compiled regexp**: `nextAssigneeFooterRe` as a package-level `var` is the correct Go idiom for a regexp used in a hot path.
- **Best-effort comment removal**: removing `postUnknownStatusComment` entirely (rather than just deduplicating) was the right architectural call given the project owner's feedback about comment spam.

### Required Next Steps for Developer

1. Update `bridge/README.md` — Work Package Format section: add `next_assignee_roles` and `agent_instructions` to the JSON example and field table. Note that `agent_instructions` is injected by the bridge (not returned by the server API).
2. Update `bridge/README.md` — `agents.json` format section: add `agent_instructions` to the field table with a description (top-level array of instruction strings injected into every agent's work package).
3. Update `README.md` — Task Queue section: add `next_assignee_roles` to the work package field list.
4. Add `TestNextRolesFrom_Terminal` to `workflow_engine_test.go` (see SHOULD FIX #1 for the test body).
5. Consider adding a `WARN`-level log in the footer-rescue path for unknown labels (SHOULD FIX #2) — this restores the operator-visibility intent of the original PR #38 fix without the spam problem.
