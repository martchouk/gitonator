# ADR-001: Dashboard API — Separate HTTP Server on Port 6666

**Date:** 2026-05-18
**Status:** Accepted
**Author:** zed-arc

## Context

The web dashboard needs a backend REST API. The existing `github-issue-orchestrator` server already runs an HTTP server (default `127.0.0.1:7777`) that handles GitHub webhooks (`/webhook/github`) and internal agent-facing endpoints (`/api/v1/work/next`, `/mcp/tools/*`). Those endpoints require HMAC signature verification and bearer-token auth respectively; exposing an unauthenticated dashboard on the same mux would require surgical per-route auth exemptions that are error-prone.

The issue specifies port 6666 as the free port available for the dashboard API.

## Decision

Run a **second `net/http` server** inside the same process, bound to `127.0.0.1:6666`, serving only dashboard API routes.

- The new server shares the same `*Store` (SQLite) and in-memory SSE broadcast hub.
- Its routes are mounted in a new `dashboard_server.go` file and started/stopped alongside the existing server via the existing `context.Context` / `errgroup` lifecycle.
- No auth is applied to the dashboard server (v1 scope: trusted internal network).
- The existing server on port 7777 is unchanged.

## Consequences

**Positive:**
- Zero changes to existing webhook / MCP auth surface — no risk of accidental auth bypass.
- Port 6666 is clearly the "dashboard port"; operators know where to point their reverse proxy.
- Dashboard routes can be iterated and deployed independently.

**Negative:**
- Two `http.Server` instances to manage; small operational overhead.
- Shared-state concurrency must be respected (all dashboard reads are read-only against the SQLite WAL-mode database; the SSE hub is guarded by a mutex).

## Alternatives Considered

1. **Add dashboard routes to the existing 7777 server with per-route auth bypass.** Rejected: complicates the auth model and risks exposing privileged endpoints.
2. **Separate process / microservice.** Rejected: adds deployment complexity (process supervision, IPC, shared DB access); overkill for v1.
3. **Use a reverse proxy (nginx) to split traffic on 6666→7777.** Rejected: doesn't solve the auth bypass problem; adds an infrastructure dependency not in scope.
