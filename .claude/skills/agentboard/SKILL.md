---
name: agentboard
description: Dogfood AgentBoard on itself. Use this skill when working inside the AgentBoard repo and the user asks to open the dev dashboard, update a feature page, record a metric, or confirm the self-hosted instance is healthy. Triggers include "dev dashboard", "project dashboard", "self-dashboard", "open agentboard", "update the feature page", "record this", "what's on the dashboard", "is the dev instance running".
---

# AgentBoard: Dogfood Skill

> **Meta-invariant: this skill is part of the product.** The long-term goal of this repo is to *use AgentBoard to build AgentBoard*, permanently. Every ship is a dogfood cycle. Whenever you change behavior that an agent uses to talk to AgentBoard — a new built-in component, a new MCP tool, a new REST route, a new `dev.*` convention, a renamed endpoint, a changed trigger phrase — **update this file in the same commit**. A stale skill means the next agent re-learns facts the project already knows, or worse, writes against drifted conventions. If the product moves and the skill doesn't, the project is lying to its own builders. Treat this file like source code: tests don't cover it, but every drift is a bug.

The AgentBoard repo runs its own instance at `http://localhost:3000` using the named project **`agentboard-dev`** (NOT `default`). The dashboard hosts:

- `/` — overview: status, component count, test metrics, recent shipped features
- `/principles` — the 8 core product principles in readable form
- `/architecture` — Mermaid diagrams of the data flow and package layout
- `/seams` — known trust-boundary deferrals from `seams_to_watch.md`
- `/features/<slug>` — one page per shipped feature

Data keys live under `dev.*`. Uploaded diagrams/screenshots live under `files/` as SVG.

---

## The port-3000 invariant

**The dev instance should be running on port 3000 whenever someone is working on the repo.** Before doing anything dashboard-related, check it:

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:3000/api/health
```

If it's not `200`, start it (background, so the session continues):

```bash
cd /Users/christophermarx/dev/agentboard
task build 2>&1 | tail -3
./agentboard --project agentboard-dev --port 3000 --no-open > /tmp/agentboard-dev.log 2>&1 &
sleep 2
curl -s http://localhost:3000/api/health   # expect {"ok":true,...}
```

Never use `--project default`. Never use `~/.agentboard/default/`. This instance is for dogfooding; the default project is for first-run UX testing.

---

## When the user ships a feature

If the user just shipped something (landed a commit, says "we shipped X", asks for the dashboard to reflect new work), update the dashboard in this order:

1. **Write a feature page** under `content/features/<slug>.md` via `PUT /api/content/features/<slug>` or MCP `agentboard_write_page`. Use the feature-page template below.
2. **Update the feature list data** at `dev.features.shipped` (array of `{id, title, status: "done", landed_at}`) via `agentboard_set` or `PUT /api/data/dev.features.shipped`. The home page Kanban/List reads from this.
3. **Bump relevant metrics** — `dev.components.count`, `dev.mcp.tools`, `dev.tests.passing`, etc.

### Feature page template

```mdx
# <Feature name>

<Deck>
  <Card title="Status">
    <Status source="dev.features.<slug>.status" />
  </Card>
  <Card title="Added">
    <Badge source="dev.features.<slug>.date" />
  </Card>
  <Card title="Tests">
    <Metric source="dev.features.<slug>.tests" />
  </Card>
</Deck>

## What it does

<Card title="Summary">
  <Markdown source="dev.features.<slug>.summary" />
</Card>

## Shape / API

<Card title="Example">
  <Code source="dev.features.<slug>.example" />
</Card>

## Architecture

<Card title="Flow">
  <Mermaid source="dev.features.<slug>.diagram" />
</Card>
```

Each card's source is a `dev.features.<slug>.*` data key. Seed them in the same write that creates the page so the dashboard isn't empty on first load.

---

## Data-key conventions

| Key | Shape | Purpose |
| --- | --- | --- |
| `dev.status` | `{state, label, detail}` | Overall build state — running / passing / failing |
| `dev.tests.passing` | number | Current green test count (run `task test:bruno` + `task test:go` to refresh) |
| `dev.tests.total` | number | Total test count; pair with passing for a Progress bar |
| `dev.components.count` | number | Built-in component total (`GET /api/components | jq length`) |
| `dev.mcp.tools` | number | MCP tool count advertised (`tools/list`) |
| `dev.stack.bundle_kb` | number | Frontend bundle size in KB |
| `dev.stack.binary_mb` | number | Go binary size in MB |
| `dev.features.shipped` | array of `{id, title, status, landed_at}` | Kanban/List input |
| `dev.architecture.flow` | string | Mermaid diagram source |
| `dev.seams` | array of `{name, status, breaks_when}` | seams_to_watch.md distilled |
| `dev.recent_commits` | array of `{timestamp, level, message}` | Log component input (use `git log --oneline -20 --format='%ad %s' --date=short` for timestamps) |

Use the **`dev.*` namespace** exclusively. Don't write to `welcome.*` (that's the default project's seed) or `demo.*`/`showcase.*` (those are the default project's demo namespaces).

---

## Component choices

The dashboard should exercise a broad set of components — it's our own visual regression. When you add or update a page, prefer:

- **Counter** over Metric for numbers that change frequently (flashes on update → visible feedback)
- **Mermaid** for any flow, sequence, or architecture description
- **Code** (not fenced markdown) for multi-line snippets so users get syntax highlighting
- **Stack** of **Badges** for inline label lists (versions, environments, tags)
- **Deck** + **Card** for every section — never let components bleed into each other
- **Kanban** for work-in-progress vs shipped features
- **Markdown** (the component, dynamically-loaded) for summaries that change often
- **File** for linking downloadable project artifacts (a PDF of the spec, an architecture SVG)

One rule: each feature page must use at least **three distinct components** plus a Mermaid diagram. If a feature has nothing diagrammable, it's probably not interesting enough to have its own page — roll it into a sibling.

---

## How to invoke

When the user asks to "update the dashboard", "add a feature page", or "show the project dashboard":

1. Confirm port 3000 is running (see invariant above).
2. Identify the feature — slug, summary, 1-2 code examples, a Mermaid diagram.
3. Write the data keys first, then the page. Page without data = broken cards.
4. Open `http://localhost:3000/features/<slug>` and confirm it renders before claiming done.

When the user says "record this metric" or "the test count changed":

1. PUT the new value to the appropriate `dev.*` key via REST or MCP.
2. SSE broadcasts it within ~100 ms. No page write needed.

When the user asks what's currently on the dashboard:

1. `GET /api/content` — lists all pages.
2. `GET /api/data?prefix=dev.` — lists data keys (if the prefix query is supported; otherwise `/api/data` then filter).
3. Return a short summary, not a dump.

---

## Hosting skills under files/skills/

AgentBoard's skills surface is a read-view on top of generic file storage. A
skill is any folder under `files/skills/<slug>/` containing a `SKILL.md` with
`name` + `description` in YAML frontmatter (Anthropic format). The storage is
ignorant of skill semantics — nothing on disk is "marked" as a skill; the
folder's location and the manifest are the only signal.

When working in this repo and a new skill needs hosting or updating:

1. Write the manifest via `agentboard_write_file` to `skills/<slug>/SKILL.md`
   (path is relative to `files/`, so no `files/` prefix in the MCP call).
2. Upload any supporting files to the same folder.
3. Verify with `agentboard_list_skills` — the skill should appear with its
   slug, name, and description.
4. Test the bundle endpoint: `GET /api/skills/<slug>` returns a zip.

Avoid teaching users about `files/skills/` directly — they should go through
the agent. The convention is enforced by documentation here, not in code.

---

## Skill-update triggers (re-read when you ship)

Before marking a feature shipped, ask: does this change affect how an agent would talk to AgentBoard? If **yes**, edit this file in the same commit. Concretely:

- **Added or removed a built-in component** → update the "Component choices" list and any data-key conventions for its typical `source`.
- **Added or removed an MCP tool** → update any tool reference (including the "How to invoke" section) plus the `dev.mcp.tools` expected count in "Data-key conventions".
- **Added or changed a REST route** → update curl examples and the endpoint references (e.g. the `PUT /api/content/...` line in "When the user ships a feature").
- **Renamed or moved a `dev.*` key** → update the table in "Data-key conventions". Old keys left in the skill are worse than no keys; they send the next agent to a dead path.
- **Changed a trigger phrase or a setup step** (e.g. new `--flag` on the binary, new port, new project name) → fix the YAML `description` up top AND any embedded command lines.
- **New convention the skill doesn't mention** (a new page pattern, a new dogfood metric, a new file-layout rule) → add it. If a convention isn't in the skill, it doesn't exist for the next agent.

Rule of thumb: if a reader of this file would write code that now fails or silently diverges from the codebase, the skill is stale and must be fixed before you commit.
