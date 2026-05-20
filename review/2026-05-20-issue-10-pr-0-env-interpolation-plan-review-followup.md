Author: rita-rev

## Code Review Report

**PR:** none linked
**Issue:** #10
**Branch:** `docs/68-process-review`
**Reviewer:** rita-rev
**Date:** 2026-05-20

### Verdict
CHANGES REQUESTED

### Summary
The plan is still narrowly scoped and preserves the existing fail-fast startup behavior, but the suggested brace-stripping implementation is too broad. As written, it would also accept malformed inputs like `$VAR}` and `${VAR`, which would hide configuration mistakes instead of failing clearly.

### Documentation Check
- `README.md` — not needed; no repo-wide setup or workflow changes
- `bridge/README.md` — planned; must document both `$VAR` and `${VAR}` plus the unset-variable failure mode
- API reference — not needed; no API shape changes
- `CHANGELOG.md` — not needed; repository does not track one
- `docs/architecture/` — not needed; no structural changes
- Inline code docs — not needed; `resolveEnv` is internal and no public types are changing
- `.env.example` — not needed; no new env vars

### Findings

#### MUST FIX — blocking
1. `bridge/bridge.go:529` — the proposed `strings.TrimSuffix(strings.TrimPrefix(v[1:], "{"), "}")` normalization is too broad. It will turn malformed inputs like `$VAR}` and `${VAR` into `VAR` instead of preserving the current clear startup error. Restrict brace handling to the exact `${VAR}` shape and add a regression test for malformed brace inputs so existing error behavior stays intact.

#### SHOULD FIX — non-blocking but important
None.

#### NIT / SUGGESTION — optional
None.

### Test Review

**Test suite result:** All tests pass (`go test ./...` in `bridge/` — 1 package, 0 failures).

**Test files reviewed:**
None — no implementation or test files were modified in this work package.

**Coverage assessment:**
- Well covered: the current repository baseline is green.
- NOT covered (missing tests): the new `${VAR}` resolution path, missing-variable behavior for brace syntax, and malformed brace inputs are not yet implemented; they must be covered when the code lands.

**Test quality findings:**
None yet. No test code changed.

**Overall test verdict:** Adequate for the current baseline, but the proposed change still needs the planned regression tests.

### Security Assessment
No concerns identified. This change only affects local config parsing and startup failure behavior.

### Performance Notes
No concerns identified.

### Positive Observations
- The plan stays narrowly scoped to one code path, one docs update, and a small regression test set.
- Existing `$VAR` behavior is explicitly preserved in the plan.

### Required Next Steps for Developer
1. Update `resolveEnv` so only exact `${VAR}` forms are normalized, while malformed brace syntax continues to fail clearly.
2. Add regression tests for `${VAR}`, bare `$VAR`, missing variables, and malformed brace forms.
3. Update `bridge/README.md` to document the supported forms and the failure mode for unset vars.
