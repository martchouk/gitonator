#!/usr/bin/env bash
set -euo pipefail

repo="${1:-${GH_REPO:-}}"
if [[ -z "$repo" ]]; then
  repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
fi

upsert_label() {
  local name="$1"
  local color="$2"
  local description="$3"

  if gh label create "$name" --repo "$repo" --color "$color" --description "$description" >/dev/null 2>&1; then
    printf 'created %s\n' "$name"
  else
    gh label edit "$name" --repo "$repo" --color "$color" --description "$description" >/dev/null
    printf 'updated %s\n' "$name"
  fi
}

printf 'Initializing full workflow labels in %s\n' "$repo"

upsert_label "status:new" "598a80" "Issue has been created and needs PO classification"
upsert_label "status:triage" "21c3d0" "PO triages the issue"
upsert_label "status:solution-design" "14b8a6" "Architect designs the solution"
upsert_label "status:ui-design" "10b981" "UI designer prepares UI or UX design"
upsert_label "status:ready-for-dev" "0052cc" "Issue is ready for development"
upsert_label "status:in-development" "f97316" "Developer is implementing the issue"
upsert_label "status:architecture-review" "0f766e" "Architect reviews implementation or design-sensitive changes"
upsert_label "status:ui-review" "059669" "UI designer reviews implemented UI"
upsert_label "status:code-review" "7c3aed" "Reviewer reviews the implementation"
upsert_label "status:testing" "f59e0b" "Tester verifies the implementation"
upsert_label "status:po-acceptance" "c5a0f5" "PO performs final acceptance"
upsert_label "status:blocked" "a530c8" "Issue is blocked and PO owns coordination"
upsert_label "status:done" "4af150" "Issue is accepted and closed"
upsert_label "status:rejected" "d6275b" "Issue is rejected or closed without implementation"

upsert_label "type:feature" "a8a56e" "Feature request"
upsert_label "type:bug" "b60205" "Bug report"
upsert_label "type:change-request" "c5def5" "Change request"

upsert_label "priority:high" "e11d48" "High priority"
upsert_label "priority:medium" "f97316" "Medium priority"
upsert_label "priority:low" "84cc16" "Low priority"
upsert_label "risk:high" "b91c1c" "High-risk issue that needs architecture handling"

upsert_label "role:po" "ec4899" "PO-owned workflow step"
upsert_label "role:architect" "14b8a6" "Architect-owned workflow step"
upsert_label "role:uidesigner" "10b981" "UI designer-owned workflow step"
upsert_label "role:developer" "6366f1" "Developer-owned workflow step"
upsert_label "role:reviewer" "8b5cf6" "Reviewer-owned workflow step"
upsert_label "role:tester" "f59e0b" "Tester-owned workflow step"

upsert_label "area:ui" "0ea5e9" "Issue affects UI"
upsert_label "area:ux" "06b6d4" "Issue affects UX"
upsert_label "needs:ui-design" "10b981" "Issue needs UI design"
upsert_label "needs:architecture" "14b8a6" "Issue needs architecture design or review"

printf 'Full workflow labels initialized in %s\n' "$repo"
