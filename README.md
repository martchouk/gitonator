# GitHub Issue Orchestrator MCP

A Go-based GitHub issue workflow orchestrator for multi-agent collaboration over GitHub Issues and Comments.

It combines:

- MCP-style stdio JSON-RPC tools
- HTTP webhook ingestion from GitHub
- strict workflow transition validation
- structured GitHub comment parsing
- SQLite-backed persistence
- task queueing for local/home agents
- optional dispatch via files, shell commands, or tmux
- transition audit trail
- agent-facing HTTP API for polling, claiming, heartbeats, completion, and comments

This service is intended to run centrally, for example on:

- `https://mcp.singularia.de`

behind nginx, with nginx proxying to:

- `http://127.0.0.1:7777`

Your agents can then run locally at home, poll the orchestrator, do work, and report back.

---

## What this service does

The orchestrator watches GitHub issues and comments, interprets workflow signals, validates whether a transition is allowed, and creates tasks for the responsible role.

It supports three main roles by default:

- **PO**: `thebesserwisser`
- **Developer**: `johnvolldepp`
- **Reviewer**: `bobwurst`

And one dynamically resolved **Stakeholder**.

Core features:

- receive GitHub webhook events on `/webhook/github`
- parse issue comments such as:
  - `[po-analysis] ... [/po-analysis]`
  - `[handoff] ... [/handoff]`
  - `/approve`
- validate workflow transitions against a strict state machine
- apply transitions by:
  - updating `status:*` labels
  - assigning the next responsible GitHub user
  - optionally posting GitHub comments
- queue internal agent tasks
- let local agents poll and claim those tasks
- record failures and transition audit history
- expose MCP tools for inspection and manual control

---

## Architecture

### Central server

One Go process provides:

- `/healthz`
- `/webhook/github`
- `/api/v1/agent/tasks`
- `/api/v1/agent/tasks/{id}/claim`
- `/api/v1/agent/tasks/{id}/heartbeat`
- `/api/v1/agent/tasks/{id}/complete`
- `/api/v1/agent/tasks/{id}/fail`
- `/api/v1/agent/comment`

and also supports MCP stdio JSON-RPC mode.

### Local agents

Agents can run on your home machine and:

- poll for tasks assigned to them
- claim a task
- send heartbeats
- complete/fail tasks
- post GitHub issue comments through the orchestrator
- optionally receive dispatched task files or tmux commands

---

## Roles

Default mapping:

- **PO**: `thebesserwisser`
- **Developer**: `johnvolldepp`
- **Reviewer**: `bobwurst`

### Stakeholder resolution

The stakeholder is resolved in this order:

1. `STAKEHOLDER_OVERRIDE` env var
2. GitHub issue label like `stakeholder:alicehuman`
3. `stakeholder:` field inside a `[po-analysis]` comment block
4. issue creator

Example label:

```text
stakeholder:alicehuman
```

Example PO analysis:

```text
[po-analysis]
type: feature
stakeholder: alicehuman
summary: Prepare user story and define acceptance criteria.
[/po-analysis]
```

---

## Workflow states

The orchestrator uses exactly one active `status:*` label per issue.

Supported status labels:

- `status:new`
- `status:po-analysis`
- `status:awaiting-stakeholder-approval`
- `status:approved-for-dev`
- `status:in-progress`
- `status:ready-for-review`
- `status:review-in-progress`
- `status:changes-requested`
- `status:ready-for-po-review`
- `status:po-review-in-progress`
- `status:awaiting-final-stakeholder-approval`
- `status:blocked`
- `status:done`
- `status:rejected`

---

## Workflow model

### Main lifecycle

Typical feature flow:

1. New issue is created
2. PO analyzes it and writes a `[po-analysis]` comment
3. Issue moves to `status:awaiting-stakeholder-approval`
4. Stakeholder approves with `/approve`
5. Issue moves to `status:approved-for-dev`
6. Developer starts work and moves to `status:in-progress`
7. Developer hands off with `[handoff]`
8. Reviewer reviews:
   - either sends back to developer with `status:changes-requested`
   - or approves to PO with `status:ready-for-po-review`
9. PO reviews:
   - either sends back to developer
   - or requests final stakeholder approval
10. Stakeholder approves with `/approve`
11. Issue moves to `status:done`

### Review loop

After developer refinements:

- reviewer gets the issue again
- reviewer may:
  - accept and assign to PO
  - reject and assign back to developer

This loop can repeat until accepted.

---

## Structured comment formats

### 1. PO analysis

Used by the PO to document feature scope or bug analysis.

```text
[po-analysis]
type: feature
stakeholder: alicehuman
summary: Introduce new patient search with parity to JSP behavior.
[/po-analysis]
```

For bugs:

```text
[po-analysis]
type: bug
stakeholder: alicehuman
summary: Analyze why patient form loses external doctor link after save.
[/po-analysis]
```

### 2. Handoff

Used by developer, reviewer, or PO to hand work to the next role.

```text
[handoff]
from: johnvolldepp
to: bobwurst
state: ready-for-review
summary: Implementation finished, please perform static review.
[/handoff]
```

Another example:

```text
[handoff]
from: bobwurst
to: thebesserwisser
state: ready-for-po-review
summary: Review accepted, ready for PO validation.
[/handoff]
```

### 3. Approval

Used by the stakeholder.

```text
/approve
```

Only exact `/approve` on its own line is recognized.

---

## Transition validation

Transitions are validated against a strict rule matrix.

Validation checks include:

- current status
- actor
- current assignee
- target status
- target assignee
- stakeholder approval requirement

Examples:

- stakeholder approval is required before:
  - `status:approved-for-dev`
  - `status:done`
- developer cannot move directly to `status:done`
- reviewer cannot move issue from arbitrary states unless the transition rule allows it

The orchestrator can:

- validate transitions without applying them
- apply validated transitions
- record rejected transitions in the audit trail

---

## Persistence

SQLite is used initially.

The database stores:

- webhook deliveries
- queued/claimed/running/completed/failed tasks
- failures
- transition audit records

### Tables

#### `deliveries`

Stores GitHub webhook delivery IDs and processing state.

#### `tasks`

Stores queued agent work.

#### `failures`

Stores operational failures.

#### `transition_audit`

Stores transition attempts and outcomes.

---

## Transition audit trail

Every meaningful transition attempt can be recorded, including:

- applied transitions
- rejected transitions
- failed transitions
- ignored comment directives

Audit fields include:

- issue number
- from status
- to status
- from assignee
- to assignee
- actor
- trigger type
- trigger comment ID
- result
- reason
- validation JSON
- metadata JSON
- created timestamp

Trigger types commonly include:

- `mcp_tool`
- `webhook_comment`

Results commonly include:

- `applied`
- `rejected`
- `failed`
- `ignored`

---

## Task queue

When the orchestrator decides a role should act next, it creates an internal task.

A task contains:

- issue number
- issue URL
- role
- assignee
- action
- current status
- dedup key
- context
- created time

### Deduplication

Tasks are deduplicated by `dedup_key`, for example:

```text
issue:123|role:reviewer|action:static-review|status:ready-for-review
```

If an active task with the same dedup key already exists, the new one is not queued.

### Stale task recovery

Tasks stuck in `claimed` or `running` can be recovered back to `queued` if they stop sending heartbeats.

---

## Dispatch options

The orchestrator can dispatch work in several ways:

- write task files into a directory
- run a shell command
- send commands into tmux

This makes it easy to bridge central orchestration with local agent sessions.

---

## MCP tools

The stdio MCP server exposes tools including:

- `get_issue_context`
- `list_issue_comments`
- `post_issue_comment`
- `assign_issue`
- `set_issue_labels`
- `add_issue_labels`
- `remove_issue_label`
- `get_workflow_state`
- `find_stakeholder_approvals`
- `parse_comments`
- `validate_transition`
- `transition_issue`
- `get_transition_matrix`
- `process_issue_event`
- `get_transition_audit`
- `get_issue_timeline`

### `get_issue_timeline`

Returns one merged chronological stream of:

- GitHub comments
- transition audit rows
- local task rows

This is the best single view of an issue’s lifecycle.

---

## Environment variables

### Required

```bash
GITHUB_TOKEN=...
GITHUB_OWNER=martchouk
GITHUB_REPO=github.mcp
```

### Core server config

```bash
HTTP_ADDR=:7777
SQLITE_PATH=orchestrator.db
LOG_LEVEL=DEBUG
```

### Webhook config

```bash
GITHUB_WEBHOOK_SECRET=...
```

### Agent API auth

```bash
AGENT_SHARED_TOKEN=...
```

### Stakeholder override

```bash
STAKEHOLDER_OVERRIDE=alicehuman
```

### Dispatch config

```bash
DISPATCH_DIR=/var/lib/github-orchestrator/tasks
DISPATCH_COMMAND=
DISPATCH_TMUX_TEMPLATE=
```

Examples:

```bash
DISPATCH_COMMAND='cp "{file}" /tmp/agent-inbox/'
```

```bash
DISPATCH_TMUX_TEMPLATE='tmux send-keys -t {assignee} "agent-task open {file}" C-m'
```

---

## Build

```bash
go build -o github-issue-orchestrator .
```

---

## Run

### HTTP server mode

```bash
export GITHUB_TOKEN=...
export GITHUB_OWNER=martchouk
export GITHUB_REPO=github.mcp
export HTTP_ADDR=:7777
export SQLITE_PATH=orchestrator.db
export AGENT_SHARED_TOKEN=supersecret
export GITHUB_WEBHOOK_SECRET=topsecret

./github-issue-orchestrator
```

### Behind nginx

nginx terminates HTTPS and forwards to the local Go service:

```nginx
location / {
    proxy_pass http://127.0.0.1:7777;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
}
```

---

## Health check

```bash
curl -sS https://mcp.singularia.de/healthz | jq
```

Example response:

```json
{
  "ok": true,
  "service": "github-issue-orchestrator"
}
```

---

## GitHub webhook setup

Configure the GitHub webhook to send issue-related events to:

```text
https://mcp.singularia.de/webhook/github
```

Recommended events:

- Issues
- Issue comments

Content type:

- `application/json`

Secret:

- must match `GITHUB_WEBHOOK_SECRET`

---

## Agent HTTP API

All agent endpoints require:

```http
Authorization: Bearer <AGENT_SHARED_TOKEN>
```

### List tasks

```bash
curl -sS   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   "https://mcp.singularia.de/api/v1/agent/tasks?assignee=bobwurst&limit=20" | jq
```

### Claim task

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{"agent":"bobwurst"}'   "https://mcp.singularia.de/api/v1/agent/tasks/42/claim" | jq
```

### Heartbeat

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{"agent":"bobwurst","message":"review in progress"}'   "https://mcp.singularia.de/api/v1/agent/tasks/42/heartbeat" | jq
```

### Complete task

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{"agent":"bobwurst","message":"review complete","result":{"summary":"Looks good"}}'   "https://mcp.singularia.de/api/v1/agent/tasks/42/complete" | jq
```

### Fail task

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{"agent":"bobwurst","message":"cannot review, missing branch","result":{"reason":"branch not found"}}'   "https://mcp.singularia.de/api/v1/agent/tasks/42/fail" | jq
```

### Post GitHub comment via orchestrator

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{"issue_number":8,"agent":"bobwurst","body":"Static review completed. See findings below."}'   "https://mcp.singularia.de/api/v1/agent/comment" | jq
```

---

## MCP / JSON-RPC examples

### Initialize

```bash
printf '%s
' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | ./github-issue-orchestrator
```

### List tools

```bash
printf '%s
' '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' | ./github-issue-orchestrator
```

### Get workflow state

```bash
printf '%s
' '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_workflow_state","arguments":{"issue_number":8}}}' | ./github-issue-orchestrator
```

### Validate transition

```bash
printf '%s
' '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"validate_transition","arguments":{"issue_number":8,"to_status":"status:ready-for-review","actor":"johnvolldepp","assignee":"bobwurst"}}}' | ./github-issue-orchestrator
```

### Apply transition

```bash
printf '%s
' '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"transition_issue","arguments":{"issue_number":8,"status":"status:ready-for-review","assignee":"bobwurst","actor":"johnvolldepp","comment":"[handoff]
from: johnvolldepp
to: bobwurst
state: ready-for-review
summary: Implementation finished, please review.
[/handoff]"}}}' | ./github-issue-orchestrator
```

### Get timeline

```bash
printf '%s
' '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_issue_timeline","arguments":{"issue_number":8,"limit":100}}}' | ./github-issue-orchestrator
```

---

## Example GitHub comment flow

### PO writes analysis

```text
[po-analysis]
type: feature
stakeholder: alicehuman
summary: Migrate patient screens to React while preserving legacy parity.
[/po-analysis]
```

### Stakeholder approves

```text
/approve
```

### Developer hands to reviewer

```text
[handoff]
from: johnvolldepp
to: bobwurst
state: ready-for-review
summary: Implementation completed. Static review requested.
[/handoff]
```

### Reviewer sends back changes

```text
[handoff]
from: bobwurst
to: johnvolldepp
state: changes-requested
summary: Please address the findings in the review comment.
[/handoff]
```

### Reviewer accepts to PO

```text
[handoff]
from: bobwurst
to: thebesserwisser
state: ready-for-po-review
summary: Review accepted. Ready for PO validation.
[/handoff]
```

---

## SQLite verification

Assume:

```bash
DB=orchestrator.db
```

### Show all tables

```bash
sqlite3 "$DB" ".tables"
```

### Show task counts by status

```bash
sqlite3 -header -column "$DB" "
SELECT status, COUNT(*) AS n
FROM tasks
GROUP BY status
ORDER BY status;
"
```

### Show newest tasks

```bash
sqlite3 -header -column "$DB" "
SELECT id, issue_number, role, assignee, action, status, dedup_key, created_at, claimed_by
FROM tasks
ORDER BY id DESC
LIMIT 20;
"
```

### Show active dedup keys

```bash
sqlite3 -header -column "$DB" "
SELECT dedup_key, COUNT(*) AS n
FROM tasks
WHERE status IN ('queued','claimed','running')
GROUP BY dedup_key
ORDER BY n DESC, dedup_key;
"
```

### Find duplicate active tasks by dedup key

```bash
sqlite3 -header -column "$DB" "
SELECT dedup_key, COUNT(*) AS n
FROM tasks
WHERE status IN ('queued','claimed','running')
GROUP BY dedup_key
HAVING COUNT(*) > 1
ORDER BY n DESC, dedup_key;
"
```

### Show transition audit latest rows

```bash
sqlite3 -header -column "$DB" "
SELECT id, issue_number, from_status, to_status, actor, trigger_type, result, reason, created_at
FROM transition_audit
ORDER BY id DESC
LIMIT 30;
"
```

### Show audit history for one issue

```bash
ISSUE=8
sqlite3 -header -column "$DB" "
SELECT id, from_status, to_status, from_assignee, to_assignee, actor, trigger_type, result, created_at
FROM transition_audit
WHERE issue_number = $ISSUE
ORDER BY id ASC;
"
```

### Show rejected transitions only

```bash
sqlite3 -header -column "$DB" "
SELECT id, issue_number, actor, from_status, to_status, reason, created_at
FROM transition_audit
WHERE result = 'rejected'
ORDER BY id DESC
LIMIT 30;
"
```

### Show failed webhook deliveries

```bash
sqlite3 -header -column "$DB" "
SELECT id, delivery_id, event_type, status, error_text, received_at, processed_at
FROM deliveries
WHERE status = 'failed'
ORDER BY id DESC
LIMIT 20;
"
```

### Show duplicate webhook delivery IDs

```bash
sqlite3 -header -column "$DB" "
SELECT delivery_id, COUNT(*) AS n
FROM deliveries
GROUP BY delivery_id
HAVING COUNT(*) > 1
ORDER BY n DESC, delivery_id;
"
```

### Show operational failures

```bash
sqlite3 -header -column "$DB" "
SELECT id, issue_number, stage, error_text, created_at
FROM failures
ORDER BY id DESC
LIMIT 30;
"
```

### Show all task history for one issue

```bash
ISSUE=8
sqlite3 -header -column "$DB" "
SELECT id, role, assignee, action, status, created_at, claimed_at, heartbeat_at, finished_at, claimed_by
FROM tasks
WHERE issue_number = $ISSUE
ORDER BY id ASC;
"
```

### Quick sanity check: issues with many task retries

```bash
sqlite3 -header -column "$DB" "
SELECT issue_number, COUNT(*) AS task_rows
FROM tasks
GROUP BY issue_number
ORDER BY task_rows DESC, issue_number
LIMIT 20;
"
```

---

## Useful operational curl checks

### Health

```bash
curl -sS https://mcp.singularia.de/healthz | jq
```

### Reviewer inbox

```bash
curl -sS   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   "https://mcp.singularia.de/api/v1/agent/tasks?assignee=bobwurst&limit=10" | jq
```

### Developer inbox

```bash
curl -sS   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   "https://mcp.singularia.de/api/v1/agent/tasks?assignee=johnvolldepp&limit=10" | jq
```

### PO inbox

```bash
curl -sS   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   "https://mcp.singularia.de/api/v1/agent/tasks?assignee=thebesserwisser&limit=10" | jq
```

---

## Common verification scenarios

### 1. New feature issue should go to PO

Expected:

- issue gets or stays in `status:new` / `status:po-analysis`
- PO task is queued

Verify:

```bash
sqlite3 -header -column "$DB" "
SELECT id, issue_number, role, assignee, action, status, created_at
FROM tasks
WHERE issue_number = 8
ORDER BY id DESC
LIMIT 10;
"
```

### 2. Stakeholder approval should unlock developer

Expected after `/approve` on `status:awaiting-stakeholder-approval`:

- transition audit row with:
  - `to_status = status:approved-for-dev`
  - `result = applied`
- developer task queued

Verify:

```bash
sqlite3 -header -column "$DB" "
SELECT id, from_status, to_status, actor, result, created_at
FROM transition_audit
WHERE issue_number = 8
ORDER BY id DESC
LIMIT 10;
"
```

```bash
sqlite3 -header -column "$DB" "
SELECT id, role, assignee, action, status, created_at
FROM tasks
WHERE issue_number = 8
ORDER BY id DESC
LIMIT 10;
"
```

### 3. Reviewer sends back to developer

Expected:

- transition to `status:changes-requested`
- assignee becomes developer
- new developer task appears unless deduplicated

Verify:

```bash
sqlite3 -header -column "$DB" "
SELECT id, from_status, to_status, from_assignee, to_assignee, result
FROM transition_audit
WHERE issue_number = 8
ORDER BY id DESC
LIMIT 10;
"
```

### 4. No duplicate active tasks

Expected:

- no duplicate active dedup keys

Verify:

```bash
sqlite3 -header -column "$DB" "
SELECT dedup_key, COUNT(*) AS n
FROM tasks
WHERE status IN ('queued','claimed','running')
GROUP BY dedup_key
HAVING COUNT(*) > 1;
"
```

This should return no rows.

---

## Notes

- The orchestrator is strict by design.
- It is intended to coordinate multi-agent work over GitHub issues.
- Agents should not mutate GitHub issues directly if you want a consistent audit trail; instead, route actions through the orchestrator.
- `get_issue_timeline` is the most useful inspection tool when debugging workflow behavior.

---

## Minimal startup checklist

1. Set GitHub token and repo env vars
2. Set webhook secret
3. Set agent shared token
4. Start the service
5. Configure GitHub webhook
6. Verify `/healthz`
7. Verify SQLite tables exist
8. Verify agents can poll `/api/v1/agent/tasks`

---

## Example quickstart

```bash
export GITHUB_TOKEN=ghp_xxx
export GITHUB_OWNER=martchouk
export GITHUB_REPO=github.mcp
export HTTP_ADDR=:7777
export SQLITE_PATH=orchestrator.db
export AGENT_SHARED_TOKEN=supersecret
export GITHUB_WEBHOOK_SECRET=topsecret
export LOG_LEVEL=DEBUG

go build -o github-issue-orchestrator .
./github-issue-orchestrator
```
