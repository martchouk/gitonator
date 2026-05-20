Author: rita-rev

## Plan Review Report

**Issue:** #77 - Code review report - fix the findings
**Planned branch:** `bugfix/77-fix-dashboard-review-findings`
**Reviewer:** rita-rev
**Date:** 2026-05-20

### Verdict
APPROVED FOR DEVELOPMENT

### Summary
The developer plan addresses all six findings in the work package and is scoped appropriately for a high-priority bug fix. The planned implementation is concrete enough to proceed, with regression tests specified for the completed-run status source, active workflow exclusion, webhook logging safety, dashboard linting, and documentation updates.

### Documentation Check
- `README.md` - planned update for log timestamp wording and completed-run endpoint documentation.
- API reference - planned update in `dashboard/src/pages/DocsApi.tsx` for completed-run endpoints and `404` behavior.
- `CHANGELOG.md` - not needed; this repository does not appear to maintain a changelog contract for these workflow fixes.
- `docs/architecture/` - not needed if the implementation stays within the existing dashboard/workflow architecture.
- Inline code docs - planned updates are not mandatory, but exported structs/functions changed during implementation should keep comments accurate.
- `.env.example` - not needed unless the lint/tooling fix or logging fix introduces new environment variables.

### Findings

#### MUST FIX - blocking
None for the plan review.

#### SHOULD FIX - non-blocking but important
1. Keep the completed-run terminal status source tied to successful `transition_audit` rows and explicitly define the terminal allow-list in code or tests. The plan names `status:done` and `status:rejected`; if the workflow registry can define additional terminal states, the implementation should use the registry rather than hard-coding only those two labels.
2. When adding ESLint, keep the dependency/configuration minimal and aligned with the existing Vite/TypeScript React stack. Avoid broad formatting churn in dashboard source files unless the lint rule requires it.

#### NIT / SUGGESTION - optional
1. In the webhook logging test, use a unique sentinel string in the payload body so the assertion can prove the full body is not emitted while still allowing bounded metadata or excerpts.

### Test Review

**Test suite result:** Not run for this plan review; no implementation branch exists yet for issue #77.

**Test files reviewed:** No changed test files yet. The plan explicitly calls for updates to:
- `dashboard_handler_test.go` - completed-run list/detail regression coverage.
- `store_test.go` or equivalent store-focused coverage - terminal audit inclusion, active task exclusion, and step count behavior.
- `webhook_test.go` - DEBUG logging safety regression coverage.

**Coverage assessment:**
- Well covered by the plan: stale final status, active workflow detail exclusion, non-terminal exclusion, completed-list inclusion from audit rows, step count derivation, webhook raw-payload logging, and dashboard lint/build verification.
- NOT covered by the plan: no blocking gaps identified. The developer should ensure any workflow-terminal-state helper added during implementation is covered if it has branch logic.

**Test quality findings:**
1. No blocking test-plan issues identified.

**Overall test verdict:** Adequate for development. The planned tests target the specific regressions that made the current root suite fail and add coverage for the new safety behavior.

### Security Assessment
The plan correctly treats full webhook payload logging as a data exposure risk and requires bounded logging. No additional security concerns identified at the planning stage.

### Performance Notes
No performance concerns identified in the plan. The completed-run queries should still be reviewed during implementation for reasonable indexes and bounded result limits.

### Positive Observations
The plan keeps the fix limited to the review findings and avoids a broad workflow redesign. It also calls out the dirty planning workspace and instructs the implementation pass to start from clean, synced `main`, which is the right constraint for this repository.

### Required Next Steps for Developer
1. Implement the issue on `bugfix/77-fix-dashboard-review-findings` from a clean, up-to-date `main`.
2. Add or update the planned regression tests before relying on implementation behavior.
3. Run and report the planned verification commands: `go test ./...`, `go test ./... -cover`, `npm ci`, `npm run lint`, and `npm run build`.
4. Keep documentation changes scoped to the changed behavior and endpoint contract.
