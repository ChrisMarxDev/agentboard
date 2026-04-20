# AgentBoard — Knowledge Feature PRD

> **Status**: Draft. Builds on the v2 spec in `spec.md` and complements `spec-sessions.md`. This feature extends AgentBoard's existing pages tree into a unified knowledge + dashboards surface. Fully backwards-compatible: existing MDX pages keep working with no changes.

---

## Table of Contents

1. [Motivation](#1-motivation)
2. [Design Principles](#2-design-principles)
3. [Non-Goals](#3-non-goals)
4. [Content Model](#4-content-model)
5. [Directory Layout](#5-directory-layout)
6. [Frontmatter Schema](#6-frontmatter-schema)
7. [REST API](#7-rest-api)
8. [Search & Retrieval (RAG)](#8-search--retrieval-rag)
9. [MCP Tools](#9-mcp-tools)
10. [Rendering & Dashboard UX](#10-rendering--dashboard-ux)
11. [File Watcher & Write Semantics](#11-file-watcher--write-semantics)
12. [Security](#12-security)
13. [Implementation Phases](#13-implementation-phases)
14. [Open Questions](#14-open-questions)
15. [Future Work (v2+)](#15-future-work-v2)

---

## 1. Motivation

AgentBoard already has a folder of MDX pages served from the project dir. That tree is, structurally, already a content management system — files, folders, live reload. The gap is that it's only been used for *dashboards*, not *documentation*. There's no way for an agent to stash something it learned ("the PROD deploy script lives in `deploy/fly.sh`, uses env `$FLY_TOKEN`"), or for a human to drop a long-form runbook, and have either of them find it again later — especially not across thousands of docs.

A knowledge repo on top of the existing pages tree closes this gap:

- **Humans read** via a browsable tree with reading-mode layout.
- **Agents read** via an MCP retrieval tool — ask a natural-language question, get ranked chunks with citations.
- **Agents write** via a REST endpoint symmetrical to `/api/data/*`.
- **Humans write** by editing files on disk with hot reload (today's workflow, unchanged).

Everything lives in one filesystem tree, typed by frontmatter. A runbook and a live dashboard are both "content"; the type field picks the render mode.

---

## 2. Design Principles

1. **One tree, frontmatter-typed.** Unify the existing `pages/` with knowledge docs. No separate storage, no separate watchers, no separate API families. The `type` field distinguishes render modes.
2. **Filesystem is source of truth.** The REST API writes files to disk. The file watcher is authoritative. No in-memory content cache that can drift.
3. **Agent-first authoring** (per `spec.md §2`). APIs are shaped for what LLMs naturally produce: plain markdown + YAML frontmatter. The `.mdx` escape hatch exists but is rarely required for knowledge docs.
4. **Retrieval is for agents.** Humans browse and full-text-search; agents ask questions through MCP and get ranked chunks with paths + heading anchors. Different UIs for different callers.
5. **Lexical first, semantic later.** SQLite FTS5 in v1. Embeddings / vector search is a v2 feature behind the same API surface — callers won't need to change.
6. **Cheap to add to an existing project.** Projects without a `content/` folder are unaffected. Dropping a single `.md` file into the project dir is enough to get it into search + retrieval.

---

## 3. Non-Goals

- **No in-browser WYSIWYG editor** in v1. Agents write via REST; humans edit on disk. (Optional v2.)
- **No external ingestion** (Notion, Confluence, Google Drive) in v1. Easy to add later via a separate sync tool that talks to the REST API.
- **No collaborative editing.** Last-write-wins. History is preserved (see §11) for audit.
- **No wikilinks `[[foo]]` / backlink graph** in v1. Add when we see the need.
- **No per-folder permissions.** All content in a project is readable and writable by any caller with project access. Use separate projects for separation-of-concerns.

---

## 4. Content Model

A project's content tree is rooted at `<project-dir>/content/` (see §5 for rename discussion). Every `.md` or `.mdx` file is a document. Folders are just folders. There is no separate folder-metadata file in v1 — the folder's name is its display name.

```
content/
├── welcome.mdx                 # existing dashboard page — unchanged
├── dashboards/
│   └── releases.mdx
├── runbooks/
│   ├── deploy.md
│   ├── rollback.md
│   └── oncall/
│       └── paging.md
└── reference/
    ├── api.md
    └── architecture.md
```

Each file maps 1:1 to:
- A URL: `runbooks/deploy.md` → `/runbooks/deploy`
- An FTS5 row (one row per chunk, see §8)
- A REST path: `PUT /api/content/runbooks/deploy`

Two file extensions are supported:

| Ext    | Behavior                                                                                 |
|--------|------------------------------------------------------------------------------------------|
| `.md`  | Pure markdown. Rendered via the existing markdown pipeline. JSX is rejected (safer for agent authors). Indexed for search & RAG verbatim. |
| `.mdx` | Existing MDX compilation. Can embed components. Indexed for search & RAG with JSX stripped before chunking (see §8). |

---

## 5. Directory Layout

The existing `pages/` folder is renamed to `content/`. Rationale: the tree now holds dashboards *and* docs — "pages" is a mild misnomer, "content" is honest.

**Migration rules** (run on server startup during the release that introduces this feature):

1. If `content/` exists → do nothing.
2. If `content/` does not exist and `pages/` does → rename `pages/ → content/` automatically. Log a one-line info message.
3. If both exist → log a warning, prefer `content/`, ignore `pages/`.

The fallback "read from `pages/` if `content/` is missing" is kept for one release cycle, then removed. Users who upgrade normally never notice.

### Subfolder conventions

Suggested, not enforced. Users organize however they want.

- `content/dashboards/` — data-driven dashboards (today's `type: dashboard` or unset)
- `content/runbooks/` — operational procedures
- `content/reference/` — architecture docs, API specs, decision records
- `content/notes/` — less-structured scratch notes

The sidebar tree mirrors the filesystem exactly.

---

## 6. Frontmatter Schema

YAML frontmatter at the top of every file. All fields are optional; sensible defaults apply.

```yaml
---
title: Rollback a Bad Release
type: doc                    # "doc" | "dashboard" — defaults to "doc" for .md, "dashboard" for .mdx
tags: [ops, incident]        # string[] — free-form
summary: |                   # one-paragraph summary; used in search results + retrieval context
  How to roll back a deploy on Fly.io in under 5 minutes.
searchable: true             # bool — default true. Set false to exclude from search & RAG.
order: 10                    # int — for explicit sidebar ordering. Lower = first. Default: alphabetical by filename.
author: oncall@example.com   # optional free-form
updated_at: 2026-04-17       # optional; otherwise derived from file mtime
---
```

### Defaults

| Field         | Default for `.md` | Default for `.mdx` |
|---------------|-------------------|--------------------|
| `type`        | `doc`             | `dashboard`        |
| `searchable`  | `true`            | `true`             |

### `type: doc` implications
- Rendered with reading-mode layout (narrow column, TOC, prev/next).
- JSX is rejected in `.md` (plain markdown only).
- Chunked for RAG (see §8).

### `type: dashboard` implications
- Rendered full-width with today's component grid layout.
- Chunked for RAG with JSX stripped (so prose around components is still retrievable).

---

## 7. REST API

Base path: `/api/content`. Symmetrical to `/api/data` where it makes sense.

### 7.1 List tree

```
GET /api/content/tree?prefix=runbooks&type=doc

→ 200 OK
{
  "nodes": [
    { "type": "folder", "path": "runbooks", "children": [
      { "type": "file", "path": "runbooks/deploy.md",   "title": "Deploy Guide",     "docType": "doc", "tags": ["ops"], "updatedAt": "..." },
      { "type": "file", "path": "runbooks/rollback.md", "title": "Rollback a Release","docType": "doc", "tags": ["ops"] }
    ]}
  ]
}
```

Filters: `prefix`, `type`, `tag` (repeatable).

### 7.2 Read

```
GET /api/content/runbooks/deploy
→ 200 OK
{
  "path": "runbooks/deploy.md",
  "title": "Deploy Guide",
  "frontmatter": { "type": "doc", "tags": ["ops"], ... },
  "markdown": "# Deploy Guide\n\nStep 1...",
  "updatedAt": "..."
}
```

Content-negotiation: `Accept: text/markdown` returns the raw file with frontmatter intact; `Accept: application/json` (default) returns the parsed structure above.

### 7.3 Create / replace

```
PUT /api/content/runbooks/deploy
Content-Type: application/json

{
  "frontmatter": { "title": "Deploy Guide", "type": "doc", "tags": ["ops"] },
  "markdown": "# Deploy Guide\n\nStep 1..."
}
```

Or raw form:

```
PUT /api/content/runbooks/deploy
Content-Type: text/markdown

---
title: Deploy Guide
type: doc
---
# Deploy Guide
...
```

File extension is inferred: `.md` unless the body contains JSX (detected as `<[A-Z]`), then `.mdx`. Override with `?ext=mdx`.

Response: `{ "ok": true, "path": "...", "updatedAt": "..." }`.

### 7.4 Patch frontmatter only

```
PATCH /api/content/runbooks/deploy
Content-Type: application/json

{ "frontmatter": { "tags": ["ops", "critical"] } }
```

Merges frontmatter, leaves markdown body untouched. RFC 7396 semantics.

### 7.5 Delete

```
DELETE /api/content/runbooks/deploy → 204 No Content
```

Removes the file from disk. Index row is deleted on the file-watcher event.

### 7.6 Move / rename

```
POST /api/content/_move
{ "from": "runbooks/deploy.md", "to": "runbooks/deploy-guide.md" }
→ 200 OK
```

Atomic where possible. Any URL redirects handled at the router level (optional 301 from the old path for one release cycle — controlled by `?keep_redirect=true`).

### 7.7 Search (lexical)

```
GET /api/content/search?q=deploy+fly&type=doc&tag=ops&top_k=10

→ 200 OK
{
  "query": "deploy fly",
  "results": [
    {
      "path": "runbooks/deploy.md",
      "title": "Deploy Guide",
      "heading": "## Fly.io",
      "excerpt": "...running `fly deploy` pushes the image to...",
      "score": 0.87,
      "tags": ["ops"],
      "url": "/runbooks/deploy#fly-io"
    }
  ],
  "total": 3
}
```

Powered by SQLite FTS5 with the `porter` tokenizer. Ranked by BM25. Query language: FTS5 match syntax (`"exact phrase"`, `foo OR bar`, `-exclude`, `NEAR(...)`).

---

## 8. Search & Retrieval (RAG)

### 8.1 Chunking

Files are chunked on ingest. Chunking rules:

1. Parse frontmatter → store separately, not chunked.
2. For `.mdx`: strip JSX tags and their attribute blobs. Preserve text children, which often have prose.
3. Split the remaining markdown by H2 (`##`) headings. H1 is the document header (used as chunk 0 context).
4. If a chunk exceeds ~500 tokens (rough char count ÷ 4), split further at H3, then paragraph boundaries.
5. Include a 1-sentence overlap between adjacent chunks to preserve context.

Each chunk row in SQLite FTS5:

```
doc_id       TEXT    -- path, e.g. "runbooks/deploy.md"
chunk_id     TEXT    -- "0", "1", ... unique within doc
title        TEXT    -- doc title
heading      TEXT    -- "H2 > H3" breadcrumb
content      TEXT    -- chunked prose (indexed)
tags         TEXT    -- space-joined for FTS match
frontmatter  JSON    -- full frontmatter (not indexed)
```

Index rebuilds on file-watcher events. Full rescan on server startup. See §11.

### 8.2 Retrieval endpoint (agent-focused)

```
POST /api/content/retrieve
{
  "query": "how do I roll back a fly deploy",
  "top_k": 5,
  "filters": { "tags": ["ops"], "type": "doc" },
  "include_body": false      // true to get full doc content, not just chunks
}

→ 200 OK
{
  "chunks": [
    {
      "doc_path": "runbooks/rollback.md",
      "chunk_id": "1",
      "title": "Rollback a Bad Release",
      "heading": "## Fly.io",
      "content": "Run `fly releases list`, find the last known-good version, then `fly deploy --image ...`",
      "score": 0.91,
      "url": "/runbooks/rollback#fly-io",
      "tags": ["ops", "incident"]
    }
  ]
}
```

Difference from `/search`: `/retrieve` is optimized for LLM consumption — larger `content` windows, fewer but denser results, includes enough heading context for grounding. `/search` is optimized for a human skimming a list.

### 8.3 Citation format

Every chunk returns `doc_path` + `heading` + `url`. Agents should cite via `[<title>](<url>)` when quoting, so human readers clicking on an LLM's answer can jump straight to the source.

### 8.4 v2: Semantic search

The same `/search` and `/retrieve` endpoints will gain a `semantic: true` flag (or become semantic-by-default if we wire it in cleanly). Agents won't need to change — just pass a flag.

Candidate stack for v2:
- SQLite + `sqlite-vec` extension for vector storage
- `bge-small-en-v1.5` via onnxruntime-go, embedded in the binary (~100MB)
- Or: external embeddings API if the user configures a key

---

## 9. MCP Tools

Five new tools ship alongside the existing thirteen:

| Tool                           | Purpose                                                                |
|--------------------------------|------------------------------------------------------------------------|
| `agentboard_knowledge_search`  | Lexical (v1) / semantic (v2) search. Returns ranked chunks.           |
| `agentboard_knowledge_retrieve`| RAG-optimized: dense results with heading context and citations.      |
| `agentboard_knowledge_get`     | Fetch a full doc by path.                                             |
| `agentboard_knowledge_list`    | List files/folders under a prefix, with filters.                      |
| `agentboard_knowledge_write`   | Create or replace a doc. Accepts frontmatter + markdown separately.   |

`agentboard_knowledge_delete` and `agentboard_knowledge_move` are deliberately left out of v1 MCP — file mutation is safer to gate behind explicit REST calls for now. Revisit in v2.

Tool schemas follow the shape of existing tools in `spec.md §11`.

---

## 10. Rendering & Dashboard UX

### 10.1 Sidebar tree

A new left-rail nav that mirrors the filesystem. Folders expand/collapse. Files show their `title` (from frontmatter, falling back to filename). Sorted by `order` frontmatter field, then alphabetically.

Optional filter chips at the top of the sidebar:
- `All` | `Docs` | `Dashboards`
- Tag filter (multi-select)

No sidebar is shown if the project has a flat tree with one file (today's behavior).

### 10.2 Reading-mode layout (`type: doc`)

- Narrow column (~72ch), serif body font, generous line-height.
- Auto-generated Table of Contents from H2/H3 headings, sticky on the right.
- Prev/Next links at the bottom, walking the sidebar order within the same folder.
- "Edit on disk" hint showing the file path (local mode only).
- Last updated timestamp.

### 10.3 Dashboard layout (`type: dashboard`)

Unchanged from today. Full-width. Component grid.

### 10.4 Search bar

A global `⌘K` / `Ctrl+K` palette:
- Fuzzy match against titles first (instant feedback)
- Full-text FTS5 search as you type past a few chars
- Grouped results: Docs / Dashboards / (future: data keys)
- Arrow keys to navigate, Enter to open

---

## 11. File Watcher & Write Semantics

### 11.1 Source of truth

The filesystem is authoritative. The server holds:
- **Compiled MDX cache** (in-memory, rebuilt on watcher event). Today's behavior, unchanged.
- **FTS5 index** (SQLite table, rebuilt on watcher event). New.

Both derive from files on disk. There is no third "content cache" — GET reads call through to the parsed file.

### 11.2 Write flow

1. `PUT /api/content/foo` validates path (see §12) and payload.
2. Writes the file atomically: write to `foo.md.tmp`, then rename.
3. Returns 200 before the watcher finishes reindexing (writes are not transactional with search).
4. The file watcher sees the rename, reparses, updates both caches. Typical latency: <100ms local.
5. SSE broadcaster fires `content.foo` to any subscribed clients.

### 11.3 Conflict handling

If a human edits `foo.md` on disk at the same time an agent `PUT`s to `/api/content/foo`, last-write-wins at the OS level. This is acceptable for a local-first tool. If it becomes a real problem, add a `If-Match: <etag>` header check in v2.

### 11.4 History

Every write is appended to a `content_history` table (same shape as `data_history`):

```sql
CREATE TABLE content_history (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  doc_path     TEXT NOT NULL,
  content      TEXT NOT NULL,         -- full post-write content (not diff — disk is cheap)
  frontmatter  JSON,
  updated_at   TIMESTAMP NOT NULL,
  updated_by   TEXT,                  -- X-Agent-Source
  session_id   TEXT                   -- X-Agent-Session (see spec-sessions.md)
);
CREATE INDEX idx_content_history_path ON content_history(doc_path);
```

Retention: configurable, default unlimited (local). `content_history` is gated by `--content-history` (on/off, default on).

A `GET /api/content/:path/history` endpoint returns recent revisions; `GET /api/content/:path/history/:revision` returns a specific version.

---

## 12. Security

Path traversal is the only real concern in v1 local mode.

- Validate all paths against a root allowlist: must resolve under `<project-dir>/content/`.
- Reject absolute paths, `..` segments, and paths containing null bytes.
- Reject symlinks that escape the content root (check with `filepath.EvalSymlinks` + prefix match).
- File size cap: 1 MB per file (configurable via `--max-content-size`).
- Enforce extensions: only `.md` and `.mdx` writable via REST. Other extensions are readable (for future formats) but not writable.

Hosted-mode auth is handled by the existing auth layer (`spec.md §12`).

---

## 13. Implementation Phases

### Phase 1 — Rename + flat read/write (1 day)
- [ ] Migrate `pages/` → `content/` with one-release fallback
- [ ] `GET /api/content/:path` and `PUT /api/content/:path` endpoints
- [ ] Frontmatter parser; default `type` based on extension
- [ ] `.md` support in the loader (alongside existing `.mdx`)
- [ ] Path traversal safety checks

### Phase 2 — Tree + filter (1 day)
- [ ] `GET /api/content/tree` with `prefix`, `type`, `tag` filters
- [ ] `PATCH`, `DELETE`, `POST /_move`
- [ ] `content_history` table + history endpoints
- [ ] Hot reload: file watcher updates tree endpoint on change, SSE event

### Phase 3 — Search (1 day)
- [ ] FTS5 table + chunker
- [ ] Full rescan on startup + incremental updates on watcher events
- [ ] `GET /api/content/search`
- [ ] `searchable: false` opt-out respected

### Phase 4 — Retrieval (0.5 day)
- [ ] `POST /api/content/retrieve` with LLM-optimized chunks
- [ ] Citation URL generation (path + heading anchor)
- [ ] Filters: tags, type, prefix

### Phase 5 — MCP tools (0.5 day)
- [ ] Five tools: search, retrieve, get, list, write
- [ ] Schemas, handler wiring, tests

### Phase 6 — Reading-mode UI (1-2 days)
- [ ] Sidebar tree component
- [ ] Reading-mode layout (narrow column, TOC, prev/next)
- [ ] `⌘K` search palette
- [ ] Filter chips

### Phase 7 — Docs & examples (0.5 day)
- [ ] README section
- [ ] Bruno collection folder: `content/` with read, write, search, retrieve
- [ ] Example MDX project with `/runbooks`, `/reference`, and a dashboard

Total: roughly 5-6 working days to full v1.

---

## 14. Decisions (resolved open questions)

All resolved in favor of the simpler path. Revisit if real usage contradicts.

1. **Default `type` for `.mdx`: `dashboard`.** Matches today's behavior — existing MDX pages keep working with zero frontmatter changes. `.md` defaults to `doc`. Explicit `type:` always wins.
2. **Chunker token counting: char heuristic (chars ÷ 4).** No embedded vocab. Swap for a real tokenizer only if retrieval quality regresses.
3. **FTS5 tokenizer: `porter`.** Better recall on English prose. Switch to `unicode61` if users complain about brand names or non-English content getting mangled.
4. **Retrieval response shape: chunks-only with inlined doc metadata.** No two-phase fetching. Simpler for LLM callers; bandwidth is not a concern locally.
5. **`agentboard_knowledge_write` does not auto-commit to git.** The human controls git. Revisit in v2 if users keep forgetting.

---

## 15. Future Work (v2+)

- **Semantic search** (sqlite-vec + onnx embeddings, or external API behind same endpoint).
- **In-browser editor** for humans (Monaco with markdown + frontmatter schema).
- **Wikilinks** `[[foo]]` with backlink index.
- **Tag pages** — `/tags/ops` auto-renders all docs with that tag.
- **External ingestion** — Notion / Confluence / Google Drive sync jobs that hit the REST API.
- **Content_history diff viewer** in the UI.
- **Knowledge "decks"** — curated collections of docs with an ordered reading flow.
- **Per-doc permissions** (hosted mode only).
- **Graph view** of wikilinks and tag clusters.

---

*See `spec.md` for architectural context (pages, MDX, SSE, file watcher, existing REST/MCP surface). See `spec-sessions.md` for the companion session-attribution feature; both features reference the same `X-Agent-Session` header.*
