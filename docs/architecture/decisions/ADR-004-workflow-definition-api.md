# ADR-004: Serving Workflow Definitions for Graph Visualisation

**Date:** 2026-05-18
**Status:** Accepted
**Author:** zed-arc

## Context

The dashboard's Workflow Visualisation page must render each workflow as a directed graph of status nodes and transition edges. The codebase has two representations of the workflow:

1. **YAML files** in `workflows/` — human-readable, complete definition including guards, categories, and `queues_next_role`. These are the canonical design documents.
2. **Go `TransitionRule` structs** in `workflow.go` — the runtime validation rules, hardcoded for the current (legacy) workflow.

The YAML files for the `full` workflow (`workflow-full-6-roles-issue.yaml`) contain richer metadata (guards, issue-type paths, status categories) than the Go structs. Additionally, the orchestrator already selects a `workflow_key` per task (currently `"full"` or `"lean"`), implying multiple workflow definitions should be supported.

## Decision

**Parse the YAML workflow definition files at server startup** and cache them in memory. Expose the parsed definitions via the dashboard API.

- A new `workflow_yaml.go` file defines Go structs mirroring the YAML schema (`WorkflowDef`, `StatusDef`, `TransitionDef`, `GuardDef`, etc.).
- At startup, `loadWorkflowDefs(dir string)` reads `*.yaml` files from the `workflows/` directory and unmarshals them using the standard `gopkg.in/yaml.v3` package (one new dependency).
- The parsed definitions are stored in a `map[string]*WorkflowDef` keyed by `workflow.id`.
- The dashboard API exposes:
  - `GET /api/v1/workflows` — list of all workflow IDs and descriptions
  - `GET /api/v1/workflows/{id}` — full definition including statuses, transitions, and guards as JSON (ready for ReactFlow)

### Graph data shape (JSON response)

```json
{
  "id": "lean_github_issue_workflow",
  "description": "...",
  "nodes": [
    { "id": "status:triage", "role": "po", "category": "intake", "terminal": false }
  ],
  "edges": [
    {
      "id": "po_start_triage",
      "source": "status:triage",
      "target": "status:triage",
      "allowedRoles": ["po"],
      "guard": null,
      "description": "PO starts or continues triage."
    }
  ]
}
```

This shape is directly usable by ReactFlow without transformation on the client.

## Consequences

**Positive:**
- YAML files are the source of truth; the API reflects them exactly, including guards and issue-type paths not available in the Go structs.
- Adding a new workflow is just adding a YAML file — no Go code changes required.
- `gopkg.in/yaml.v3` is a well-maintained, zero-CVE library.

**Negative:**
- One new Go dependency (`gopkg.in/yaml.v3`). Acceptable given the existing dependency count (one: `go-sqlite3`).
- If YAML files are edited while the server is running, a restart is required to pick up changes. Hot-reload is out of scope for v1.
- YAML parsing errors at startup are fatal; workflow directory must be present.

## Alternatives Considered

1. **Expose the hardcoded `TransitionRule` structs from `workflow.go`.** Rejected: incomplete metadata (no guards, no categories, no issue-type paths), and tightly couples the API to the legacy workflow implementation rather than the canonical YAML files.
2. **Generate the graph client-side from YAML fetched as raw text.** Rejected: requires a YAML parser in the frontend bundle (non-trivial size), duplicates parsing logic, and violates the principle that the Go server owns the workflow definition schema.
3. **Store workflow definitions in SQLite.** Rejected: adds migration complexity and a management UI for what are essentially configuration files. YAML files in the repo are simpler and version-controlled.
