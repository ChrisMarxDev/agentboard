# AgentBoard — Rewrite Spec

> **Status**: Implementation contract for the rewrite that supersedes `spec-file-storage.md`. Locks the design decisions reached in the planning conversation. **Pre-launch — no backward compatibility, no migrators.** The legacy SQLite KV, `/api/data/*` routes, dual-store fallback, `v2` framing, and `<DataView>` escape hatch all get deleted.

## 1. Thesis

Pages, data, and binary files were three storage idioms living in parallel. The rewrite collapses them into one:

- **One leaf type** — `.md` files. Frontmatter holds structured fields; the body holds prose and JSX components.
- **Folders are collections.** `tasks/` is a kanban board; each `tasks/<id>.md` is a card with frontmatter + body.
- **Streams stay** as `.ndjson` — the only non-`.md` content leaf. Append-only, lock-free.
- **Binaries are files.** Unchanged.

Everything that was `data/<key>.json` becomes either:
- A frontmatter field on the page that displays it, or
- A literal value in the page's JSX (`<Counter value={42} />` or `<Counter>42</Counter>`).

The whole project is one tree under the project root. There is no `data/` namespace. There is no v2.

## 2. The leaf rules

| Shape | When | Atomicity |
|---|---|---|
| `.md` doc | Pages, kanban cards, customers, runbooks, anything with structured fields and/or prose | Full-file CAS via `_meta.version`. Read-modify-write for partial updates. |
| `.ndjson` stream | Append-only logs, telemetry, activity | `O_APPEND` atomic for lines ≤ PIPE_BUF. No CAS surface. |
| Binary | PDFs, screenshots, images | SET-only via presigned URL. Sidecar `<name>.meta.json` for envelope. |

**No atomic field-level ops.** No INCREMENT, no field-level CAS. Agents read the whole doc, modify, write the whole doc back. File-level `_meta.version` CAS handles concurrent writers.

The single exception is **`POST /api/<path>:append`** for streams — they cannot be read-modify-written.

## 3. Frontmatter contract

Standard YAML frontmatter. Server-managed fields live under `_meta`:

```mdx
---
title: Build the kanban
col: in-progress
priority: 2
_meta:
  version: 2026-04-28T14:32:11.123456789Z
  created_at: 2026-04-20T10:00:00Z
  modified_by: alice
---

# Build the kanban

The detailed task description lives in the body.
You can drop screenshots, links, conversation excerpts.

<Status state="active" />
```

Rules:
- `_meta` is server-owned. Agents echo `_meta.version` for CAS but cannot forge other fields. Server strips agent-supplied `_meta` fields except `version`.
- All other frontmatter keys are user-owned. Server treats them opaquely.
- A `.md` with no body is fine. A `.md` with no frontmatter is fine. A doc that is just `42\n` is fine.

## 4. Folder rules

A folder is a collection. Members are direct `.md` children. Subfolders are nested collections.

- `tasks/index.md` — optional. The page rendered when you visit `/tasks`.
- Without `index.md` — the server renders a default list view of frontmatter snippets.
- `tasks/task-42.md` — one card. Frontmatter is the structured fields the Kanban reads.
- `tasks/_archive/` — subfolders are nested collections; not flattened into the parent.

## 5. REST surface

One namespace, no `data/` vs `content/`:

```
GET    /api/<path>              read doc / serve binary / list folder / tail stream
PUT    /api/<path>              full write; CAS via _meta.version or If-Match
PATCH  /api/<path>              merge frontmatter + optional body
DELETE /api/<path>              remove (idempotent)
POST   /api/<path>:append       stream append (only `:` verb)

GET    /api/index               flat catalog of every leaf
GET    /api/search?q=...        full-text across frontmatter + body + stream lines
GET    /api/<path>/history      per-doc history NDJSON
GET    /api/activity            global write log

POST   /api/files/request-upload          mint presigned URL
PUT    /api/upload/<token>                accept raw bytes (no auth — token gates)
```

Conflict response (`412`) embeds the current envelope. Wrong-shape errors (`409`) name actual + expected. Rate limit (`429`) carries `Retry-After` + `retry_after_seconds`. All per `CORE_GUIDELINES §12`.

## 6. MCP surface — eight tools

```
agentboard_read(path)                     — any leaf
agentboard_list(path)                     — folder children with frontmatter
agentboard_write(path, frontmatter?, body?) — full doc write
agentboard_patch(path, frontmatter_patch?, body?)
agentboard_append(path, value | items)    — stream
agentboard_delete(path)
agentboard_search(q)
agentboard_request_file_upload(name, size_bytes)
```

Down from 25 (legacy + v2). No `_set`/`_get`/`_merge`/`_increment`/`_cas`/`_upsert_by_id`/`_merge_by_id`/`_delete_by_id`/`_get_data_schema`/`_list_keys` — every one of those collapses into the eight above.

## 7. Component `source=` semantics

Components stop indirecting through dotted KV keys. Three forms only:

- `value={...}` or children (`<Counter>42</Counter>`) — literal.
- `source="field.path"` — frontmatter field on the doc the component is rendering in. JSON-pointer-style nesting via dots.
- `source="folder/"` — collection: read every child's frontmatter.
- `source="path/to.ndjson"` — stream tail.

No cross-page `source="other-page:field"` syntax. If two pages need the same value, denormalize.

## 8. What gets deleted (irreversibly)

- `internal/data/` — SQLite KV store, schema, history, ifmatch, mergepatch, all of it.
- `/api/data/*` REST routes.
- `agentboard_set/merge/append/delete/get/list_keys/get_data_schema/upsert_by_id/merge_by_id/delete_by_id` MCP tools.
- The `Store: data.DataStore` field on `server.Server`, `mcp.Server`, `ServerConfig`.
- `<DataView>` component.
- `useData` hook (folded into `useData`).
- `dev.mcp.tools_v2` data key (the metric just becomes `dev.mcp.tools`).
- `/api/*` route prefix (collapses into `/api/`).
- The `bruno/tests/01-data/` folder (legacy KV tests).
- `internal/data/store_test.go` and any test that opens `data.NewSQLiteStore`.
- The view broker's dual-store fallback (legacy-then-v2). Single source now.
- `/data/` namespace on disk. Project root has the unified tree.

## 9. What gets renamed

| Old | New |
|---|---|
| `/api/data/<key>` | `/api/<path>` |
| `/api/index` | `/api/index` |
| `/api/search` | `/api/search` |
| `/api/files/request-upload` | `/api/files/request-upload` |
| `agentboard_*` | `agentboard_*` |
| `useData` | `useData` |
| `internal/store` | `internal/store` (kept; just no longer "the v2 store") |
| `data` SSE event | `data` (the only data-event type) |

## 10. What survives

- The envelope + version + CAS contract — for both `.md` (in frontmatter) and binary (sidecar).
- Server-monotonic timestamp versions.
- Atomic rename for all writes.
- Activity log + per-doc history (NDJSON, rotated at 100 MB × 5).
- Presigned URL upload flow.
- Token-bucket rate limiter.
- `agentboard backup / restore` CLI.

## 11. Cut order

1. **Cut 1 — rip legacy data.** Delete `internal/data/`, all `/api/data/*` routes, all legacy MCP tools. Build clean. Dashboard stays alive on the existing v2 store.
2. **Cut 2 — unify shapes.** Singleton/Collection/Stream → one Doc model. Folders are collections. `.md` is the only leaf type (plus `.ndjson` for streams).
3. **Cut 3 — drop the v2 framing.** `/api/*` → `/api/*`. `agentboard_*` → `agentboard_*`. Rip `useData` and `<DataView>`.
4. **Cut 4 — rewrite components.** The 25-ish built-ins read frontmatter / folders / ndjson via the new `source=` rules.
5. **Cut 5 — dogfood reset.** Wipe `~/.agentboard/agentboard-dev/`. Reseed home, `/dev`, `/architecture`, `/principles`, `/seams`, `/features/*`, `/roadmap` on the new model.

Each cut is one PR. Cut 1 is destructive but bounded — the rest of the system keeps working through the broker bridge until Cut 2.

## 12. Open questions

None. Every load-bearing decision is locked. Concrete coding decisions (route paths, error code strings, exact JSON shapes) are settled by spec-file-storage.md §B; this doc just supersedes the framing.

---

**End of spec.**
