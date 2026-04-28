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

## Authenticating against the dev instance

The running `agentboard-dev` instance enforces auth. Every API call except `GET /api/health` returns `401 Unauthorized` without a Bearer token.

**The dev token lives at `/tmp/agentboard-token`.** It's a working admin token for `@chris` on the `agentboard-dev` project. Read it into the session and use it as a Bearer:

```bash
export AB_TOKEN=$(cat /tmp/agentboard-token)
curl -H "Authorization: Bearer $AB_TOKEN" http://localhost:3000/api/content | jq
```

The file is outside the repo tree (in `/tmp`), so it will never be committed. If a session's permission policy blocks reading it directly, ask the user to paste the token — don't try to bypass.

If `/tmp/agentboard-token` is missing or the token is 401'ing (rotated, revoked), recover without minting a new identity:

```bash
./agentboard --project agentboard-dev admin list                    # verify state
./agentboard --project agentboard-dev admin rotate chris <label>    # rotate existing token

# If all admins are locked out entirely, wipe the DB so boot re-mints
# a first-admin invitation URL to stdout. Destructive — only for the
# dogfood project; ask the user first.
./agentboard --project agentboard-dev admin list-invitations        # shows active invite URLs
```

There is **no `mint-admin` CLI** in Auth v1. The first-admin path is an invitation URL printed at server boot when the users table is empty; additional users are added by admins creating invitations at `/admin`, not by CLI token minting.

Whichever path, write the fresh token back to `/tmp/agentboard-token` (mode `600`) so the next session picks it up.

**Never fall back to writing `content/…md` files directly on disk.** The file watcher accepts it, but direct disk writes bypass auth, activity attribution, rate limits, `content_history`, and optimistic concurrency. It's a product-invariant violation — if you can't authenticate, that's a config problem worth stopping to report, not routing around.

The `admin` CLI resolves `--project` like `serve` does; forget the flag and you'll operate on `~/.agentboard/default/` instead of agentboard-dev. Always pass `--project agentboard-dev` (or `AGENTBOARD_PROJECT=agentboard-dev` in the env).

---

## v2 store surface (files-first)

A parallel data surface lives at `/api/v2/*` backed by **files on disk** instead of SQLite. Full design in [`spec-file-storage.md`](spec-file-storage.md). Key points the agent needs to know:

**Three immutable shapes** per key, set on first write:
- **Singleton** — one value, atomic write. `data/<key>.json` on disk.
- **Collection** — per-ID files. `data/<key>/<id>.json`. Siblings never contend.
- **Stream** — append-only NDJSON. `data/<key>.ndjson`. Lock-free.

**Envelope format** every value is wrapped in:
```json
{"_meta": {"version", "created_at", "modified_by", "shape"}, "value": <...>}
```
The `version` is a server-monotonic timestamp; agents echo it back on `PUT`/`PATCH` for optimistic CAS. Conflict responses (412) **embed the current envelope** — no follow-up `GET` needed.

**Endpoints:**
- `GET  /api/v2/index` — Tier 1 catalog (every key, shape, version)
- `GET  /api/v2/search?q=…` — Tier 2 substring search across values
- `GET  /api/v2/data/<key>[/<id>]` — read; shape determined by catalog
- `PUT  /api/v2/data/<key>[/<id>]` — set/upsert; CAS via `_meta.version` or `If-Match` header
- `PATCH /api/v2/data/<key>[/<id>]` — RFC 7396 merge; never conflicts
- `POST /api/v2/data/<key>?op=append|increment|cas` — atomic ops
- `DELETE /api/v2/data/<key>[/<id>]` — idempotent
- `GET  /api/v2/data/<key>/history` — per-key NDJSON, last 100 entries
- `GET  /api/v2/activity` — global write log, filterable by actor/path/time

**Rate limit:** 200 writes/min sustained, 50/sec burst, per token. Reads bypass. `429` carries `Retry-After` + structured body. If you hit it in normal flow, switch to a batched op (`?by=N` increment, `items: [...]` append).

**Binary uploads — kill base64:**
- `POST /api/v2/files/request-upload` mints a one-shot URL
- Then `curl -X PUT --data-binary @file <upload_url>`
- Or via MCP: `agentboard_v2_request_file_upload` returns the URL
- Tokens are one-shot, TTL 5 min

**Live mirror component:** `<V2Display source="some.key" />` renders the envelope live in any MDX page. Updates on every SSE event. Read-only by design.

**MCP tools (12):** `agentboard_v2_index`, `_search`, `_read`, `_write`, `_merge`, `_append`, `_increment`, `_cas`, `_delete`, `_history`, `_activity`, `_request_file_upload`. Prefer these over the legacy resource-CRUD tools — same data, smaller surface, conflict-aware errors.

**Read path through the dashboard:** the view broker reads from BOTH stores (legacy SQLite first, files-first as fallback). Existing `<Status source="key" />` etc. on a page transparently picks up v2 keys. SSE events for v2 writes are re-shaped into the legacy `data` event so live updates work without component changes.

---

## When the user ships a feature

If the user just shipped something (landed a commit, says "we shipped X", asks for the dashboard to reflect new work), update the dashboard in this order:

1. **Write a feature page** under `content/features/<slug>.md` via `PUT /api/content/features/<slug>` or MCP `agentboard_write_page`. Use the feature-page template below.
2. **Update the feature list data** at `dev.features.shipped` (array of `{id, title, status: "done", landed_at}`) via `agentboard_set` or `PUT /api/data/dev.features.shipped`. The home page Kanban/List reads from this.
3. **Bump relevant metrics** — `dev.components.count`, `dev.mcp.tools`, `dev.tests.passing`, etc.
4. **If the session was a bigger phase** (multi-day push, rewrote a subsystem, shipped a cohort of related features together), **also write a `/showcase/<session-slug>` page** — see the "Showcase folder" section below.

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

## Showcase folder — one page per major session

`/showcase/<session-slug>` is the narrative timeline of the project. **Every bigger feature session gets one page there.** Sidebar-sorted by order, the folder becomes a browsable history of *how the product evolved* — not just *what's in it right now*.

A "bigger session" is any of:

- A multi-day push against a single theme (view broker, auth rework, approval)
- A cohort of related features shipped together (e.g. v0.5 dogfood cut landing kanban + public-routes + onboarding + share + approval in one session)
- A rewrite of an existing subsystem where the *before/after* story is itself interesting

Small one-off tweaks don't get a showcase page — those live on feature pages or in `dev.recent_commits`. The bar: **would someone reading the repo history want to see this session in a single page, with live data and a clear "what changed"?**

### Slugs

Use a descriptive kebab-case slug. Examples of good slugs from actual sessions:

- `v0-5-dogfood-cut` — the cohort of 5 dogfood-ready features
- `view-broker` — the read-path rewrite
- `auth-tokens` — the agent/admin token migration (if that were a showcase)

Don't prefix with a date; slugs live alongside an explicit `shipped_at` data key on the page. Don't make the slug the feature name if the session landed several features — use the theme.

### Template

```mdx
# <Session theme>

<one-paragraph lede answering: what problem did this session solve, and what shape does the answer take?>

## Shipped

<Deck>
  <Card title="<feature 1>"><Status source="showcase.<slug>.feature1.status" /></Card>
  <Card title="<feature 2>"><Status source="showcase.<slug>.feature2.status" /></Card>
  … one Card per concrete deliverable
</Deck>

## By the numbers

<Deck>
  <Card title="<metric>"><Counter source="showcase.<slug>.metric_a" label="…" /></Card>
  …
</Deck>

## The big idea

<Card title="Before">…</Card>
<Card title="After">…</Card>

## How it works

Explain the mechanism, with one <Code> block showing the shape of the API / the new convention.

## What got deleted

<Card title="Legacy the axe took">
  - bullet list of removed code / old behaviour / obsolete patterns
</Card>

## Phases shipped

<Card title="In order">
  <Kanban source="showcase.<slug>.timeline" groupBy="status" columns={["planned","in_progress","done"]} />
</Card>

## Invariants

<Card title="Provable, not aspirational">
  <Table source="showcase.<slug>.invariants" />
</Card>

## Try it yourself

A numbered list of 3-5 things the reader can do in this running instance to see the session's work in action.
```

### Data-key namespace

Showcase pages read from `showcase.<slug>.*` — do **not** reuse `demo.*` (that's for the first-run-default project's seeds) or `dev.*` (that's for the live project dashboard). Typical shapes:

- `showcase.<slug>.shipped_at` — ISO date string
- `showcase.<slug>.<feature>.status` — `{state, label, detail}` for Status card
- `showcase.<slug>.timeline` — array `[{id, title, status, order}]` for Kanban
- `showcase.<slug>.invariants` — array `[{rule, status, proof}]` for Table
- `showcase.<slug>.metric_*` — numbers for Counter cards

Seed these in parallel with the `PUT /api/content/showcase/<slug>` call so the page isn't empty on first load.

### Sidebar ordering

The content tree sorts subpages alphabetically by path. When the session naturally has a chronological ordering, prefix slugs with `NN-` (e.g. `01-v0-5-dogfood-cut`, `02-view-broker`) so the sidebar reads top-to-bottom in session order. Don't re-number existing slugs when inserting a new one — take the next available number.

Keep the showcase folder's root page (`/showcase`) as an index that lists every session with a one-line summary — again driven by a `showcase.index` data key so the timeline itself is live.

---

## Data-key conventions

| Key | Shape | Purpose |
| --- | --- | --- |
| `dev.status` | `{state, label, detail}` | Overall build state — running / passing / failing |
| `dev.tests.passing` | number | Current green test count (run `task test:bruno` + `task test:go` to refresh) |
| `dev.tests.total` | number | Total test count; pair with passing for a Progress bar |
| `dev.components.count` | number | Built-in component total (`GET /api/components | jq length`) |
| `dev.mcp.tools` | number | Total MCP tool count advertised (`tools/list`) |
| `dev.mcp.tools_v2` | number | Tier-shaped v2 tool subset (`agentboard_v2_*`) |
| `dev.stack.bundle_kb` | number | Frontend bundle size in KB |
| `dev.stack.binary_mb` | number | Go binary size in MB |
| `dev.features.shipped` | array of `{id, title, status, landed_at}` | Kanban/List input |
| `dev.architecture.flow` | string | Mermaid diagram source |
| `dev.seams` | array of `{name, status, breaks_when}` | seams_to_watch.md distilled |
| `dev.recent_commits` | array of `{timestamp, level, message}` | Log component input (use `git log --oneline -20 --format='%ad %s' --date=short` for timestamps) |

Use **`dev.*`** for live project metrics and **`showcase.*`** for per-session showcase page data (see "Showcase folder" above). Don't write to `welcome.*` (that's the default project's seed) or `demo.*` (that's for the default project's demo pages).

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
- **ApiList** for surfacing any `/api/*` endpoint that returns an array of objects (skills, errors, pages…) — prefer this over a bespoke React route when you want a listing page. See `content/skills.md` for the canonical example. Per CORE_GUIDELINES §9.
- **Mention** for `@username` and `@team` pills. Resolution order: user → stored team → reserved (`@all`, `@admins`, `@agents`, `@here`) → plain text.
- **TeamRoster** (`<TeamRoster slug="marketing" />`) for a roster card on MDX pages — header pill + description + member chips with optional role labels.

### MDX parse pitfalls

Two patterns break MDX silently — the page compiles, but a Card body comes out empty. Avoid both:

1. **`<...>` placeholders inside JSX attribute strings.** `<Markdown text="run agentboard rotate <user> <label>" />` will fail because MDX parses `<user>` as a JSX element start tag. Either escape the angle brackets, drop them (`USER LABEL`), or move the content into the Card's body and use native markdown.
2. **Multi-line JSX expressions with template literals + URLs inside Card children.** `<Card><Code>{`git clone https://...`}</Code></Card>` is flaky — the URL's `://` interacts badly with the JSX expression boundary. For multi-line code in a Card, prefer a plain fenced code block (```` ```bash ```` ... ```` ``` ````) at the page level. The standalone `<Code source="dev.foo">` pattern (sourced from a data key) is also safe because the value isn't inline.

Rule of thumb: if you put MDX-active syntax (`<…>`, JSX expressions, template literals with special chars) inside an attribute string OR inside `<Card>` children, you're in flaky territory. Native markdown text inside `<Card>...</Card>` body is reliable; JSX attribute strings are not.

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

## Hosting skills under content/skills/

AgentBoard's skills surface is a read-view on top of generic file storage. A
skill is any folder under `content/skills/<slug>/` containing a `SKILL.md`
with `name` + `description` in YAML frontmatter (Anthropic format). The
storage is ignorant of skill semantics — nothing on disk is "marked" as a
skill; the folder's location and the manifest are the only signal.

> The REST path is still `/api/files/skills/<slug>/…` (historical name — the
> `files/` and `content/` trees were consolidated into one folder, see
> CORE_GUIDELINES §9). On disk that resolves to `content/skills/<slug>/`.

When working in this repo and a new skill needs hosting or updating:

1. Write the manifest via `PUT /api/files/skills/<slug>/SKILL.md` (or the MCP
   tool `agentboard_write_file` with path `skills/<slug>/SKILL.md`). A `.md`
   upload triggers an inline page reindex, so the SKILL renders at
   `/skills/<slug>/SKILL` and is full-text searchable immediately.
2. Upload any supporting files to the same folder.
3. Verify with `agentboard_list_skills` — the skill should appear with its
   slug, name, and description.
4. Test the bundle endpoint: `GET /api/skills/<slug>` returns a zip.

Avoid teaching users about `content/skills/` directly — they should go
through the agent. The convention is enforced by documentation here, not in
code.

---

## Skill-update triggers (re-read when you ship)

Before marking a feature shipped, ask: does this change affect how an agent would talk to AgentBoard? If **yes**, edit this file in the same commit. Concretely:

- **Added or removed a built-in component** → update the "Component choices" list and any data-key conventions for its typical `source`.
- **Added or removed an MCP tool** → update any tool reference (including the "How to invoke" section) plus the `dev.mcp.tools` expected count in "Data-key conventions". For tier-shaped v2 tools, also bump `dev.mcp.tools_v2`.
- **Touched the v2 store / surface** (new envelope field, new shape, new endpoint, new error code) → update the "v2 store surface" section. The v2 contract is the agent-facing contract for all new work; drift here is the highest-cost kind.
- **Added or changed a REST route** → update curl examples and the endpoint references (e.g. the `PUT /api/content/...` line in "When the user ships a feature").
- **Renamed or moved a `dev.*` key** → update the table in "Data-key conventions". Old keys left in the skill are worse than no keys; they send the next agent to a dead path.
- **Changed a trigger phrase or a setup step** (e.g. new `--flag` on the binary, new port, new project name) → fix the YAML `description` up top AND any embedded command lines.
- **New convention the skill doesn't mention** (a new page pattern, a new dogfood metric, a new file-layout rule) → add it. If a convention isn't in the skill, it doesn't exist for the next agent.

Rule of thumb: if a reader of this file would write code that now fails or silently diverges from the codebase, the skill is stale and must be fixed before you commit.
