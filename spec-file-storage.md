# AgentBoard — Files-First Data Store Spec

> **Status**: Phases 0-4 shipped (additive, parallel to legacy), plus §12 presigned uploads, §18 rate limiter, §17 activity-log rotation, and §20 backup CLI. Phase 5 (remove SQLite KV) is the only outstanding deferred work. Synthesizes the design conversation in branch `claude/file-based-storage-concept-EUSPh`. **Scoped to the data KV store only** — auth, teams, locks, invitations stay in SQLite for this phase and get their own spec later.

> **Migration**: explicitly out of scope. The existing dogfood instance gets reset; we ship as a clean break, not a compatibility layer.

---

## 1. Motivation

Today AgentBoard has two storage idioms living side-by-side:

- **SQLite** for the JSON KV data store (`internal/data/`)
- **On-disk files** for pages (`pages/`), components (`components/`), skills, and binary attachments (`files/`)

This duality creates measurable tension:

1. **The dual-write footgun.** `CLAUDE.md` already warns agents "do not write to content files on disk" — that warning exists *because* the file watcher will silently accept disk writes that bypass auth, attribution, rate limits, and history. We have the problem; SQLite just papers over it for one of the two paths.
2. **Two reload models, two failure modes.** Pages hot-reload via fsnotify; data hot-reloads via SSE from the store. Adding a feature means picking which idiom; cross-cutting features (search, history, audit) implement twice.
3. **Backup story is split.** `sqlite3 .backup` for data; `tar` for everything else. `HOSTING.md` documents the dance; we'd rather not have one.
4. **Inspectability gap.** Half the project state is `cat`-able; half requires `sqlite3` shell. For our own dogfooding and support burden this matters.

Files-first collapses all of this onto one storage idiom. The win is not raw performance — both layers are well below their performance ceilings at our scale — it is **architectural unity**: one write path, one watcher pattern (in dev only), one backup story, one inspection model.

## 2. ICP framing — what this is NOT for

The Ideal Customer Profile for AgentBoard is **a non-technical user who plugs an endpoint into Claude / GPT / Codex and expects it to work**. They never:

- Open the project folder in an editor
- Run `git clone` or `git push`
- Mount the volume via SSH
- Install `aws-cli`, `rclone`, or any other ops tool

They access AgentBoard exclusively through:

- The dashboard (humans browsing)
- The REST API (agents writing/reading)
- The MCP surface (agents writing/reading)

This shapes the design in two non-obvious ways:

1. **Editor compatibility ("open in Obsidian / VS Code") is irrelevant.** The user never sees the folder. We don't optimize for human-readable folder structure beyond what helps *us* support them.
2. **Operational portability is a non-goal.** The user never moves their project folder between hosts. They may *back it up* (we ship a CLI for that) but they don't sync, mount, or share it. Migration is a one-shot, not a workflow.

## 3. Non-goals (Phase 1)

- **No SQLite migration.** We do not import existing SQLite data. Fresh start. The dogfood instance gets wiped and reseeded.
- **No auth/teams/locks/invitations rework.** Those tables stay in SQLite; their own spec(s) move them later.
- **No git anywhere in the architecture.** Backup is server-driven via CLI, not `git push`.
- **No editor / Obsidian features.** No assumption that anyone reads the folder by hand in production.
- **No multi-key transactions, ever.** Every operation is single-key. This is a load-bearing constraint — see §17.
- **No external storage backends (S3, etc.) as primary store.** Local disk is the source of truth. S3 may appear later as a backup destination.
- **No full S3-compatible API** (no SigV4). If users want `aws s3 sync`, we can add it later.

## 4. Mental model

> **Files are the durable source of truth. Everything else (catalog, search, locks, tail buffers) is derived state held in the server process and rebuilt on restart.**

If we hold this line, the rest of the architecture works. The places it gets messy are where we're tempted to make an index authoritative — don't. A bug in an index is at worst a perf or visibility bug; the file is always the truth.

Two corollaries:

- **Server is the sole writer to disk in production.** No fsnotify on the data correctness path. fsnotify exists in `--dev` mode for hot-reloading author-edited pages/components, never for data.
- **Crashes never produce half-written state.** Atomic rename (`tmp + fsync + rename(2)`) means every file is either old-version or new-version. No "in between."

## 5. On-disk layout

```
my-project/
├── agentboard.yaml                          # config (existing)
├── .agentboard/
│   ├── tokens.json                          # auth — stays in SQLite for Phase 1
│   ├── activity.ndjson                      # global audit log (append-only)
│   ├── history/                             # per-key snapshots
│   │   ├── dev.kanban.sprint-7.ndjson
│   │   └── dev.metrics.requests.ndjson
│   └── search.bleve/                        # persisted full-text index
├── data/                                    # NEW — replaces SQLite KV store
│   ├── dev.metrics.requests.json            # singleton
│   ├── welcome.users.json                   # singleton
│   ├── dev.kanban.sprint-7/                 # collection (per-ID files)
│   │   ├── task-1.json
│   │   ├── task-42.json
│   │   └── task-99.json
│   ├── dev.events.log.ndjson                # stream
│   └── dev.events.log.1.ndjson              # rotated stream segment
├── pages/                                   # MDX (existing)
├── components/                              # JSX (existing)
└── files/                                   # binary blobs (existing)
```

**Naming convention for data files:**

- The dotted key is the filename literally — `dev.metrics.requests` → `dev.metrics.requests.json`.
- **Flat layout, not nested directories**, to avoid the "is `dev.metrics` a file or a directory?" ambiguity. The dotted-key namespace is in the filename, not the path.
- The only directory inside `data/` is for collection keys (per-ID file containers).

**Reserved characters in keys:** `/`, `\`, null, leading `.`, `..`. Existing key validation in `internal/data/keys.go` (or equivalent) carries forward; only the storage backend changes.

## 6. The envelope (`_meta`)

Every JSON file in `data/` is wrapped in a server-managed envelope:

```json
{
  "_meta": {
    "version":     "2026-04-27T14:32:11.123456789Z",
    "created_at":  "2026-04-20T10:00:00Z",
    "modified_by": "agent-alice",
    "shape":       "singleton"
  },
  "value": <user data>
}
```

The user's data lives under `value`. The envelope is uniform across all data types — number, string, object, array, null. A primitive value of `42` is stored as `{"_meta": {...}, "value": 42}`, **always**.

### Field ownership

| Field | Set by | When | Mutable by agent? |
|---|---|---|---|
| `version` | Server | Every write | **Echoed** by agent for CAS; server overwrites with new value on success |
| `created_at` | Server | First write only | No — immutable |
| `modified_by` | Server | Every write | No — derived from auth token |
| `shape` | Server | First write only | No — immutable; determines storage layout |

**Hard rule: agents never set `_meta` fields except for `version`.** On write, the server strips any agent-provided `_meta` fields except `version`, which it interprets as the CAS precondition. This prevents agents from forging timestamps or rewriting attribution.

### MDX page envelope (informational — out of scope for Phase 1)

Pages already have YAML frontmatter; the same `_meta` block can live there in a future phase:

```mdx
---
title: Engineering Principles
_meta:
  version: 2026-04-27T14:32:11.123456789Z
  modified_by: agent-alice
---
# Content
```

Phase 1 leaves pages alone. Mention here for design coherence only.

### Binary file envelope

Binary files can't embed metadata. They get a sidecar `<name>.meta.json`:

```
files/
├── q4-report.pdf
├── q4-report.pdf.meta.json   ← {_meta: {version, ...}}
```

Out of scope for Phase 1; the `files/` story is already covered by `spec-files.md` and remains as-is. Mentioned for design symmetry.

## 7. The three shapes

A key has exactly one shape, **set on its first write and immutable thereafter**. Trying to use the wrong op against an existing key returns `409 WRONG_SHAPE` with a friendly message naming the expected ops.

| Shape | First-write op | File layout | Concurrency primitive |
|---|---|---|---|
| **Singleton** | `SET` or `MERGE` | `data/<key>.json` (one envelope file) | Per-path mutex + atomic rename + version CAS |
| **Collection** | `UPSERT_BY_ID` or `MERGE_BY_ID` | `data/<key>/<id>.json` (directory of envelope files) | Per-`<key>/<id>` mutex; siblings never contend |
| **Stream** | `APPEND` | `data/<key>.ndjson` (newline-delimited JSON) | `O_APPEND`, lock-free for writes ≤ PIPE_BUF |

Collections are conflict-free by construction for distinct IDs. Streams are conflict-free by construction (POSIX `O_APPEND` atomicity). The only shape with a real conflict surface is singletons — and even there, `MERGE` doesn't surface conflicts to the agent (server retries internally).

### Why immutable shape?

The first instinct is "let any key be any value at any time" (which is what SQLite gives us). Files force a choice: a file cannot also be a directory. Pinning the shape at first-write:

- Eliminates the question "is this an object or a directory of items?"
- Lets the storage layout be determined deterministically from the key + first op
- Lets the agent's mental model stay simple: pick the right op, server tells you if you picked wrong
- Forces deliberate design — "should this be a collection or a singleton?" is a question worth answering once

The agent learns the shape via `agentboard_index()` (Tier 1 read; see §10) which lists every key with its shape.

## 8. Operations — the 10 atomic ops

The existing 7 ops carry forward, plus 2 new ones (`INCREMENT`, `CAS`) and 1 helper (`READ`/`GET`):

| Op | Shape | Conflict surface |
|---|---|---|
| `READ` | any | n/a |
| `SET` | singleton | full replace; surfaces 412 if version mismatch |
| `MERGE` | singleton | server retries under lock; never returns 412 |
| `UPSERT_BY_ID` | collection | per-ID; only same-ID race surfaces 412 |
| `MERGE_BY_ID` | collection | server retries under lock per-ID; never returns 412 |
| `DELETE` | singleton or collection (whole) | idempotent; "already gone" returns 204 |
| `DELETE_BY_ID` | collection | per-ID; idempotent |
| `APPEND` | stream | `O_APPEND` atomic; never returns 412 |
| `INCREMENT` | singleton (number) | server-side atomic; never returns 412 |
| `CAS` | singleton | atomic test-and-set; returns 409 with current value on mismatch |

### `INCREMENT` (new)

Atomic counter operation for the metric-bumping pattern that's currently a frequent source of contended writes:

```
POST /api/data/dev.metrics.requests:increment
{"by": 1}
→ 200 OK
{"_meta": {"version": "...", ...}, "value": 43}
```

Server-side: lock path → read → add → write. Conflict-free. Default `by` is 1. Body may also be omitted. Negative values allowed.

### `CAS` (new)

Test-and-set for state-machine transitions:

```
POST /api/data/dev.kanban.sprint-7/task-42:cas
{"expected": {"column": "todo"}, "new": {"column": "in-progress"}}
→ 200 OK on match
→ 409 CAS_MISMATCH on mismatch (with current value embedded)
```

Useful for "move card from todo → in-progress only if it's currently in todo" without surfacing version-CAS to the agent. The agent reasons about content, not opaque versions.

### Batched variants

To keep agents off the rate limiter (§13), every write op accepts a batch form:

```
POST /api/data/dev.events.log:append
{"items": [{...}, {...}, {...}]}        ← batch append

POST /api/data/dev.metrics.requests:increment
{"by": 50}                              ← single call instead of 50 increments

PATCH /api/data/dev.kanban.sprint-7
{"items": {"task-1": {...}, "task-2": {...}}}   ← bulk merge_by_id
```

Batch ops convert "agent in a tight loop" patterns from N calls into 1.

## 9. REST API per shape

URL pattern: `/api/data/<key>` for the key as a whole, `/api/data/<key>/<id>` for items in a collection. Action verbs use `:<action>` suffix (Google Cloud style).

### Singleton

| Method | Path | Op | Notes |
|---|---|---|---|
| `GET` | `/api/data/<key>` | `READ` | Returns envelope. Honors `If-None-Match` for 304. |
| `PUT` | `/api/data/<key>` | `SET` | Body: `{value: ..., _meta?: {version: "..."}}`. Honors `If-Match` header *or* body `_meta.version`. |
| `PATCH` | `/api/data/<key>` | `MERGE` | JSON Merge Patch on `value`. Server retries; never 412. |
| `DELETE` | `/api/data/<key>` | `DELETE` | Idempotent. |
| `POST` | `/api/data/<key>:increment` | `INCREMENT` | Body: `{by: <number>}`. |
| `POST` | `/api/data/<key>:cas` | `CAS` | Body: `{expected, new}`. |

### Collection

| Method | Path | Op | Notes |
|---|---|---|---|
| `GET` | `/api/data/<key>` | `LIST` | Returns `{_meta: {shape: "collection", count}, items: [{_meta, value}, ...]}`. Pagination via `?limit=&after=<id>`. |
| `GET` | `/api/data/<key>/<id>` | `READ` | Returns single item envelope. |
| `PUT` | `/api/data/<key>/<id>` | `UPSERT_BY_ID` | Body: `{value, _meta?: {version}}`. Honors `If-Match`. |
| `PATCH` | `/api/data/<key>/<id>` | `MERGE_BY_ID` | JSON Merge Patch on item. |
| `DELETE` | `/api/data/<key>/<id>` | `DELETE_BY_ID` | Idempotent. |
| `DELETE` | `/api/data/<key>` | `DELETE` | Removes entire collection (the directory). Requires confirmation: `?confirm=true`. |
| `POST` | `/api/data/<key>/<id>:cas` | `CAS` | Same as singleton `CAS`. |

### Stream

| Method | Path | Op | Notes |
|---|---|---|---|
| `GET` | `/api/data/<key>` | `READ` | Returns `{_meta: {shape: "stream", line_count}, lines: [{_meta: {ts}, value}, ...]}`. Tail is in-memory; older segments paged on demand. Query: `?limit=100&since=<ts>&until=<ts>`. |
| `POST` | `/api/data/<key>:append` | `APPEND` | Body: `{value: ...}` for one line, `{items: [...]}` for batch. Server adds per-line `_meta.ts`. |
| `DELETE` | `/api/data/<key>` | `DELETE` | Removes stream + all rotated segments. |

### Conflict response shape (412 / 409)

When a CAS check fails:

```http
HTTP/1.1 412 Precondition Failed
Content-Type: application/json
ETag: "2026-04-27T14:32:11.500000000Z"

{
  "error":         "version_mismatch",
  "message":       "This key was modified by agent-bob 3 seconds ago. Read the current state, reconcile, and retry.",
  "current": {
    "_meta":  {"version": "2026-04-27T14:32:11.500000000Z", "modified_by": "agent-bob", ...},
    "value":  { ... full current value ... }
  },
  "your_version":   "2026-04-27T14:32:11.123456789Z",
  "current_version":"2026-04-27T14:32:11.500000000Z"
}
```

**Critical:** the conflict response **embeds the current value**. The agent does not need a follow-up `GET`. One conflict → two HTTP calls total (the failed PUT + the retry PUT), not three.

For `CAS` mismatches, same shape but `error: "cas_mismatch"`.

## 10. Three-tier read API

The agent's primary "find things" surface, designed to replace what `git clone + ripgrep` gives a developer.

### Tier 1: Discovery — `GET /api/index`

One call returns a flat catalog of everything in the project:

```json
{
  "data": [
    {"key": "dev.metrics.requests", "shape": "singleton",  "version": "...", "size": 142, "type": "number"},
    {"key": "dev.kanban.sprint-7",  "shape": "collection", "version": "...", "count": 12},
    {"key": "dev.events.log",       "shape": "stream",     "version": "...", "line_count": 4827}
  ],
  "pages": [
    {"path": "principles.md", "version": "...", "title": "Engineering Principles", "summary": "First 200 chars..."}
  ],
  "components": [...],
  "files":      [...]
}
```

Lightweight. Bounded payload (one row per key). The "I just woke up, orient me" call. **This single endpoint replaces 80% of what `git clone` gave the agent.**

### Tier 2: Search — `GET /api/search?q=<query>&scope=...`

Server-side full-text search via Bleve:

```json
{
  "results": [
    {"path": "principles.md",   "score": 0.92, "snippet": "...we treat <em>concurrency</em> as a UX problem...", "line": 14},
    {"path": "architecture.md", "score": 0.81, "snippet": "...optimistic <em>concurrency</em> via ETag...",      "line": 47}
  ]
}
```

Scopes: `pages`, `components`, `data`, `all`. Replaces `rg` for the agent.

### Tier 3: Targeted read — `GET /api/data/<key>` etc.

Already covered in §9. Returns the envelope + `ETag` header. Honors `If-None-Match` for 304.

### Bulk export (rare, migration only)

`GET /api/export.tar` for whole-project tarball. **Not on the operational hot path.** If an agent reaches for this in normal operation, the discovery+search APIs are too weak. Reserved for `agentboard backup`.

## 11. MCP tool surface — consolidated

The current 13 resource-CRUD tools collapse into ~10 tier-shaped tools. The CRUD-per-resource shape grows linearly with new resource types (every new content type doubles the tool count); tier-shaped tools scale better.

| Tool | Maps to | Notes |
|---|---|---|
| `agentboard_index` | `GET /api/index` | First call after wakeup |
| `agentboard_search` | `GET /api/search` | Full-text |
| `agentboard_read` | `GET /api/data/<key>[/<id>]` | Singleton, item, or stream |
| `agentboard_write` | `PUT /api/data/<key>[/<id>]` | SET / UPSERT_BY_ID |
| `agentboard_merge` | `PATCH /api/data/<key>[/<id>]` | MERGE / MERGE_BY_ID |
| `agentboard_append` | `POST :append` | Stream |
| `agentboard_delete` | `DELETE /api/data/<key>[/<id>]` | DELETE / DELETE_BY_ID |
| `agentboard_increment` | `POST :increment` | Atomic counter |
| `agentboard_cas` | `POST :cas` | Test-and-set |
| `agentboard_request_file_upload` | (see §12) | Mints presigned URL for binary upload |

Old tools (`agentboard_set`, `agentboard_set_value`, etc.) removed in Phase 1. **No deprecation period — fresh start, breaking change.**

## 12. Binary file uploads — kill base64

Current `spec-files.md` uses base64 in the MCP `agentboard_write_file` tool. This is wasteful (~33% bandwidth + context overhead) and the community has converged on the better pattern:

**Two-step upload via presigned URL:**

```
Step 1 — agent calls MCP tool:
  agentboard_request_file_upload({"name": "q4-report.pdf", "size_bytes": 248192})
  → {"upload_url": "https://abc.fly.dev/api/files/q4-report.pdf?upload_token=ut_abc123",
     "expires_at": "2026-04-27T15:00:00Z",
     "max_size_bytes": 52428800}

Step 2 — agent shells out (Bash, code interpreter, fetch):
  curl -X PUT --data-binary @q4-report.pdf "$UPLOAD_URL"
  → {"ok": true, "name": "q4-report.pdf", "size": 248192, ...}
```

The `upload_token` is a one-shot, time-bounded credential the server mints + holds in an in-process map with TTL eviction. Scoped to one filename, one-shot, expires after 5 minutes. Avoids implementing SigV4; gives us the *idea* of presigned URLs without AWS-flavored complexity.

For agents that genuinely cannot shell out (rare), keep `agentboard_write_file` with base64 as a fallback, capped at 1 MB. Same code path on the server after decode.

## 13. Conflict resolution flow

The full agent UX for an optimistic-CAS write:

```
Round 1 — first read:
  GET /api/data/dev.kanban.sprint-7/task-42
  → 200 OK, ETag: "...A..."
     {"_meta": {"version": "...A...", ...}, "value": {"column": "todo"}}

Round 2 — agent mutates and writes back:
  PUT /api/data/dev.kanban.sprint-7/task-42
  body: {"_meta": {"version": "...A..."}, "value": {"column": "in-progress"}}
  → 200 OK, ETag: "...B..."
     {"_meta": {"version": "...B...", ...}, "value": {"column": "in-progress"}}

Round 2-conflict path — another agent wrote meanwhile:
  PUT /api/data/dev.kanban.sprint-7/task-42
  body: {"_meta": {"version": "...A..."}, "value": {"column": "in-progress"}}
  → 412 Precondition Failed, ETag: "...C..."
     {"error": "version_mismatch",
      "current": {"_meta": {"version": "...C...", "modified_by": "agent-bob", ...},
                  "value": {"column": "review"}},
      ...}

Round 3 — agent reconciles and retries with new version:
  PUT /api/data/dev.kanban.sprint-7/task-42
  body: {"_meta": {"version": "...C..."}, "value": {"column": "in-progress"}}
  → 200 OK
```

### CAS without echoing version (bypass)

Three explicit states for write intent:

- `_meta.version` present in body → CAS write (412 on mismatch)
- `_meta.version` absent + key doesn't exist → first write, succeeds
- `_meta.version` absent + key exists → `409 VERSION_REQUIRED` with friendly message
- `_meta.version: "*"` → explicit force-overwrite, server skips CAS

No accidental lost updates; no ceremony when the agent genuinely doesn't care.

### MERGE never surfaces conflicts

Server-side: lock path → read → apply patch → write new envelope → release. Two agents PATCHing different fields both succeed (last-write-wins per field, both fields present). Two agents PATCHing the same field: last-write-wins, no error. **`MERGE` should never return 412.**

### Why server-monotonic timestamp (not content hash) for the version

The version token is round-tripped opaquely by the agent. It needs:

- To change on every write
- To never collide
- To round-trip cleanly

Three candidates were considered:

| Token | Pros | Cons |
|---|---|---|
| Content hash (SHA-256) | Idempotent (same content = same version) | Opaque to humans; debugging requires extra step |
| File mtime | Free; on disk already | Filesystem-dependent granularity (1ms–1s); can collide; can lie under NTP correction |
| **Server-monotonic timestamp** | Human-readable, sortable, debuggable; never collides (always increments by ≥1ns) | None significant for our scale |

**Decided: server-monotonic timestamp.** Per-key counter computed as `max(now_nanos, last_version + 1ns)`, persisted as the `version` field inside the file's envelope. The catalog index in memory holds a copy for fast `If-None-Match` validation without opening the file.

The HTTP `ETag` header value is the same timestamp string, in quotes. Browsers and CDNs treat it as opaque (correctly). Humans reading logs see a real time.

## 14. Indexes — derived state, ephemeral

Three in-memory structures, each rebuilt on startup. **None is authoritative.**

### Catalog index — `key → {version, mtime, size, shape, type}`

Built by walking `data/` at startup; updated synchronously on every write. ~100 bytes/key in memory; 1k keys = 100 KB.

**Never persisted.** Rebuild cost is sub-second at our scale. Persistence would only add invalidation risk.

### Search index — Bleve, persisted to `.agentboard/search.bleve/`

[Bleve](https://github.com/blevesearch/bleve) is pure Go (no CGO), used by Couchbase. Honors CORE_GUIDELINES §1 (single binary, no runtime deps).

Updated synchronously on writes (~1ms per typical document). Persisted to disk so we don't re-tokenize 100MB of MDX on every restart.

**Drift recovery on crash:** on startup, compare bleve doc IDs against catalog. Reindex orphans. Acceptable cost on rare crashes.

Indexed content: page bodies, component metadata, data values (selectively — only string values and string fields in objects; numbers don't need full-text).

### Stream tail buffers — last N lines of each stream in memory

Cap at 1000 lines per stream (~1MB). Covers the typical "Log component shows last 50 events" query without seeking through files.

For history queries beyond the buffer, walk numbered segment files (§16) in reverse order until enough lines are collected.

## 15. Live updates (SSE)

Server is sole writer → server already knows when keys change → broadcasts to connected SSE clients via in-process `chan event`.

### Event shape

```
event: data-changed
data: {"key": "dev.metrics.requests", "version": "2026-04-27T14:32:11.500Z", "shape": "singleton", "value": 43}
```

For small values (< 4KB), embed `value`. For larger values, omit `value`; client refetches:

```
event: data-changed
data: {"key": "dev.kanban.sprint-7", "version": "...", "shape": "collection", "size": 248192}
```

### Subscriptions

`GET /api/events` — subscribe to all events. Client-side filtering. Simpler than per-prefix server-side routing; revisit if bandwidth becomes an issue.

### Reconnection

Standard SSE `Last-Event-ID` header. Server keeps a small in-memory ring buffer (last 1000 events). On reconnect with `Last-Event-ID: <id>`, server replays events since. Beyond the buffer, client refetches via `GET /api/index`.

### Why no fsnotify on data

Production: server is sole writer, knows of every change without filesystem signals. fsnotify would be redundant.

Dev (`--dev` flag): pages and components may be hot-edited by the developer; fsnotify on `pages/` and `components/` triggers reload (current behavior, unchanged). **fsnotify never runs on `data/` even in dev** — data writes go through the API.

## 16. Activity log + per-key history

Two append-only NDJSON files, distinct purposes.

### Activity log — `.agentboard/activity.ndjson`

Global ledger: "who did what when." One line per write (across all keys):

```json
{"ts":"2026-04-27T14:32:11.500Z","actor":"agent-alice","op":"SET","path":"dev.metrics.requests","version_before":"...","version_after":"..."}
```

Append-only. Rotated by size at 100MB (§17). Read via `GET /api/activity?since=<ts>&actor=<name>&path_prefix=<prefix>&limit=<n>`.

### Per-key history — `.agentboard/history/<key>.ndjson`

Per-key value snapshots:

```json
{"ts":"2026-04-27T14:32:11.500Z","actor":"agent-alice","version":"...","op":"SET","value":<previous_value>}
```

Retention: keep last 100 entries per key, rolling truncate. Older entries dropped silently.

Read via `GET /api/data/<key>/history?limit=<n>&since=<ts>`. Powers "show me how this metric changed" and "revert to version X" workflows.

### Write atomicity

On every successful primary write:

1. Atomic-rename the data file
2. Append to activity log
3. Append to per-key history

If steps 2 or 3 fail after step 1 succeeds, the API call still returns success. Audit is auxiliary; durability of the value matters more. Failures logged for operator alerting.

CORE_GUIDELINES §10 explicitly mandates `content_history` for "pages, files, data keys" — this design preserves it.

## 17. Stream rotation

Streams are NDJSON files that grow indefinitely. Rotation is logrotate-style, transparent to readers.

```
At t=0:    dev.events.log.ndjson    (active, agents append)

When active hits 100MB:
           dev.events.log.ndjson    (fresh, empty)
           dev.events.log.1.ndjson  (just rotated)

Next rotation:
           dev.events.log.ndjson    (fresh)
           dev.events.log.1.ndjson  (recently rotated)
           dev.events.log.2.ndjson  (older)
```

Cap at 5 segments (`dev.events.log.5.ndjson` is the oldest kept). Older segments deleted silently on next rotation.

**Implementation:** when an `APPEND` would push the active file past 100MB, server renames the chain (`5→delete, 4→5, 3→4, 2→3, 1→2, active→1`) and opens a fresh active file. Atomic, fast (rename only, no copies), happens once per ~100MB of writes.

**Reads** for "last N lines" use the in-memory tail buffer (covers active file). History reads (§16) walk numbered files in reverse order.

**Practical retention:** 100MB × 5 segments = 500MB max per stream. At ICP scale most streams will never rotate once.

## 18. Rate limiting

Friendly throttle, not a security mitigation. We assume non-malicious actors (CORE_GUIDELINES §2 reads "local-first, hosted-possible"; for hosted mode the friendly tier is sufficient — a real abuse-prevention layer is a separate concern).

### Defaults

- **Writes:** 200/min sustained, 50/sec burst, per token
- **Reads:** 1000/min, per token
- **Per-token override:** `rate_limit_override` field on the token record (admin-set; not in Phase 1 UI but designed in)

### Implementation

[`golang.org/x/time/rate`](https://pkg.go.dev/golang.org/x/time/rate) token bucket, one `*rate.Limiter` per token in a `sync.Map`. Evict idle entries after 1 hour.

### The 429 response

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 2
Content-Type: application/json

{
  "error": "rate_limited",
  "message": "You're writing too fast — try again in 2 seconds.",
  "retry_after_seconds": 2,
  "limit": "200 writes/min, 50/sec burst",
  "current_rate": "187 writes/sec"
}
```

Honors CORE_GUIDELINES §12 (responses are repair manuals). Standard `Retry-After` header for HTTP-aware clients; explicit message for the LLM agent reading its own logs.

### Structural mitigation

Batched ops (§8) convert tight-loop patterns from N calls into 1. The rate limit becomes a true safety net; agents rarely encounter it in normal flow. **The fix for "agent kept hitting 429" is "use the batch op," not "raise the limit."**

## 19. Crash + restart story

### What survives a crash

- All singleton files (atomic rename guarantees old-or-new state)
- All collection files (atomic rename per item)
- All NDJSON content that was fsynced (default for SET/MERGE; coalesced for INCREMENT/APPEND — see below)
- Activity log up to last fsync
- Search index (Bleve persists; orphans reindexed on startup)

### What's lost

- In-memory write buffers for INCREMENT and APPEND (last ~100ms or ~64KB per stream)
- All in-memory indexes (rebuilt on startup; no data loss, just startup cost)
- SSE ring buffer of recent events (clients refetch via `/api/index`)

### What can't get corrupted

- Files mid-write. Atomic rename guarantees the file is either old or new, never in between. The temp file lives on the same filesystem (must — rename across mount points is non-atomic).

### Hot-path write coalescing

The pure "write tmp → fsync → rename" pattern costs ~3-10ms on a Fly volume. For metrics being incremented at 100/sec, that's wasteful. **Mitigation: in-memory write coalescing for INCREMENT and APPEND only.**

- INCREMENT: hold new value in memory; flush to disk every 100ms or every Nth increment.
- APPEND: buffer lines; flush on timer or when buffer crosses ~64KB.

Crash window: lose <100ms of increments / <64KB of stream data. Acceptable for metrics + logs; explicitly **not** acceptable for SET/MERGE (those flush per-write).

This is the **one place** an in-memory value precedes the disk value. Document loudly. Agents reading the value see in-memory (authoritative for buffer duration). For strict durability, expose `?sync=true` flag on INCREMENT/APPEND.

### Startup sequence

```
Process starts
├── Read agentboard.yaml
├── Walk data/ → build catalog index in memory       (<100ms for 1k keys)
├── Walk pages/, components/                          (existing behavior, unchanged)
├── Open or build .agentboard/search.bleve/           (instant if persisted)
├── Open .agentboard/activity.ndjson for append
├── Open stream NDJSON files; populate tail buffers   (last 1000 lines each)
└── Start HTTP server
```

Cold start at ICP scale: <1 second. At 10× ICP scale: a few seconds, dominated by tail-buffer warmup.

## 20. Backup + restore

ICP doesn't install `aws-cli`, `rclone`, or any other ops tool. We ship backup as built-in CLI subcommands of the AgentBoard binary.

### Subcommands

```bash
# Backup to local tarball
agentboard backup --to ./snapshot.tar.gz

# Backup to S3 / R2 / B2 (uses AWS SDK on our side)
agentboard backup --to s3://bucket/path/

# Backup to peer instance (REST push)
agentboard backup --to https://other-agentboard.example.com --token <bearer>

# Restore from tarball
agentboard restore --from ./snapshot.tar.gz [--target-dir ./fresh-project/]
```

### Backup mechanics

- Brief stop-the-world on the active server (acquire global write lock for ~50ms)
- Snapshot in-memory state to disk (flush coalesced INCREMENT/APPEND buffers)
- Tar the project folder (`.agentboard/`, `data/`, `pages/`, `components/`, `files/`, `agentboard.yaml`)
- Resume writes
- Stream tarball to target

For S3-target: use AWS SDK for Go on the AgentBoard server side. The user's S3 credentials are provided via env vars (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`) or `agentboard.yaml`. The user never installs `aws-cli`.

### Restore mechanics

- Server stopped (precondition)
- Untar into target directory
- Validate envelope versions; rebuild indexes on next start

### Hosted mode (future)

Hosted AgentBoard runs `agentboard backup --to s3://...` on a cron managed by the server itself. User just toggles "Backups: enabled" in settings. Not in Phase 1.

## 21. Dev vs prod modes

Two operating modes, same binary.

### Production (default, `--prod` or no flag)

- Server is sole writer to all content directories
- fsnotify disabled on `data/`
- fsnotify disabled on `pages/`, `components/` (writes go through API)
- Direct disk edits are not detected (and shouldn't happen — the user has no shell access)

### Dev (`--dev` flag)

- Server is still sole writer to `data/` (data-write paths always go through API; this is non-negotiable)
- fsnotify enabled on `pages/` and `components/` for hot-reload
- Author can edit MDX/JSX in their editor; changes reload in browser
- Direct edits to `data/` are *not* picked up — agents go through the API even in dev

This makes the dev experience "emulate remote" for the data layer while preserving the live-edit ergonomics for content authoring.

## 22. Rate-of-change concerns / hold the line

Six rules that keep this design tractable. Cross any one and complexity explodes:

1. **No multi-key transactions, ever.** Every op is single-key. Day one of "atomically update A and B" we say no, or restructure B as a child of A.
2. **No fsnotify on the data correctness path.** Server is sole writer in production. Period.
3. **First write determines shape; shape is immutable.** No "this used to be a singleton, now it's a collection."
4. **Indexes are derived, never authoritative.** A bug in an index is at worst a perf or visibility bug; the file is the truth.
5. **NDJSON lines stay ≤ PIPE_BUF (4 KB on Linux, 512 B on macOS).** Document it. Truncate-with-marker if a write would exceed it.
6. **Atomic rename, always, for non-stream writes.** Never write directly to the destination file.

If any of these is bargained away in implementation, **stop and reconsider whether files-only is still the right call**. Bargaining is how you reinvent a database badly.

## 23. Honest tradeoffs

### What we gain

| Win | Magnitude |
|---|---|
| Architectural unity (one storage layer) | Real, compounds over time |
| Backup story (`tar` vs `sqlite3 .backup` + tar) | Genuine simplification |
| No migrations (schema changes = file format changes, often lazy) | Real |
| Operator inspection (`cat`, `find`, `grep` work) | Real for our debugging; invisible to ICP |
| Smaller binary (~2 MB drop from `modernc.org/sqlite`) | Marginal |
| No SQLite WAL / lock semantics to reason about | Real for ops complexity |

### What we lose / pay for

| Cost | Magnitude |
|---|---|
| Reimplement what SQLite gave free (per-path locking, version CAS, atomic rename, coalesced writes, search via Bleve, history) | Real — ~2-3 weeks careful work |
| Maturity gap (SQLite is 25 years of bug fixes; we're new code) | Real, mitigated by simplicity (no joins, no transactions, no schemas) |
| Many-small-files inode overhead | Negligible at ICP scale |
| Per-write fsync cost on networked storage (~5ms on Fly volumes) | Real on hot path; mitigated by coalescing for INCREMENT/APPEND |

### What we don't lose (commonly mistaken)

- **Transactions:** the existing 7 ops are all single-key; we never had multi-key transactions to lose.
- **Query expressiveness:** the search index covers the queries the dashboard needs.
- **Cross-platform consistency:** POSIX `rename(2)` is atomic on Linux/macOS; Go's `os.Rename` uses `MoveFileExW(MOVEFILE_REPLACE_EXISTING)` on Windows (NTFS atomic).

## 24. Phased implementation plan

Each phase is its own PR with passing integration tests. Phases land sequentially; no phase merges until prior is green on `main`.

### Phase 0 — this spec

Land `spec-file-storage.md`, get explicit sign-off, update `CORE_GUIDELINES.md` references if needed.

### Phase 1 — new storage package (parallel to existing, no API impact)

- New `internal/store/` package implementing files-first store
  - `store.go` — Store interface
  - `singleton.go`, `collection.go`, `stream.go` — per-shape file ops
  - `version.go` — server-monotonic version generation
  - `lock.go` — per-path mutex map
  - `envelope.go` — `_meta` marshal/unmarshal
  - `atomic.go` — `tmp + fsync + rename` helper
- New `internal/index/` package
  - `catalog.go` — in-memory key catalog
  - `search.go` — Bleve wrapper
  - `tail.go` — stream tail buffers
- Unit tests for all of the above
- **Not yet wired into the server.** Old SQLite path remains primary.

### Phase 2 — REST API rework

- New handlers: `/api/data/*` per §9
- `/api/index` (Tier 1)
- `/api/search` (Tier 2)
- `/api/data/<key>/history` per-key history
- `/api/activity` global activity log
- Conflict response shape per §13
- Rate limiting middleware per §18
- Bruno tests for new API
- Old SQLite-backed routes still mounted at `/api/v1/data/*` (parallel) until Phase 5

### Phase 3 — MCP tool consolidation

- New tier-shaped tools per §11
- Old tools removed (no deprecation period — fresh start)
- `agentboard_request_file_upload` (§12)
- Update `spec.md` §16 (SQLite Schema Reference) — note removal
- Update `CORE_GUIDELINES.md` §2.3 (MCP tool count)

### Phase 4 — frontend migration

- `useData` hook unwraps envelope (`data._meta` + `data.value`)
- All 9 built-in components updated to consume new shape
- SSE event-shape change handled
- Dashboard tests pass

### Phase 5 — old surface removal

- Remove `internal/data/` (SQLite store)
- Remove `/api/v1/data/*` routes
- Remove `modernc.org/sqlite` dependency *for the data store* (still used by auth/teams/locks/invitations)
- Wipe and reseed dogfood instance

### Out of scope (future spec)

- Phase 6+: migrate auth/teams/locks/invitations to files (kills SQLite entirely)
- Phase 7+: Obsidian-style external editing (pages frontmatter `_meta`, fsnotify reconciliation)
- S3-compatible API for backup pipelines (rclone, mountpoint-s3)
- Per-key/per-prefix SSE filtering

## 25. Resolved decisions

| # | Decision | Why |
|---|---|---|
| 1 | Files-only, **no SQLite** for the data store | Architectural unity; backup simplification; remove dual-write footgun |
| 2 | **No migration from existing SQLite.** Fresh start. | Dogfood is the only existing data; user explicitly confirmed migration not needed |
| 3 | Server is sole writer in production | Eliminates fsnotify-on-correctness-path; matches "remote-first, ICP has no shell" |
| 4 | Three immutable shapes: singleton, collection, stream | Maps storage to the existing 7-op semantics; eliminates "is it a file or directory?" ambiguity |
| 5 | Envelope format with server-managed `_meta` | One source of truth (the file); inspectable; round-trips cleanly |
| 6 | Server-monotonic timestamp for version (not content hash) | Human-readable, debuggable, no collision; HTTP `ETag`-compatible |
| 7 | Per-path mutex + atomic rename + version CAS for singletons | POSIX-safe, cross-platform, no lock files on disk |
| 8 | NDJSON streams with `O_APPEND` for atomic appends | Lock-free for writes ≤ PIPE_BUF; matches Log/TimeSeries patterns |
| 9 | Bleve for search index (pure Go, persisted) | Honors §1 (no CGO); avoids re-tokenizing on every restart |
| 10 | Three-tier read API (index, search, read) | Replaces `git clone + ripgrep` for agents; bounded payloads |
| 11 | MCP surface consolidates 13 → 10 tier-shaped tools | Scales sublinearly with new content types |
| 12 | Presigned URL pattern for binary uploads (kill base64) | 33% bandwidth + context window saved; community-converged pattern |
| 13 | Conflict responses embed current value | Saves a round-trip; agent sees state without GET-after-412 |
| 14 | MERGE never returns 412 (server-side retry under lock) | Most common write op stays conflict-free for the agent |
| 15 | Add INCREMENT and CAS as new atomic ops | Eliminates 90% of contended-write scenarios |
| 16 | Backup as built-in CLI subcommand (`agentboard backup`) | ICP doesn't install `aws-cli`/`rclone`; we ship the tool |
| 17 | No S3-compatible API in Phase 1 | High implementation cost (SigV4); ICP doesn't need it |
| 18 | Logrotate-style stream rotation (100MB × 5 segments) | Standard pattern; transparent to readers |
| 19 | Rate limit with friendly 429 + `Retry-After` header | Honors §12 (poka-yoke responses); no security claim |
| 20 | Batch ops (INCREMENT by N, APPEND items[], etc.) | Structural mitigation; agents rarely need to write fast |
| 21 | History via per-key NDJSON; activity via global NDJSON | Append-only, easy to tail; honors §10 (history retention) |
| 22 | fsnotify only in `--dev`, only for `pages/` + `components/` | Data writes always API-only, even in dev |
| 23 | Phase 1 scope: data KV only; auth/teams/etc. stay in SQLite | Bounded blast radius; auth has its own contract concerns |

## 26. Open questions for implementation

These don't block the spec but want resolution during Phase 1:

1. **Stream `since` query semantics.** `?since=<ts>` exact-match vs. `?since=<ts>` "after this timestamp"? Lean toward "after."
2. **Collection delete confirmation UX.** `DELETE /api/data/<key>?confirm=true` — should the dashboard show a confirmation modal? (Yes; details in frontend phase.)
3. **Activity log retention.** 100MB rotation matches streams. Total bound? (Lean toward 5 segments × 100MB = 500MB max, same as streams.)
4. **Per-key history retention.** "Last 100 entries" — should this be configurable per-key? (Yes, via `_meta.history_limit` on the key, set on first write or via admin API. Default 100.)
5. **Bleve indexing for object values.** Index all string fields recursively, or only top-level strings, or only those tagged in component `meta.props`? (Lean toward recursive top-level + first-level nesting; revisit at scale.)
6. **`agentboard backup` while writes ongoing.** Stop-the-world for ~50ms vs. snapshot-and-stream. (Lean toward stop-the-world; brief, simple, durable.)
7. **SSE ring buffer size.** 1000 events sounds right; revisit if reconnects miss events.
8. **What does `GET /api/data/<key>` on a stream return by default?** Lean toward last 100 lines, configurable via `?limit=`.
9. **Schema inference.** Currently inferred at GET-schema time from SQLite contents. Should the catalog index pre-compute this on write, or continue to infer on demand? (Lean toward pre-compute on write; cheap and avoids cold-cache latency.)
10. **Token attribution in `_meta.modified_by`.** Username vs. token ID vs. token label? (Lean toward username for human-readability; token label as fallback for bot tokens.)

---

## Appendix A — Conversation lineage

This spec consolidates the design discussion in branch `claude/file-based-storage-concept-EUSPh`. Decision sequence (chronological):

1. **Initial framing (concept session).** Files-only as Obsidian-style replacement for SQLite; concurrency is a UX problem for agents (they can handle 412s).
2. **Remote-storage reframe.** ICP context: AgentBoard is remote storage of company knowledge; "don't write to files directly" rule emulates the production remote case.
3. **Non-technical ICP reframe.** ICP has no git, no CLI tools — kills "git-native history" pro; weakens but doesn't eliminate files-only case. Forces backup CLI design.
4. **Remote interface research.** Surveyed S3, WebDAV, tus.io, plain REST. Decided: presigned URL pattern for binary upload; skip S3-compat for Phase 1.
5. **Agent ergonomics for remote.** Three-tier read API (index, search, read); embedded current value in conflict responses; tier-shaped MCP tools.
6. **Files-only commitment.** User leans into files + indexes for simplicity. Architectural decisions: three shapes, envelope format, version-as-timestamp, atomic rename.
7. **Version-as-frontmatter refinement.** Version lives inside the file's `_meta`; server overrides server-managed fields on write; agent only echoes `version`.
8. **Stream rotation, rate limiting, hot-path coalescing.** Logrotate-style segments; friendly 429; in-memory write coalescing for INCREMENT/APPEND.
9. **Page-locking work landed on main.** Pessimistic admin-only lock for pages — orthogonal to optimistic version-CAS for data; coexists.
10. **Migration explicitly out of scope.** User confirmed: fresh start, no SQLite import.

---

## Appendix B — Implementation deltas (post-spec adjustments)

The spec above is the design contract. These notes record where the implementation diverged or extended during the build, so the spec stays the canonical reference without rewriting history.

### B.1 — REST action verbs use `?op=`, not `:<action>`

The spec described `POST /api/data/<key>:append` (Google Cloud style). The implementation uses `POST /api/data/<key>?op=append` because chi's path parser handles query params more cleanly and the URL is just as self-documenting once an agent reads `?op=` once. Same operations, same body shapes, same error codes. Path-style remains a possibility for a future v3.

### B.2 — `/api/v2` mount, not `/api/data`

The new surface is mounted at `/api/data/*` parallel to the existing `/api/data/*` so the dashboard, MCP, and external integrations keep working through the migration window. Phase 5 removes the legacy mount; the v2 prefix can be dropped at the same time or kept as an alias.

### B.3 — Search uses a substring scanner, not Bleve

The spec called for Bleve as the search index. The implementation ships with a pure-Go substring scanner that walks the catalog and reads each file. Decision rationale: Bleve adds ~10 MB of binary weight + persistence complexity for a project size where walk-and-grep finishes in a few milliseconds. The Bleve upgrade is a drop-in replacement when scale demands it; the `Search()` interface is stable.

### B.4 — Presigned upload (§12) shipped end-to-end

Implemented as designed:
- `POST /api/files/request-upload` (auth required) mints a `ut_<43 chars>` token, scoped to one filename + size cap, TTL 5 minutes.
- `PUT /api/upload/{token}` (no auth) accepts raw bytes; tokens are one-shot, deleted on first redemption.
- `agentboard_request_file_upload` MCP tool returns `{upload_url, expires_at, max_size_bytes}`. Agents shell out with `curl --data-binary @file <url>`.

The legacy base64 path through `agentboard_write_file` remains for agents that genuinely cannot shell out, capped at 1 MiB per spec.

### B.5 — Rate limiter (§18) shipped

`golang.org/x/time/rate` token bucket per actor (200 writes/min sustained, 50/sec burst) gates every v2 mutation. Reads bypass. 429 response carries `Retry-After`, `retry_after_seconds`, and the configured limit per CORE_GUIDELINES §12. Idle limiters evicted after 1 hour by a janitor goroutine.

### B.6 — Activity log rotation (§17 mechanics, applied to audit)

The activity log uses the same logrotate-style scheme as streams: 100 MB cap on the active file, 5 segments retained. `ReadActivity` walks segments oldest → newest then the active file, producing a contiguous timeline across rotation boundaries.

### B.6.1 — Backup CLI (§20) shipped

`agentboard backup --to <path.tar.gz>` and `agentboard restore --from <path.tar.gz> --path <target>` are built-in CLI subcommands. Includes the entire project folder; excludes SQLite WAL/SHM (mid-flight state), `.DS_Store`/`Thumbs.db`, partial atomic-rename targets, and the bootstrap-secret URL. Restore refuses non-empty targets without `--force` and rejects tar entries with path traversal. S3 destinations are reserved for Phase 2 (AWS SDK pending).

### B.7 — Phase 4 split into "plumbing" and "polish"

The original Phase 4 plan was "migrate `useData` and the 9 components to envelope-aware semantics." What shipped:
- **Plumbing** — the view broker reads from both stores (legacy first, files-first fallback) and re-shapes `data` SSE events into the legacy `data` event shape. Existing components consume the same bare-value shape; pages can reference v2-only keys without component changes.
- **Polish (deferred)** — making `useData` envelope-aware, migrating dogfood pages to write through `/api/v2`. Not strictly necessary because the broker bridge handles the read path transparently.

The added `useData` hook + `<DataView>` component are the escape hatch for code that wants to consume the envelope directly.

### B.8 — MCP surface count

Final v2 tool count is **12**: the 11 originally specified plus `agentboard_request_file_upload`. Legacy tools (`agentboard_set`, `_merge`, `_append`, `_delete`, `_get`, etc.) remain registered and dispatch to the SQLite store; Phase 5 retires them.

---

**End of spec.**
