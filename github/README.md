# GitHub-side setup

This document explains what must be configured **on the GitHub website** to start using the `github.mcp` orchestrator with a repository.

It covers:

- repository permissions
- labels
- webhook setup
- role users
- first test issue
- verification steps

This is the GitHub-side onboarding guide to connect a repository to your central orchestrator server.

---

## Goal

You want a GitHub repository to participate in the orchestrated workflow handled by your server, for example:

- orchestrator server: `https://mcp.singularia.de`
- webhook endpoint: `https://mcp.singularia.de/webhook/github`

After setup, GitHub should send issue and comment events to the orchestrator, and the orchestrator should be able to drive your issue workflow using:

- labels
- assignments
- structured comments
- approvals

---

## Prerequisites

Before configuring GitHub, make sure the orchestrator server is already running and reachable.

At minimum, on the server side you should already have:

- the HTTP server running
- HTTPS handled by nginx
- the webhook endpoint available
- a valid GitHub token configured in the orchestrator
- the same webhook secret available on the server

You should be able to verify:

```bash
curl -sS https://mcp.singularia.de/healthz | jq
```

Expected response:

```json
{
  "ok": true,
  "service": "github-issue-orchestrator"
}
```

---

## 1. Grant repository access to the workflow users

Your workflow depends on these GitHub users being assignable and able to work with issues:

- **PO**: `thebesserwisser`
- **Developer**: `johnvolldepp`
- **Reviewer**: `bobwurst`

These users should have appropriate repository access.

### Recommended minimum

For a private repo, give them at least:

- **Write** access

This is important because the workflow relies on:

- assigning issues
- adding/removing labels
- posting comments
- reviewing and handing off work

### Where to do it

On GitHub:

- **Repository → Settings → Collaborators and teams**

Add the required users or team memberships there.

---

## 2. Create the workflow labels

Your orchestrator uses `status:*` labels as the issue state machine.

These labels are **repository-local**, so you must create them in every repository you want to use with the orchestrator.

Use the setup scripts from the repository root to create or update the required labels:

```bash
deploy/init_repo_lean.sh owner/repo
deploy/init_repo_full.sh owner/repo
```

If `owner/repo` is omitted, the scripts use `GH_REPO` or the current `gh repo view` repository.

### Lean workflow status labels

`deploy/init_repo_lean.sh` creates these status labels:

- `status:new`
- `status:story-definition`
- `status:dev-planning`
- `status:plan-review`
- `status:ready-for-development`
- `status:in-development`
- `status:code-review`
- `status:po-approval`
- `status:blocked`
- `status:done`
- `status:rejected`

### Full workflow status labels

`deploy/init_repo_full.sh` creates these status labels:

- `status:new`
- `status:triage`
- `status:solution-design`
- `status:ui-design`
- `status:ready-for-dev`
- `status:in-development`
- `status:architecture-review`
- `status:ui-review`
- `status:code-review`
- `status:testing`
- `status:po-acceptance`
- `status:blocked`
- `status:done`
- `status:rejected`

The scripts also create the matching `type:*`, `priority:*`, `role:*`, and workflow-specific helper labels.

Do not create or use legacy status labels such as `status:ready-for-review`, `status:ready-for-po-review`, `status:approved-for-dev`, `status:in-progress`, or `status:changes-requested` unless they are explicitly added to the active YAML workflow.

You may also use stakeholder labels like:

- `stakeholder:alicehuman`

### Where to do it

On GitHub:

- **Repository → Issues → Labels**
- then **New label**

### Suggested label colors

You can choose any colors, but a simple convention is:

- status labels: blue / purple / gray family
- type labels: green/orange
- stakeholder labels: yellow

The orchestrator only cares about label names, not colors.

---

## 3. Create the repository webhook

This is the most important GitHub-side step.

### Where to do it

On GitHub:

- **Repository → Settings → Webhooks → Add webhook**

### Webhook settings

Use:

- **Payload URL**: `https://mcp.singularia.de/webhook/github`
- **Content type**: `application/json`
- **Secret**: must exactly match the server’s `GITHUB_WEBHOOK_SECRET`
- **Active**: enabled

### Events to subscribe to

Select:

- **Issues**
- **Issue comments**

That is enough for the current workflow.

You do **not** need pull request events unless you later extend the orchestrator to handle PR workflows too.

### What happens after saving

GitHub will usually send a **ping** delivery immediately.

That is useful to verify that:

- GitHub can reach your server
- nginx routing works
- your webhook secret is accepted

---

## 4. Decide stakeholder handling strategy

The orchestrator resolves the stakeholder in this order:

1. `STAKEHOLDER_OVERRIDE` server env var
2. issue label like `stakeholder:alicehuman`
3. `stakeholder:` field inside a `[po-analysis]` comment
4. issue creator

You should decide which strategy your repository wants to use.

### Recommended approach

For most repos, this is the most flexible setup:

- do **not** use global `STAKEHOLDER_OVERRIDE`
- use either:
  - `stakeholder:<github-user>` labels
  - or the stakeholder field inside PO analysis comments

### Example stakeholder label

```text
stakeholder:alicehuman
```

### Example PO analysis comment

```text
[po-analysis]
type: feature
stakeholder: alicehuman
summary: Prepare user story for the new workflow.
[/po-analysis]
```

---

## 5. Create the first test issue

After labels and webhook are ready, create a test issue.

### Example test issue title

```text
Test orchestrator onboarding
```

### Example issue body

```text
Please verify that the GitHub MCP orchestrator receives this issue and routes it to the PO.
```

### Initial label

Apply:

- `status:new`

You may also add:

- `type:feature`
- `type:smoke-test` for workflow smoke tests that should route through roles without code, branches, or PRs

Optionally:

- `stakeholder:alicehuman`

### Expected behavior

After the issue is created and webhook delivered:

- the orchestrator should receive the event
- the issue should become visible to the workflow
- a PO task should be created internally for `thebesserwisser`

---

## 6. First workflow test on GitHub

Now test the lean issue workflow.

### Step A — PO story definition

As `thebesserwisser`, post:

```text
Author: thebesserwisser

Summary:
Define user story and acceptance criteria.

[next assignee role -> developer]
```

Then transition the issue into:

- `status:dev-planning`

### Step B — Developer handoff

As `johnvolldepp`, after implementation, comment:

```text
Author: johnvolldepp

Implementation finished and ready for review.

[next assignee role -> reviewer]
```

Then transition the issue into the next status listed in the work package `valid_transitions`, for example `status:code-review`.

### Step C — Reviewer loop

As `bobwurst`, either:

reject back to developer:

```text
Author: bobwurst

Please address the review findings.

[next assignee role -> developer]
```

or accept to PO approval:

```text
Author: bobwurst

Review accepted.

[next assignee role -> po]
```

When accepting, transition to `status:po-approval`.

### Step D — PO final approval

As `thebesserwisser`, confirm rollout approval, transition to `status:done`, remove assignees, and close the issue.

---

## 7. Verify webhook deliveries on GitHub

GitHub itself gives you a very useful debugging page.

### Where to check

- **Repository → Settings → Webhooks**
- open your webhook
- inspect **Recent Deliveries**

### What to verify

For each delivery, check:

- the event type is correct
- the HTTP response is success
- there are no signature errors
- the payload looks as expected

### Good signs

You want to see successful deliveries for:

- `issues`
- `issue_comment`

### Bad signs

Common problems:

- `404` → wrong nginx path or server route
- `401` or `403` → bad secret or auth logic
- `500` → server-side processing error
- timeout → server unreachable or blocked

---

## 8. Verify issue assignment behavior

Because your workflow uses assignees, check that GitHub can actually assign the issue to your workflow users.

On the issue page, verify that you can assign to:

- `thebesserwisser`
- `johnvolldepp`
- `bobwurst`

If GitHub does not allow assignment to one of them, fix repository permissions first.

---

## 9. Recommended repository conventions

To make the system reliable, it helps to adopt a few conventions.

### Recommended conventions

- every workflow issue has exactly one `status:*` label
- use `type:feature`, `type:bug`, `type:change-request`, or `type:smoke-test`
- stakeholder is explicit for important issues
- structured workflow comments are used consistently:
  - `[po-analysis]`
  - `[handoff]`
  - `/approve`

### Avoid

- manually applying multiple `status:*` labels at once
- assigning to users who are not part of the workflow
- using typo variants like `status:ready for review`
- posting malformed structured blocks

---

## 10. GitHub onboarding checklist for one repository

Use this as a quick checklist.

### Access and roles

- [ ] `thebesserwisser` has repo access
- [ ] `johnvolldepp` has repo access
- [ ] `bobwurst` has repo access

### Labels

- [ ] all required `status:*` labels created
- [ ] optional `type:*` labels created
- [ ] stakeholder label convention decided

### Webhook

- [ ] webhook created
- [ ] payload URL = `https://mcp.singularia.de/webhook/github`
- [ ] content type = `application/json`
- [ ] secret matches server
- [ ] events = Issues + Issue comments
- [ ] ping delivery succeeded

### First test

- [ ] test issue created
- [ ] test issue labeled `status:new`
- [ ] PO analysis comment posted
- [ ] stakeholder `/approve` tested
- [ ] developer handoff tested
- [ ] reviewer handoff tested

---

## Example onboarding sequence

A minimal real-world onboarding for a new repo could be:

1. Add the three workflow users as collaborators
2. Create the full set of `status:*` labels
3. Add webhook pointing to `https://mcp.singularia.de/webhook/github`
4. Save and verify webhook ping
5. Create issue:
   - title: `Test orchestrator onboarding`
   - label: `status:new`
6. Add comment as PO:
   - `[po-analysis] ... [/po-analysis]`
7. Add stakeholder `/approve`
8. Continue the workflow with `[handoff]` comments

---

## Suggested repository README snippet

You can add this short note to the repository README:

```md
## Workflow

This repository is connected to the GitHub MCP orchestrator.

Issue workflow is driven by:

- `status:*` labels
- assignments
- structured comments:
  - `[po-analysis]`
  - `[handoff]`
  - `/approve`

Main roles:

- PO: `thebesserwisser`
- Developer: `johnvolldepp`
- Reviewer: `bobwurst`
```

---

## Troubleshooting

### Webhook exists but nothing happens

Check:

- webhook delivery status in GitHub
- server health endpoint
- nginx proxy path
- webhook secret match

### Issue cannot be assigned to workflow user

Check:

- repo collaborator/team permissions
- whether the user is actually assignable in that repository

### Labels exist but workflow still does not move

Check:

- exact spelling of labels
- only one active `status:*` label should be used
- comment blocks must be correctly formatted

### `/approve` comment ignored

Check:

- it must be exactly `/approve` on its own line
- the commenter must match the resolved stakeholder

---

## Manual GitHub-side verification checklist

After finishing setup, verify these directly on GitHub:

- [ ] webhook ping is successful
- [ ] issue creation sends a delivery
- [ ] issue comment sends a delivery
- [ ] workflow users are assignable
- [ ] required labels are present
- [ ] test issue can move through the workflow

---

## Summary

To start using `github.mcp` with a repository, on GitHub you need to:

1. give the workflow users access
2. create the workflow labels
3. add the webhook
4. decide stakeholder handling
5. create and test the first issue workflow

Once that is done, the repository is connected and ready to participate in the orchestrated multi-agent issue workflow.
