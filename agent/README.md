# agent-task

`agent-task` is a small local CLI helper for working with work package files delivered by the Bridge.

It is designed to be used by a human operator, a local AI agent, or any automated process that receives a work package JSON file.

The tool reads one work package JSON file and lets you:

- inspect the package
- open the linked GitHub issue in a browser
- post a GitHub comment directly via the GitHub API
- post comments directly via the GitHub API

Comments go directly to GitHub using the agent's own `GITHUB_TOKEN` — the orchestrator is not involved.

---

## Where it fits

```
orchestrator → (webhook transition) → queues task
bridge       → (polls /api/v1/work/next) → writes work package JSON to temp file → spawns agent
agent        → reads work package JSON → uses agent-task to inspect / comment
```

Division of responsibility:

- **orchestrator**: workflow engine, state machine, queue, GitHub label + assignee management
- **bridge**: polling, atomic task dispatch, agent spawning
- **agent-task**: operator/agent helper for acting on a single work package

---

## Project files

```text
agent/
├── agent-task
├── agent-task.go
├── build.sh
└── go.mod
```

---

## Build

```bash
cd agent
go build -o agent-task .
```

Or:

```bash
./build.sh
```

---

## Commands

```text
agent-task show    <package-file>
agent-task open    <package-file>
agent-task comment <package-file> --message "..." [--field key=value]...
```

---

## Command overview

### `show`

Prints a readable summary of the work package, including:

- task ID
- repo
- issue ID and URL
- role
- assignee
- last comment ID
- current workflow status

Example:

```bash
agent-task show /tmp/work-42-issue-8.json
```

Output:

```
Package file:   /tmp/work-42-issue-8.json
Task ID:        42
Repo:           martchouk/github.mcp
Issue ID:       8
Issue URL:      https://github.com/martchouk/github.mcp/issues/8
Role:           developer
Assignee:       bud-dev
Last comment:   123
Current status: status:in-development
```

### `open`

Opens the issue URL in the default browser.

Supported platforms:

- macOS: `open`
- Linux: `xdg-open`
- Windows: `rundll32 url.dll,FileProtocolHandler`

Example:

```bash
agent-task open /tmp/work-42-issue-8.json
```

### `comment`

Posts a GitHub issue comment directly to the GitHub API.

Required:

- `--message "..."`

Optional:

- multiple `--field key=value` (appended as bullet lines to the comment body)

Example:

```bash
agent-task comment /tmp/work-42-issue-8.json \
  --message "Implementation finished, opening PR." \
  --field pr="https://github.com/martchouk/github.mcp/pull/5"
```

Resulting comment body:

```text
Implementation finished, opening PR.

- pr: https://github.com/martchouk/github.mcp/pull/5
```

---

## Work package format

`agent-task` reads the JSON work package written by the Bridge to a temp file.

```json
{
  "id": 42,
  "repo": "martchouk/github.mcp",
  "issue_id": 8,
  "role": "developer",
  "assignee": "bud-dev",
  "last_comment_id": 123,
  "current_status": "status:in-development"
}
```

| Field | Description |
|---|---|
| `id` | Orchestrator task database ID |
| `repo` | `owner/repo` string |
| `issue_id` | GitHub issue number |
| `role` | Role this task was queued for |
| `assignee` | Current GitHub assignee at queue time |
| `last_comment_id` | Most recent comment ID at queue time — use to know where to start reading |
| `current_status` | Workflow status label at queue time |

---

## Environment variables

`agent-task` uses these environment variables for `comment`:

| Variable | Required | Purpose |
|---|---|---|
| `GITHUB_TOKEN` | **yes** | Agent's own GitHub token for posting comments |
| `GITHUB_OWNER` | fallback | Owner portion of `owner/repo`, used if `pkg.repo` is empty |
| `GITHUB_REPO` | fallback | Repo name, used if `pkg.repo` is empty |

`show` and `open` require no environment variables.

### Example

```bash
export GITHUB_TOKEN="ghp_yourtoken"
```

---

## curl equivalent for comment

What `agent-task comment` effectively sends:

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  -H "Content-Type: application/json" \
  -d '{"body": "Implementation finished.\n\n- pr: https://github.com/martchouk/github.mcp/pull/5"}' \
  "https://api.github.com/repos/martchouk/github.mcp/issues/8/comments" | jq
```

---

## Usage examples

### Inspect a work package

```bash
agent-task show /tmp/work-42-issue-8.json
```

### Open the issue in browser

```bash
agent-task open /tmp/work-42-issue-8.json
```

### Post a comment

```bash
export GITHUB_TOKEN="ghp_yourtoken"
agent-task comment /tmp/work-42-issue-8.json \
  --message "Review completed. See findings in the issue thread." \
  --field outcome=changes-requested
```

---

## Exit behavior

- On success: exits with status `0`
- On invalid usage or runtime error: exits with non-zero and prints usage text to stderr

---

## Minimal quickstart

```bash
cd agent
export GITHUB_TOKEN="ghp_yourtoken"
go build -o agent-task .
./agent-task show /tmp/work-42-issue-8.json
```
