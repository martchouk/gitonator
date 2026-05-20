# ADR-003: Frontend Technology Stack

**Date:** 2026-05-18
**Status:** Accepted
**Author:** zed-arc

## Context

The issue specifies: React + TypeScript, Material 3 design using `@material/web`, light/dark themes switchable by the user, and a workflow graph visualisation. The frontend must be deployable as a static build to `singularia.de`.

## Decision

| Concern | Choice | Rationale |
|---------|--------|-----------|
| Framework | **React 18 + TypeScript 5** | Specified by issue |
| Build tool | **Vite 5** | Fastest HMR, first-class TS support, produces optimised static output |
| UI components | **`@material/web` (v2)** | Specified by issue; official Material 3 web components from Google |
| React / MWC bridge | **`@lit-labs/react`** | Generates typed React wrappers for Lit-based web components |
| Theme | **CSS custom properties** with `prefers-color-scheme` + `localStorage` override | M3 token system is CSS-variable-based; no runtime JS theming library needed |
| Routing | **React Router v6** | Lightweight SPA routing for Live View / Graph / Docs pages |
| Data fetching | **SWR** | Stale-while-revalidate + polling fallback; tiny bundle; works alongside SSE |
| Graph rendering | **ReactFlow v12** | Purpose-built for node/edge diagrams; handles layout, zoom, pan; MIT licence |
| Graph layout | **`@dagrejs/dagre`** | Automatic directed-graph layout for the workflow status graph |
| API docs rendering | **Embedded `<pre>` blocks + custom React components** | No heavy OpenAPI UI library; keeps bundle small |

### Directory layout

```
dashboard/
├── src/
│   ├── api/            # Typed fetch helpers (issues.ts, tasks.ts, workflows.ts, stream.ts)
│   ├── components/     # Reusable UI (IssueCard, StatusChip, ThemeToggle, WorkflowGraph)
│   ├── pages/          # Route-level components (LiveView, WorkflowViz, DocsSetup, DocsApi)
│   ├── theme/          # ThemeContext.tsx, tokens.css (M3 CSS custom properties)
│   ├── App.tsx
│   └── main.tsx
├── public/
├── package.json
├── vite.config.ts
└── tsconfig.json
```

### Theme switching

- On load, read `localStorage.getItem('theme')` → `'light' | 'dark' | null`.
- If null, default to `prefers-color-scheme`.
- Apply by toggling a `data-theme` attribute on `<html>`; CSS targets `:root[data-theme="dark"]` to override M3 colour tokens.
- User toggle writes back to `localStorage`.

## Consequences

**Positive:**
- Fully aligned with the issue specification.
- `@material/web` delivers authentic M3 visual fidelity and accessibility.
- ReactFlow + dagre produces professional-quality workflow graphs with minimal custom code.
- Vite static output is straightforward to deploy to any CDN or web server.

**Negative:**
- `@material/web` web components require `@lit-labs/react` wrappers; adds a small integration layer.
- ReactFlow is not trivial (learning curve for layout customisation), but the default dagre layout handles the workflow graph well.
- Bundle size will be moderate (~300–500 KB gzipped) due to `@material/web` components.

## Alternatives Considered

1. **MUI v6 (React-native M3 components).** Better React DX, but the issue explicitly links `material-components/material-web`. Using MUI risks diverging from the specified design system. Rejected.
2. **D3.js for graph.** More powerful but far more low-level; ReactFlow achieves the same result with a fraction of the custom code. Rejected.
3. **Next.js.** SSR is not needed; adds deployment complexity for a static site. Rejected.
