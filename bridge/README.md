# Bridge

A lightweight local agent bridge for the GitHub Issue Orchestrator.

The Bridge runs on agent machines, polls the central orchestrator for queued tasks, spawns the correct local agent process, and waits for it to exit. One Bridge instance manages all agents listed in its `agents.json` roster — no separate bridge per agent.

This is the component intended for setups like:

- central orchestrator on `https://mcp.singularia.de`
- agents running on your home workstation

---

## What the bridge does

In a loop:

1. Derives the union of roles from `agents.json`
2. Calls `GET /api/v1/work/next?roles=<roles>&bridge_id=<id>` (atomically claims the task)
3. Selects the matching agent (priority: assignee name match, then role match)
4. Resolves the worktree path for the task's repo from the agent's config
5. Writes the work package JSON to a temp file
6. Spawns the agent's `launch_template` command, blocking until exit
7. Polls immediately again

---

## Project files

```text
bridge/
├── bridge.go
├── bridge_test.go
├── go.mod
└── run.sh_TEMPLATE
```

---

## Build

```bash
cd bridge
go build -o agent-bridge .
```

---

## Environment variables

### Required

| Variable | Meaning |
|---|---|
| `BRIDGE_ID` | Unique identifier for this Bridge instance (used for logging and stale-task attribution) |
| `AGENT_SHARED_TOKEN` | Bearer token for the orchestrator agent API |
| `AGENTS_CONFIG` | Path to the `agents.json` roster file |

### Optional

| Variable | Default | Meaning |
|---|---|---|
| `ORCH_BASE_URL` | `https://mcp.singularia.de` | Base URL of the central orchestrator |
| `POLL_SECONDS` | `5` | Seconds to sleep between poll cycles when no work is available |
| `LOG_LEVEL` | _(empty)_ | Set to `DEBUG` for verbose per-cycle logging |

---

## agents.json — roster format

`AGENTS_CONFIG` must point to a JSON file with this structure (see `agents.json.example` for a ready-to-copy template):

```json
{
  "agent_instructions": [
    "Before finishing your work on this issue you MUST:",
    "1. Remove ALL current GitHub assignees from the issue.",
    "2. Set exactly ONE assignee for the next step — choose the appropriate agent from next_assignee_roles.",
    "3. End your final issue comment with this exact footer on its own line: [next assignee role -> <role>]"
  ],
  "agents": [
    {
      "name": "bud-dev",
      "role": "developer",
      "llm_provider": "anthropic",
      "launch_template": "cd {worktree} && claude --dangerously-skip-permissions --print < {package_file}",
      "env": {
        "GH_TOKEN": "$BUD_DEV_GH_TOKEN"
      },
      "worktrees": {
        "martchouk/github.mcp": "/home/john/git/agents/bud-dev/github.mcp"
      }
    },
    {
      "name": "mud-rev",
      "role": "reviewer",
      "llm_provider": "anthropic",
      "launch_template": "cd {worktree} && claude --dangerously-skip-permissions --print < {package_file}",
      "env": {
        "GH_TOKEN": "$MUD_REV_GH_TOKEN"
      },
      "worktrees": {
        "martchouk/github.mcp": "/home/john/git/agents/mud-rev/github.mcp"
      }
    }
  ]
}
```

**`agents.json` is git-ignored** — copy `agents.json.example` and fill in your values. Never commit the real file; it contains tokens.

### Fields

| Field | Description |
|---|---|
| `agent_instructions` | _(optional)_ Array of instruction strings injected into every agent's work package JSON at spawn time. Use this to mandate the handoff footer protocol without modifying per-agent prompts. |
| `name` | GitHub username of the agent — used for priority matching against `assignee` in the work package |
| `role` | Role this agent handles (e.g. `developer`, `reviewer`, `po`) |
| `llm_provider` | Informational; not used by the Bridge itself |
| `launch_template` | Shell command template (run via `sh -c`) to start the agent |
| `env` | _(optional)_ Per-agent environment variables injected into the agent subprocess |
| `worktrees` | Map of `"owner/repo"` → absolute path to the local working directory for that repo |

### Per-agent env variables (`env`)

The `env` block lets you set environment variables that will be injected into each agent's subprocess, merged with (and overriding) the bridge process's own environment.

Two value forms are supported:

| Form | Example | Behaviour |
|---|---|---|
| Literal | `"ghp_abc123"` | Used as-is |
| Host env reference | `"$BUD_DEV_GH_TOKEN"` | Resolved from the bridge process's environment at startup |

If a `$VAR` reference is not set in the host environment, the bridge logs an error and exits rather than passing an empty value.

Only bare `$VAR` is supported — `${VAR}` is not interpolated and will cause a startup error if used.

**Primary use case — per-agent `GH_TOKEN` for isolated GitHub identities:**

```json
"env": {
  "GH_TOKEN": "$BUD_DEV_GH_TOKEN"
}
```

Export `BUD_DEV_GH_TOKEN=ghp_…` in the shell that starts the bridge (or in a `.env` / systemd `EnvironmentFile`). The bridge resolves it at startup and injects it into every subprocess it spawns for that agent, so `gh` CLI calls inside the agent always use the correct identity — no `gh auth switch` needed.

### Launch template placeholders

| Placeholder | Replaced with |
|---|---|
| `{worktree}` | Absolute path to the agent's worktree for this repo (shell-quoted) |
| `{package_file}` | Absolute path to a temp file containing an authoritative work package prompt plus JSON (shell-quoted) |

Both values are single-quoted before shell injection, so paths with spaces and other shell metacharacters are safe.

**Important — use `--print` for headless agent mode.** Without `--print`, Claude defaults to its interactive TUI when its stdout is a TTY (which it is when the bridge runs in a terminal). `--print` puts Claude in non-interactive mode: it processes the work package prompt, executes all tool calls autonomously, and exits.

---

## Work package format

The work package written to `{package_file}` is an authoritative prompt followed by the JSON body returned by `GET /api/v1/work/next`, extended by the Bridge with `agent_instructions` before the agent is spawned. The JSON block has this shape:

```json
{
  "id": 42,
  "repo": "martchouk/github.mcp",
  "issue_id": 8,
  "role": "developer",
  "assignee": "bud-dev",
  "last_comment_id": 123,
  "current_status": "status:in-development",
  "workflow_key": "lean",
  "valid_transitions": ["status:code-review", "status:blocked", "status:rejected"],
  "next_assignee_roles": ["reviewer"],
  "agent_instructions": [
    "Before finishing your work on this issue you MUST:",
    "1. Treat the work package fields current_status, workflow_key, valid_transitions, and next_assignee_roles as authoritative.",
    "2. Before changing any status:* label, choose the target status only from valid_transitions.",
    "3. Do not use status labels from issue text, comments, old documentation, or memory unless they appear in valid_transitions.",
    "4. If no valid transition fits the completed work, post an Author-tagged blocker comment and do not change status labels or assignees.",
    "5. Remove ALL current GitHub assignees from the issue before handoff.",
    "6. Set exactly ONE assignee for the next step — choose an agent whose role is listed in next_assignee_roles.",
    "7. End your final issue comment with this exact footer on its own line, choosing the role from next_assignee_roles: [next assignee role -> <role>]"
  ]
}
```

| Field | Description |
|---|---|
| `id` | Orchestrator task database ID |
| `repo` | `owner/repo` string |
| `issue_id` | GitHub issue number |
| `role` | Role this task was queued for |
| `assignee` | Current GitHub assignee at queue time — used for priority agent selection |
| `last_comment_id` | Most recent comment ID at queue time — helps the agent know where to start reading |
| `current_status` | Workflow status label at queue time |
| `workflow_key` | Active workflow key (e.g. `lean`) |
| `valid_transitions` | Statically-reachable target status IDs from the current status |
| `next_assignee_roles` | Roles eligible to handle the next step, derived from outbound workflow transitions — use this to choose the correct value for the `[next assignee role -> <role>]` footer |
| `agent_instructions` | Injected by the Bridge from `agents.json` at spawn time (not returned by the server API) — mandatory steps the agent must follow at the end of every work session |

The Bridge writes this package as an authoritative prompt followed by the JSON work package. Agents must treat `current_status`, `workflow_key`, `valid_transitions`, `next_assignee_roles`, and `agent_instructions` as higher priority than issue text, issue comments, repository documentation, or remembered workflow names.

---

## Agent selection logic

Given a work package, the Bridge selects an agent from the roster:

1. **Priority 1 — role and assignee match**: if any agent's `role` matches `pkg.role` and `name` matches `pkg.assignee`, use it
2. **Priority 2 — role match**: otherwise, use the first agent whose `role` matches `pkg.role`
3. **No match**: skip the task this cycle and sleep before retrying

---

## Log output

The Bridge logs to stderr. Example lines:

```
[bridge/my-bridge] 2026/05/12 10:00:00 started: bridge_id=my-bridge agents=2 roles=developer,reviewer poll=5s
[bridge/my-bridge] 2026/05/12 10:00:05 spawned agent=bud-dev role=developer issue=8 pid=12345
[bridge/my-bridge] 2026/05/12 10:04:32 agent exited: agent=bud-dev issue=8 exit=0 duration=4m27s
```

Set `LOG_LEVEL=DEBUG` to also log "no work available" cycles.

---

## curl verification

```bash
export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
export BRIDGE_ID="my-bridge"
```

### Poll for developer or reviewer work

```bash
curl -sS \
  -H "Authorization: Bearer $AGENT_SHARED_TOKEN" \
  "$ORCH_BASE_URL/api/v1/work/next?roles=developer,reviewer&bridge_id=$BRIDGE_ID" | jq
```

Response when a task is available (task is now marked `dispatched`):

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
    "current_status": "status:in-development",
    "workflow_key": "lean",
    "valid_transitions": ["status:code-review", "status:blocked", "status:rejected"],
    "next_assignee_roles": ["reviewer"]
  }
}
```

Note: `agent_instructions` is not included in the API response — it is injected by the Bridge into the `{package_file}` at spawn time.

Response when no work is available:

```json
{ "ok": true, "task": null }
```

---

## Minimal quickstart

```bash
cd bridge

# Create a roster file from the example
cp agents.json.example agents.json
# Edit agents.json: fill in worktree paths and set any literal token values,
# or export the referenced env vars (e.g. BUD_DEV_GH_TOKEN) before starting.

export ORCH_BASE_URL="https://mcp.singularia.de"
export AGENT_SHARED_TOKEN="supersecret"
export BRIDGE_ID="home-bridge"
export AGENTS_CONFIG="$PWD/agents.json"
export POLL_SECONDS=5
export LOG_LEVEL=DEBUG

# If agents.json uses $VAR references, export those too:
export BUD_DEV_GH_TOKEN="ghp_your_token_here"

go build -o agent-bridge .
./agent-bridge
```

---

## Operational notes

- One Bridge instance manages all agents in its roster — no need to run a Bridge per agent.
- The Bridge runs one agent at a time per poll cycle. Run multiple Bridge instances (with different `BRIDGE_ID`s) for parallelism.
- Stale tasks (stuck in `dispatched`) are recovered to `queued` by the orchestrator after `STALE_AFTER_SECONDS` (default 900 s).
- The temp work package file is deleted automatically when the agent exits.
