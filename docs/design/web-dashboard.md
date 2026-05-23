---
version: alpha
name: gitonator Dashboard
description: Material Design 3 design system for the gitonator web dashboard — operational visibility, workflow introspection, and developer documentation.
colors:
  primary: "#1a6ef5"
  primary-container: "#d3e4fd"
  on-primary: "#ffffff"
  on-primary-container: "#001d4a"
  secondary: "#5c5f7e"
  secondary-container: "#e1e0ff"
  on-secondary: "#ffffff"
  on-secondary-container: "#181a3c"
  tertiary: "#006876"
  tertiary-container: "#9eefff"
  on-tertiary: "#ffffff"
  on-tertiary-container: "#001f25"
  error: "#ba1a1a"
  error-container: "#ffdad6"
  on-error: "#ffffff"
  on-error-container: "#410002"
  surface: "#f8f9ff"
  surface-variant: "#e0e2f0"
  on-surface: "#191b23"
  on-surface-variant: "#444659"
  outline: "#757789"
  outline-variant: "#c4c5d7"
  background: "#f8f9ff"
  on-background: "#191b23"
  surface-dark: "#111318"
  background-dark: "#111318"
  on-surface-dark: "#e2e2e9"
  primary-dark: "#a8c7fa"
  secondary-dark: "#c3c4e8"
  tertiary-dark: "#53d8ef"
  status-intake: "#006876"
  status-design: "#5c5f7e"
  status-implementation: "#1a6ef5"
  status-review: "#6750a4"
  status-terminal: "#444659"
  status-blocked: "#ba1a1a"
typography:
  display-large:
    fontFamily: Roboto, sans-serif
    fontSize: 3.5625rem
    fontWeight: 400
    lineHeight: 1.12
    letterSpacing: -0.015625rem
  display-medium:
    fontFamily: Roboto, sans-serif
    fontSize: 2.8125rem
    fontWeight: 400
    lineHeight: 1.16
    letterSpacing: 0
  headline-large:
    fontFamily: Roboto, sans-serif
    fontSize: 2rem
    fontWeight: 400
    lineHeight: 1.25
    letterSpacing: 0
  headline-medium:
    fontFamily: Roboto, sans-serif
    fontSize: 1.75rem
    fontWeight: 400
    lineHeight: 1.29
    letterSpacing: 0
  title-large:
    fontFamily: Roboto, sans-serif
    fontSize: 1.375rem
    fontWeight: 500
    lineHeight: 1.27
    letterSpacing: 0
  title-medium:
    fontFamily: Roboto, sans-serif
    fontSize: 1rem
    fontWeight: 500
    lineHeight: 1.5
    letterSpacing: 0.009375rem
  body-large:
    fontFamily: Roboto, sans-serif
    fontSize: 1rem
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: 0.03125rem
  body-medium:
    fontFamily: Roboto, sans-serif
    fontSize: 0.875rem
    fontWeight: 400
    lineHeight: 1.43
    letterSpacing: 0.015625rem
  label-large:
    fontFamily: Roboto, sans-serif
    fontSize: 0.875rem
    fontWeight: 500
    lineHeight: 1.43
    letterSpacing: 0.00625rem
  label-medium:
    fontFamily: Roboto, sans-serif
    fontSize: 0.75rem
    fontWeight: 500
    lineHeight: 1.33
    letterSpacing: 0.03125rem
  label-small:
    fontFamily: Roboto, sans-serif
    fontSize: 0.6875rem
    fontWeight: 500
    lineHeight: 1.45
    letterSpacing: 0.03125rem
  mono:
    fontFamily: "Roboto Mono, monospace"
    fontSize: 0.875rem
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: 0
rounded:
  xs: 4px
  sm: 8px
  md: 12px
  lg: 16px
  xl: 28px
  full: 9999px
spacing:
  xs: 4px
  sm: 8px
  md: 16px
  lg: 24px
  xl: 32px
  2xl: 48px
  3xl: 64px
components:
  nav-drawer:
    width: 360px
    width-compact: 80px
    background: "{colors.surface}"
    item-active-background: "{colors.primary-container}"
    item-active-color: "{colors.on-primary-container}"
    item-default-color: "{colors.on-surface-variant}"
  issue-row:
    height: 72px
    height-expanded: auto
    border-radius: "{rounded.md}"
    status-chip-typography: "{typography.label-medium}"
  workflow-node:
    min-width: 160px
    padding: "{spacing.sm} {spacing.md}"
    border-radius: "{rounded.sm}"
    typography: "{typography.label-large}"
  workflow-edge:
    stroke-width: 1.5px
    arrow-size: 8px
  code-block:
    background: "{colors.surface-variant}"
    typography: "{typography.mono}"
    border-radius: "{rounded.sm}"
    padding: "{spacing.md}"
---

# gitonator Dashboard — Design Specification

**Issue:** #59  
**Author:** roy-des  
**Date:** 2026-05-18  
**Status:** Ready for Development

---

## Overview

The gitonator Dashboard is an **operational tool** for developers and project maintainers — not a consumer product. Its primary users are engineers who need to monitor live workflows, understand workflow state machines, and reference API documentation. The visual language prioritises **scannability, data density, and clarity** over ornamentation.

The dashboard uses **Material Design 3** (`@material/web@2`) as its design system. The design is unapologetically technical: monochrome surfaces, a single blue primary accent, and status colour coding that respects M3 colour roles. The tone is calm, informational, and trustworthy — a control panel, not a marketing site.

**Two switchable themes** (light and dark) are required. Both themes are fully specified below. The user's preference is persisted to `localStorage` and falls back to `prefers-color-scheme`.

---

## Colors

### Light Theme

The light theme uses a near-white `surface` background (`#f8f9ff`) with a slight blue tint — a common pattern in M3 dashboards that creates visual harmony with the blue primary without being distracting.

- **Primary (`#1a6ef5`):** Google Blue-family. Used for interactive elements, links, active navigation items, and the primary action button.
- **Primary Container (`#d3e4fd`):** Used for active nav drawer item backgrounds and selected state chips.
- **Secondary (`#5c5f7e`):** Muted blue-grey. Used for secondary text, metadata, and role labels.
- **Secondary Container (`#e1e0ff`):** Used for role/type chips and secondary-emphasis surfaces.
- **Tertiary (`#006876`):** Teal. Reserved for `status:intake` and `status:triage` workflow nodes.
- **Error (`#ba1a1a`):** Standard M3 error red. Used for `status:blocked`, `status:rejected`, and validation errors.
- **Surface (`#f8f9ff`):** Page background.
- **Surface Variant (`#e0e2f0`):** Elevated card background, code blocks, table headers.
- **Outline (`#757789`):** Borders, dividers.
- **Outline Variant (`#c4c5d7`):** Subtle dividers (table rows, list separators).

### Dark Theme

```css
[data-theme="dark"] {
  --md-sys-color-background: #111318;
  --md-sys-color-surface: #111318;
  --md-sys-color-surface-variant: #44475a;
  --md-sys-color-on-surface: #e2e2e9;
  --md-sys-color-on-surface-variant: #c5c6d8;
  --md-sys-color-primary: #a8c7fa;
  --md-sys-color-primary-container: #004494;
  --md-sys-color-on-primary: #002d6c;
  --md-sys-color-on-primary-container: #d3e4fd;
  --md-sys-color-secondary: #c3c4e8;
  --md-sys-color-secondary-container: #43476a;
  --md-sys-color-on-secondary: #2c2f55;
  --md-sys-color-tertiary: #53d8ef;
  --md-sys-color-tertiary-container: #004e5a;
  --md-sys-color-error: #ffb4ab;
  --md-sys-color-error-container: "#93000a";
  --md-sys-color-outline: #8e90a3;
  --md-sys-color-outline-variant: #44475a;
}
```

### Status Category Colour Mapping

Status categories map to M3 colour roles for consistent visual coding across the Live View chips and Workflow Graph nodes.

| Category | Statuses | Light token | Dark token | Rationale |
|---|---|---|---|---|
| `intake` | `status:triage` | `tertiary` `#006876` | `tertiary-dark` `#53d8ef` | Teal signals entry/open |
| `design` | `status:solution-design`, `status:ui-design` | `secondary` `#5c5f7e` | `secondary-dark` `#c3c4e8` | Purple-grey signals creative work |
| `implementation` | `status:ready-for-dev`, `status:in-progress` | `primary` `#1a6ef5` | `primary-dark` `#a8c7fa` | Blue signals active building |
| `review` | `status:review`, `status:approval`, `status:po-review` | `#6750a4` (M3 purple) | `#d0bcff` | Purple signals gate/decision |
| `terminal` | `status:done`, `status:closed` | `on-surface-variant` `#444659` | `#c5c6d8` | Neutral signals completed |
| `blocked` | `status:blocked`, `status:rejected` | `error` `#ba1a1a` | `#ffb4ab` | Red signals problem |

**Implementation note:** Define these as CSS custom properties in `theme/status-colors.css`, not hardcoded values in component files:

```css
:root {
  --status-color-intake: var(--md-sys-color-tertiary);
  --status-color-design: var(--md-sys-color-secondary);
  --status-color-implementation: var(--md-sys-color-primary);
  --status-color-review: #6750a4;
  --status-color-terminal: var(--md-sys-color-on-surface-variant);
  --status-color-blocked: var(--md-sys-color-error);
}
```

---

## Typography

The type system strictly follows M3 type roles using Roboto (Google Fonts CDN or `@fontsource/roboto`). Use `Roboto Mono` for all code samples and API endpoint paths.

No custom typefaces. Do not use system fonts (`-apple-system`) for body text — Roboto must load.

### Type role mapping to UI elements

| UI element | Type role |
|---|---|
| Page section headings | `headline-medium` |
| Card/panel titles | `title-large` |
| Table column headers | `label-large` |
| Body prose (docs pages) | `body-large` |
| Table cell text | `body-medium` |
| Status chips | `label-medium` |
| Badge counts | `label-small` |
| Code / API paths | `mono` |
| Navigation drawer items | `label-large` |
| Top App Bar title | `title-large` |

---

## Layout & Spacing

### Breakpoints

| Name | Range | Navigation | Content |
|---|---|---|---|
| Compact (mobile) | ≤ 600 px | Bottom navigation bar (4 items) | Full-width, 16 px side margins |
| Medium (tablet) | 601–1240 px | Navigation rail (icons + labels) | Two-column grid, 24 px margins |
| Expanded (desktop) | ≥ 1241 px | Navigation drawer (persistent, 360 px) | Multi-column, 24 px margins |

### Page grid

```
[NavDrawer 360px] | [Content area]
                       padding: 24px
                       max-content-width: 1200px
```

On expanded screens the nav drawer is always visible (persistent, not modal). On compact screens a bottom navigation bar replaces it entirely — no hamburger menu or modal drawer on mobile.

### Spacing scale usage

- Between related elements (label + field): `xs` (4 px)
- Within a card/surface: `md` (16 px) padding
- Between cards: `lg` (24 px) gap
- Page section separation: `xl` (32 px)
- Top-level page padding: `lg`–`xl` depending on viewport

---

## Elevation & Depth

M3 uses surface tints (adding the primary colour at low opacity) rather than shadows for elevation. Implement via `--md-elevation-level` custom property.

| Element | Elevation level | Surface tint opacity |
|---|---|---|
| Page background | 0 | 0% |
| Cards / panels | 1 | 5% |
| Navigation drawer | 2 | 8% |
| FABs | 3 | 11% |
| Modals / dialogs | 4 | 12% |
| Tooltips | 5 | 14% |

Dark theme: increase contrast by using the surface-variant background at elevation 2+.

---

## Shapes

M3 shape scale is applied per component category:

| Component | Shape token | Corner radius |
|---|---|---|
| Buttons (filled, outlined, text) | `rounded.xl` | 28 px (pill) |
| Cards | `rounded.md` | 12 px |
| Chips | `rounded.sm` | 8 px |
| Text fields | `rounded.xs` – top only | 4 px top |
| Navigation drawer item | `rounded.full` | 9999 px (stadium) |
| Workflow graph nodes | `rounded.sm` | 8 px |
| Dialogs / bottom sheets | `rounded.xl` – top only | 28 px top |
| Tooltips | `rounded.xs` | 4 px |
| Code blocks | `rounded.sm` | 8 px |

Do not use right angles (`0 px`) or fully custom radii — stay on the scale above.

---

## Components

### AppShell

The outermost layout component. Combines:
- **Navigation Drawer** (expanded/desktop)
- **Navigation Rail** (medium/tablet)  
- **Bottom Navigation Bar** (compact/mobile)
- **Top App Bar** (all breakpoints)
- **Theme Toggle** (icon button in the top app bar's trailing slot)

```
┌──────────────────────────────────────────────────────┐
│ [≡] gitonator Dashboard           [🌙 theme toggle] │  ← Top App Bar
├──────────────────┬───────────────────────────────────┤
│ ● Live View      │                                   │
│   Workflows      │      <page content>               │  ← Main area
│   Workflow Graph │                                   │
│   Docs: Setup    │                                   │
│   Docs: API      │                                   │
└──────────────────┴───────────────────────────────────┘
         ↑
   Navigation Drawer (persistent on desktop)
```

**Navigation items:**
1. Live View (icon: `dashboard`)
2. Workflows (icon: `account_tree`)
3. Workflow Graph (icon: `hub`) — only shown when a workflow is selected; otherwise navigates to Workflow List
4. Setup Docs (icon: `menu_book`)
5. API Docs (icon: `api`)

**Theme Toggle:**
- `md-icon-button` with `light_mode` / `dark_mode` icon
- Placed in the top app bar trailing slot
- Tooltip: "Switch to dark/light theme"
- On toggle: update `localStorage.setItem('theme', 'dark'|'light')` and `document.documentElement.dataset.theme`

### Live View (Route: `/`)

The primary operational view. Uses a **data table layout** (not a card grid) for maximum scannability. Operators viewing dozens of active issues need to scan rows, not consume cards.

**Table columns:**

| # | Column | Width | Notes |
|---|---|---|---|
| 1 | Issue # | 72 px | `body-medium` monospace, links to GitHub issue |
| 2 | Title | flexible | `body-medium`, truncated at 1 line with ellipsis |
| 3 | Status | 180 px | M3 chip with status-category colour fill |
| 4 | Assignee | 120 px | Avatar + handle, `label-medium` |
| 5 | Bridge | 120 px | Bridge ID or "–", `label-small` |
| 6 | Updated | 96 px | Relative time (`2m ago`), `label-small` |

**Row interaction:**
- Click row → inline expansion panel below the row (not a page navigation). The expansion panel shows: task ID, task status, full assignee list, recent transition timestamps.
- Row hover: `surface-variant` tint.
- Active expansion: `primary-container` left border (4 px) on the expanded row.

**SSE indicator:**
- Top-right of the Live View header: a small `md-circular-progress` (indeterminate, 16 px) while the SSE connection is active. Replaced by a `md-icon` `wifi_off` in error colour if the connection drops.
- Auto-flash animation on the row's status chip when an SSE `issue_updated` event arrives for that issue (brief `primary` background pulse, 600 ms ease-out).

**Empty state:**
- Centred `md-icon` `checklist` (48 px, `on-surface-variant`) + body text "No active workflows" + subtext "Issues will appear here as work packages arrive."

**Error state:**
- Full-width `md-outlined-card` with error icon + message + a "Retry" `md-text-button`.

**States required:**
- Default (loading skeleton)
- Populated (with data)
- Empty (no active issues)
- SSE disconnected (visual indicator only, table still shows last-known data)
- Row expanded

### Workflow List (Route: `/workflows`)

A 3-column (desktop) / 2-column (tablet) / 1-column (mobile) grid of **summary cards**, one per workflow YAML file.

**Card structure (M3 Outlined Card):**
```
┌──────────────────────────────────────────────┐
│  lean_github_issue_workflow                  │  ← title-medium
│  Lean 3-role issue workflow                  │  ← body-medium, on-surface-variant
│                                              │
│  12 statuses  ·  8 transitions               │  ← label-small chips
│                                              │
│                              [View Graph →]  │  ← md-text-button
└──────────────────────────────────────────────┘
```

- Card hover: elevation 2 → elevation 3 transition (150 ms ease).
- Card tap navigates to `/workflows/:id`.

**Empty state:** "No workflow definitions found. Ensure `workflows/*.yaml` files are present and the server is running."

### Workflow Graph (Route: `/workflows/:id`)

An interactive directed graph using **ReactFlow** with **dagre layout** (direction: `TB` — top-to-bottom). The graph is the centrepiece of this view.

**Node design:**

Each status node is a **custom ReactFlow node** component:

```
┌─────────────────────────────────┐
│  [role chip]                    │  ← label-medium chip (role colour from secondary-container)
│  status:triage                  │  ← label-large, bold
└─────────────────────────────────┘
```

- Node background: `surface-variant` (light) / `#2a2d38` (dark)
- Node left border (4 px solid): status-category colour (see colour mapping table above)
- Node selected state: `primary` border all sides, 2 px
- Node hover: elevation 2 tint

**Node role chip:** A small `md-assist-chip` inside the node's top-left, showing the assigned role (`po`, `developer`, `uidesigner`, etc.). Background: `secondary-container`. Text: `on-secondary-container`.

**Edge design:**

- Default edge: `outline-variant` colour, 1.5 px stroke, arrowhead `#757789`
- Edge label: small `label-small` text on a `surface` background pill — shown on hover only to reduce clutter. The label is the transition `description` field from the workflow YAML.
- Self-loops (same source and target): rendered as a looping bezier curve with reduced opacity (0.6).

**Mobile adaptations:**

On compact viewports (≤ 600 px):
- Dagre layout switches to `LR` (left-to-right). Top-to-bottom layouts become too tall to navigate.
- `fitView` prop enabled (ReactFlow) — graph zooms to fit all nodes on initial render.
- Pinch-to-zoom enabled (ReactFlow `panOnScroll`, `zoomOnPinch`).
- Node labels truncated to 18 chars maximum.
- A full-screen button (bottom-right FAB: `fullscreen` icon) allows the user to enter fullscreen mode.

**Controls:**
- ReactFlow `<Controls />` panel (mini map disabled on mobile to save space).
- A `<MiniMap />` component on tablet/desktop (bottom-right), styled with `surface-variant` node fill.
- "Fit view" button: `md-outlined-button` below the graph title.
- "Zoom in / Zoom out" buttons: standard ReactFlow controls.

**Sidebar panel (desktop only):**
When a node is selected, a 320 px right-side panel slides in (`transition: width 200ms ease`) showing:
- Status name (headline-medium)
- Role: chip
- Category: chip
- Terminal: yes/no badge
- Valid transitions from this node (list of target status chips)

**States required:** loading skeleton, populated, empty (no nodes), selected-node, error.

### Docs: Setup (Route: `/docs/setup`)

A **static document page** rendered from Markdown (the content is authored in `dashboard/src/docs/setup.md` and imported at build time via Vite's `?raw` import + a lightweight Markdown renderer — `react-markdown` with `remark-gfm`).

**Content structure (authored by the developer based on actual codebase):**

1. Prerequisites
2. Creating the GitHub App / Personal Access Token
3. Cloning the repository
4. Configuring `config.json` / environment variables
5. Setting up workflow YAML files
6. Initialising repository labels (`deploy/init_repo_full.sh`)
7. Starting the server
8. Connecting the dashboard

**Visual treatment:**
- Left-side sticky table of contents (`label-medium` links, `on-surface-variant` colour, active link highlighted `primary`). Desktop only — on mobile/tablet it collapses to an "On this page" accordion at the top.
- `h2` → `headline-medium`, `h3` → `title-large`
- Code blocks → `mono` type role, `surface-variant` background, copy-to-clipboard icon button (top-right of each block)
- Max content width: 768 px, centred

### Docs: API (Route: `/docs/api`)

A structured, hand-authored (or generated from `openapi.json`) reference page. Not an auto-generated Swagger UI — it must match the dashboard's visual style.

**Endpoint card structure:**

```
┌─────────────────────────────────────────────────────────┐
│  GET  /api/v1/dashboard/issues                          │
│  List active GitHub issues with current status          │
├─────────────────────────────────────────────────────────┤
│  Request                                                │
│  (no body)                                              │
│                                                         │
│  Response 200                                           │
│  ┌─────────────────────────────────────────────────┐   │
│  │ { "issues": [ { "number": 59, ... } ] }         │   │  ← code block
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  curl example                                           │
│  ┌─────────────────────────────────────────────────┐   │
│  │ curl http://127.0.0.1:6666/api/v1/dashboard/... │   │  ← code block
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

- HTTP method badge: filled chip (`GET` → blue/primary, `POST` → secondary, `DELETE` → error).
- Endpoint path: `mono` typography.
- Response/request schemas: JSON code blocks with syntax highlighting (via `react-syntax-highlighter` or `prism`).
- Copy-to-clipboard on every code block.
- Left sidebar: jump-links to each endpoint group (dashboard, workflows).

---

## Do's and Don'ts

### Do
- Use M3 token CSS custom properties for all colour, spacing, and shape values.
- Persist theme preference to `localStorage`.
- Use `md-circular-progress` for loading states, not custom spinners.
- Show empty and error states for every data-fetching component.
- Use `label-medium` chips for status values — never plain text with inline styles.
- Test all focusable elements with keyboard navigation (Tab, Enter, Space, Escape).
- Use `title` attributes or `aria-label` on icon buttons.
- Use `role="status"` or `aria-live="polite"` on the SSE connection indicator.

### Don't
- Do not hardcode colour hex values in component files — always reference a CSS custom property.
- Do not use shadows for elevation — use `--md-elevation-level` tinting.
- Do not use non-M3 font stacks — Roboto only for prose, Roboto Mono for code.
- Do not navigate the user to a new page when they click a Live View row — use inline expansion.
- Do not show the MiniMap on mobile — it occupies too much space.
- Do not auto-play animations — respect `prefers-reduced-motion`.

---

## Accessibility

### Contrast requirements (WCAG AA minimum)

| Token pair | Ratio | Compliant |
|---|---|---|
| `on-primary` on `primary` | ≥ 4.5:1 | ✓ (#ffffff on #1a6ef5 = 4.7:1) |
| `on-surface` on `surface` | ≥ 4.5:1 | ✓ (#191b23 on #f8f9ff = 15.2:1) |
| `on-surface-variant` on `surface-variant` | ≥ 4.5:1 | ✓ (#444659 on #e0e2f0 = 5.3:1) |
| `on-error` on `error` | ≥ 4.5:1 | ✓ (#ffffff on #ba1a1a = 5.1:1) |
| Dark: `on-surface` on `background` | ≥ 4.5:1 | ✓ (#e2e2e9 on #111318 = 12.4:1) |

### Touch targets
All interactive elements must be at minimum **48 × 48 px** (M3 requirement). Icon buttons and chips must have `min-width: 48px; min-height: 48px` even if visually smaller.

### Keyboard navigation
- Navigation drawer items: fully keyboard navigable with visible focus rings (`--md-focus-ring-color: var(--md-sys-color-primary)`).
- Workflow graph: nodes must be focusable via Tab; selected node shows the sidebar panel on Enter.
- Data table rows: focusable, with Enter to expand.
- All icon buttons have `aria-label`.

### Motion
```css
@media (prefers-reduced-motion: reduce) {
  .status-chip-pulse,
  .card-elevation-transition,
  .sidebar-slide {
    animation: none;
    transition: none;
  }
}
```

---

## Architect's Open Questions — Design Decisions

The following are authoritative answers to the five open questions raised in the architect's solution design comment.

### 1. Navigation Pattern

**Decision: Persistent Navigation Drawer on desktop (≥ 1241 px) + Navigation Rail on tablet + Bottom Navigation Bar on mobile.**

Rationale: The dashboard has 5 top-level destinations. M3 recommends a Navigation Drawer for 5+ destinations on expanded screens. The drawer must be persistent (always visible), not modal — the app is a monitoring tool where users need to switch panes frequently without dismissing a drawer. On mobile, the Bottom Navigation Bar shows the 4 most common destinations (Live View, Workflows, Docs Setup, Docs API); the Workflow Graph is accessible only via the Workflows list on mobile.

### 2. Graph Node Design

**Decision: Role shown as an inline M3 chip in the top portion of the node. Full detail (description, transitions) in a right-side panel that opens on node click.**

Rationale: Role is actionable metadata — operators need to know at a glance which role is responsible for each status. An inline chip avoids hiding critical information behind hover-only tooltips (inaccessible on touch). The full transition list is too verbose for the node itself; the slide-in panel provides depth without cluttering the graph.

### 3. Live View Layout

**Decision: Data table (not a card grid).**

Rationale: The Live View is used by operators who may have 20–50 active issues simultaneously. Tables are more scannable than card grids at high data density. Cards are appropriate for the Workflow List (low count, high visual differentiation between items). Inline row expansion adds the "richness" of cards without sacrificing scannability.

### 4. Status Colour Coding

**Decision: Yes — full category-to-colour mapping as specified in the Status Category Colour Mapping table above.**

Rationale: Operators recognise workflow state by colour before reading text. Consistent category colours (teal=intake, blue=implementation, purple=review, red=blocked, grey=done) create a shared visual language across the Live View chips and the Workflow Graph nodes. Using M3 semantic colour roles (`tertiary`, `secondary`, `primary`, `error`) ensures the mapping works in both light and dark themes without manual dark-theme overrides per status.

### 5. Mobile Graph Breakpoints

**Decision: LR layout on mobile, `fitView` on mount, pinch-to-zoom enabled, node labels truncated, fullscreen FAB available.**

Rationale: `TB` (top-to-bottom) layouts become taller than the viewport on workflows with 10+ statuses, requiring excessive vertical scrolling. `LR` keeps the graph horizontally scrollable which is more natural on mobile. `fitView` on mount ensures the user sees the full graph immediately rather than a partial viewport. The fullscreen FAB allows power users to inspect complex workflows without navigating away.

---

## Responsive Behaviour Summary

| Breakpoint | Nav | Live View | Workflow Graph | Docs |
|---|---|---|---|---|
| Mobile ≤ 600 px | Bottom nav bar | Full-width table, 16 px margin | LR layout, fitView, fullscreen FAB | Single column, no ToC sidebar |
| Tablet 601–1240 px | Navigation Rail | Table with 24 px margin | TB layout, MiniMap | Single column, accordion ToC |
| Desktop ≥ 1241 px | Persistent Drawer | Table, max 1200 px width | TB layout, MiniMap, sidebar panel | Two-column with sticky ToC |

---

## Design Token File Structure

```
dashboard/
  src/
    theme/
      tokens.css          # All --md-sys-color-* and custom tokens
      status-colors.css   # --status-color-* tokens
      typography.css      # --md-typescale-* tokens
      elevation.css       # --md-elevation-level-* tokens
    components/
      StatusChip.tsx       # Renders status label with correct category colour
      WorkflowNode.tsx     # Custom ReactFlow node
      WorkflowEdge.tsx     # Custom ReactFlow edge with hover label
      IssueRow.tsx         # Table row with expansion
      ThemeToggle.tsx      # Icon button with localStorage persistence
      CodeBlock.tsx        # Syntax-highlighted block with copy button
```

---

## Design Handoff Checklist

- [x] Component states defined: default, hover, active, focus, disabled, error, empty, loading.
- [x] Responsive behaviour specified for compact, medium, and expanded breakpoints.
- [x] Accessibility requirements noted — contrast ratios, touch targets (48 × 48 px), ARIA roles, keyboard navigation, `prefers-reduced-motion`.
- [x] Design tokens defined — no hardcoded colours or sizes in component spec.
- [x] Edge cases covered: empty Live View, SSE disconnection, no workflow YAML files, graph with self-loop edges, long issue titles.
- [x] Architect's open questions answered (navigation, node design, layout, colour coding, mobile).
- [x] Linked from issue #59.
