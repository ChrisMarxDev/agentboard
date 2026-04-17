# AgentBoard — Project Specification (v2, Implementation Ready)

> **Status**: Ready for implementation. Experimental phase — we are testing hypotheses, so expect iteration on specifics, but the architectural shape is locked.

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Problem & Philosophy](#2-problem--philosophy)
3. [Architecture Overview](#3-architecture-overview)
4. [Project & Folder Structure](#4-project--folder-structure)
5. [Data Layer](#5-data-layer)
6. [Markdown Layer (MDX)](#6-markdown-layer-mdx)
7. [Component System](#7-component-system)
8. [Built-in Component Catalog](#8-built-in-component-catalog)
9. [Transports](#9-transports)
10. [Realtime (SSE)](#10-realtime-sse)
11. [MCP Integration & Claude Plugin](#11-mcp-integration--claude-plugin)
12. [Authentication](#12-authentication)
13. [First-Run Experience](#13-first-run-experience)
14. [CLI Reference](#14-cli-reference)
15. [REST API Reference](#15-rest-api-reference)
16. [SQLite Schema Reference](#16-sqlite-schema-reference)
17. [Tech Stack & Dependencies](#17-tech-stack--dependencies)
18. [Go Package Layout](#18-go-package-layout)
19. [Frontend Architecture](#19-frontend-architecture)
20. [Build & Distribution](#20-build--distribution)
21. [Implementation Phases](#21-implementation-phases)
22. [Future Work](#22-future-work)
23. [Open Questions](#23-open-questions)

---

## 1. Executive Summary

**AgentBoard** is a single-binary dashboard server for agent-driven workflows. Agents write data via REST; humans read a live dashboard in the browser. Pages are authored in MDX with embedded components. Data lives in SQLite. Every widget is a component (JSX), and users can add custom ones by dropping files into a folder.

**One-line pitch:** Evidence.dev for normal people, where AI agents are the authors and the data source.

**Key properties:**
- Single Go binary, no runtime dependencies
- Works locally with zero config (`agentboard` just runs)
- Same binary runs hosted with auth enabled (future)
- MDX pages with hybrid syntax (JSX interpolation + component `source` props)
- SQLite for storage (JSON1 for structured data), history recorded silently
- Hot reload for pages AND components (via embedded esbuild)
- Baked-in MCP server for Claude integration
- One-line install: `curl -fsSL https://agentboard.dev/install.sh | bash`

**Target user:** Non-technical people who want a live dashboard of what their AI agents are doing, without configuring anything.

---

## 2. Problem & Philosophy

### The Problem

Agentic work is invisible. When you run multiple AI agents, scheduled jobs, or CI pipelines, the only way to see what's happening is watching terminal output or checking files manually. There's no glanceable, real-time, shareable visualization layer for agent-driven workflows.

Existing dashboard tools (Grafana, Metabase, Retool, Evidence) are built for analysts writing SQL against databases. They don't fit a world where the author is an AI agent and the audience is non-technical humans.

### The Philosophy

Seven principles that drive every design decision in this spec:

1. **Agent-first authoring.** The primary writer is an LLM. Syntax and APIs are optimized for what agents naturally produce, not for human ergonomics.

2. **Non-technical reading.** The primary reader is a non-developer. The rendered output is a polished, readable document, not a "dashboard" that requires training to parse.

3. **Data and UI are separated.** Agents push data to a simple key-value store. Pages are MDX files. Data changes constantly; pages change rarely.

4. **Single binary, zero dependencies.** One executable, one folder. No Node, no Python, no Docker required.

5. **Local-first, hosted-possible.** The same binary works locally (no config) and on a server (with auth enabled). No separate product editions.

6. **Extensible via components.** New visualization needs mean writing a JSX component and dropping it in a folder. No plugin runtime, no config language, just React.

7. **Reliable rails for an agentic world.** AI is the universal adapter, but adapters have overhead. Anything that can be built without AI (connectors, deterministic pipes) should be, and AgentBoard must remain connector-friendly.

---

## 3. Architecture Overview

```
    Agents / Claude / Scripts / CI / Humans
                    │
                    ▼
        ┌──────────────────────────┐
        │   Transport Layer         │
        │   REST · CLI · stdin · SSE│
        └────────────┬──────────────┘
                     ▼
        ┌──────────────────────────┐
        │   Auth Layer              │
        │   (disabled in local)     │
        └────────────┬──────────────┘
                     ▼
        ┌──────────────────────────┐
        │   Data Store (SQLite)     │
        │   data + data_history     │
        └────────────┬──────────────┘
                     ▼
        ┌──────────────────────────┐
        │   SSE Broadcaster         │
        │   key-scoped subscriptions│
        └────────────┬──────────────┘
                     ▼
        ┌──────────────────────────┐
        │   Browser                 │
        │   React + compiled MDX    │
        │   useData hooks           │
        └──────────────────────────┘

    File watcher ──┐
    (components/, │
     pages/)       ▼
               ┌──────────────────────┐
               │   esbuild (embedded) │
               │   compiles on change │
               │   notifies browser   │
               └──────────────────────┘
```

**Request flow (data write):**
1. Agent POSTs to `/api/data/sentry/issues`
2. Auth layer validates (no-op in local mode)
3. Write goes into SQLite `data` table
4. Previous value recorded in `data_history`
5. SSE broadcaster notifies subscribers of key `sentry.issues`
6. Browser useData hook receives update, component re-renders

**Request flow (page render):**
1. Browser requests `/` (or any page path)
2. Server returns the frontend shell (HTML + embedded bundle)
3. Frontend fetches compiled MDX for the page from `/api/pages/index`
4. MDX references resolve: static JSX runs, components with `source` props call `useData`
5. Data fetches populate, components render
6. SSE connection opened for the page's data keys

**Request flow (component hot reload):**
1. User saves `components/MyWidget.jsx`
2. File watcher detects change
3. esbuild recompiles `MyWidget.jsx`
4. Server broadcasts "components updated" via SSE
5. Browser fetches new bundle and swaps the component

---

## 4. Project & Folder Structure

### Project Model

A **project** is a folder on disk. One project = one dashboard = multiple pages.

A single AgentBoard binary serves one project at a time. To run multiple projects, run multiple binaries on different ports. This keeps the architecture simple and isolates projects completely.

### Default Project Location

If no path is specified, AgentBoard uses `~/.agentboard/default/` (or the OS-appropriate equivalent: `%USERPROFILE%\.agentboard\default\` on Windows). If that folder doesn't exist, it's created with a welcome template on first run.

### Project Folder Layout

```
my-project/                    ← the project folder
├── index.md                   ← homepage (required)
├── pages/                     ← additional pages (optional)
│   ├── ops.md
│   ├── metrics.md
│   └── releases.md
├── components/                ← custom components (optional)
│   ├── MyWidget.jsx
│   └── Kanban.jsx
├── agentboard.yaml            ← config (optional, sensible defaults)
└── .agentboard/               ← runtime data (hidden, auto-managed)
    ├── data.sqlite            ← data store
    ├── build/                 ← compiled MDX and components cache
    └── state.json             ← runtime state (port, last started, etc.)
```

### Multi-Project Workflow

```bash
# Default project (~/.agentboard/default/)
agentboard

# Named project (~/.agentboard/preeo/)
agentboard --project preeo

# Arbitrary path
agentboard --path ./my-dashboard

# Two projects at once, different ports
agentboard --project preeo --port 3000 &
agentboard --project openclaw --port 3001 &
```

### Config File (`agentboard.yaml`)

All fields are optional. Defaults make everything work.

```yaml
title: "Preeo Ops"              # Dashboard title (default: folder name)
port: 3000                      # Default port (default: 3000)
theme: dark                     # light | dark | auto (default: auto)
history_retention_days: 30      # How long to keep data history (default: 30)

# Navigation (optional — auto-generated from pages/ if not specified)
nav:
  - path: /
    label: Home
  - path: /ops
    label: Operations
  - path: /metrics
    label: Metrics

# Future: auth, connectors, action launchers, query index hints
```

---

## 5. Data Layer

### Storage

SQLite with JSON1 extension. Single database file at `.agentboard/data.sqlite`. All data (current values and history) lives in this one file.

### Data Model

Data is a key-value store. Keys are dotted paths (`sentry.issues`, `analytics.dau`). Values are JSON (any valid JSON: scalars, objects, arrays).

**Collections** are a convention, not a separate type. If a value is a JSON array of objects each with an `id` field, it's treated as a collection by certain operations (SET by ID, DELETE by ID, MERGE by ID).

### Operations

Seven operations, expressed as REST verbs on `/api/data/:key` or `/api/data/:key/:id`:

| Operation | HTTP | Path | Semantics |
|---|---|---|---|
| **SET** | `PUT` | `/api/data/:key` | Replace entire value at key |
| **MERGE** | `PATCH` | `/api/data/:key` | Deep merge JSON into existing value |
| **UPSERT by ID** | `PUT` | `/api/data/:key/:id` | Upsert item in collection |
| **MERGE by ID** | `PATCH` | `/api/data/:key/:id` | Deep merge into specific item |
| **APPEND** | `POST` | `/api/data/:key` | Append to array (treats value as array) |
| **DELETE** | `DELETE` | `/api/data/:key` | Remove key entirely |
| **DELETE by ID** | `DELETE` | `/api/data/:key/:id` | Remove item from collection |

**Read operations:**

| Operation | HTTP | Path | Semantics |
|---|---|---|---|
| **GET** | `GET` | `/api/data/:key` | Return current value |
| **GET BY ID** | `GET` | `/api/data/:key/:id` | Return specific item from collection |
| **LIST** | `GET` | `/api/data` | Return all keys with values |
| **SCHEMA** | `GET` | `/api/data/schema` | Return map of keys to inferred JSON shapes |

### Key Rules

- Keys are dotted paths: `[a-zA-Z0-9_-]+` segments separated by `.`
- Max 10 levels of nesting
- Max 256 characters total
- Reserved prefixes: `_system.*` (internal use, read-only for agents)

### Value Rules

- Any valid JSON
- Max value size: 1 MB (configurable, hard cap 10 MB)
- For collections: each item must have an `id` field (string or number) for ID-based operations

### History

Every write records the **previous value** in `data_history` before overwriting. This is for disaster recovery, not for active use. Not exposed via API in v1.

Retention: default 30 days, configurable in `agentboard.yaml`. A background job prunes history rows older than the retention window.

### Conflict Handling

In local mode, writes are serialized through a per-key mutex in the Go server. Last-write-wins semantics. No CRDTs, no merge logic beyond what JSON1's `json_patch` provides for MERGE operations.

---

## 6. Markdown Layer (MDX)

### Format

Pages are MDX files (`.md` extension — we don't require `.mdx`). MDX supports standard markdown plus JSX expressions and components.

### Hybrid Syntax

We use two patterns side by side, chosen for clarity:

**Inline interpolation** (JSX expressions) for text:

```mdx
Current DAU: {data.analytics.dau}

Revenue this week: {data.revenue.weekly.toLocaleString()}
```

**Component `source` prop** for visualizations:

```mdx
<Metric source="analytics.dau" label="Daily Active Users" />

<LineChart source="analytics.history" x="date" y="value" />
```

**Why hybrid:** inline interpolation is natural for prose, but components benefit from declaring their data source explicitly (enables lazy loading, query DSL later, component-specific subscription).

### Global `data` Object

Every MDX page has a `data` object in scope. It contains the current values of all data keys, reactive via the `useData` hook internally.

```mdx
{data.sentry?.issues?.length ?? 0} open issues

{data.migration.status === 'done' ? '✅ Complete' : '⏳ In progress'}
```

Accessing undefined keys returns `undefined` (not an error). Use optional chaining and nullish coalescing for safety.

### Page Files

- `index.md` is the homepage (required, auto-created if missing)
- `pages/*.md` become additional pages at `/<filename>` (without the `.md` extension)
- Subfolders in `pages/` are allowed and create URL paths: `pages/ops/deploys.md` → `/ops/deploys`
- Navigation is auto-generated from the page tree (alphabetical by default)
- Custom nav order via `nav:` in `agentboard.yaml`

### Frontmatter

Optional YAML frontmatter per page:

```mdx
---
title: "Operations"
description: "Live status of all systems"
order: 2
icon: server
---

# Operations

...
```

Frontmatter fields are all optional. `title` is used in nav and page title; others are available for future theme features.

---

## 7. Component System

### Two Kinds of Components

1. **Built-in components** — shipped with the binary, always available. Nine in the MVP catalog.
2. **User components** — `.jsx` files in the `components/` folder of a project. Loaded at server startup, hot-reloaded on change.

User components with the same name as a built-in component **override** the built-in. No special config needed.

### Component File Format

A user component is a `.jsx` file exporting a default React component and a `meta` object:

```jsx
// components/RevenueGauge.jsx
import { useData } from '@agentboard/client';

export const meta = {
  name: "RevenueGauge",
  description: "A circular gauge showing revenue vs target.",
  props: {
    source: {
      type: "string",
      description: "Data key to read from. Expects { current, target } shape.",
      required: true
    },
    size: {
      type: "number",
      description: "Diameter in pixels",
      default: 200
    }
  }
};

export default function RevenueGauge({ source, size = 200 }) {
  const { data, loading } = useData(source);
  if (loading) return <div>Loading...</div>;

  const percent = Math.min(100, (data.current / data.target) * 100);
  return (
    <svg width={size} height={size}>
      {/* ... */}
    </svg>
  );
}
```

### Component Discovery

On startup and on file change, the server:

1. Reads all `.jsx` files in `components/`
2. Parses the `meta` export
3. Registers the component in an in-memory catalog
4. Compiles the component with esbuild to a single bundle
5. Broadcasts a "components updated" SSE event so browsers reload

The `/api/components` endpoint returns the full catalog including `meta` for each component. This is how agents discover what's available.

### The `useData` Hook

Every component reads data via the `useData` hook:

```jsx
const { data, loading, error } = useData(source, options);
```

- `source` — a dotted path key (e.g., `"sentry.issues"`)
- `options` — reserved for future query DSL (`{ where, orderBy, limit }`)
- Returns an object with `data`, `loading`, `error`
- Subscribes to SSE updates for that key automatically
- Cleans up subscription on unmount

The hook handles all reactivity. Components don't manage subscriptions directly.

### Component Loading and Hot Reload

**On startup:**
- esbuild compiles all user components in `components/` into a single bundle at `.agentboard/build/components.js`
- Server serves this bundle at `/api/components.js`
- Frontend imports it dynamically and registers components in the MDX renderer

**On file change:**
- File watcher detects `.jsx` change
- esbuild recompiles
- Server broadcasts `components-updated` via SSE
- Frontend fetches new bundle, swaps components in the registry
- Pages using those components re-render

**On component error:**
- Failed compilation: server logs error, broadcasts `component-error` with filename and message
- Frontend shows an inline error banner on pages using that component
- Previous working version stays loaded until the error is fixed

### Security Note

User components are arbitrary JavaScript. They run in the browser with full page privileges. This is acceptable for local mode (you trust yourself). For hosted multi-tenant mode (future), we'll need sandboxing (iframe per component, or shadow DOM with CSP). Flagged in [Open Questions](#23-open-questions).

---

## 8. Built-in Component Catalog

All built-in components live in `frontend/src/components/builtin/` and are compiled into the main frontend bundle. They follow the same API (`useData` hook, `meta` export) as user components.

### Component List

#### `<Status>`

A single state indicator with a label.

**Props:**
- `source` (string, required) — expects `{ state, label, detail? }`
- `state` values: `running | passing | failing | waiting | stale`

**Example:**
```mdx
<Status source="ci.pipeline" />
```

**Data shape:**
```json
{ "state": "passing", "label": "CI Pipeline", "detail": "Build #142" }
```

#### `<Metric>`

A single large number with optional trend.

**Props:**
- `source` (string, required) — expects number or `{ value, label?, trend?, comparison? }`
- `label` (string) — overrides label from data
- `format` (string) — `number | currency | percent | duration`

**Example:**
```mdx
<Metric source="analytics.dau" label="Daily Active Users" />
```

#### `<Progress>`

A progress bar.

**Props:**
- `source` (string, required) — expects `{ value, max, label? }`
- `label` (string)

**Example:**
```mdx
<Progress source="migration.progress" />
```

**Data shape:**
```json
{ "value": 7, "max": 12, "label": "DB Migration" }
```

#### `<Table>`

A table of rows and columns.

**Props:**
- `source` (string, required) — expects `{ columns, rows }` OR an array of objects
- `linkField` (string) — if rows are objects, which field contains a URL

**Example:**
```mdx
<Table source="sentry.issues" />
```

#### `<Chart>`

A static chart rendered from a complete snapshot.

**Props:**
- `source` (string, required) — expects `{ variant, labels, values }` or array form
- `variant` (string) — `bar | pie | donut | horizontal_bar`

**Example:**
```mdx
<Chart source="sales.by_category" variant="bar" />
```

#### `<TimeSeries>`

A time series line or bar chart, grown via APPEND operations.

**Props:**
- `source` (string, required) — expects array of `{ x, y }` points OR `{ points, x_field, y_field }`
- `variant` (string) — `line | bar`
- `x` (string) — field name for x axis
- `y` (string) — field name for y axis

**Example:**
```mdx
<TimeSeries source="analytics.dau_history" x="date" y="value" />
```

#### `<Log>`

An append-only text log, newest at top.

**Props:**
- `source` (string, required) — expects array of `{ timestamp, level?, message }` items
- `limit` (number) — max entries to show (default 50)

**Example:**
```mdx
<Log source="deploy.log" limit={20} />
```

#### `<List>`

An ordered or unordered list with optional status badges and links.

**Props:**
- `source` (string, required) — expects array of strings OR objects with `{ text, status?, url? }`
- `variant` (string) — `ordered | unordered`

**Example:**
```mdx
<List source="todos.today" />
```

#### `<Kanban>`

A non-interactive kanban board grouping items by a field.

**Props:**
- `source` (string, required) — expects array of objects
- `groupBy` (string, required) — field name to group by (typically `status`)
- `columns` (array of strings) — explicit column order (default: inferred from data)
- `titleField` (string) — field to show as card title (default: `title`)

**Example:**
```mdx
<Kanban
  source="issues"
  groupBy="status"
  columns={["todo", "in_progress", "done"]}
/>
```

**Data shape:**
```json
[
  { "id": "1", "title": "Fix auth bug", "status": "todo" },
  { "id": "2", "title": "Deploy v1.2", "status": "in_progress" },
  { "id": "3", "title": "Write docs", "status": "done" }
]
```

### Text block

Markdown paragraphs are just markdown. No component needed for prose. Inline interpolation covers variable text.

---

## 9. Transports

All transports converge on the same `DataStore.Write(key, op, value, source)` function. Each transport is a way to invoke that function.

### REST (primary, always on)

See [REST API Reference](#15-rest-api-reference) for full details.

Default base URL: `http://localhost:3000/api`

### CLI

The `agentboard` binary doubles as a client when invoked with subcommands:

```bash
# Set a value
agentboard set sentry.issue_count 12

# Set JSON value
agentboard set sentry.issues '[{"id": 1, "title": "Bug"}]'

# From file
agentboard set sentry.issues --file issues.json

# From stdin
cat issues.json | agentboard set sentry.issues --stdin

# Merge
agentboard merge config '{"theme": "dark"}'

# Append
agentboard append events.log '{"message": "Deploy started", "level": "info"}'

# Delete
agentboard delete sentry.issues

# Get
agentboard get sentry.issues

# List all keys
agentboard list
```

The CLI communicates with a running AgentBoard server via HTTP (default `localhost:3000`). Override with `--server` flag or `AGENTBOARD_SERVER` env var.

### stdin pipe

```bash
echo '{"count": 42}' | agentboard set metrics --stdin
```

### Future transports (not in v1)

- **WebSocket** — for agents sending high-frequency updates
- **File drop** — watch a folder for YAML files, ingest on appearance

---

## 10. Realtime (SSE)

### Why SSE (not WebSocket)

SSE is simpler, works through proxies, uses standard HTTP, and is sufficient for our unidirectional update pattern (server → browser). We don't need the bidirectional channel WebSocket provides; data writes come in via REST.

### Granularity

**We push changed keys, not "something changed" signals.** When a write lands, the server broadcasts an event containing the new value of the specific key. Browsers use the new value directly without refetching.

### Event Types

```
event: data
data: {"key": "sentry.issues", "value": [...]}

event: components-updated
data: {"names": ["MyWidget", "Kanban"]}

event: page-updated
data: {"path": "/ops"}

event: component-error
data: {"name": "MyWidget", "error": "Syntax error at line 12"}

event: heartbeat
data: {}
```

### Subscription

Every browser connects to `GET /api/events` as an EventSource. All events are broadcast to all connected clients. Client-side filtering in the `useData` hook determines which events matter for which component.

**Why broadcast everything:** simpler implementation, scales fine to hundreds of subscribers, avoids subscription state management on the server. If we hit scale issues, we can add per-key subscription later.

### Heartbeat

Every 30 seconds, server sends a `heartbeat` event. Clients use this to detect dead connections and auto-reconnect.

### Reconnection

On connection drop, client auto-reconnects with exponential backoff (1s, 2s, 4s, max 30s). On reconnect, client re-fetches initial data state via `GET /api/data` and resumes subscriptions.

---

## 11. MCP Integration & Claude Plugin

### Goal

A non-technical user installs AgentBoard, runs it, and connects Claude with one command. Claude can now read and write data, list and create pages, and discover components — all without the user configuring anything.

### Architecture

The MCP server is baked into the AgentBoard binary. It exposes MCP tools over HTTP (MCP Streamable HTTP transport) at `http://localhost:3000/mcp`.

### Install Flow (target UX)

```bash
# 1. Install AgentBoard
curl -fsSL https://agentboard.dev/install.sh | bash

# 2. Run it
agentboard

# 3. Connect Claude (one command)
claude mcp add agentboard http://localhost:3000/mcp
```

Now Claude has AgentBoard tools available. User goes back to their conversation and asks Claude to do dashboard things.

### MCP Tools Exposed

**Data tools:**
- `agentboard_set` — set a data value
- `agentboard_merge` — merge into a data value
- `agentboard_append` — append to a data array
- `agentboard_delete` — remove a data key
- `agentboard_get` — read a data value
- `agentboard_list_keys` — list all data keys with types

**Page tools:**
- `agentboard_list_pages` — list all pages
- `agentboard_read_page` — read a page's MDX source
- `agentboard_write_page` — create or overwrite a page
- `agentboard_delete_page` — delete a page

**Component tools:**
- `agentboard_list_components` — list all registered components with their `meta`
- `agentboard_read_component` — read a component's source (for reference/copying)

**Schema tools:**
- `agentboard_get_data_schema` — return inferred schema of current data (key paths and JSON shapes)

### Skill File

A static skill file (`skill.md`) ships with the binary and is available at `http://localhost:3000/skill`. It's a markdown document that teaches Claude:

1. What AgentBoard is
2. How to think about data vs UI separation
3. The MDX syntax (hybrid style: interpolation for text, `source` props for components)
4. Examples of good pages
5. The workflow: "when the user asks for X, first check the data schema, then decide what components to use, then write the page"

The skill file is static in v1 (bundled with the binary). Claude reads it once and uses the MCP tools to do the actual work. Dynamic schema discovery happens at tool-call time via `agentboard_get_data_schema`.

Example skill file excerpt:

```markdown
# AgentBoard Skill

When the user asks you to track something or build a dashboard:

1. Call `agentboard_list_components` to see what visualization components
   are available.
2. Call `agentboard_get_data_schema` to see what data already exists.
3. Decide what data to track and what components to use.
4. Use `agentboard_set` to populate initial data.
5. Use `agentboard_write_page` to create or update the MDX page.

## MDX Syntax

Use JSX interpolation for inline text:
  Current users: {data.analytics.dau}

Use components with `source` prop for visualizations:
  <Metric source="analytics.dau" label="Daily Active Users" />

## Example

User says: "Track my daily coffee intake."

1. Set initial data:
   agentboard_set("coffee.today", 0)
   agentboard_set("coffee.history", [])

2. Write a page:
   agentboard_write_page("index.md", `
   # Coffee Tracker

   Today: <Metric source="coffee.today" label="Cups" />

   <TimeSeries source="coffee.history" x="date" y="cups" />
   `)

3. Tell the user the dashboard is at http://localhost:3000
```

---

## 12. Authentication

### Local Mode (v1 scope)

**No authentication.** The binary listens on `localhost:3000` and accepts all requests. This is safe because localhost is only accessible to the local user (and same-machine processes).

If users bind to `0.0.0.0` or deploy the binary remotely without auth, that's user error. We'll warn in logs if binding to non-localhost without auth configured.

### Single Token (future, simple hosted)

Environment variable `AGENTBOARD_TOKEN=<secret>`. All requests require `Authorization: Bearer <secret>`. Quick way to protect a personal hosted instance.

### Multi-User (future, team hosted)

Full role-based auth with tokens in a `users` table. See earlier discussion in the conversation history; this spec explicitly defers it to a future phase.

---

## 13. First-Run Experience

### What Happens When You Run `agentboard` for the First Time

1. Check for project folder at `~/.agentboard/default/`
2. If missing, create it with a welcome template
3. Initialize SQLite database with sample data
4. Start server on port 3000
5. Print helpful startup message with next steps
6. Open default browser to `http://localhost:3000` (opt-out via `--no-open` flag)

### Startup Output

```
AgentBoard v0.1.0

Project:  default (~/.agentboard/default)
Dashboard: http://localhost:3000
MCP:       http://localhost:3000/mcp

Connect Claude:
  claude mcp add agentboard http://localhost:3000/mcp

Or ask any agent to POST to:
  http://localhost:3000/api/data/:key

Opening browser...
Press Ctrl+C to stop.
```

### Welcome Template Contents

`~/.agentboard/default/index.md`:

```mdx
# Welcome to AgentBoard

This dashboard updates live as your agents work.
You're looking at MDX — markdown with embedded components.

## Try it

Run this in your terminal:

    agentboard set welcome.users 42

Watch the number below update in real time:

<Metric source="welcome.users" label="Sample Metric" />

## Connect Claude

    claude mcp add agentboard http://localhost:3000/mcp

Then ask Claude to build you something. For example:

  "Track my daily reading progress with a chart."

## Sample Components

<Progress source="welcome.progress" />

<Status source="welcome.status" />

<Kanban source="welcome.tasks" groupBy="status" />

## Learn more

- Edit this page: `~/.agentboard/default/index.md`
- Add pages to `pages/`
- Add custom components to `components/`
- Docs: https://agentboard.dev/docs
```

Initial sample data populated on first run:

```json
{
  "welcome.users": 42,
  "welcome.progress": { "value": 3, "max": 10, "label": "Getting Started" },
  "welcome.status": { "state": "running", "label": "AgentBoard", "detail": "All systems go" },
  "welcome.tasks": [
    { "id": "1", "title": "Install AgentBoard", "status": "done" },
    { "id": "2", "title": "Connect Claude", "status": "in_progress" },
    { "id": "3", "title": "Build your first dashboard", "status": "todo" }
  ]
}
```

---

## 14. CLI Reference

### Server Commands

```bash
# Start server (default project, default port)
agentboard

# Alias
agentboard serve

# Explicit project (by name under ~/.agentboard/)
agentboard --project preeo

# Explicit path
agentboard --path ./my-dashboard

# Custom port
agentboard --port 3001

# Don't open browser
agentboard --no-open

# Init a new project (creates folder + welcome template)
agentboard init --project my-new-dashboard

# List all projects
agentboard projects
```

### Data Commands

All data commands require a running server (they hit the REST API). Use `--server <url>` to target a specific instance.

```bash
# Set a value (JSON auto-detected; use --string for literal string)
agentboard set KEY VALUE
agentboard set analytics.dau 1420
agentboard set config '{"theme": "dark"}'
agentboard set greeting "hello" --string

# Set from file
agentboard set KEY --file path/to.json

# Set from stdin
cat data.json | agentboard set KEY --stdin

# Merge (PATCH)
agentboard merge KEY VALUE

# Append to array
agentboard append KEY VALUE

# Delete a key
agentboard delete KEY

# Delete an item from a collection
agentboard delete KEY --id ID

# Read a value
agentboard get KEY

# List all keys
agentboard list
agentboard list --prefix analytics

# Get schema
agentboard schema
```

### Utility Commands

```bash
# Show version
agentboard version

# Print config (merged with defaults)
agentboard config

# Open the dashboard in browser
agentboard open

# Print MCP endpoint URL
agentboard mcp-url
```

### Environment Variables

- `AGENTBOARD_SERVER` — URL for CLI client commands (default `http://localhost:3000`)
- `AGENTBOARD_PROJECT` — default project name
- `AGENTBOARD_PATH` — default project path (overrides project name)
- `AGENTBOARD_PORT` — default port

---

## 15. REST API Reference

### Conventions

- Base path: `/api`
- Content type: `application/json` for requests and responses
- Keys in URLs are URL-encoded (`foo.bar` stays as `foo.bar`, but `foo/bar` must be encoded)
- Errors return JSON: `{ "error": "message", "code": "ERROR_CODE" }`
- Success responses include `{ "ok": true }` where there's no other body

### Data Endpoints

#### `GET /api/data`

Returns all data as a flat object of key → value.

Response:
```json
{
  "sentry.issues": [...],
  "analytics.dau": 1420,
  "migration.progress": { "value": 7, "max": 12 }
}
```

Query params:
- `prefix` — filter to keys starting with this string
- `keys` — comma-separated list of specific keys

#### `GET /api/data/:key`

Returns the value at a key.

Response:
```json
{
  "key": "analytics.dau",
  "value": 1420,
  "updated_at": "2026-04-16T14:30:00Z",
  "updated_by": "ci-agent"
}
```

Returns 404 if key doesn't exist.

#### `GET /api/data/:key/:id`

Returns a specific item from a collection (value must be array of objects with `id` field).

#### `PUT /api/data/:key`

Replace value at key. Body is the new value (any JSON).

Headers:
- `X-Agent-Source: <name>` — optional source tag (defaults to `anonymous`)

Body: any valid JSON.

Response: `{ "ok": true, "updated_at": "..." }`

#### `PATCH /api/data/:key`

Deep merge JSON into existing value. If key doesn't exist, behaves like PUT.

Semantics follow RFC 7396 (JSON Merge Patch): objects merge recursively, arrays are replaced wholesale, `null` values remove keys.

#### `PUT /api/data/:key/:id`

Upsert an item in a collection. Value at `key` must be an array (or be absent, in which case a new array is created). Item's `id` field is checked against existing items.

Body: the item (must include `id` field matching path, or `id` is added from path).

#### `PATCH /api/data/:key/:id`

Deep merge into a specific item in a collection.

#### `POST /api/data/:key`

Append to an array value. If key doesn't exist, creates a new array.

Body: the item to append.

#### `DELETE /api/data/:key`

Remove the key entirely.

#### `DELETE /api/data/:key/:id`

Remove an item from a collection by ID.

#### `GET /api/data/schema`

Returns inferred JSON schema for all keys.

Response:
```json
{
  "analytics.dau": { "type": "number" },
  "sentry.issues": {
    "type": "array",
    "items": { "type": "object", "properties": { ... } }
  }
}
```

### Page Endpoints

#### `GET /api/pages`

List all pages in the project.

Response:
```json
[
  { "path": "/", "file": "index.md", "title": "Home", "order": 0 },
  { "path": "/ops", "file": "pages/ops.md", "title": "Operations", "order": 1 }
]
```

#### `GET /api/pages/:path`

Returns the compiled MDX for a page (as executable JS).

Headers:
- `Accept: application/javascript` — returns compiled JS (default)
- `Accept: text/markdown` — returns raw MDX source

#### `PUT /api/pages/:path`

Create or overwrite a page. Body is MDX source.

Headers:
- `Content-Type: text/markdown`

Body: raw MDX.

Response: `{ "ok": true, "compiled": true }` or error with compilation details.

#### `DELETE /api/pages/:path`

Delete a page. Cannot delete `index.md`.

### Component Endpoints

#### `GET /api/components`

Returns the catalog of all registered components (built-in + user).

Response:
```json
[
  {
    "name": "Metric",
    "type": "builtin",
    "meta": {
      "description": "A single large number with optional trend",
      "props": { ... }
    }
  },
  {
    "name": "RevenueGauge",
    "type": "user",
    "file": "components/RevenueGauge.jsx",
    "meta": { ... }
  }
]
```

#### `GET /api/components.js`

Returns the compiled JS bundle of all components (built-in + user). The frontend imports this dynamically.

#### `GET /api/components/:name`

Returns a specific component's source (for reading only, not execution).

### Realtime

#### `GET /api/events`

Server-Sent Events stream. See [Realtime (SSE)](#10-realtime-sse).

### Meta

#### `GET /api/health`

Health check. Returns `{ "ok": true, "version": "..." }`.

#### `GET /api/config`

Return merged config (defaults + `agentboard.yaml`).

#### `GET /skill`

Returns the static skill file (markdown) that teaches Claude about AgentBoard.

### MCP

#### `POST /mcp`

MCP Streamable HTTP transport. Implements standard MCP protocol.

### Error Codes

- `NOT_FOUND` — key or resource doesn't exist
- `INVALID_KEY` — key format invalid
- `INVALID_VALUE` — value doesn't parse as JSON
- `VALUE_TOO_LARGE` — exceeds size limit
- `COMPILATION_FAILED` — MDX or component compilation failed (includes `details` with line numbers)
- `NOT_A_COLLECTION` — tried ID-based op on non-collection value
- `INTERNAL_ERROR` — unexpected server error (details logged, not returned)

---

## 16. SQLite Schema Reference

### Schema

```sql
-- Core data table. One row per data key.
CREATE TABLE data (
    key         TEXT NOT NULL PRIMARY KEY,
    value       TEXT NOT NULL,                  -- JSON
    updated_at  TEXT NOT NULL,                  -- ISO 8601
    updated_by  TEXT NOT NULL                   -- source identifier
) STRICT;

-- History table for disaster recovery. One row per write.
CREATE TABLE data_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,                  -- JSON; the PREVIOUS value
    written_at  TEXT NOT NULL,
    written_by  TEXT NOT NULL
) STRICT;

CREATE INDEX idx_history_key ON data_history(key, written_at);
CREATE INDEX idx_history_written_at ON data_history(written_at);

-- Metadata table for runtime state
CREATE TABLE meta (
    key   TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

-- Records in meta:
--   ("schema_version", "1")
--   ("created_at", "2026-04-16T...")
--   ("last_pruned_at", "...")
```

### Migrations

Schema version tracked in `meta.schema_version`. Migrations run on server startup if version mismatch.

Initial migration creates the tables above. Future migrations append.

### Query Patterns

**SET:**
```sql
INSERT INTO data_history (key, value, written_at, written_by)
  SELECT key, value, updated_at, updated_by FROM data WHERE key = ?;

INSERT INTO data (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
  ON CONFLICT (key) DO UPDATE
  SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by;
```

**MERGE:**
```sql
-- Archive previous
INSERT INTO data_history ...;

-- Merge via json_patch
UPDATE data
SET value = json_patch(value, ?),
    updated_at = ?,
    updated_by = ?
WHERE key = ?;
```

**UPSERT BY ID:** in Go, load current value, modify JSON, SET the new value. Wrapped in a transaction with a key-level mutex.

**History pruning (background job, hourly):**
```sql
DELETE FROM data_history WHERE written_at < ?;
```

Where `?` is `now - retention_days`.

---

## 17. Tech Stack & Dependencies

### Backend (Go)

- **Language**: Go 1.22+
- **SQLite**: `modernc.org/sqlite` (pure Go, no CGO, includes JSON1)
- **HTTP**: `net/http` (stdlib) with routing via `github.com/go-chi/chi/v5` (small, idiomatic router)
- **SSE**: custom implementation on stdlib (~100 lines)
- **File watching**: `github.com/fsnotify/fsnotify`
- **YAML parsing**: `gopkg.in/yaml.v3`
- **esbuild integration**: `github.com/evanw/esbuild/pkg/api` (pure Go API, pulls in JS compiler)
- **Embed**: stdlib `embed` for frontend bundle and skill file
- **MCP**: either a library (if one exists for Go) or hand-rolled (the protocol is simple)
- **JSON schema inference**: small custom util (no library needed for our scope)
- **CLI framework**: `github.com/spf13/cobra` (standard for Go CLIs)

### Frontend

- **Framework**: React 18 (not Preact — better MDX tooling support)
- **MDX**: `@mdx-js/mdx` for compilation, `@mdx-js/react` for runtime
- **Routing**: `react-router-dom` (file-based routing via our config)
- **Charts**:
  - `recharts` for line/bar/pie charts (good defaults, decent bundle size)
  - Custom SVG for simple widgets (progress, gauge)
- **State**: React Context + useState (no Redux, no Zustand — scope doesn't need it)
- **SSE**: native `EventSource`
- **Styling**: Tailwind CSS (small runtime, great DX) OR CSS modules (smaller bundle). Lean Tailwind for MVP.
- **Build**: Vite (for dev, not for production — production build is handled by esbuild through the Go binary)

### Build Tooling

- **esbuild**: embedded in Go binary via `github.com/evanw/esbuild/pkg/api`. Compiles MDX pages and JSX components at runtime.
- **Frontend pre-build**: Vite builds the React shell + built-in components into a static bundle at build time. This bundle is embedded in the Go binary via `go:embed`.

### Runtime Dependency Summary

**On user's machine**: just the `agentboard` binary. Nothing else.

**Inside the binary**: Go stdlib + SQLite + fsnotify + esbuild (all Go) + embedded frontend bundle + embedded skill file + embedded welcome template.

---

## 18. Go Package Layout

```
agentboard/
├── cmd/
│   └── agentboard/
│       └── main.go               ← entry point, CLI routing
├── internal/
│   ├── server/
│   │   ├── server.go             ← HTTP server setup
│   │   ├── handlers_data.go      ← data endpoints
│   │   ├── handlers_pages.go     ← page endpoints
│   │   ├── handlers_components.go
│   │   ├── handlers_meta.go      ← /health, /config, /skill
│   │   ├── sse.go                ← SSE broadcaster
│   │   └── middleware.go
│   ├── data/
│   │   ├── store.go              ← DataStore interface + SQLite impl
│   │   ├── history.go            ← history recording + pruning
│   │   ├── operations.go         ← SET, MERGE, UPSERT, APPEND, etc.
│   │   └── schema.go             ← JSON schema inference
│   ├── mdx/
│   │   ├── compile.go            ← esbuild-driven MDX compilation
│   │   ├── page.go               ← page model + caching
│   │   └── watch.go              ← file watcher for pages/
│   ├── components/
│   │   ├── builtin.go            ← registers built-in components
│   │   ├── user.go               ← scans user components/
│   │   ├── compile.go            ← esbuild for .jsx files
│   │   ├── meta.go               ← parse meta exports
│   │   └── watch.go              ← file watcher for components/
│   ├── project/
│   │   ├── project.go            ← project model (path, config)
│   │   ├── init.go               ← creates new projects from template
│   │   └── config.go             ← loads agentboard.yaml
│   ├── mcp/
│   │   ├── server.go             ← MCP protocol handler
│   │   ├── tools.go              ← MCP tool definitions
│   │   └── skill.go              ← serves /skill
│   ├── cli/
│   │   ├── root.go               ← cobra root command
│   │   ├── serve.go
│   │   ├── init.go
│   │   ├── data.go               ← set/get/list/etc
│   │   └── client.go             ← HTTP client for CLI-to-server comms
│   └── embed/
│       ├── frontend.go           ← go:embed for frontend bundle
│       ├── template.go           ← go:embed for welcome template
│       └── skill.go              ← go:embed for skill.md
├── frontend/
│   ├── src/
│   │   ├── App.tsx
│   │   ├── main.tsx
│   │   ├── hooks/
│   │   │   ├── useData.ts
│   │   │   └── useSSE.ts
│   │   ├── components/
│   │   │   ├── builtin/
│   │   │   │   ├── Metric.tsx
│   │   │   │   ├── Status.tsx
│   │   │   │   ├── Progress.tsx
│   │   │   │   ├── Table.tsx
│   │   │   │   ├── Chart.tsx
│   │   │   │   ├── TimeSeries.tsx
│   │   │   │   ├── Log.tsx
│   │   │   │   ├── List.tsx
│   │   │   │   └── Kanban.tsx
│   │   │   └── shell/
│   │   │       ├── Layout.tsx
│   │   │       ├── Nav.tsx
│   │   │       └── PageRenderer.tsx
│   │   └── lib/
│   │       ├── mdxRuntime.ts
│   │       └── componentRegistry.ts
│   ├── package.json
│   ├── vite.config.ts
│   └── index.html
├── templates/
│   └── welcome/
│       ├── index.md              ← welcome template MDX
│       └── agentboard.yaml       ← sample config
├── skill.md                      ← static Claude skill file
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### Key Interfaces

```go
// internal/data/store.go

type DataStore interface {
    Set(key string, value any, source string) error
    Merge(key string, patch any, source string) error
    UpsertById(key, id string, item any, source string) error
    MergeById(key, id string, patch any, source string) error
    Append(key string, item any, source string) error
    Delete(key string, source string) error
    DeleteById(key, id string, source string) error

    Get(key string) (any, error)
    GetById(key, id string) (any, error)
    GetAll(prefix string) (map[string]any, error)
    Schema() (map[string]Schema, error)

    Subscribe() <-chan DataEvent
    Close() error
}

// internal/server/sse.go

type Broadcaster interface {
    Broadcast(event SSEEvent)
    Subscribe() (id string, ch <-chan SSEEvent)
    Unsubscribe(id string)
}

// internal/mdx/compile.go

type PageCompiler interface {
    Compile(source string) (jsCode string, err error)
    CompileFile(path string) (jsCode string, err error)
}

// internal/components/compile.go

type ComponentBundler interface {
    BundleAll(dir string) (jsCode string, componentList []ComponentMeta, err error)
    Watch(dir string, onChange func()) error
}
```

---

## 19. Frontend Architecture

### Entry Point

```tsx
// frontend/src/main.tsx
import { createRoot } from 'react-dom/client';
import App from './App';

createRoot(document.getElementById('root')!).render(<App />);
```

### App Shell

```tsx
// frontend/src/App.tsx
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import Layout from './components/shell/Layout';
import PageRenderer from './components/shell/PageRenderer';

export default function App() {
  return (
    <BrowserRouter>
      <Layout>
        <Routes>
          <Route path="*" element={<PageRenderer />} />
        </Routes>
      </Layout>
    </BrowserRouter>
  );
}
```

### Page Rendering

`PageRenderer` does:
1. Reads the current URL path
2. Fetches compiled MDX JS from `/api/pages/:path`
3. Dynamically imports it as an ES module
4. Renders the exported MDX component with the component registry + `data` in scope

### Component Registry

```tsx
// frontend/src/lib/componentRegistry.ts
import * as builtins from '../components/builtin';

class Registry {
  private components = new Map<string, React.ComponentType>();

  register(name: string, component: React.ComponentType) {
    this.components.set(name, component);
  }

  get(name: string): React.ComponentType | undefined {
    return this.components.get(name);
  }

  all() {
    return Object.fromEntries(this.components);
  }
}

export const registry = new Registry();

// Register built-ins
for (const [name, component] of Object.entries(builtins)) {
  registry.register(name, component);
}

// Load user components
fetch('/api/components.js')
  .then(r => r.text())
  .then(async code => {
    const module = await import(`data:text/javascript,${encodeURIComponent(code)}`);
    for (const [name, component] of Object.entries(module)) {
      if (typeof component === 'function') registry.register(name, component as any);
    }
  });
```

### useData Hook

```tsx
// frontend/src/hooks/useData.ts
import { useState, useEffect } from 'react';
import { useDataContext } from './DataContext';

export function useData(source: string) {
  const context = useDataContext();
  const [data, setData] = useState(context.get(source));
  const [loading, setLoading] = useState(data === undefined);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    const unsubscribe = context.subscribe(source, (value) => {
      setData(value);
      setLoading(false);
    });

    if (data === undefined) {
      context.fetchKey(source).catch(setError);
    }

    return unsubscribe;
  }, [source]);

  return { data, loading, error };
}
```

### SSE Integration

Single `EventSource` at the app root. Events are dispatched to a central data context that individual `useData` hooks subscribe to via key-scoped callbacks.

### Theming

Tailwind with CSS variables for theme colors. Toggle dark/light via `data-theme` attribute on `<html>`. Theme respects system preference by default; user can override.

---

## 20. Build & Distribution

### Development

```bash
# Terminal 1: Frontend dev server (Vite)
cd frontend
npm install
npm run dev

# Terminal 2: Go server in dev mode (uses frontend dev server as proxy)
go run ./cmd/agentboard --dev
```

In dev mode, the Go server proxies non-API requests to the Vite dev server for hot module replacement on the frontend shell.

### Production Build

```bash
# Build frontend
cd frontend
npm install
npm run build
# Output: frontend/dist/

# Build Go binary (frontend dist is embedded)
go build -ldflags="-s -w" -o agentboard ./cmd/agentboard

# Cross-compile
GOOS=darwin GOARCH=arm64 go build -o dist/agentboard-darwin-arm64 ./cmd/agentboard
GOOS=darwin GOARCH=amd64 go build -o dist/agentboard-darwin-amd64 ./cmd/agentboard
GOOS=linux  GOARCH=amd64 go build -o dist/agentboard-linux-amd64  ./cmd/agentboard
GOOS=linux  GOARCH=arm64 go build -o dist/agentboard-linux-arm64  ./cmd/agentboard
GOOS=windows GOARCH=amd64 go build -o dist/agentboard-windows-amd64.exe ./cmd/agentboard
```

### Installation

**Install script** (`https://agentboard.dev/install.sh`):
```bash
# Detects OS and arch, downloads appropriate binary from GitHub releases,
# places it at /usr/local/bin/agentboard (with sudo if needed) or ~/bin
```

**Homebrew** (future):
```bash
brew install agentboard
```

**Manual**: download from GitHub releases page.

### Docker

```dockerfile
FROM scratch
COPY agentboard /agentboard
EXPOSE 3000
VOLUME /data
CMD ["/agentboard", "--path", "/data"]
```

### Binary Size Target

Aim for under 30 MB compressed. Main contributors:
- Go runtime: ~5 MB
- SQLite: ~2 MB
- esbuild: ~15 MB
- Frontend bundle: ~500 KB (Tailwind + React + recharts + MDX runtime)
- Everything else: ~5 MB

30 MB is a reasonable download for a dashboard tool. If it bloats beyond 50 MB, revisit esbuild inclusion (could shell out to a separate `esbuild` binary that's downloaded on first run).

---

## 21. Implementation Phases

### Phase 1 — Core (MVP)

**Goal**: User runs `agentboard`, opens browser, sees welcome page with sample data. Claude can be connected via MCP and modify the dashboard.

**Includes:**
- Single binary build for macOS (amd64 + arm64) and Linux (amd64)
- `agentboard` default command starts server on port 3000
- Default project at `~/.agentboard/default/` with welcome template
- SQLite data store with all 7 operations + history recording
- REST API complete per [Section 15](#15-rest-api-reference)
- SSE broadcasting with key-level granularity
- MDX page rendering (hybrid syntax)
- Built-in components: Metric, Status, Progress, Table, Chart, TimeSeries, Log, List, Kanban
- User components via `components/*.jsx` with hot reload (esbuild)
- Page hot reload (file watcher + SSE page-updated event)
- Auto-generated navigation from page tree
- CLI commands: `serve`, `init`, `set`, `get`, `list`, `merge`, `append`, `delete`
- MCP server with 12 tools (data, pages, components, schema)
- Static skill file served at `/skill`
- Install script (`curl | bash`)
- First-run experience (welcome template, sample data, browser auto-open)
- Multi-project support via `--project` flag (different ports)

**Explicitly not in Phase 1:**
- Authentication (local-only, no auth at all)
- Query DSL on components (components work on full data)
- Connectors (standalone binaries, future)
- Action/launch system (deep links to Claude Code / terminal)
- Homebrew formula
- Windows build
- Litestream or any replication

**Estimated scope**: 4-6 weeks of focused work for a proficient Go developer with some frontend experience. The frontend is the likely bottleneck (MDX runtime integration, useData hook, hot reload coordination).

### Phase 2 — Ecosystem

- Query DSL for components (`where`, `orderBy`, `limit` props; server-side translation to SQL)
- Action/launch system (deep links to open Claude Code, VS Code, terminal with prompts)
- More built-in components (gauge, heatmap, diff viewer, markdown renderer, image)
- Homebrew formula + Windows build
- Better error UX (inline compilation errors for MDX and components)
- Data export (zip the project folder, or snapshot to static HTML)
- Board templates (pre-built dashboards for common use cases: ops, product metrics, content calendar)

### Phase 3 — Multi-User & Hosted

- Authentication: single-token mode + full user/role mode
- User management CLI and REST API
- Deploy-to-hosted story: documented Fly.io / Railway / VPS deploy guides
- Optional Litestream for replication
- Public read-only pages (for status page use case)

### Phase 4 — Connectors & Community

- Connector framework: standalone binary model with documented data API contract
- Reference connectors: Sentry, PostHog, GitHub, Linear, Stripe
- Connector supervision (AgentBoard manages connector processes)
- Plugin marketplace / discovery
- WebSocket transport for high-frequency agents
- File-drop transport for sandboxed agents

---

## 22. Future Work

### Query DSL

Components will gain filtering, sorting, and aggregation props:

```mdx
<List
  source="sentry.issues"
  where="status = 'open' AND severity > 2"
  orderBy="-created_at"
  limit={5}
/>
```

Translates to SQLite with JSON1 path expressions. Server-side evaluation. Component receives already-filtered data.

**Why later**: we want to see what queries people actually write before baking the DSL. Start with components doing in-memory filtering; add DSL when patterns are clear.

### Connectors

Non-AI data pipes. Standalone binaries that poll or receive webhooks from specific services and write to AgentBoard's data API. Examples: Sentry, PostHog, GitHub, Stripe.

Architecture: completely separate binaries (no plugin runtime in v1). Each connector:
- Is distributed and run independently
- Gets an AgentBoard token with `agent` role
- Writes to a documented data key namespace (e.g., `sentry.*`)
- Can be supervised by AgentBoard (Phase 2+) via a config block

The data API is already connector-ready. No changes needed now.

### Action / Launch System

Clickable buttons on widgets that spawn external tools:

```mdx
<Status source="ci" />
<Action label="Retry CI" launch="terminal" command="gh workflow run ci.yml" />
<Action label="Ask Claude" launch="claude-code" prompt="Investigate why CI is failing" />
```

Browser calls `/api/actions/launch`, Go server uses OS-level process spawn to open terminal / Claude Code / VS Code with pre-filled content.

### Hosted Multi-Tenant

Single binary serves multiple projects, each with its own auth. Each project is a subfolder. Users log in with tokens, see only their projects.

This is a significant architecture change — the current spec assumes one project per binary. Multi-tenant is Phase 3+ work.

### Generative UI / Dynamic Components

Components generated on-the-fly by an LLM based on data shape. E.g., Claude writes a custom component for a specific visualization need, AgentBoard compiles and loads it.

This is the natural extension of the user-components-via-folder model: Claude writes a file via MCP, the hot reload picks it up. Already works in v1 architecturally — just needs Claude to know the component API well enough to produce good code.

---

## 23. Open Questions

These are intentional unknowns flagged for the implementer to decide as they go or escalate if blocked.

### Q1 — Frontend framework for the shell

React vs Preact. React is chosen for MDX ecosystem compatibility, but Preact would save ~30 KB. If MDX works cleanly with Preact (some testing needed), switching is fine. Default: React.

### Q2 — Chart library

`recharts` vs `uPlot` vs custom SVG. `recharts` is chosen for DX and breadth but adds ~150 KB. For timeseries specifically, uPlot is 10x smaller but has a steeper API. Default: `recharts` for all charts in v1, migrate to uPlot for TimeSeries if bundle size becomes an issue.

### Q3 — Component sandboxing

User components run with full browser privileges. Acceptable for local trusted use. For hosted multi-tenant (Phase 3), we'll need iframes or shadow DOM + CSP. Implementer should leave a clean extension point for this (component rendering goes through a single function that can be swapped later).

### Q4 — MCP library for Go

As of writing, the MCP ecosystem is young and Go libraries vary in quality. Implementer should either use the best available (as of implementation date) or hand-roll the Streamable HTTP transport (protocol is simple).

### Q5 — Hot reload mechanism for pages

Full re-fetch and re-render vs. HMR-style partial update. Full re-fetch is simpler and matches our "pages change rarely" assumption. Default: full re-fetch.

### Q6 — Binary size

esbuild embedded in the Go binary adds ~15 MB. If binary size becomes a pain point, alternatives: download esbuild on first run, ship as a separate binary, or use a lighter JSX compiler. Default: embed for Phase 1.

### Q7 — Schema inference strategy

`GET /api/data/schema` needs to infer JSON schema from current values. Simple version: for each key, recursively describe shape. Edge cases (mixed array types, null values) need a policy. Default: infer conservatively, use "any" for ambiguous cases.

---

## Appendix A — Sample Agent Interaction

Illustrative transcript of a non-technical user asking Claude to build a dashboard.

**User:** "I want to track my kids' chore completion. Each kid has a weekly checklist, and I want to see who's done what."

**Claude** (mentally):
1. Call `agentboard_list_components` → sees `Kanban`, `List`, `Progress` available.
2. Call `agentboard_get_data_schema` → empty, fresh project.
3. Decide on data structure: `chores` is a list of items with `{id, child, task, status, day}`.
4. Call `agentboard_set`:
   ```json
   "chores": [
     {"id": "1", "child": "Max", "task": "Make bed", "status": "done", "day": "Mon"},
     {"id": "2", "child": "Max", "task": "Feed fish", "status": "todo", "day": "Mon"},
     {"id": "3", "child": "Lea", "task": "Make bed", "status": "done", "day": "Mon"}
   ]
   ```
5. Call `agentboard_write_page("index.md", ...)`:
   ```mdx
   # Chore Tracker

   This week's chores for the kids.

   <Kanban source="chores" groupBy="status" columns={["todo", "in_progress", "done"]} />

   ## By Child

   ### Max
   <List source="chores" filter={item => item.child === 'Max'} />

   ### Lea
   <List source="chores" filter={item => item.child === 'Lea'} />
   ```

   (Note: `filter` prop is v2 query DSL. In v1, Claude would write two separate data keys `chores.max` and `chores.lea` instead.)

6. Respond to user: "Done! Open http://localhost:3000 to see the tracker. Add a new chore by asking me, and I'll update it."

**User:** "Max finished feeding the fish."

**Claude**:
1. Call `agentboard_merge_by_id("chores", "2", {"status": "done"})`
2. Respond: "Updated."

The dashboard updates live in the user's browser without refresh.

---

## Appendix B — Glossary

- **AgentBoard** — this project. Single-binary dashboard server for agents.
- **Project** — a folder containing a dashboard's pages, components, and data. One project per binary instance.
- **Page** — a single `.md` file rendered as a dashboard page.
- **Component** — a JSX component used in pages. Can be built-in or user-provided.
- **Data key** — a dotted-path string (`sentry.issues`) identifying a piece of data.
- **Collection** — a JSON array of objects with `id` fields, allowing ID-based operations.
- **Source** — the identifier of the agent, user, or system that wrote a value.
- **MCP** — Model Context Protocol, Anthropic's protocol for AI tool use.
- **Skill file** — a markdown document that teaches Claude how to use AgentBoard.
- **Connector** — (future) a standalone binary that pipes data from an external service into AgentBoard without AI.

---

## Appendix C — Implementation Checklist

A rough sequence for implementing Phase 1. Each item should take 1-5 days.

### Week 1: Foundation
- [ ] Initialize Go module, set up package layout
- [ ] SQLite schema + basic DataStore implementation (SET, GET, DELETE)
- [ ] Chi router, basic REST endpoints for data
- [ ] CLI scaffold with cobra (`serve`, `init`, `set`, `get`)

### Week 2: Data Complete + SSE
- [ ] Implement MERGE, UPSERT by ID, MERGE by ID, APPEND, DELETE by ID
- [ ] History table + recording
- [ ] SSE broadcaster
- [ ] History retention background job
- [ ] Schema inference

### Week 3: MDX & Components (Backend)
- [ ] Embed esbuild, compile user .jsx files
- [ ] Parse `meta` exports from components
- [ ] Component catalog endpoint
- [ ] MDX compilation pipeline
- [ ] File watcher for `pages/` and `components/`
- [ ] Compilation error handling (broadcast errors via SSE)

### Week 4: Frontend Shell
- [ ] React + Vite scaffold
- [ ] Layout, Nav, PageRenderer components
- [ ] useData hook with SSE integration
- [ ] Component registry (built-ins + dynamic user components)
- [ ] Tailwind setup, dark/light theme

### Week 5: Built-in Components
- [ ] Metric, Status, Progress (simple — knock out quick)
- [ ] Table, List, Log
- [ ] Chart, TimeSeries (uses recharts)
- [ ] Kanban

### Week 6: MCP + Polish
- [ ] MCP server, all 12 tools
- [ ] Static skill file
- [ ] First-run experience (welcome template, auto-browser-open)
- [ ] Install script
- [ ] Multi-project support (`--project`, `--path`)
- [ ] Cross-compile release binaries
- [ ] README, docs, examples

---

**End of specification.**
