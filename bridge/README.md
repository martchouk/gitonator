# Bridge

A lightweight local agent bridge for the GitHub Issue Orchestrator.

The bridge runs on your local machine, polls the central orchestrator for tasks assigned to one GitHub user, claims a task, writes a local task file, optionally dispatches it to tmux or another local command, keeps the task alive with heartbeats, and waits for a local `.done.json` or `.fail.json` result file.

This is the component intended for setups like:

- central orchestrator on `https://mcp.singularia.de`
- agents running on your home workstation

---

## What the bridge does

For one configured assignee, the bridge loops forever:

1. polls `/api/v1/agent/tasks`
2. takes the first queued task
3. claims it
4. writes a local task file into `TASKS_DIR`
5. dispatches that file to a local worker
6. sends regular heartbeats
7. waits for:
   - `*.done.json` to complete the task
   - `*.fail.json` to fail the task

It processes one task per poll cycle.

---

## How it fits into the system

Typical flow:

1. The orchestrator creates a task for `johnvolldepp`
2. The bridge running for `johnvolldepp` polls the orchestrator
3. The bridge claims the task
4. The bridge writes a local JSON task file
5. A local helper, tmux session, or manual workflow picks it up
6. The local worker writes either:
   - `task-<id>-issue-<n>.done.json`
   - `task-<id>-issue-<n>.fail.json`
7. The bridge sends the completion/failure back to the orchestrator

---

## Project files

Current bridge directory:

```text
bridge/
├── bridge.go
├── go.mod
└── run.sh
```

---

## Build

From the `bridge/` directory:

```bash
go build -o bridge .
```

Or with the provided script if you use one:

```bash
./run.sh
```

---

## Environment variables

The bridge is configured entirely through environment variables.

### Required

```bash
AGENT_SHARED_TOKEN=...
```

### Main settings

| Variable | Default | Meaning |
|---|---|---|
| `ORCH_BASE_URL` | `https://mcp.singularia.de` | Base URL of the central orchestrator |
| `AGENT_SHARED_TOKEN` | _(required)_ | Bearer token used for the agent API |
| `AGENT_ASSIGNEE` | `johnvolldepp` | GitHub username whose tasks this bridge should poll |
| `POLL_SECONDS` | `5` | Delay between poll cycles |
| `HEARTBEAT_SECONDS` | `20` | Delay between heartbeats while waiting for local completion |
| `TMUX_TARGET` | `johnvolldepp` | tmux target used by the default dispatch behavior |
| `TASKS_DIR` | `./agent_tasks` | Local directory where task/result files are stored |
| `DISPATCH_COMMAND` | _(empty)_ | Optional shell command used instead of the default tmux dispatch |
| `CLAIM_LIMIT` | `10` | Number of queued tasks fetched per poll |

---

## Default behavior

If `DISPATCH_COMMAND` is empty, the bridge uses tmux:

```bash
tmux send-keys -t "$TMUX_TARGET" "echo 'NEW TASK FILE: <path>'" C-m
```

That means the default mode only notifies the session that a new task file exists.

A richer setup typically uses either:

- a custom `DISPATCH_COMMAND`
- a helper such as `agent-task`
- a tmux command that opens the task directly

---

## Recommended startup example

### Developer bridge

```bash
export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
export AGENT_ASSIGNEE="johnvolldepp"
export POLL_SECONDS=5
export HEARTBEAT_SECONDS=20
export TMUX_TARGET="johnvolldepp"
export TASKS_DIR="$HOME/github-mcp-agent/tasks"
export DISPATCH_COMMAND='tmux send-keys -t johnvolldepp "agent-task open {file}" C-m'

go build -o bridge .
./bridge
```

### Reviewer bridge

```bash
export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
export AGENT_ASSIGNEE="bobwurst"
export POLL_SECONDS=5
export HEARTBEAT_SECONDS=20
export TMUX_TARGET="bobwurst"
export TASKS_DIR="$HOME/github-mcp-agent/tasks"
export DISPATCH_COMMAND='tmux send-keys -t bobwurst "agent-task open {file}" C-m'

go build -o bridge .
./bridge
```

### PO bridge

```bash
export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
export AGENT_ASSIGNEE="thebesserwisser"
export POLL_SECONDS=5
export HEARTBEAT_SECONDS=20
export TMUX_TARGET="thebesserwisser"
export TASKS_DIR="$HOME/github-mcp-agent/tasks"
export DISPATCH_COMMAND='tmux send-keys -t thebesserwisser "agent-task open {file}" C-m'

go build -o bridge .
./bridge
```

---

## Task file format

When a task is claimed, the bridge writes a JSON file like:

```json
{
  "task": {
    "id": 42,
    "issue_number": 8,
    "role": "reviewer",
    "assignee": "bobwurst",
    "action": "static-review",
    "status": "queued",
    "dedup_key": "issue:8|role:reviewer|action:static-review|status:ready-for-review",
    "payload": {
      "issue_number": 8,
      "issue_url": "https://github.com/org/repo/issues/8",
      "role": "reviewer",
      "assignee": "bobwurst",
      "action": "static-review",
      "status": "status:ready-for-review",
      "context": {
        "title": "Example issue",
        "stakeholder": "alicehuman"
      }
    },
    "created_at": "2026-05-09T10:00:00Z"
  },
  "fetched_at_utc": "2026-05-09T10:00:05Z",
  "result_hint": {
    "done_file": "./agent_tasks/task-42-issue-8.done.json",
    "fail_file": "./agent_tasks/task-42-issue-8.fail.json"
  }
}
```

---

## Result files

The local worker finishes by writing one of these:

- `task-<taskId>-issue-<issueNumber>.done.json`
- `task-<taskId>-issue-<issueNumber>.fail.json`

### Success file

Example:

```json
{
  "message": "static review completed",
  "result": {
    "summary": "No blocking findings",
    "issue_comment_url": "https://github.com/org/repo/issues/8#issuecomment-1"
  }
}
```

### Failure file

Example:

```json
{
  "message": "unable to review",
  "result": {
    "reason": "branch not found"
  }
}
```

### Empty result file

An empty file is also accepted. In that case the bridge uses fallback messages:

- `"completed by local worker"`
- `"failed by local worker"`

---

## File naming convention

For task ID `42` and issue `8`:

- task file: `task-42-issue-8.json`
- success file: `task-42-issue-8.done.json`
- failure file: `task-42-issue-8.fail.json`

These files are created inside `TASKS_DIR`.

---

## Local workflow examples

### Example 1: manual handling

1. bridge writes `task-42-issue-8.json`
2. you inspect it manually
3. you do the work
4. you write:

```bash
cat > ./agent_tasks/task-42-issue-8.done.json <<'EOF'
{
  "message": "review completed",
  "result": {
    "summary": "Looks good"
  }
}
EOF
```

The bridge sees the file and completes the task remotely.

### Example 2: manual fail

```bash
cat > ./agent_tasks/task-42-issue-8.fail.json <<'EOF'
{
  "message": "cannot proceed",
  "result": {
    "reason": "missing repository checkout"
  }
}
EOF
```

---

## Dispatch command placeholders

If `DISPATCH_COMMAND` is set, the bridge replaces:

- `{file}` with the local task file path
- `{assignee}` with the configured assignee
- `{tmux_target}` with the configured tmux target

Example:

```bash
export DISPATCH_COMMAND='tmux send-keys -t {tmux_target} "agent-task open {file}" C-m'
```

Another example:

```bash
export DISPATCH_COMMAND='cp {file} /tmp/agent-inbox/'
```

Note: the bridge shell-quotes `{file}`, `{assignee}`, and `{tmux_target}` before injecting them into the shell command.

---

## HTTP endpoints used by the bridge

The bridge talks to the orchestrator only through the agent HTTP API.

### 1. List tasks

```http
GET /api/v1/agent/tasks?assignee=<user>&limit=<n>
Authorization: Bearer <AGENT_SHARED_TOKEN>
```

### 2. Claim task

```http
POST /api/v1/agent/tasks/{id}/claim
Authorization: Bearer <AGENT_SHARED_TOKEN>
Content-Type: application/json
```

Body:

```json
{
  "agent": "johnvolldepp"
}
```

### 3. Heartbeat

```http
POST /api/v1/agent/tasks/{id}/heartbeat
Authorization: Bearer <AGENT_SHARED_TOKEN>
Content-Type: application/json
```

Body:

```json
{
  "agent": "johnvolldepp",
  "message": "waiting for local completion file"
}
```

### 4. Complete

```http
POST /api/v1/agent/tasks/{id}/complete
Authorization: Bearer <AGENT_SHARED_TOKEN>
Content-Type: application/json
```

### 5. Fail

```http
POST /api/v1/agent/tasks/{id}/fail
Authorization: Bearer <AGENT_SHARED_TOKEN>
Content-Type: application/json
```

---

## curl verification

Assume:

```bash
export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
```

### List reviewer tasks

```bash
curl -sS   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   "$ORCH_BASE_URL/api/v1/agent/tasks?assignee=bobwurst&limit=10" | jq
```

### Claim task 42

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{"agent":"bobwurst"}'   "$ORCH_BASE_URL/api/v1/agent/tasks/42/claim" | jq
```

### Send heartbeat for task 42

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{"agent":"bobwurst","message":"review in progress"}'   "$ORCH_BASE_URL/api/v1/agent/tasks/42/heartbeat" | jq
```

### Complete task 42

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{"agent":"bobwurst","message":"review completed","result":{"summary":"Looks good"}}'   "$ORCH_BASE_URL/api/v1/agent/tasks/42/complete" | jq
```

### Fail task 42

```bash
curl -sS -X POST   -H "Authorization: Bearer $AGENT_SHARED_TOKEN"   -H "Content-Type: application/json"   -d '{"agent":"bobwurst","message":"cannot review","result":{"reason":"branch missing"}}'   "$ORCH_BASE_URL/api/v1/agent/tasks/42/fail" | jq
```

---

## Bash verification on local machine

Assume:

```bash
TASKS_DIR=./agent_tasks
```

### Show current task files

```bash
find "$TASKS_DIR" -maxdepth 1 -type f | sort
```

### Watch task directory

```bash
watch -n 1 "find '$TASKS_DIR' -maxdepth 1 -type f | sort"
```

### Pretty-print all current task files

```bash
for f in "$TASKS_DIR"/*.json; do
  [ -e "$f" ] || continue
  echo "===== $f"
  jq . "$f"
done
```

### Show only pending task envelopes

```bash
find "$TASKS_DIR" -maxdepth 1 -type f -name 'task-*-issue-*.json' ! -name '*.done.json' ! -name '*.fail.json' | sort
```

### Create a success result quickly

```bash
f="$TASKS_DIR/task-42-issue-8.done.json"
cat > "$f" <<'EOF'
{
  "message": "completed locally",
  "result": {
    "summary": "ok"
  }
}
EOF
```

### Create a failure result quickly

```bash
f="$TASKS_DIR/task-42-issue-8.fail.json"
cat > "$f" <<'EOF'
{
  "message": "failed locally",
  "result": {
    "reason": "example"
  }
}
EOF
```

---

## Expected bridge log behavior

Typical sequence:

1. poll tasks
2. claim one queued task
3. write task file
4. send initial heartbeat
5. dispatch locally
6. keep heartbeating every `HEARTBEAT_SECONDS`
7. detect `.done.json` or `.fail.json`
8. notify orchestrator
9. remove the result file

The task envelope file itself stays in `TASKS_DIR` unless you clean it up separately.

---

## Failure behavior

If something goes wrong:

- if task file creation fails, the bridge fails the task remotely
- if dispatch fails, the bridge fails the task remotely
- if the done/fail result file contains invalid JSON, the bridge fails the task remotely
- if no completion file appears, the bridge keeps heartbeating indefinitely

That means stale-task recovery on the orchestrator side remains the safety net if the bridge itself dies.

---

## Operational notes

- One bridge instance should generally serve one assignee.
- Running multiple bridges for the same assignee is possible, but not usually desirable unless you intentionally want parallel task claiming behavior.
- The bridge only processes one task per poll cycle.
- `CLAIM_LIMIT` controls how many tasks are fetched, not how many are executed in parallel.
- Parallelism is currently controlled outside the bridge, by how many bridge instances you run.

---

## Minimal quickstart

```bash
cd bridge

export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
export AGENT_ASSIGNEE="johnvolldepp"
export POLL_SECONDS=5
export HEARTBEAT_SECONDS=20
export TMUX_TARGET="johnvolldepp"
export TASKS_DIR="$PWD/agent_tasks"
export DISPATCH_COMMAND='tmux send-keys -t johnvolldepp "agent-task open {file}" C-m'

mkdir -p "$TASKS_DIR"
go build -o bridge .
./bridge
```

---

## Recommended next companion tools

This bridge works best together with:

- `agent-task show <taskfile>`
- `agent-task open <taskfile>`
- `agent-task comment <taskfile>`
- `agent-task handoff <taskfile> --to bobwurst --state ready-for-review --summary "..."`
- `agent-task approve <taskfile>`

Those tools make it easy for a human or agent to consume the bridge-generated task files and write the corresponding result files.
