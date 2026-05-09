# agent-task

`agent-task` is a small local CLI helper for working with task files created by the bridge.

It is designed to be used by a human operator, a local AI agent, or a tmux-based workflow.

The tool reads one local task envelope JSON file and lets you:

- inspect the task
- open the linked GitHub issue in a browser
- post a normal GitHub comment through the orchestrator
- post a structured `[handoff]` comment
- post `/approve`
- mark the task as locally completed
- mark the task as locally failed

It does **not** talk directly to GitHub. Comments are posted through the orchestrator HTTP API.

---

## Where it fits

Typical flow:

1. The central orchestrator creates a task
2. The local bridge claims it and writes a local task file
3. `agent-task` is used to inspect and act on that file
4. `agent-task complete` or `agent-task fail` writes the result file
5. The bridge detects that result file and reports it back to the orchestrator

So the division of responsibility is:

- **orchestrator**: workflow engine, queue, GitHub integration
- **bridge**: polling, claiming, heartbeat, result upload
- **agent-task**: local operator/agent helper on one task file

---

## Project files

Current directory:

```text
agent/
├── agent-task
├── agent-task.go
├── build.sh
└── go.mod
```

---

## Build

From the `agent/` directory:

```bash
go build -o agent-task .
```

If you already have `build.sh`:

```bash
./build.sh
```

---

## Commands

```text
agent-task show     <taskfile>
agent-task open     <taskfile>
agent-task comment  <taskfile> --message "..." [--field key=value]...
agent-task handoff  <taskfile> --to bobwurst --state ready-for-review --summary "..."
agent-task approve  <taskfile>
agent-task complete <taskfile> [--message "..."] [--result key=value]...
agent-task fail     <taskfile> [--message "..."] [--result key=value]...
```

---

## Command overview

### `show`

Prints a readable summary of the task file, including:

- task ID
- issue number
- issue URL
- role
- assignee
- action
- status
- dedup key
- created/fetched timestamps
- done/fail result file paths
- pretty-printed payload JSON

Example:

```bash
agent-task show ./agent_tasks/task-17-issue-42.json
```

### `open`

Opens the issue URL from the task file in the default browser.

Supported platforms:

- macOS: `open`
- Linux: `xdg-open`
- Windows: `rundll32 url.dll,FileProtocolHandler`

Example:

```bash
agent-task open ./agent_tasks/task-17-issue-42.json
```

### `comment`

Posts a normal GitHub issue comment through the orchestrator.

Required:

- `--message`

Optional:

- multiple `--field key=value`

Fields are appended as bullet lines to the comment body.

Example:

```bash
agent-task comment ./agent_tasks/task-17-issue-42.json   --message "Static review completed and posted below."   --field outcome=accepted   --field severity=none
```

This produces a body like:

```text
Static review completed and posted below.

- outcome: accepted
- severity: none
```

### `handoff`

Posts a structured `[handoff]` comment through the orchestrator.

Required:

- `--to`
- `--state`
- `--summary`

The `from:` field is taken from:

1. `AGENT_ASSIGNEE` environment variable, if set
2. otherwise the `assignee` value from the task file

Example:

```bash
agent-task handoff ./agent_tasks/task-17-issue-42.json   --to bobwurst   --state ready-for-review   --summary "Implementation finished, ready for static review."
```

This posts:

```text
[handoff]
from: johnvolldepp
to: bobwurst
state: ready-for-review
summary: Implementation finished, ready for static review.
[/handoff]
```

### `approve`

Posts exactly:

```text
/approve
```

through the orchestrator.

Example:

```bash
agent-task approve ./agent_tasks/task-17-issue-42.json
```

### `complete`

Writes the local `.done.json` result file referenced by the task envelope.

Optional:

- `--message "..."`
- repeated `--result key=value`

If `--message` is omitted, the default is:

```text
completed by local worker
```

Example:

```bash
agent-task complete ./agent_tasks/task-17-issue-42.json   --message "Review finished and posted to GitHub"   --result outcome=accepted   --result review_url="https://github.com/org/repo/issues/42#issuecomment-1"
```

### `fail`

Writes the local `.fail.json` result file referenced by the task envelope.

Optional:

- `--message "..."`
- repeated `--result key=value`

If `--message` is omitted, the default is:

```text
failed by local worker
```

Example:

```bash
agent-task fail ./agent_tasks/task-17-issue-42.json   --message "Repository checkout missing"   --result reason=missing_checkout
```

---

## Environment variables

`agent-task` uses these environment variables when posting comments:

| Variable | Required for comment/handoff/approve | Purpose |
|---|---|---|
| `ORCH_BASE_URL` | yes | Base URL of the orchestrator, e.g. `https://mcp.singularia.de` |
| `AGENT_SHARED_TOKEN` | yes | Bearer token for the orchestrator agent API |
| `AGENT_ASSIGNEE` | recommended | Used as `agent` in comment requests and as `from:` for handoffs |

These are **not required** for `show`, `open`, `complete`, or `fail`.

### Example

```bash
export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
export AGENT_ASSIGNEE="johnvolldepp"
```

---

## Task file format

`agent-task` expects the JSON task envelope written by the bridge.

A typical file looks like:

```json
{
  "task": {
    "id": 17,
    "issue_number": 42,
    "issue_url": "https://github.com/org/repo/issues/42",
    "role": "developer",
    "assignee": "johnvolldepp",
    "action": "implement-or-refine",
    "status": "queued",
    "dedup_key": "issue:42|role:developer|action:implement-or-refine|status:approved-for-dev",
    "payload": {
      "issue_number": 42,
      "issue_url": "https://github.com/org/repo/issues/42",
      "role": "developer",
      "assignee": "johnvolldepp",
      "action": "implement-or-refine",
      "status": "status:approved-for-dev"
    },
    "created_at": "2026-05-09T10:00:00Z"
  },
  "fetched_at_utc": "2026-05-09T10:00:05Z",
  "result_hint": {
    "done_file": "./agent_tasks/task-17-issue-42.done.json",
    "fail_file": "./agent_tasks/task-17-issue-42.fail.json"
  }
}
```

---

## Result file format

### Success

`complete` writes the `done_file` path from `result_hint`.

Example output file:

```json
{
  "message": "Review finished and posted to GitHub",
  "result": {
    "outcome": "accepted",
    "review_url": "https://github.com/org/repo/issues/42#issuecomment-1"
  }
}
```

### Failure

`fail` writes the `fail_file` path from `result_hint`.

Example output file:

```json
{
  "message": "Repository checkout missing",
  "result": {
    "reason": "missing_checkout"
  }
}
```

---

## `--result` parsing behavior

For `complete` and `fail`, each `--result key=value` is parsed as follows:

- if `value` is valid JSON, it is stored as parsed JSON
- otherwise it is stored as a plain string

Examples:

```bash
--result outcome=accepted
--result count=5
--result ok=true
--result meta='{"severity":"low","notes":["a","b"]}'
```

These become:

```json
{
  "outcome": "accepted",
  "count": 5,
  "ok": true,
  "meta": {
    "severity": "low",
    "notes": ["a", "b"]
  }
}
```

For `comment`, `--field key=value` always stores values as strings.

---

## Usage examples

### Inspect a task

```bash
agent-task show ./agent_tasks/task-17-issue-42.json
```

### Open the issue in browser

```bash
agent-task open ./agent_tasks/task-17-issue-42.json
```

### Post a normal comment

```bash
agent-task comment ./agent_tasks/task-17-issue-42.json   --message "Review completed and posted to the issue."   --field outcome=accepted
```

### Handoff from developer to reviewer

```bash
agent-task handoff ./agent_tasks/task-17-issue-42.json   --to bobwurst   --state ready-for-review   --summary "Implementation finished, ready for static review."
```

### Stakeholder approval

```bash
agent-task approve ./agent_tasks/task-17-issue-42.json
```

### Mark task complete

```bash
agent-task complete ./agent_tasks/task-17-issue-42.json   --message "Work finished and issue updated"   --result outcome=accepted
```

### Mark task failed

```bash
agent-task fail ./agent_tasks/task-17-issue-42.json   --message "Repo checkout missing"   --result reason=missing_checkout
```

---

## curl equivalents for comment API

These are the requests `agent-task` effectively sends for comment-style operations.

Assume:

```bash
export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
export AGENT_ASSIGNEE="johnvolldepp"
```

### Normal comment

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{
    "issue_number": 42,
    "agent": "johnvolldepp",
    "body": "Review completed and posted below.

- outcome: accepted"
  }'   "$ORCH_BASE_URL/api/v1/agent/comment" | jq
```

### Handoff comment

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{
    "issue_number": 42,
    "agent": "johnvolldepp",
    "body": "[handoff]
from: johnvolldepp
to: bobwurst
state: ready-for-review
summary: Implementation finished, ready for static review.
[/handoff]"
  }'   "$ORCH_BASE_URL/api/v1/agent/comment" | jq
```

### Approve comment

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{
    "issue_number": 42,
    "agent": "johnvolldepp",
    "body": "/approve"
  }'   "$ORCH_BASE_URL/api/v1/agent/comment" | jq
```

---

## Local shell verification

Assume:

```bash
TASK=./agent_tasks/task-17-issue-42.json
```

### Show task

```bash
./agent-task show "$TASK"
```

### Pretty-print task file directly

```bash
jq . "$TASK"
```

### Show target result paths

```bash
jq -r '.result_hint.done_file, .result_hint.fail_file' "$TASK"
```

### Create a success result manually

```bash
DONE="$(jq -r '.result_hint.done_file' "$TASK")"
mkdir -p "$(dirname "$DONE")"
cat > "$DONE" <<'EOF'
{
  "message": "completed manually",
  "result": {
    "summary": "ok"
  }
}
EOF
```

### Create a failure result manually

```bash
FAIL="$(jq -r '.result_hint.fail_file' "$TASK")"
mkdir -p "$(dirname "$FAIL")"
cat > "$FAIL" <<'EOF'
{
  "message": "failed manually",
  "result": {
    "reason": "example"
  }
}
EOF
```

### Verify the issue URL in the task file

```bash
jq -r '.task.issue_url' "$TASK"
```

### Verify the issue number in the task file

```bash
jq -r '.task.issue_number' "$TASK"
```

---

## Exit behavior

- on success, `agent-task` exits with status `0`
- on invalid usage or runtime error, it exits with non-zero status and prints usage text

---

## Typical integrations

### tmux

A common bridge dispatch command is:

```bash
export DISPATCH_COMMAND='tmux send-keys -t johnvolldepp "agent-task open {file}" C-m'
```

Or for a pure local summary first:

```bash
export DISPATCH_COMMAND='tmux send-keys -t johnvolldepp "agent-task show {file}" C-m'
```

### Human-assisted workflow

1. bridge writes task file
2. tmux prints/open task
3. human reads issue
4. human posts comment or handoff via `agent-task`
5. human writes `complete` or `fail`

### AI-assisted workflow

1. bridge writes task file
2. local AI worker consumes it
3. AI uses `agent-task comment` / `handoff` / `approve`
4. AI ends with `agent-task complete` or `agent-task fail`

---

## Notes

- `agent-task` does not complete or fail tasks remotely by itself. It writes the local result file for the bridge to pick up.
- `comment`, `handoff`, and `approve` require orchestrator connectivity and auth.
- `complete` and `fail` are purely local file operations.
- `open` requires a supported OS launcher to exist.
- `show` is safe and offline.

---

## Minimal quickstart

```bash
cd agent

export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
export AGENT_ASSIGNEE="johnvolldepp"

go build -o agent-task .
./agent-task show ../bridge/agent_tasks/task-17-issue-42.json
```
