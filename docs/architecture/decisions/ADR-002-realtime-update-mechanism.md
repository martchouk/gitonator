# ADR-002: Real-time Updates via Server-Sent Events (SSE)

**Date:** 2026-05-18
**Status:** Accepted
**Author:** zed-arc

## Context

The Live Workflow View must reflect status changes without a full page reload. Options for delivering server-push updates are: polling, WebSocket, and Server-Sent Events (SSE).

The data flow is inherently one-directional: the server emits workflow state changes; the browser only reads them. Issue status changes are triggered by GitHub webhook delivery (roughly seconds to tens of seconds apart), not by high-frequency events.

## Decision

Use **Server-Sent Events (SSE)** via a `GET /api/v1/dashboard/stream` endpoint.

- Implemented in Go using standard `http.ResponseWriter` with `Content-Type: text/event-stream` and `Flusher`.
- An in-process `SSEHub` (`sync.RWMutex` + `map[chan Event]struct{}`) receives events from the webhook processing path and fans them out to all connected clients.
- Event types: `issue_updated`, `task_queued`, `task_dispatched`, `task_completed`.
- Clients that cannot connect SSE fall back to polling `/api/v1/dashboard/issues` every 10 seconds.
- A reconnection interval of 5 s is sent in the SSE `retry:` field.

## Consequences

**Positive:**
- SSE is natively supported by all modern browsers; no client library required.
- One-directional semantics match the use case perfectly.
- Standard `net/http` in Go handles SSE trivially; no external dependency.
- Automatic browser reconnection on disconnect.

**Negative:**
- SSE connections are long-lived HTTP/1.1 connections; each open browser tab holds one. At low scale (dozens of users) this is negligible.
- HTTP/1.1 has a per-domain connection limit of 6; with HTTP/2 this is not an issue. The dashboard frontend should share one SSE connection across views.

## Alternatives Considered

1. **WebSocket.** More complex handshake, bidirectional capability unused here, requires more careful goroutine lifecycle management. Rejected as over-engineered for read-only push.
2. **Client-side polling.** Simpler but wastes bandwidth and introduces latency proportional to poll interval. Kept only as SSE fallback.
3. **Long-polling.** More complex server implementation than SSE with similar trade-offs. Rejected.
