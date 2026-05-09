# GitHub Issue Orchestrator MCP

A Go-based GitHub issue workflow orchestrator for multi-agent collaboration.

It supports:

- MCP-style stdio JSON-RPC tools
- GitHub issue/comment workflow orchestration
- strict transition validation
- structured comment parsing
- webhook ingestion from GitHub
- SQLite persistence
- simple local agent dispatch through files, shell commands, or tmux

## Roles

Default role mapping:

- PO: `thebesserwisser`
- Developer: `johnvolldepp`
- Reviewer: `bobwurst`

Stakeholder is resolved in this order:

1. `STAKEHOLDER_OVERRIDE` env var
2. `stakeholder:` issue label
3. `stakeholder:` field inside `[po-analysis]`
4. issue creator

## Workflow states

Supported `status:*` labels:

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

## Structured comment formats

### PO analysis

```text
[po-analysis]
type: feature
stakeholder: alicehuman
summary: Prepare user story and define acceptance criteria.
[/po-analysis]
