---
name: agentboard
description: Dogfood AgentBoard on itself. Use this skill when working inside the AgentBoard repo and the user asks to open the dev dashboard, update a feature page, record a metric, or confirm the self-hosted instance is healthy. Triggers include "dev dashboard", "project dashboard", "self-dashboard", "open agentboard", "update the feature page", "record this", "what's on the dashboard", "is the dev instance running".
---

# AgentBoard: Dogfood Skill

> **Meta-invariant: this skill is part of the product.** The long-term goal of this repo is to *use AgentBoard to build AgentBoard*, permanently. Every ship is a dogfood cycle. Whenever you change behavior that an agent uses to talk to AgentBoard — a new built-in component, a new MCP tool, a new REST route, a new `dev.*` convention, a renamed endpoint, a changed trigger phrase — **update this file in the same commit**. A stale skill means the next agent re-learns facts the project already knows, or worse, writes against drifted conventions. If the product moves and the skill doesn't, the project is lying to its own builders. Treat this file like source code: tests don't cover it, but every drift is a bug.

The AgentBoard repo runs its own instance at `http://localhost:3000` using the named project **`agentboard-dev`** (NOT `default`). The dashboard hosts:

- `/` — overview: status, component count, test metrics, recent shipped features
- `/principles` — the 12 core product principles in readable form (mirrors `CORE_GUIDELINES.md`)
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
# from the repo root
task build 2>&1 | tail -3
./agentboard --project agentboard-dev --port 3000 --no-open > /tmp/agentboard-dev.log 2>&1 &
sleep 2
curl -s http://localhost:3000/api/health   # expect {"ok":true,...}
```

Never use `--project default`. Never use `~/.agentboard/default/`. This instance is for dogfooding; the default project is for first-run UX testing.

---

## Wiring AgentBoard into a Claude.ai Custom Connector

The dev instance speaks the MCP authorization spec (OAuth 2.1 + PKCE +
RFC 9728/8414/7591), so adding it as a Custom Connector in Claude.ai
is the supported path for browser-driven agents:

1. In Claude.ai → Settings → Connectors → **Add Custom Connector**.
2. URL: `https://<your-public-host>/mcp` (Cloudflare-fronted; never
   localhost — Claude.ai's connector infrastructure can't reach it).
   Leave OAuth Client ID/Secret blank — DCR registers Claude.ai
   automatically.
3. Click Add → browser pops the consent page. Three ways to
   authenticate the consent decision, in priority order:
   - **Already signed in to AgentBoard in another tab?** The page
     shows "Logged in as @you · Allow / Deny" — one click and
     you're done.
   - Type your username + password in the consent form.
   - Or paste an AgentBoard PAT (`ab_*`) — the legacy path.
   On Allow, Claude.ai receives a scoped `oat_*` access token
   bound to `<host>/mcp`. Your credentials are never shared with
   the client.

Tokens minted via this path are audience-scoped — they work on `/mcp`
only and 401 anywhere else. That's correct per the spec; if a connector
flow needs broader access, mint a PAT instead. See `AUTH.md` →
"OAuth-issued tokens" for the full surface.

For Claude Code on a laptop, **PATs remain the path** — the OAuth
dance is overhead for a CLI that already has filesystem access. Only
use OAuth where the hosting environment can't accept a pasted token.

## Authenticating against the dev instance

The running `agentboard-dev` instance enforces auth. Every API call except `GET /api/health`, `/api/setup/status`, `/api/invitations/*`, and the `/api/auth/{login,logout,me}` trio returns `401 Unauthorized` without credentials. Two credential paths are accepted:

- **Bearer token (the agent / CLI / MCP path).** `Authorization: Bearer ab_…`, HTTP Basic with the token as password, or `?token=…`. Audience-scoped OAuth tokens (`oat_…`, minted via `/oauth/authorize`) work on `/mcp` only.
- **Browser session (the human path).** `POST /api/auth/login` with `{username, password}` mints an `agentboard_session` HttpOnly cookie + `agentboard_csrf` companion. Cookie-authenticated state-changing requests must echo the CSRF cookie value in the `X-CSRF-Token` header. Bearer requests skip CSRF by design.

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
./agentboard --project agentboard-dev admin list-invitations        # shows active invite URLs
./agentboard --project agentboard-dev admin invite --role member    # mint a new invite (no token needed)
./agentboard --project agentboard-dev admin set-password chris      # reset the browser password
./agentboard --project agentboard-dev admin revoke-sessions chris   # nuke active cookie sessions

# If all admins are locked out entirely, wipe the DB so boot re-mints
# a first-admin invitation URL to stdout. Destructive — only for the
# dogfood project; ask the user first.
```

`admin invite` is the CLI escape hatch for minting invitations without an existing admin token (e.g. after a fresh deploy where the first-admin URL was lost, or when the user explicitly asks for a new invite link and rotating an existing token would log them out). It writes directly to the local SQLite DB, so it requires filesystem access to the project. There is **no `mint-admin` CLI** for tokens themselves — token minting still flows through invitation redemption.

Whichever path, write the fresh token back to `/tmp/agentboard-token` (mode `600`) so the next session picks it up.

**Never fall back to writing `content/…md` files directly on disk.** The file watcher accepts it, but direct disk writes bypass auth, activity attribution, rate limits, `content_history`, and optimistic concurrency. It's a product-invariant violation — if you can't authenticate, that's a config problem worth stopping to report, not routing around.

The `admin` CLI resolves `--project` like `serve` does; forget the flag and you'll operate on `~/.agentboard/default/` instead of agentboard-dev. Always pass `--project agentboard-dev` (or `AGENTBOARD_PROJECT=agentboard-dev` in the env).

---

## Store surface (files-first)

The data surface lives at `/api/*` and is backed by `.md` files on disk with YAML frontmatter. Full design in [`spec-rework.md`](../../../spec-rework.md). Key points:

**Two leaf types:**
- **`.md` doc** — frontmatter holds structured fields, body holds optional prose + JSX. Used for singletons, collection items, and pages alike.
- **`.ndjson` stream** — append-only logs / telemetry / activity. Lock-free for lines ≤ 4 KB.

A folder of `.md` files is a collection. `tasks/index.md` is the page; `tasks/task-42.md` is one card. The shape (singleton vs collection) is implicit from "is there a folder at that path?".

**On-disk format:**
```mdx
---
_meta:
  version: 2026-04-28T...
  created_at: ...
  modified_by: alice
  shape: singleton
title: Build the kanban       # user fields splat at top level
col: in-progress
priority: 2
---

Optional markdown body here, with JSX components inline.
```
For primitives/arrays, use `value:` instead of splatting top-level keys.

**Endpoints (one namespace, spec §5):**
- `GET    /api/<path>` — read doc / list folder / tail stream / serve binary
- `PUT    /api/<path>` — full write, CAS via `_meta.version` or `If-Match`
- `PATCH  /api/<path>` — RFC 7396 merge on frontmatter, optional body replace
- `DELETE /api/<path>` — idempotent
- `POST   /api/<path>:append` — stream append (only `:` verb)
- `GET    /api/index` — flat catalog of every leaf
- `GET    /api/search?q=…` — substring search across values
- `GET    /api/<path>/history` — per-doc NDJSON history
- `GET    /api/activity` — global write log

`<path>` covers any content leaf — pages, singletons, collection items, streams. The dispatcher resolves page tree first, then data catalog. A path with a slash defaults to the page tree on new writes; flat keys default to the data tier as singletons. Reserved `/api/*` prefixes (admin, auth, view, files, etc.) take precedence per chi's specific-first routing.

The legacy `/api/content/<path>` and `/api/data/<key>[/<id>]` routes still work during the migration window. Prefer `/api/<path>` in new code.

**Atomic ops are gone** — no INCREMENT, no field-level CAS. Agents read-modify-write the whole doc; the file-level `_meta.version` CAS handles concurrent writers.

**Rate limit:** 200 writes/min sustained, 50/sec burst per token. Reads bypass. `429` carries `Retry-After` and a structured body.

**Binary uploads — kill base64:**
- `POST /api/files/request-upload` mints a one-shot URL
- `curl -X PUT --data-binary @file <upload_url>`
- Or via MCP: `agentboard_request_file_upload` returns the URL
- Tokens are one-shot, TTL 5 min

**MCP tools (10, locked by spec §6).** Cut 6 collapsed the surface from 38 domain-specific tools to 10 generic batch tools that dispatch by path. Always-plural batch shape; best-effort partial-success semantics; native JSON values; full envelope on read; non-blocking shape warnings on write.

```
Read tier:
  agentboard_read(paths)              — paths: [string]
                                         → [{path, frontmatter, body, version, warnings?}]
  agentboard_list(path)               — folder children + frontmatter snippets
  agentboard_search(q, scope?)        — FTS + substring across the tree
                                         scope: pages | data | all (default)

Write tier (always batch):
  agentboard_write(items)             — items: [{path, frontmatter?, body?, version?}]
  agentboard_patch(items)             — items: [{path, frontmatter_patch?, body?, version?}]
  agentboard_append(path, items)      — items: [any]; one stream per call; race-free
  agentboard_delete(items)            — items: [{path, version?}]

Files:
  agentboard_request_file_upload(items) — items: [{name, size_bytes}]
                                          → [{name, upload_url, expires_at, max_size_bytes}]

Named extensions:
  agentboard_grab(picks)              — cross-page materializer
  agentboard_fire_event(event, payload?) — emit on webhook bus
```

**Single-item operations wrap in a one-element array.** `agentboard_write({items: [{path, frontmatter}]})` — there is no singular form. Two writes, two patches, twenty deletes — all go in one call, partial-success per item.

**Native JSON values.** `frontmatter` is a JSON object. `frontmatter.value: 23` round-trips as the number `23`, not the string `"23"`. The MCP wrapper does NOT call `JSON.stringify` on already-decoded payloads (this was Issue 1 + 2 — fixed in Cut 6).

**Full envelope on read.** `agentboard_read([paths])` returns one result per path with `{frontmatter, body, version, shape}` plus a structured `error` if the read failed. The body-only `read_page` is gone.

**Shape warnings.** When a write lands at a path matching a suggested-shape glob (spec §8 — `tasks/*`, `metrics/*`, `skills/*/SKILL`) and the frontmatter is missing the suggested fields, the result includes a non-blocking `shape_hint` warning naming the missing fields. The write still succeeds. Agents are free to ignore.

**Bearer-to-user attribution.** MCP writes attribute to the bearer's actual user (Issue 7 — fixed in Cut 6). The MCP server resolves the bearer from the HTTP request context just like the REST middleware does.

**Admin operations stay on REST + CLI.** Webhook subscribe/revoke/list, page locks, team management — all moved to `/api/admin/*` and the `agentboard admin` CLI. MCP is the agent realm; admin operations never expose through MCP. See AUTH.md §"MCP invariant".

### Suggested shapes (spec §8)

The server stores any well-formed payload — schemas are documentation, not enforcement. But certain paths look like they belong to a known shape, and missing fields trigger a `shape_hint` warning:

| Path glob | Suggested fields |
|---|---|
| `tasks/*`, `tasks/**`, `*/tasks/*` | `title`, `status` |
| `metrics/*`, `metrics/**`, `*/metrics/*` | `value`, `label` |
| `skills/*/SKILL` | `name`, `description` |

Adding a shape is a one-liner in `internal/store/shapes.go` plus a new subsection in spec §8.

### Path layout (spec §1)

| Concept | Path |
|---|---|
| Pages (MDX docs) | `<path>` |
| Singleton values | `<key>` (frontmatter only, no body required) |
| Collection items | `<path>/<id>` |
| User-composed streams | `<path>` (.ndjson on disk) |
| Binary files | `files/<name>` (mint via `agentboard_request_file_upload`) |
| Skills | `skills/<slug>/SKILL` |

**Backup:** `agentboard backup --to ./snapshot.tar.gz` and `agentboard restore --from ./snapshot.tar.gz`. Tar excludes SQLite WAL/SHM and the bootstrap-secret URL.

---

## When the user ships a feature

If the user just shipped something (landed a commit, says "we shipped X", asks for the dashboard to reflect new work), update the dashboard in this order:

1. **Write a feature page** under `content/features/<slug>.md` via `PUT /api/content/features/<slug>` or MCP `agentboard_write({items: [{path: "features/<slug>", frontmatter: {…}, body: "…"}]})`. Use the feature-page template below.
2. **Update the feature list data** at `dev.features.shipped` (array of `{id, title, status: "done", landed_at}`) via `agentboard_write({items: [{path: "dev.features.shipped", frontmatter: {value: [...]}}]})` or `PUT /api/data/dev.features.shipped`. The home page Kanban/List reads from this.
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

1. PUT the new value to the appropriate `dev.*` key via REST (`PUT /api/data/dev.foo`) or MCP (`agentboard_write({items: [{path: "dev.foo", frontmatter: {value: 42}}]})`).
2. SSE broadcasts it within ~100 ms. No page write needed.

When the user asks what's currently on the dashboard:

1. `GET /api/content` — lists all pages.
2. `GET /api/index` — flat catalog of every leaf in the project, including data keys.
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

1. Write the manifest via `PUT /api/content/skills/<slug>/SKILL.md` (or the MCP
   tool `agentboard_write({items: [{path: "skills/<slug>/SKILL", frontmatter: {…}, body: "…"}]})`). A `.md`
   upload triggers an inline page reindex, so the SKILL renders at
   `/skills/<slug>/SKILL` and is full-text searchable immediately.
2. Upload any supporting files to the same folder via `agentboard_request_file_upload`.
3. Verify with `agentboard_list({path: "skills/"})` — the skill folder should appear.
4. Test the bundle endpoint: `GET /api/skills/<slug>` returns a zip.

Avoid teaching users about `content/skills/` directly — they should go
through the agent. The convention is enforced by documentation here, not in
code.

---

## Skill-update triggers (re-read when you ship)

Before marking a feature shipped, ask: does this change affect how an agent would talk to AgentBoard? If **yes**, edit this file in the same commit. Concretely:

- **Added or removed a built-in component** → update the "Component choices" list and any data-key conventions for its typical `source`.
- **Added or removed an MCP tool** → update any tool reference (including the "How to invoke" section) plus the `dev.mcp.tools` expected count in "Data-key conventions".
- **Touched the store surface** (new envelope field, new shape, new endpoint, new error code) → update the "Store surface (files-first)" section. The store contract is the agent-facing contract for all new work; drift here is the highest-cost kind.
- **Added or changed a REST route** → update curl examples and the endpoint references (e.g. the `PUT /api/content/...` line in "When the user ships a feature").
- **Renamed or moved a `dev.*` key** → update the table in "Data-key conventions". Old keys left in the skill are worse than no keys; they send the next agent to a dead path.
- **Changed a trigger phrase or a setup step** (e.g. new `--flag` on the binary, new port, new project name) → fix the YAML `description` up top AND any embedded command lines.
- **New convention the skill doesn't mention** (a new page pattern, a new dogfood metric, a new file-layout rule) → add it. If a convention isn't in the skill, it doesn't exist for the next agent.

Rule of thumb: if a reader of this file would write code that now fails or silently diverges from the codebase, the skill is stale and must be fixed before you commit.
