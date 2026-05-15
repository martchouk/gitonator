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

printf 'Initializing lean workflow labels in %s\n' "$repo"

upsert_label "status:new" "598a80" "Issue has been created and needs PO classification"
upsert_label "status:story-definition" "0075ca" "PO defines the story or requirement"
upsert_label "status:dev-planning" "0052cc" "Developer creates or revises the development plan"
upsert_label "status:plan-review" "7c3aed" "Reviewer reviews the developer plan"
upsert_label "status:ready-for-development" "0ea5e9" "Reviewed plan is accepted and development can begin"
upsert_label "status:in-development" "f97316" "Developer is implementing the approved plan"
upsert_label "status:code-review" "7c3aed" "Reviewer reviews the implementation"
upsert_label "status:po-approval" "c5a0f5" "PO approves rollout and closure"
upsert_label "status:blocked" "a530c8" "Issue is blocked and PO owns coordination"
upsert_label "status:done" "4af150" "Issue is approved and closed"
upsert_label "status:rejected" "d6275b" "Issue is rejected or closed without implementation"

upsert_label "type:feature" "a8a56e" "Feature request"
upsert_label "type:bug" "b60205" "Bug report"
upsert_label "type:change-request" "c5def5" "Change request"

upsert_label "priority:high" "e11d48" "High priority"
upsert_label "priority:medium" "f97316" "Medium priority"
upsert_label "priority:low" "84cc16" "Low priority"

upsert_label "role:po" "ec4899" "PO-owned workflow step"
upsert_label "role:developer" "6366f1" "Developer-owned workflow step"
upsert_label "role:reviewer" "8b5cf6" "Reviewer-owned workflow step"

printf 'Lean workflow labels initialized in %s\n' "$repo"
