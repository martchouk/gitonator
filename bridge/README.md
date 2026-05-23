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
3. Selects the matching agent (explicit assignee, then issue-role affinity, then role round-robin)
4. Resolves the worktree path for the task's repo from the agent's config
5. Acquires a `.agent-bridge.lock` file in the worktree to prevent concurrent bridge processes
6. Writes the work package JSON to a temp file
7. Spawns the agent using the structured `command`/`args`/`stdin`/`workdir` config (or legacy `launch_template` with `ENABLE_LEGACY_LAUNCH_TEMPLATE=true`), blocking until exit
8. On capacity unavailability (all agents busy, provider cooling, worktree locked): calls `POST /api/v1/work/release` to return the task to the queue without recording a failure
9. Handles `SIGINT`/`SIGTERM` gracefully — stops polling and waits for running agents to exit

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
| `MODEL` | `MODEL_POLICY.default_profile` | Semantic model profile to resolve for every agent, e.g. `basic`, `standard`, `advanced`, `premium` |
| `MODEL_POLICY` | _(empty)_ | Path to a model-policy JSON file used to fill `{model_args}` in agent args |
| `POLL_SECONDS` | `5` | Seconds to sleep between poll cycles when no work is available |
| `AGENT_TIMEOUT_SECONDS` | `3600` | Kill the agent process after this many seconds; reported as a failure |
| `AGENT_FAILURE_COOLDOWN_SECONDS` | `300` | Seconds to pause a provider after transient failures such as quota exhaustion, rate limits, or network errors |
| `ENABLE_LEGACY_LAUNCH_TEMPLATE` | `false` | Set to `true` to allow agents with a `launch_template` field; required for `{worktree}` shell expansion via `sh -c` |
| `WORK_RELEASE_FALLBACK_TO_FAIL` | `true` | When `/work/release` is unavailable, fall back to `/work/fail` with exit code `-2` |
| `LOG_LEVEL` | _(empty)_ | Set to `DEBUG` for verbose per-cycle logging |

---

## agents.json — roster format

`AGENTS_CONFIG` must point to a JSON file with this structure (see `agents.json.example` for a ready-to-copy template):

```json
{
  "agent_instructions": [
    "Before finishing your work on this issue you MUST:",
    "1. Treat current_status, workflow_key, type_labels, valid_transitions, next_assignee_roles, and past_workers as authoritative.",
    "2. Do not choose or hardcode concrete GitHub usernames for the next step.",
    "3. If you write the final routing footer, remove all current GitHub issue assignees before finishing.",
    "4. End your single final issue comment with this exact footer on its own line: [next assignee role -> <role>]",
    "5. If type_labels contains type:smoke-test, treat this as a no-code workflow-routing smoke test."
  ],
  "agents": [
    {
      "name": "bud-dev",
      "role": "developer",
      "llm_provider": "anthropic",
      "command": "claude",
      "args": ["{model_args}", "--dangerously-skip-permissions", "--print"],
      "stdin": "{package_file}",
      "workdir": "{worktree}",
      "env": {
        "GH_TOKEN": "$BUD_DEV_GH_TOKEN"
      },
      "worktrees": {
        "martchouk/gitonator": "/home/john/git/agents/bud-dev/gitonator"
      }
    },
    {
      "name": "mud-rev",
      "role": "reviewer",
      "llm_provider": "anthropic",
      "command": "claude",
      "args": ["{model_args}", "--dangerously-skip-permissions", "--print"],
      "stdin": "{package_file}",
      "workdir": "{worktree}",
      "env": {
        "GH_TOKEN": "$MUD_REV_GH_TOKEN"
      },
      "worktrees": {
        "martchouk/gitonator": "/home/john/git/agents/mud-rev/gitonator"
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
| `name` | GitHub username of the agent — used for explicit-assignee matching and issue-role affinity |
| `role` | Role this agent handles (e.g. `developer`, `reviewer`, `po`) |
| `llm_provider` | Provider key used for provider-level cooldown after transient failures |
| `command` | Executable to run (e.g. `claude`). Use this instead of `launch_template`. |
| `args` | List of arguments for `command`. Supports `{model_args}`, `{worktree}`, `{package_file}` placeholders. `{model_args}` expands to one or more args from the model policy. |
| `stdin` | _(optional)_ Path to a file to pipe to the process's stdin. Supports `{package_file}`. |
| `workdir` | _(optional)_ Working directory for the process. Supports `{worktree}`. Defaults to `{worktree}`. |
| `launch_template` | _(legacy, requires `ENABLE_LEGACY_LAUNCH_TEMPLATE=true`)_ Shell command template run via `sh -c`. Supports `{model_args}`, `{worktree}`, `{package_file}`. Prefer `command`/`args` instead. |
| `env` | _(optional)_ Per-agent environment variables injected into the agent subprocess. The agent receives a minimal, controlled environment — this is the only way to pass tokens or other secrets. |
| `worktrees` | Map of `"owner/repo"` → absolute path to the local working directory for that repo |

### Structured command format (`command`/`args`/`stdin`/`workdir`)

The preferred way to configure agent launch is with individual fields rather than a shell template:

```json
"command": "claude",
"args": ["{model_args}", "--dangerously-skip-permissions", "--print"],
"stdin": "{package_file}",
"workdir": "{worktree}"
```

Placeholders available in `args`, `stdin`, and `workdir`:

| Placeholder | Replaced with |
|---|---|
| `{model_args}` | One or more args from the resolved model policy (only valid as a **standalone** element in `args`) |
| `{worktree}` | Absolute path to the agent's worktree for this repo |
| `{package_file}` | Absolute path to a temp file containing the authoritative work package prompt and JSON |

Each arg is passed directly to the OS — no shell quoting, no `sh -c`. This avoids shell injection and is safer than `launch_template`.

### Legacy launch_template (opt-in)

If you have existing config using `launch_template`, set `ENABLE_LEGACY_LAUNCH_TEMPLATE=true`. All placeholders are shell-quoted before injection into `sh -c`:

```bash
export ENABLE_LEGACY_LAUNCH_TEMPLATE=true
```

```json
"launch_template": "cd {worktree} && claude {model_args} --dangerously-skip-permissions --print < {package_file}"
```

Roster validation rejects `launch_template` entries when `ENABLE_LEGACY_LAUNCH_TEMPLATE` is false.

### Model policy and `{model_args}`

If an agent `args` list contains `"{model_args}"`, the Bridge resolves it at startup from `MODEL_POLICY` and `MODEL`.

Example:

```bash
export MODEL="standard"
export MODEL_POLICY="$HOME/model-policy.json"
```

```json
{
  "default_profile": "standard",
  "fallbacks": {
    "basic": ["basic"],
    "standard": ["standard", "basic"],
    "advanced": ["advanced", "standard", "basic"],
    "premium": ["premium", "advanced", "standard", "basic"]
  },
  "role_profiles": {
    "developer": "advanced",
    "reviewer": "standard",
    "po": "basic"
  },
  "providers": {
    "anthropic": {
      "basic": { "model": "haiku", "args": ["--model", "haiku"] },
      "standard": { "model": "sonnet", "args": ["--model", "sonnet"] },
      "advanced": { "model": "opus", "args": ["--model", "opus"] }
    },
    "openai": {
      "basic": { "model": "gpt-5.4-mini", "args": ["--model", "gpt-5.4-mini"] },
      "standard": { "model": "gpt-5.3-codex", "args": ["--model", "gpt-5.3-codex"] },
      "advanced": { "model": "gpt-5.4", "args": ["--model", "gpt-5.4"] },
      "premium": { "model": "gpt-5.5", "args": ["--model", "gpt-5.5"] }
    }
  }
}
```

#### Per-role model profiles (`role_profiles`)

`role_profiles` lets you assign a different model profile to each role without changing the global `MODEL` setting:

```json
"role_profiles": {
  "developer": "advanced",
  "reviewer": "standard",
  "po": "basic"
}
```

If a role has no entry in `role_profiles`, the global `MODEL` (or `default_profile`) is used. Role profiles take priority over the global setting.

Provider model counts do not need to match. The selected profile is resolved through the configured fallback chain. For example, `MODEL=premium` uses OpenAI `premium` if present; Anthropic has no `premium`, so it falls back to `advanced`.

The Bridge validates all `{model_args}` templates at startup. If a provider/profile cannot be resolved, it exits before polling work.

### Per-agent env variables (`env`)

The `env` block sets environment variables that will be injected into each agent's subprocess. Child agents receive a **minimal, controlled environment** (PATH, LANG, HOME, USER, SHELL, TERM, TMPDIR) instead of the full parent environment. This prevents accidental leakage of PATs and `AGENT_SHARED_TOKEN` into every agent process. Use `env` to explicitly pass the credentials each agent needs.

Two value forms are supported:

| Form | Example | Behaviour |
|---|---|---|
| Literal | `"ghp_abc123"` | Used as-is |
| Host env reference | `"$BUD_DEV_GH_TOKEN"` | Resolved from the bridge process's environment at startup |
| Braced reference | `"${BUD_DEV_GH_TOKEN}"` | Resolved from the bridge process's environment at startup |

If a host environment reference is not set, the bridge logs an error and exits rather than passing an empty value. Malformed values such as `$BUD_DEV_GH_TOKEN}` or `${BUD_DEV_GH_TOKEN` fail fast at startup.

**Primary use case — per-agent `GH_TOKEN` for isolated GitHub identities:**

```json
"env": {
  "GH_TOKEN": "$BUD_DEV_GH_TOKEN"
}
```

Export `BUD_DEV_GH_TOKEN=ghp_…` in the shell that starts the bridge. The bridge resolves it at startup and injects it into every subprocess for that agent.

---

## Work package format

The work package written to `{package_file}` is an authoritative prompt followed by the JSON body returned by `GET /api/v1/work/next`, extended by the Bridge with `agent_instructions` before the agent is spawned. The JSON block has this shape:

```json
{
  "id": 42,
  "repo": "martchouk/gitonator",
  "issue_id": 8,
  "role": "developer",
  "assignee": "bud-dev",
  "past_workers": ["bud-dev", "mud-rev"],
  "last_comment_id": 123,
  "current_status": "status:in-development",
  "workflow_key": "lean",
  "type_labels": ["type:smoke-test"],
  "valid_transitions": ["status:code-review", "status:blocked", "status:rejected"],
  "next_assignee_roles": ["reviewer"],
  "agent_instructions": [
    "Before finishing your work on this issue you MUST:",
    "1. Treat the work package fields current_status, workflow_key, type_labels, valid_transitions, next_assignee_roles, and past_workers as authoritative.",
    "2. Before changing any status:* label, choose the target status only from valid_transitions.",
    "3. Do not use status labels from issue text, comments, old documentation, or memory unless they appear in valid_transitions.",
    "4. If no valid transition fits the completed work, post one Author-tagged blocker comment and do not change status labels or routing state.",
    "5. Do not choose or hardcode concrete GitHub usernames for the next step. Route handoff by role only.",
    "6. Do not add a concrete next assignee unless the work package explicitly instructs you to use that exact user. The bridge chooses an available agent from the role pool.",
    "7. If you write the final routing footer, remove all current GitHub issue assignees before finishing. Do not add the next agent as a concrete assignee; the bridge assigns the next agent from the role pool.",
    "8. End your single final issue comment with this exact footer on its own line, choosing the role from next_assignee_roles: [next assignee role -> <role>]",
    "9. If type_labels contains type:smoke-test, treat this as a no-code workflow-routing smoke test: do not create branches, commits, PRs, review artifacts, or implementation changes unless the work package explicitly asks for them. Do not treat unrelated failing tests or missing PRs as blockers; mention them briefly and continue routing."
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
| `past_workers` | Author-tagged workers and completed task assignees already seen on the issue; used for issue-role stickiness |
| `last_comment_id` | Most recent comment ID at queue time — helps the agent know where to start reading |
| `current_status` | Workflow status label at queue time |
| `workflow_key` | Active workflow key (e.g. `lean`) |
| `type_labels` | Current `type:*` labels at queue time. `type:smoke-test` marks a no-code workflow-routing smoke test. |
| `valid_transitions` | Statically-reachable target status IDs from the current status |
| `next_assignee_roles` | Roles eligible to handle the next step, derived from outbound workflow transitions |
| `agent_instructions` | Injected by the Bridge from `agents.json` at spawn time (not returned by the server API) — mandatory steps the agent must follow at the end of every work session |

---

## Agent selection logic

Given a work package, the Bridge selects an agent from the roster:

1. **Priority 1 — explicit assignee**: if any available agent's `role` matches `pkg.role` and `name` matches `pkg.assignee`, use it
2. **Priority 2 — issue-role affinity**: otherwise, prefer the most recent available `past_workers` entry whose roster role matches `pkg.role`
3. **Priority 3 — role round-robin**: otherwise, rotate across available agents whose `role` matches `pkg.role`
4. **No match**: release the task via `/work/release` and sleep before retrying

Providers that recently failed with a transient resource error (quota, rate limit, session limit, etc.) are put into an in-memory cooldown and skipped until the cooldown expires. If all matching agents are on cooling providers, the Bridge calls `/work/release` to requeue the task and sleeps until a matching provider becomes available, preventing tight claim/fail/requeue loops.

Worktrees are locked via `.agent-bridge.lock` files. If a worktree is already locked by another agent or bridge process, the task is released and retried without recording a failure.

---

## Graceful shutdown

The Bridge handles `SIGINT` and `SIGTERM` by:

1. Stopping the poll loop immediately
2. Waiting for all running agent processes to finish (`wg.Wait()`)
3. Logging "shutdown complete" and exiting

Tasks claimed by agents that are still running at shutdown time will be recovered by the orchestrator's stale-task recovery after `STALE_AFTER_SECONDS` (default 900 s).

---

## Log output

The Bridge logs to stderr. Example lines:

```
[bridge/my-bridge] 2026/05/12 10:00:00 started: bridge_id=my-bridge agents=2 roles=developer,reviewer poll=5s timeout=1h0m0s
[bridge/my-bridge] 2026/05/12 10:00:05 agent started: agent=bud-dev issue=8 task=42 worktree=/home/john/git/agents/bud-dev/gitonator
[bridge/my-bridge] 2026/05/12 10:00:05 spawned agent=bud-dev role=developer issue=8 pid=12345
[bridge/my-bridge] 2026/05/12 10:04:32 agent exited: agent=bud-dev issue=8 exit=0 duration=4m27s
[bridge/my-bridge] 2026/05/12 10:04:32 agent finished: agent=bud-dev issue=8 task=42
```

Set `LOG_LEVEL=DEBUG` to also log "no work available" cycles and per-agent model selections.

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
    "repo": "martchouk/gitonator",
    "issue_id": 8,
    "role": "developer",
    "assignee": "bud-dev",
    "last_comment_id": 123,
    "current_status": "status:in-development",
    "workflow_key": "lean",
    "type_labels": ["type:smoke-test"],
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

### Release a task when no local capacity is available

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $AGENT_SHARED_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"task_id":42,"issue_id":8,"bridge_id":"my-bridge","reason":"no_available_agent","detail":"all matching agents are cooling down","retry_after_seconds":30}' \
  "$ORCH_BASE_URL/api/v1/work/release" | jq
```

Response:

```json
{ "ok": true, "released": true, "retry_after_seconds": 30 }
```

### Report a real agent failure

```bash
curl -sS -X POST \
  -H "Authorization: Bearer $AGENT_SHARED_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"task_id":42,"issue_id":8,"bridge_id":"my-bridge","agent":"bud-dev","exit_code":1,"error_text":"agent process exited with error"}' \
  "$ORCH_BASE_URL/api/v1/work/fail" | jq
```

---

## Exit codes reported to `/work/fail`

| Code | Meaning |
|---|---|
| `-1` | Generic bridge/agent spawn failure |
| `-2` | Capacity fallback — `/work/release` unavailable, `WORK_RELEASE_FALLBACK_TO_FAIL=true` |
| `-3` | Bridge configuration or model/launch validation failure |
| `-4` | Agent timeout (exceeded `AGENT_TIMEOUT_SECONDS`) |

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
export MODEL="standard"
export MODEL_POLICY="$PWD/model-policy.json"
export POLL_SECONDS=5
export AGENT_TIMEOUT_SECONDS=3600
export AGENT_FAILURE_COOLDOWN_SECONDS=300
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
- Each agent's worktree is protected by a `.agent-bridge.lock` file. A stale lock from a crashed bridge can be removed manually.
- Agent subprocesses receive a minimal environment (PATH, LANG, HOME, USER, SHELL, TERM, TMPDIR) rather than the full parent env. All tokens and secrets must be passed explicitly via the `env` block.
