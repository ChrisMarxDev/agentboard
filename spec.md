# AgentBoard — Design Spec

> **Status:** the locked contract for the project shape. Every load-bearing structural decision lives here. Implementation drift from this doc is a bug; if reality changes, update this doc in the same PR. **Pre-launch — no backward compatibility, no migrators.**
>
> **Companion docs (live):** [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md) — the 13 product principles. [`AUTH.md`](./AUTH.md), [`HOSTING.md`](./HOSTING.md), [`spec-plugins.md`](./spec-plugins.md), [`seams_to_watch.md`](./seams_to_watch.md), [`SCALE.md`](./SCALE.md) — domain contracts. [`ROADMAP.md`](./ROADMAP.md) — what ships next. [`ISSUES.md`](./ISSUES.md) — known bugs (the spec wins ties).
>
> **Historical context:** earlier rewrite snapshots and aspirational drafts live under [`docs/archive/`](./docs/archive/). They are not load-bearing; do not link from agent-facing skills.

## 1. Thesis — content is files; operational state is SQLite

The project has two storage tiers, separated by who composes the data:

**Content tier — files under one tree.** Everything humans and agents compose directly:

| Concept | Path |
|---|---|
| Pages (MDX docs) | `<path>.md` |
| Singleton values | `<key>.md` (frontmatter only, no body required) |
| Collection items (tasks, customers, runbooks, kanban cards…) | `<path>/<id>.md` |
| User-composed streams | `<path>.ndjson` |
| Binary files | `files/<name>` (sidecar `<name>.meta.json`) |
| Components | `components/<name>.jsx` (gated by `--allow-component-upload`) |
| Skills | `skills/<slug>/SKILL.md` (+ supporting files in folder) |

**Operational tier — SQLite.** Machine-managed indexes the user/agent never composes as text:

| Domain | Why SQLite |
|---|---|
| Users, tokens, sessions, invitations, OAuth clients | Auth — sensitive, rapidly-mutating, indexed lookups |
| Teams, team members | Membership — admin-managed |
| Page locks | Edit-state metadata — admin-managed |
| Webhook subscriptions | Operational config with secrets |
| Inbox messages, share tokens | Internal messaging metadata |
| `content_history` index, `activity` log index | Content audit — system-written, queryable |
| FTS5 search index | Derived from the file tree |
| Rate-limit bucket | Ephemeral; in-memory |

The line: *do agents and humans compose this directly?* If yes, file. If no, row.

There is **no parallel namespace** in the content tier. No `data/` vs `content/` split. Pages, data, tasks, skills are all the same `.md` shape under one tree, distinguished by path convention.

## 2. The leaf rules (content tier)

| Shape | When | Atomicity |
|---|---|---|
| `.md` doc | Anything with structured fields and/or prose | Full-file CAS via `_meta.version`. Read-modify-write for partial updates. |
| `.ndjson` stream | User-composed append-only logs (daily journals, custom event streams) | `O_APPEND` atomic for lines ≤ PIPE_BUF. No CAS surface. |
| Binary | Uploads served at `/api/files/<name>` | SET-only via presigned URL. Sidecar `<name>.meta.json` for envelope. |

No atomic field-level ops. No INCREMENT, no field-level CAS. Agents read the whole doc, modify, write the whole doc back. File-level `_meta.version` CAS handles concurrent writers. Stream append is the single exception (`POST /api/<path>:append`).

## 3. Frontmatter contract

Standard YAML frontmatter. Server-managed fields live under `_meta`:

```mdx
---
title: Build the kanban
status: in-progress
priority: 2
_meta:
  version: 2026-04-28T14:32:11.123456789Z
  created_at: 2026-04-20T10:00:00Z
  modified_by: alice
---

# Build the kanban
…
```

Rules:

- `_meta` is server-owned. Agents echo `_meta.version` for CAS; the server strips agent-supplied `_meta` fields except `version`.
- All other frontmatter keys are user-owned. Server treats them opaquely. **`order:` in particular is opaque** — agents write whatever sort hint they want, the server stores it verbatim and components are free to read it. Page-tree traversal order is server-derived from path-sort and surfaces under the separate `_meta.order` field; the two never collide.
- A `.md` with no body is fine (singletons typically have only frontmatter). A `.md` with no frontmatter is fine. A doc that is just `42\n` is fine.
- **No shape validation on writes.** Per `CORE_GUIDELINES §8`, the server stores whatever it parses. Wrong-shape errors come from components at render time, not from the write path. Section 8 of this spec gives suggested shapes for common types — those are *hints to the authoring agent*, never enforcement.
- **Speak, don't reject.** When a write under a known suggested-shape path is missing common fields, the response includes a non-blocking `warnings` array per §6 (Shape warnings) and `CORE_GUIDELINES §12`. The write still succeeds. Agents are free to ignore the warning.

## 4. Folder rules

A folder is a collection. Members are direct `.md` children. Subfolders are nested collections.

- `tasks/index.md` — optional. The page rendered when you visit `/tasks`.
- Without `index.md` — the server renders a default list view of frontmatter snippets.
- `tasks/task-42.md` — one card. Frontmatter is the structured fields the Kanban reads.
- `tasks/_archive/` — subfolders are nested collections; not flattened into the parent.
- `<Kanban>` with no `source` attribute auto-attaches to its own folder (the kanban page is the index of its own folder).

## 5. REST surface (content tier)

One namespace, one CRUD per leaf:

```
GET    /api/<path>              read doc / serve binary / list folder / tail stream
PUT    /api/<path>              full write; CAS via _meta.version or If-Match
PATCH  /api/<path>              merge frontmatter + optional body
DELETE /api/<path>              remove (idempotent)
POST   /api/<path>:append       stream append (only `:` verb)

GET    /api/index               flat catalog of every leaf
GET    /api/search?q=...        full-text across frontmatter + body + stream lines
GET    /api/<path>/history      per-doc history (server reads from the SQLite content_history index)
GET    /api/activity            global write log         (server reads from the SQLite activity index)

POST   /api/files/request-upload          mint presigned URL
PUT    /api/upload/<token>                accept raw bytes (no auth — token gates)
```

Conflict response (`412`) embeds the current envelope. Wrong-shape errors (`409`) name actual + expected. Rate limit (`429`) carries `Retry-After` + `retry_after_seconds`. All per `CORE_GUIDELINES §12`.

**Initial-write semantics.** A PUT to a path with no existing leaf MUST succeed without `If-Match` — the absence of a prior version means there's nothing to be stale against. CAS only applies when an existing version is being overwritten. (This corrects an ergonomic friction noted in `ISSUES.md`.)

**Auth surfaces are separate.** `POST /api/auth/login`, `GET /api/auth/me`, `POST /api/invitations/<id>/redeem`, the OAuth `/oauth/*` flow, the admin endpoints `/api/admin/*` and `/api/users/*` — all read and write SQLite tables, not files. They are not part of the content surface.

**Implementation status.** Cut 6 ships the MCP surface change (§6) and the shape-warning system (§8). The REST unification described above is the locked target shape; the live HTTP layer still mounts pages under `/api/content/<path>` and data under `/api/data/<key>` while the legacy routes get retired in a follow-up cut. The spec wins on intent; the wire is one cut behind.

## 6. MCP surface — eight tools + named extensions, always-plural

The MCP server exposes the **content tier** only. Auth and operational state are out of scope: agents don't manage users, tokens, or webhook subscriptions through MCP — those are admin operations behind dedicated REST + CLI surfaces.

Every write/read/delete tool accepts a **batch by default** (Notion / Sanity / GitHub pattern). Single-item operations wrap in a one-element array. This collapses N-page builds into one round-trip and matches what every winning content-MCP server does.

```
Read tier:
  agentboard_read(paths)              — paths: [string]
                                         → returns [{path, frontmatter, body, version, warnings?}]
  agentboard_list(path)               — folder children + frontmatter snippets (single path)
  agentboard_search(q, scope?)        — FTS5 + substring across the tree

Write tier (always batch):
  agentboard_write(items)             — items: [{path, frontmatter?, body?, version?}]
  agentboard_patch(items)             — items: [{path, frontmatter_patch?, body?, version?}]
  agentboard_append(path, items)      — items: [any]; one stream per call; race-free
  agentboard_delete(items)            — items: [{path, version?}]

Files:
  agentboard_request_file_upload(items) — items: [{name, size_bytes}]
                                          → returns [{name, upload_url, token, expires_at}]

Named extensions:
  agentboard_grab(picks)              — cross-page materializer
  agentboard_fire_event(event, body?) — emit on webhook bus
```

**Eight generic tools cover the content tier.** No `_page` vs `_data` split. No `read_skill` / `write_skill` — skills are pages at `skills/<slug>/SKILL.md`. No `create_team` — teams are an operational concern, managed via `/api/admin/*`, not MCP. **10 tools total** (8 generic + grab + fire_event), down from 38.

### Batch response shape

Write/patch/delete return a per-item result. **Best-effort semantics** — partial success is normal; the agent inspects per-item status and retries failures:

```json
{
  "results": [
    {
      "path": "tasks/task-1.md",
      "success": true,
      "version": "2026-04-30T...",
      "warnings": [{"code": "shape_hint", "message": "...", "see": "spec.md §8"}]
    },
    {
      "path": "tasks/task-2.md",
      "success": false,
      "error": {"code": "version_conflict", "current_version": "...", "current_envelope": {...}}
    }
  ],
  "all_succeeded": false
}
```

All-or-nothing transactional batching is **not** in v1. The filesystem doesn't give us atomic multi-doc commits for free, and best-effort + retry covers the happy path (the only path 95% of the time). A future `agentboard_apply(operations)` with transactional semantics is reserved for v2 if pain materializes; it requires a staged-write/journal primitive in `internal/store/`.

### Native JSON values

`agentboard_write` and `agentboard_patch` accept payloads as **native JSON** — the MCP wrapper does not double-stringify. REST and MCP have identical type semantics for a given write. (Today's `agentboard_write` / `agentboard_merge` double-stringify; Cut 6 fixes this.)

### Reads return the full envelope

`agentboard_read` returns `[{path, frontmatter, body, version}]`. Frontmatter and body are always both included. The patch-and-verify loop never needs an out-of-band REST call.

### Shape warnings — speak, don't reject

Per `CORE_GUIDELINES §8` and §13, the server stores any well-formed payload — schemas are suggestions, not gates. But silence on a drifting shape is unhelpful: per `CORE_GUIDELINES §12` (responses are repair manuals), the response includes a non-blocking `warnings` array when a write looks like it's missing fields from the path's suggested shape (§8). The write succeeds. The warning names the shape and the missing fields.

Example warning body:

```json
{
  "code": "shape_hint",
  "message": "Path looks like a task (under tasks/) but frontmatter has no `title` or `status`. The Kanban component will use the filename as the card label and won't group this card.",
  "see": "spec.md §8 — Suggested shape: tasks",
  "missing_suggested_fields": ["title", "status"]
}
```

Agents are free to ignore. The warning is for the agent that *would have wanted to know*. The server never refuses a well-formed write because of shape drift.

## 7. Component `source=` semantics

Components stop indirecting through dotted KV keys. Three forms only:

- `value={...}` or children (`<Counter>42</Counter>`) — literal.
- `source="field.path"` — frontmatter field on the doc the component is rendering in. JSON-pointer-style nesting via dots.
- `source="folder/"` — collection: read every child's frontmatter.
- `source="path/to.ndjson"` — stream tail.

No cross-page `source="other-page:field"` syntax. If two pages need the same value, denormalize. Folder collections are the only cross-doc reference allowed.

Components are **liberal in what they accept** (Postel's Law inverted, per `CORE_GUIDELINES §8`). A `<Kanban>` over `tasks/` works whether each card has `status` or `col` or no grouping field at all. A `<Chart>` accepts array-of-objects, `{labels, values}`, or `{name, value}` pairs. Missing fields render as graceful blanks; never as 500s.

## 8. Loose shapes — frontmatter as suggestion

Per `CORE_GUIDELINES §8` and §13, the server validates **safety invariants** (path, size, auth) and stores anything else opaquely. Components handle missing/extra fields gracefully. **Authoring agents are free to invent shapes.**

To reduce drift across instances, the spec ships **suggested frontmatter shapes** for common collection types. These are documentation, not enforcement — the server accepts a task with no fields, with extra fields, or with renamed fields. Components fall back. The suggestion exists so that agents tend to converge on the same shape across projects, and so that the bundled components have something predictable to render against by default.

### Suggested shape: tasks (`tasks/<id>.md`)

```yaml
---
# All fields below are SUGGESTIONS — agents may omit, add, or rename
# any of them. The server accepts whatever is written; components
# render around what's present and ignore what isn't.
title: Build the kanban             # short summary; falls back to filename if absent
status: in-progress                  # arbitrary string; Kanban groupBy="status" by default
priority: medium                     # arbitrary string or number
assignees: ["@alice", "@team:eng"]   # @username or @team mentions
due: 2026-05-15                      # ISO date or null
tags: ["backend", "urgent"]
description: Optional one-liner; the page body holds the full description
---

The full task body lives here as MDX. Drop screenshots, links, or
conversation excerpts. The Kanban card shows `title` + `status` +
`assignees`; clicking opens this page.
```

### Suggested shape: data singletons (`metrics/<key>.md`)

```yaml
---
value: 42                            # literal scalar OR object OR array
label: Daily active users            # human-readable name
unit: users                          # optional
updated_at: 2026-04-30T08:15:00Z     # agent-set or server-derived
---
```

### Suggested shape: skills (`skills/<slug>/SKILL.md`)

```yaml
---
name: agentboard                     # required by the Anthropic skill format
description: Authoring AgentBoard pages, data, and collections via MCP.
---

# Skill body in standard Anthropic skill format.
```

### Adding more suggested shapes

When a new content type emerges (incidents, customers, deploy events…), add a "Suggested shape" subsection here. The bar: at least two collections in the wild that converged on roughly the same shape. Keep each suggestion ≤ 10 fields; remember it's a hint, not a schema.

### Path → shape mapping

The server maps paths to suggested shapes for the warning system in §6. Currently:

| Path glob | Suggested shape |
|---|---|
| `tasks/**/*.md`, `*/tasks/*.md` | tasks (above) |
| `metrics/**/*.md`, `*/metrics/*.md` | data singletons (above) |
| `skills/*/SKILL.md` | skills (above) |

A write under a matching glob with all suggested fields present yields no warnings. A write missing some fields yields one `shape_hint` warning naming the missing fields. A write that doesn't match any glob yields no shape warnings (the server has no opinion). Adding a glob is a one-liner in `internal/store/shapes.go` plus a new subsection in this section of the spec.

## 9. What gets deleted in the next rewrite

Cuts 5–6 (§11) collapse the content tier onto the spec. Operational SQLite stays — see §10.

| Surface | Replacement |
|---|---|
| `internal/mdx/` separate read path | folded into `internal/store/` (`/tasks/migrate-pages-store`) |
| Per-domain MCP tools (`agentboard_lock_page`, `agentboard_create_team`, `agentboard_search_pages`, `agentboard_read_page`, `agentboard_write_page`, `agentboard_list_skills`, `agentboard_get_skill`, `agentboard_*_webhook`, `agentboard_*_team`, `agentboard_clear_errors`, etc.) | gone — replaced by the 8-tool generic CRUD + 2 named extensions in §6. Admin operations (lock/team/webhook) move to REST + CLI, not MCP |
| MCP `agentboard_write` / `agentboard_merge` value double-stringification | normalized to native JSON; same shape as REST |
| Two `search` tools (`agentboard_search` + `agentboard_search_pages`) | one `agentboard_search` with optional `scope` |
| Singular write/read/delete | always-plural batch shape (Notion pattern); see §6 |

## 10. What survives

- Files-first storage envelope, `_meta.version`, server-monotonic timestamps, atomic rename writes (content tier).
- The full SQLite operational tier — auth (`internal/auth/`), teams, locks, webhooks, inbox, share, invitations, OAuth clients. None of this moves.
- The `content_history` and `activity` SQLite tables (planned in `ROADMAP.md` Milestone B) — write-paths still stream to the file tree, but the queryable index is SQL.
- Presigned URL upload flow.
- Token-bucket rate limiter (in-memory; derived).
- `agentboard backup / restore` CLI — content via `tar`, operational via `sqlite3 .backup`.
- The 13 principles in `CORE_GUIDELINES.md`.
- The OAuth 2.1 + DCR + PKCE flow on `/oauth/*`.
- The current 32 built-in components.
- The `<DataContext>` bundle / `useData` hook on the SPA.
- The skill system: `skills/<slug>/SKILL.md` + supporting files in the folder.

## 11. Cut order — the next rewrite

Two cuts. Both are content-tier surgery; auth/operational SQLite is untouched.

1. **Cut 5 — pages + store file-layer merge.** Fold `internal/mdx` into `internal/store`. One read path, one write path, one watcher, one CAS for the entire content tree. Tracks at `/tasks/migrate-pages-store`.
2. **Cut 6 — MCP collapse to 10 tools.** Replace the 38 domain-specific tools with the 8 generic + 2 named extensions (§6). The skill manifest gains a path-layout teaching section + the suggested-shape catalog from §8. Existing per-domain tools removed in one PR; agents re-onboard via the new manifest. Includes the `value` JSON-serialization fix on `write`/`patch`.

Each cut is one PR. Cut 5 is a prerequisite for Cut 6 — without one read path, the generic tools can't dispatch correctly. After Cut 6, every issue tagged `[obsolete]` in `ISSUES.md` can be removed; `[cut 6]` issues need verification on the new tools.

## 12. What's NOT in the content tier

The principle is about *content state*, not *every byte of state*.

- **Operational SQLite tables** (auth, teams, locks, webhooks, inbox, share, invitations, oauth_clients) — explicit, by design. See §10.
- **Open SSE connections** — process state, evicted on restart.
- **Rate-limit bucket** — in-memory; derived.
- **FTS5 search index** — derived from the file tree; rebuilt on `agentboard reindex` or first cold start.
- **Page watcher RefStore** — derived; rebuilt by walking the tree on startup.
- **Server config (`agentboard.yaml`)** — file, but *not* in the project tree; it's the project's bootstrap. Editable by the operator, not by agents.

The test: *can I tar the project root, drop the SQLite operational database, restore both, and have the dashboard come back identical?* Both sides need to round-trip.

## 13. Open questions

None. Every load-bearing decision is locked. Concrete coding decisions (route paths, error code strings, exact JSON shapes) follow the §5/§6 surfaces.

If you find a decision that *feels* open while implementing, the spec is wrong — fix this doc in the same PR as the code.

---

**End of spec.**
