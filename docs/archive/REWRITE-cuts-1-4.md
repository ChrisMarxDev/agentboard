# Rewrite landing doc

> **Read this first if you've been away.** AgentBoard went through a structural rewrite in cuts 1–4 (commits `03ca3d1` → `5c3d8a8`). The old SQLite key/value store, `/api/data/*` REST surface, `/api/v2/*` parallel surface, `agentboard_v2_*` MCP tools, and `<DataView>` escape hatch are all gone. This doc is the snapshot of where things landed; the locked contract that drove the cuts is in [`spec-rework.md`](./spec-rework.md). Product invariants stay in [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md). Full design is in [`spec.md`](./spec.md).

## The new mental model in one screen

- **One leaf type — `.md`.** YAML frontmatter holds structured fields; the body holds prose + JSX components. A page *is* its data.
- **Folders are collections.** `tasks/<id>.md` cards make up the `tasks/` board. `<Kanban source="tasks/" />` walks the folder.
- **Streams stay** as `.ndjson` (the only non-`.md` content leaf). Append-only, lock-free.
- **Binaries are files.** Unchanged from before — uploaded via presigned URL, served from `/api/files/`.
- **Single tree under the project root.** No `data/` vs `content/` split. No `v2`.
- **No atomic field-level ops.** No `INCREMENT`, no field CAS. Read-modify-write the whole doc; full-file CAS via `_meta.version` (or HTTP `If-Match`).

If you remember `agentboard_set("foo.bar", 42)` — that's gone. The new equivalent is "find the page that displays `foo.bar`, edit its frontmatter, write the page."

## What got deleted

| Surface | Replacement |
|---|---|
| `internal/data/` (SQLite KV, schema, history, ifmatch, mergepatch) | `internal/store/` (files-first; `internal/db/` is the bare SQLite wrapper for auth/teams/locks/invitations/inbox) |
| `/api/data/*` REST routes | `/api/content/*` for pages; `/api/files/*` for binaries; `/api/view/open` for SPA reads |
| `agentboard_set / merge / append / delete / get / list_keys / get_data_schema / upsert_by_id / merge_by_id / delete_by_id` | The 8-tool MCP set — `agentboard_read_page`, `agentboard_write_page`, `agentboard_list_pages`, `agentboard_search_pages`, `agentboard_read_file`, `agentboard_write_file`, `agentboard_list_files`, `agentboard_get_skill`, etc. (run `tools/list` to see the live set) |
| `<DataView>` component, `useData_envelope` hook | `useData(key)` reads from the per-page `DataContext` bundle; `<ApiList src=...>` covers ad-hoc REST list rendering |
| Server `cfg.Store data.DataStore` | `cfg.Conn *sql.DB` (only auth-tier modules touch it now) |
| `bruno/tests/01-data/` (16 legacy KV contract tests) | `bruno/tests/10-data/` (24 file-first contract tests) |
| `scripts/v2-smoke-test.sh` framing | `scripts/smoke-test.sh` (35 assertions, single tree) |

## What's new

- **Files-first store** (`internal/store/`). One `Doc` model, atomic rename writes, content-addressed ETags, content-history NDJSON. Streams via `.ndjson`. Singleton at `<key>.md`; collection items at `<key>/<id>.md`.
- **YAML frontmatter as data.** Page authors put values in frontmatter; components reference them with `source="key"`. The view broker splats matching keys into the bundle so components don't make extra HTTP calls.
- **Folder-collection resolver** (`internal/server/handlers_view.go:folderChildren`). `<Kanban source="tasks/" />` walks the page tree by prefix and returns each child's frontmatter as a row. The trailing `/` is the marker.
- **Three-layer broker resolution** (per page open):
  1. Page's own frontmatter — splatted under the same keys.
  2. Folder-source `source="path/"` — walks the page tree.
  3. Files-first store — `readV2Unwrapped` for keys not handled above.
- **MDX-everywhere page tree.** `internal/mdx/page.go` parses frontmatter (typed + full `map[string]any`), normalizes `SKILL.md` to its parent folder URL, orders pages hierarchically. The watcher updates `RefStore` only when writes go through the API — direct disk edits silently break refs (see "Gotchas" below).
- **Dogfood content rebuilt** under `~/.agentboard/agentboard-dev/`: 11 core pages, 28 component subpages with `## Full MDX` examples, a real `/tasks` folder-collection kanban with 6+ child docs.
- **Stable shell, keyed bundle** (`frontend/src/App.tsx`). `Layout` now wraps the keyed `DataProvider`, so `Nav` and sidebar state survive navigation. The page bundle still re-fetches per path.

## Component catalog (28 builtins)

`GET /api/components` and `agentboard_list_components` return the full catalog. Each one has a subpage at `/components/<name>` with live examples, props table, and a `## Full MDX` block of literal source. Recently added to the Go meta (cut 4 cleanup): `Button`, `Inbox`, `Mention`, `RichText`, `Sheet`, `SkillInstall`, `TeamRoster`.

Components consume one of three input shapes:
```mdx
<Counter value={42} />            {/* inline literal — preferred for hand-authored pages */}
<Counter>42</Counter>              {/* same as value=, syntactic sugar */}
<Counter source="cuts_shipped" /> {/* reads from this page's frontmatter */}
```

There is no cross-page `source="other-page:field"` syntax. If two pages need the same value, denormalize. Folder collections (`source="tasks/"`) are the one cross-doc reference allowed.

## Gotchas (the things that bit during the rewrite)

- **Don't write `content/*.md` directly from the file system.** The page watcher picks up the new file and `ScanPages()` indexes it, but `RefStore` only updates inside the `PUT /api/content/*` handler. So the page renders, but `<Kanban source="tasks/">` returns empty data because `tasks/` isn't in scope. Always go through the API for any page that components reference. CLAUDE.md flagged this; the rewrite confirmed it the hard way.
- **chi `r.Route("/", ...)` swallows sibling `/api/search` registrations** when used purely for middleware scoping. Use `r.Group(...)` for middleware-only nesting.
- **Two `agentboard_search` collisions** (legacy page search vs new store search) and two `/api/search` collisions were resolved by renaming the legacy → `agentboard_search_pages` and `/api/search/pages`. Future unified search will collapse them again (see "Pending design intent").
- **Direct SQL UPDATE to promote a user is denied by the permission system.** Use `agentboard --project <name> admin rotate <username> <label>` to mint a fresh token slot.
- **404 from `/api/view/open` is not an error.** `DataContext` returns a null bundle without setting `error`; `PageRenderer` falls through to the "page not found" affordance. Treating 404 as auth-required produced spurious login prompts.

## Pending design intent

These are agreed-on directions, not open questions:

- **Kanban folder autowiring** — when a page contains `<Kanban>` (no `source`), cards should auto-attach from the page's *own* folder. Currently `<Kanban source="tasks/">` requires a sibling `content/tasks/` folder; the user expects the kanban page to be the index of its own folder. Memory: `feedback_kanban_folder_autowire.md`.
- **Pages + store file-layer merge** — `internal/mdx` and `internal/store` still maintain separate read/write paths over the same on-disk tree. Merging them is the largest pending refactor. Tracked at `/tasks/migrate-pages-store`.
- **Unified search** — collapse `agentboard_search` (store) + `agentboard_search_pages` (pages) into one ranked feed. Tracked at `/tasks/unified-search`.
- **Component prop polish** — bring `meta.description` strings up to lead with the inline form (`<Counter value={42} />`) rather than the source form. Tracked at `/tasks/component-prop-polish`.

## How to verify the rewrite is healthy

```bash
task test:go                                         # 17 packages, all green
task test:frontend                                   # 98 vitest passes
task test:integration                                # 35-assertion smoke + bruno contract tests
./agentboard --project agentboard-dev --port 3000 --no-open &
curl -s http://localhost:3000/api/health             # → 200
curl -s http://localhost:3000/api/components | jq 'length'   # → 28
```

For browser-level checks, the dogfood instance lives at <http://localhost:3000>. The `/components` catalog page lists every builtin; `/tasks` is the canonical folder-collection example; component subpages each demonstrate inline + `source` usage with copyable MDX.

## Where to find what

| Concern | Path |
|---|---|
| Files-first store | `internal/store/` (`envelope.go`, `keys.go`, `singleton.go`, `collection.go`, `stream.go`) |
| Page manager + frontmatter parse | `internal/mdx/page.go`, `internal/mdx/refs.go`, `internal/mdx/watch.go` |
| View broker (the SPA's read path) | `internal/server/handlers_view.go`, `internal/view/scope.go` |
| Component meta | `internal/components/manager.go` (`registerBuiltins()`) |
| MCP tool surface | `internal/mcp/tools.go` (legacy page tools), `internal/mcp/store_tools.go` (8-tool set) |
| Auth (unchanged) | `internal/auth/`, `AUTH.md` |
| Dogfood seed | `~/.agentboard/agentboard-dev/` (per-user, not in repo) |
| Skill (auto-loaded by Claude Code) | `.claude/skills/agentboard/SKILL.md` |
| Smoke + contract tests | `scripts/smoke-test.sh`, `bruno/tests/10-data/` |

## When you change something an agent sees

If you alter the wire contract — a new builtin, a new MCP tool, a renamed REST route, a changed trigger phrase, a new data-key convention — update **`.claude/skills/agentboard/SKILL.md`** in the same commit. The skill auto-loads in this repo; a stale skill means the next agent (you, tomorrow) builds against outdated assumptions.
