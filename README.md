# GitHub Issue Orchestrator

A Go-based GitHub issue workflow orchestrator for multi-agent collaboration over GitHub Issues and Comments.

It combines:

- HTTP webhook ingestion from GitHub
- strict workflow transition validation
- SQLite-backed persistence
- role-based task queuing for autonomous agents
- a Bridge polling protocol for local/home agent instances
- MCP tools for inspection and manual control
- transition audit trail

This service is intended to run centrally, for example on:

- `https://mcp.singularia.de`

behind nginx, with nginx proxying to:

- `http://127.0.0.1:7777`

Agents run locally, poll the orchestrator via the Bridge, do work, and the orchestrator handles all GitHub state transitions.

---

## What this service does

The orchestrator watches GitHub issues and comments, interprets workflow signals, validates whether a transition is allowed, and creates tasks for the responsible role.

Roles are defined externally in the Bridge's `agents.json` — the orchestrator itself has no hardcoded usernames. A task is routed to a role (e.g. `developer`, `reviewer`, `po`), and the Bridge picks it up for the correct local agent.

Core features:

- receive GitHub webhook events on `/webhook/github`
- validate workflow transitions against a strict state machine
- apply transitions by:
  - updating `status:*` labels
  - assigning the next responsible GitHub user
- queue internal agent tasks with role-based routing
- let local Bridge instances atomically poll and claim those tasks
- expose MCP tools for inspection and manual control
- record every transition attempt in an audit trail

---

## Dashboard

A web dashboard is available at `http://127.0.0.1:6666` (configurable via `DASHBOARD_ADDR`).

Enable it by setting the environment variable:

```bash
DASHBOARD_ADDR=127.0.0.1:6666
```

### Dashboard features

| Area | URL | Description |
|------|-----|-------------|
| Live View | `/` | Real-time table of active workflows with SSE updates |
| Workflow List | `/workflows` | Grid of loaded workflow definitions |
| Workflow Graph | `/workflows/:key` | Interactive directed graph (ReactFlow + dagre layout) |
| Setup Docs | `/docs/setup` | Step-by-step configuration guide |
| API Docs | `/docs/api` | Full REST API reference with curl examples |

### Dashboard REST API (port 6666)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/dashboard/issues` | Active issues + task-queue state |
| `GET` | `/api/v1/dashboard/issues/{number}` | Single issue detail + audit history |
| `GET` | `/api/v1/dashboard/tasks` | Recent tasks (all statuses) |
| `GET` | `/api/v1/dashboard/audit` | Recent transition audit entries |
| `GET` | `/api/v1/dashboard/stream` | SSE live event stream |
| `GET` | `/api/v1/workflows` | List loaded workflow definitions |
| `GET` | `/api/v1/workflows/{id}` | Full workflow as graph-ready JSON |

The dashboard API is unauthenticated (v1: trusted internal network).

### Building the frontend

```bash
cd dashboard
npm install
npm run build        # produces dashboard/dist/
```

The backend URL is set at build time via `VITE_API_BASE_URL`. For local development the default `http://127.0.0.1:6666` is used. For a public deployment (e.g. singularia.de) set the variable so the browser can reach the backend:

```bash
VITE_API_BASE_URL=https://singularia.de:6666 npm run build
```

Deploy `dashboard/dist/` to a static web server (e.g. nginx on singularia.de).

---

## Architecture

### Central server

One Go process provides:

- `/healthz`
- `/webhook/github`
- `/api/v1/work/next` — Bridge polling endpoint
- `/mcp/tools/call`
- `/mcp/tools/list`
- `/api/v1/dashboard/*` — Dashboard API (when `DASHBOARD_ADDR` is set)

### Local Bridge + agents

The Bridge (`bridge/`) runs on agent machines and:

- reads a local `agents.json` roster describing which agents it manages
- polls `/api/v1/work/next?roles=...&bridge_id=...` for queued tasks
- atomically claims a task (marks it `dispatched`)
- spawns the correct agent process via a configurable launch template
- cools down providers after transient quota/rate-limit/provider failures to avoid tight retry loops
- polls immediately again after the agent exits

The `agent-task` CLI (`agent/`) is the tool an agent process uses to inspect the work package, open the issue in a browser, post GitHub comments, or post `/approve`.

---

## Roles

Roles are strings defined in the Bridge's `agents.json`. Common conventions:

- `po` — product owner
- `developer` — developer
- `reviewer` — code and requirements reviewer
- `architect` — architect (requirements analysis and architecture design)

The orchestrator routes tasks to roles. The Bridge maps roles to agent processes.

### Stakeholder resolution

The current GitHub assignee of the issue is passed to the Bridge in the work package as `assignee`. The Bridge first honors that explicit assignee when the matching agent is available, then uses issue-role affinity from `past_workers`, then round-robins over the role pool (see `bridge/README.md`).

The stakeholder identity (used for manual approval lookups) is resolved from a `stakeholder:<username>` label on the issue, falling back to the issue creator. The `find_stakeholder_approvals` MCP tool uses this to locate `/approve` comments. The workflow engine itself does not act on `/approve` comments — approval is a manual MCP tool operation, not an automatic engine trigger.

---

## YAML-driven workflow engine

Workflow definitions are loaded from `*.yaml` files in the `workflows/` directory at server startup. Any file that does not declare a `workflow.key` field is skipped (e.g. documentation or legacy extraction files).

### Selecting a workflow per webhook call

Append a `workflow` query parameter to the GitHub webhook URL to select the active workflow:

```
https://mcp.singularia.de/webhook/github?workflow=lean   # default
https://mcp.singularia.de/webhook/github?workflow=full
```

If the parameter is absent or unknown, the server uses the **lean** workflow (the default).

### Built-in workflows

| Key | File | Roles | Description |
|-----|------|-------|-------------|
| `lean` | `workflow-lean-3-roles-issue.yaml` | po, developer, reviewer | Streamlined 3-role flow |
| `full` | `workflow-full-6-roles-issue.yaml` | po, architect, uidesigner, developer, reviewer, tester | Full 6-role flow with guards |

### Configuring the workflows directory

```bash
WORKFLOWS_DIR=workflows   # default; path relative to working directory
```

### Startup validation

On startup the server reads every `*.yaml` file in `WORKFLOWS_DIR` that has a `workflow.key` field and validates it:

- every transition `from`/`to` references a known status ID
- every guard reference resolves to a declared guard
- no non-terminal status is left without outgoing transitions (dead-end check)

The server exits on any validation failure.

### Issue metadata (blocked_from)

Workflows that support a `status:blocked` state with a dynamic resume transition store the pre-block status as `blocked_from` metadata in the `issue_metadata` SQLite table. This enables the PO to resume an issue to exactly the state it came from.

```sql
CREATE TABLE issue_metadata (
  issue_id   INTEGER NOT NULL,
  key        TEXT    NOT NULL,
  value      TEXT    NOT NULL,
  updated_at TEXT    NOT NULL,
  PRIMARY KEY (issue_id, key)
);
```

---

## Workflow states

The orchestrator uses exactly one active `status:*` label per issue. The supported labels depend on the active workflow.

Initialize labels in a target GitHub repository before running a workflow:

```bash
# Uses the current gh repo context.
./deploy/init_repo_lean.sh
./deploy/init_repo_full.sh

# Or pass an explicit target repository.
./deploy/init_repo_lean.sh OWNER/REPO
./deploy/init_repo_full.sh OWNER/REPO
```

The scripts are idempotent: existing labels are updated with the expected color and description, and missing labels are created. The lean script creates the labels required by `workflows/workflow-lean-3-roles-issue.yaml`; the full script creates the labels required by `workflows/workflow-full-6-roles-issue.yaml`, including guard labels such as `needs:architecture`, `needs:ui-design`, `area:ui`, `area:ux`, and `risk:high`.

`deploy/init_repo_lean.sh` and `deploy/init_repo_full.sh` are generated from the workflow YAML files. After changing a workflow definition, regenerate them and verify they are in sync:

```bash
go run ./deploy/sync_init_repo_scripts.go
go run ./deploy/sync_init_repo_scripts.go -check
```

**Lean workflow** (`?workflow=lean`) statuses:

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

**Full workflow** (`?workflow=full`) statuses:

- `status:new` / `status:triage`
- `status:solution-design` / `status:ui-design`
- `status:ready-for-dev` / `status:in-development`
- `status:architecture-review` / `status:ui-review`
- `status:code-review`
- `status:testing`
- `status:po-acceptance`
- `status:blocked`
- `status:done` / `status:rejected`

---

## Workflow model

### Main lifecycle (lean workflow)

Typical feature flow using the default `lean` workflow:

1. New issue is created → `status:new` → PO task queued
2. PO defines the story and publishes it to the developer → `status:dev-planning` → Developer task queued
3. Developer creates a plan → `status:dev-planning` → Developer task queued; transitions to `status:plan-review`
4. Reviewer approves the plan → `status:ready-for-development` → Developer task queued
5. Developer implements → `status:in-development` → Developer task queued; transitions to `status:code-review`
6. Reviewer accepts the code → `status:po-approval` → PO task queued
7. PO approves rollout → `status:done`

### Review loop

The reviewer may send work back instead of accepting:

- from `status:plan-review` → back to `status:dev-planning` (plan rework)
- from `status:code-review` → back to `status:in-development` (code changes requested)

Each loop repeats until the reviewer accepts.

### Main lifecycle (full workflow)

The `full` workflow uses one PO intake pass before the first handoff. New issues no longer move from `status:new` to `status:triage` for a second PO task. Instead, PO defines the requirement and routes directly:

- architecture needed → `status:solution-design` → Architect task queued
- UI/UX needed without architecture first → `status:ui-design` → UI designer task queued
- small or clear work → `status:ready-for-dev` → Developer task queued
- invalid or out of scope → `status:rejected`
- blocked or missing information → `status:blocked` → PO task queued

`status:triage` remains for exception paths such as reopen, blocked re-triage, or clarification requested by architect/UI/developer.

### Bridge failure handling

When an agent process exits unsuccessfully, the bridge reports the failed work package to `POST /api/v1/work/fail`. The server immediately moves the dispatched task back to `queued`, preserving the same task id, so another bridge can claim and retry it. For transient provider/resource failures, such as quota exhaustion or rate limits, the bridge also cools down that provider for `AGENT_FAILURE_COOLDOWN_SECONDS` seconds before selecting agents from it again. Stale dispatched task recovery remains a fallback for crashed bridges that cannot report failure.

### Blocked state

Any actor may block an issue (`status:blocked`). The `blocked_from` metadata records the originating status. When the block is resolved, the issue resumes from exactly the state it came from.

---

## Transition validation

Transitions are validated against the loaded YAML workflow definition.

Validation checks include:

- current status
- actor role
- target status
- guard conditions (label-based gates, used in the full workflow)
- dynamic target resolution (e.g. `$metadata.blocked_from` for resume-from-blocked)

The orchestrator can:

- validate transitions without applying them (`validate_transition` MCP tool)
- apply validated transitions (`transition_issue` MCP tool)
- record all attempts in the transition audit trail

---

## Task queue

When the orchestrator decides a role should act next, it creates an internal task.

A task (work package) contains:

- `id` — task database ID
- `repo` — `owner/repo` string
- `issue_id` — GitHub issue number
- `role` — role expected to handle this task
- `assignee` — current GitHub assignee (used for explicit-assignee Bridge routing)
- `past_workers` — Author-tagged workers and completed task assignees already seen on the issue; used for issue-role stickiness
- `last_comment_id` — ID of the most recent issue comment at queue time
- `current_status` — current workflow status label at queue time
- `workflow_key` — active workflow key (e.g. `lean`)
- `valid_transitions` — statically-reachable target status IDs from the current status
- `next_assignee_roles` — roles eligible to handle the next step, derived from outbound workflow transitions

### Task lifecycle

```
queued → dispatched → completed
```

- `queued`: task is waiting for a Bridge to claim it
- `dispatched`: Bridge has claimed it; agent is running
- `completed`: agent run finished (success or failure from orchestrator's perspective)

### Deduplication

Tasks are deduplicated by `dedup_key = issue:<N>`. If an active task for the same
issue already exists **with the same role, same status, and same assignee**, a new one is not queued.
If the role, status, or assignee changed, the stale task is
superseded and a fresh task is queued with the updated state.

### Stale task recovery

Tasks stuck in `dispatched` are recovered back to `queued` if they exceed `STALE_AFTER_SECONDS` (default 900 s). This handles Bridge crashes.

---

## Bridge polling protocol

The Bridge atomically fetches and claims a task in one request:

```http
GET /api/v1/work/next?roles=developer,reviewer&bridge_id=my-bridge
Authorization: Bearer <AGENT_SHARED_TOKEN>
```

Response when work is available:

```json
{
  "ok": true,
  "task": {
    "id": 42,
    "repo": "martchouk/github.mcp",
    "issue_id": 8,
    "role": "developer",
    "assignee": "bud-dev",
    "last_comment_id": 123,
    "current_status": "status:in-development"
  }
}
```

Response when no work is available:

```json
{
  "ok": true,
  "task": null
}
```

See `bridge/README.md` for the full Bridge setup guide.

---

## MCP tools

All MCP tools are available over HTTP (`POST /mcp/tools/call`) and via the `transition_issue` / inspection paths.

Available tools:

- `get_issue_context`
- `list_issue_comments`
- `post_issue_comment`
- `assign_issue`
- `set_issue_labels`
- `add_issue_labels`
- `remove_issue_label`
- `get_workflow_state`
- `find_stakeholder_approvals`
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

This is the best single view of an issue's lifecycle.

### List available tools

```bash
curl -sS \
  -H "Authorization: Bearer $AGENT_SHARED_TOKEN" \
  "https://mcp.singularia.de/mcp/tools/list" | jq
```

### Call a tool

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $AGENT_SHARED_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"get_workflow_state","arguments":{"issue_number":8}}' \
  "https://mcp.singularia.de/mcp/tools/call" | jq
```

### Validate a transition

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $AGENT_SHARED_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"validate_transition","arguments":{"issue_number":8,"to_status":"status:dev-planning","actor":"ada-pow","assignee":"bud-dev","workflow":"lean"}}' \
  "https://mcp.singularia.de/mcp/tools/call" | jq
```

### Apply a transition

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $AGENT_SHARED_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"transition_issue","arguments":{"issue_number":8,"status":"status:dev-planning","assignee":"bud-dev","actor":"ada-pow","workflow":"lean"}}' \
  "https://mcp.singularia.de/mcp/tools/call" | jq
```

---

## Persistence

SQLite is used for all persistent state.

### Tables

#### `deliveries`

GitHub webhook delivery IDs and processing state (idempotency guard).

#### `tasks`

Queued and completed agent work packages.

#### `failures`

Operational failures recorded for debugging.

#### `transition_audit`

Every transition attempt — applied, rejected, failed, or ignored.

#### `issue_metadata`

Per-issue key/value metadata used by YAML workflow transitions (e.g., `blocked_from` for the blocked-resume feature).

---

## Transition audit trail

Audit fields include:

- issue number
- from / to status
- from / to assignee
- actor
- trigger type (`mcp_tool`, `webhook_comment`)
- trigger comment ID
- result (`applied`, `rejected`, `failed`, `ignored`)
- reason
- created timestamp

---

## Environment variables

### Required

```bash
GITHUB_TOKEN=...       # GitHub personal access token or app token
```

The server is multi-repo: `owner/repo` is derived from each incoming webhook payload (`repository.full_name`), so no static owner/repo configuration is needed.

### Core server config

```bash
HTTP_ADDR=127.0.0.1:7777   # default
SQLITE_PATH=orchestrator.db # default
```

### Webhook config

```bash
WEBHOOK_SECRET=...
```

### Agent API auth

```bash
AGENT_SHARED_TOKEN=...
```

### Stale task recovery

```bash
STALE_AFTER_SECONDS=900     # default; dispatched tasks older than this are re-queued
RECOVER_EVERY_SECONDS=30    # default; interval between recovery runs
```

### Workflow engine

```bash
WORKFLOWS_DIR=workflows   # default; path to YAML workflow definitions
```

### Optional

```bash
LOG_LEVEL=DEBUG   # set to DEBUG for verbose stderr logs
```

---

## Debug logging

Set `LOG_LEVEL=DEBUG` to enable verbose `DEBUG`-prefixed lines on stderr. All log lines — INFO and DEBUG — include a `YYYY/MM/DD HH:MM:SS` UTC timestamp and a component identifier (`[github-mcp]` for the server, `[bridge/<bridge_id>]` for the bridge), making concurrent events from multiple processes easy to correlate.

Example lines:
```
2026/05/12 15:04:05 [github-mcp] started: repo=martchouk/github.mcp addr=127.0.0.1:7777 sqlite=orchestrator.db
2026/05/12 15:04:05 [github-mcp] DEBUG config: stale_after=900s agent_auth=true webhook_secret=true
2026/05/12 15:04:10 [bridge/home-bridge] DEBUG poll: bridge=home-bridge roles=developer,reviewer
2026/05/12 15:04:10 [bridge/home-bridge] DEBUG no work available
2026/05/12 15:04:12 [github-mcp] task queued: issue=8 role=developer assignee=bud-dev task_id=42 status=status:in-development
2026/05/12 15:04:15 [bridge/home-bridge] work claimed: bridge=home-bridge roles=developer task=42 issue=8 role=developer assignee=bud-dev
2026/05/12 15:04:15 [bridge/home-bridge] DEBUG work received: task=42 issue=8 role=developer assignee=bud-dev status=status:in-development
```

**Server** — additional debug output includes:

| Event | Log line |
|---|---|
| Startup config summary | `DEBUG config: stale_after=… agent_auth=… webhook_secret=…` |
| Shutdown drain | `DEBUG cancelling context and waiting for http server to drain` |
| Webhook — duplicate delivery skipped | `DEBUG webhook: duplicate delivery ignored delivery=… event=…` |
| Work/next — no task available | `DEBUG work/next: bridge=… roles=… no work available` |
| Tool call dispatched | `DEBUG tool call: name=…` |
| Tool call outcome | `DEBUG tool call ok/failed: name=…` |
| Tools list served | `DEBUG tools/list: serving N tools` |
| Issue state computed | `DEBUG processIssue: issue=… status=… suggested_role=…` |
| Issue bootstrapped to status:new | `DEBUG processIssue: issue=… no status label — bootstrapping to status:new` |
| Bootstrap skipped (transient event) | `DEBUG processIssue: issue=… no status label but has task history — skipping bootstrap` |
| Task deduplication | `DEBUG processIssue: issue=… task deduplicated existing_task_id=…` |
| No-action decision | `DEBUG processIssue: issue=… no action — terminal or wait state` |
| Transition validation | `DEBUG transitionIssue: issue=… from=… to=… actor=… allowed=…` |
| Transition rejected (with violations) | `DEBUG transitionIssue: issue=… rejected violations=…` |
| Transition applied | `DEBUG transitionIssue: issue=… applied from=… to=…` |
| Recovery tick — no stale tasks | `DEBUG recovery tick: no stale tasks` |

Always-on INFO lines (regardless of `LOG_LEVEL`) cover: startup, HTTP listen, task queued, work claimed, webhook received, transition applied, and shutdown signal.

**Bridge** — debug output includes:

| Event | Log line |
|---|---|
| Startup — agent env keys | `DEBUG agent env configured: agent=… keys=…` |
| Each poll cycle | `DEBUG poll: bridge=… roles=…` |
| No work available | `DEBUG no work available` |
| Work received | `DEBUG work received: task=… issue=… role=… assignee=… status=…` |

Enable for the Bridge the same way:

```bash
export LOG_LEVEL=DEBUG
./agent-bridge
```

---

## Build

```bash
go build -o github-issue-orchestrator .
```

---

## Run

```bash
export GITHUB_TOKEN=ghp_xxx
export HTTP_ADDR=127.0.0.1:7777
export SQLITE_PATH=orchestrator.db
export AGENT_SHARED_TOKEN=supersecret
export WEBHOOK_SECRET=topsecret
export LOG_LEVEL=DEBUG

go build -o github-issue-orchestrator .
./github-issue-orchestrator
```

### Behind nginx

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

Response:

```json
{
  "ok": true,
  "service": "github-issue-orchestrator"
}
```

---

## GitHub webhook setup

Configure the GitHub webhook to send events to:

```text
https://mcp.singularia.de/webhook/github
```

Recommended events:

- Issues
- Issue comments

Content type: `application/json`

Secret: must match `WEBHOOK_SECRET`

---

## SQLite verification

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
SELECT id, issue_number, role, assignee, current_status, status, dedup_key, bridge_id, created_at
FROM tasks
ORDER BY id DESC
LIMIT 20;
"
```

### Show active tasks

```bash
sqlite3 -header -column "$DB" "
SELECT id, issue_number, role, assignee, status, bridge_id, created_at
FROM tasks
WHERE status IN ('queued','dispatched')
ORDER BY id ASC;
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

### Show rejected transitions

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

### Show all task history for one issue

```bash
ISSUE=8
sqlite3 -header -column "$DB" "
SELECT id, role, assignee, current_status, status, bridge_id, created_at, finished_at
FROM tasks
WHERE issue_number = $ISSUE
ORDER BY id ASC;
"
```

---

## Minimal startup checklist

1. Set `GITHUB_TOKEN`
2. Set `WEBHOOK_SECRET`
3. Set `AGENT_SHARED_TOKEN`
4. Start the service
5. Configure GitHub webhook to send Issues + Issue comments events
6. Verify `/healthz`
7. Verify SQLite tables exist: `sqlite3 orchestrator.db ".tables"`
8. Configure and start a Bridge (see `bridge/README.md`)

---

## Notes

- The orchestrator is strict by design — invalid transitions are rejected and recorded.
- Agents should route GitHub mutations through the orchestrator (via MCP tools or comments) to keep a consistent audit trail.
- `get_issue_timeline` is the most useful inspection tool when debugging workflow behavior.
