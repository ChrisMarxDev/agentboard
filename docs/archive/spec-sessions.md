# AgentBoard — Sessions Feature Spec

> **Status**: Draft. Builds on the v2 spec in `spec.md`. This feature is optional to turn on — AgentBoard continues to work with zero session awareness for callers who don't use it.

---

## Table of Contents

1. [Motivation](#1-motivation)
2. [Design Principles](#2-design-principles)
3. [Non-Goals](#3-non-goals)
4. [Data Model](#4-data-model)
5. [REST API](#5-rest-api)
6. [Attribution — Tagging Writes](#6-attribution--tagging-writes)
7. [Lifecycle & States](#7-lifecycle--states)
8. [Binding Data Records to Sessions](#8-binding-data-records-to-sessions)
9. [Dashboard UX](#9-dashboard-ux)
10. [MCP Integration](#10-mcp-integration)
11. [Claude Code Hooks Adapter (Reference)](#11-claude-code-hooks-adapter-reference)
12. [Other Agent Adapters](#12-other-agent-adapters)
13. [Implementation Phases](#13-implementation-phases)
14. [Open Questions](#14-open-questions)

---

## 1. Motivation

Today, AgentBoard data answers *what* is happening (ticket moved, metric changed). It doesn't answer *who* or *where to find the agent that did it*. When a human watching the dashboard sees a card flip from `todo` → `doing`, there's no way to click through to the live agent, inspect its context, or resume it.

Sessions fill that gap. An **agent session** is a first-class entity that represents one running agent — Claude Code, Codex, a custom script, CI job, anything. Every write can be attributed to a session. Every record (a ticket, a task, a row in a table) can be *bound* to the session currently working on it. The dashboard joins the two: click a card, see the agent, jump to its transcript or resume command.

Crucially, this is *agent-agnostic*: sessions are created via plain HTTP, and any tool that can `curl` can integrate. Claude Code integration ships as one reference adapter built on its native hooks, but nothing about the data model or API is Claude-specific.

---

## 2. Design Principles

1. **Generic over Claude-specific.** The primitive is `POST /api/sessions`. Claude hooks are one caller among many.
2. **Sessions are just data.** Stored in SQLite under the same KV store, indexed, queryable — no new storage engine.
3. **Opt-in.** AgentBoard without sessions still works exactly as it does today. Every write accepting a session header is backwards-compatible.
4. **Metadata, not transcripts.** AgentBoard stores session *state* (status, timestamps, working dir, bound items). Transcript files stay with the agent (e.g. `~/.claude/projects/...`). We expose a *pointer* to the transcript, not a copy.
5. **Cheap to write, fast to read.** Registration is one HTTP call, status updates are PATCHes. Listing active sessions is a single SQL query on an indexed column.
6. **Lifecycle through heartbeats, not trust.** An agent crashing mid-task must not leave its session forever marked `running`. Stale sessions auto-transition to `stale` after N seconds without a heartbeat.

---

## 3. Non-Goals

- **Not a session store.** Transcripts, prompts, tool-call payloads — none of this lives in AgentBoard. Only status + pointers.
- **Not a spawner.** AgentBoard does not start, kill, or resume agents. It surfaces the commands a human can run.
- **Not cross-project.** A session belongs to exactly one AgentBoard project. Multi-project visibility is a separate feature.
- **Not an auth system.** Session tokens (if used) prevent accidental cross-talk, not malicious impersonation. Real auth is still the responsibility of the `Authentication` layer in `spec.md §12`.

---

## 4. Data Model

### 4.1 SQLite schema

Add one table:

```sql
CREATE TABLE agent_sessions (
  id             TEXT PRIMARY KEY,
  status         TEXT NOT NULL,           -- see §7
  agent          TEXT,                    -- free-form: "claude-code", "codex", "cron-etl", ...
  cwd            TEXT,                    -- working directory (if relevant)
  project        TEXT,                    -- AgentBoard project name
  title          TEXT,                    -- optional short label shown in UI
  started_at     TIMESTAMP NOT NULL,
  last_seen_at   TIMESTAMP NOT NULL,      -- updated on any heartbeat/update
  ended_at       TIMESTAMP,               -- null while running
  metadata       JSON,                    -- free-form: git branch, prompt, model, etc.
  resume_cmd     TEXT                     -- optional shell command to resume this session
);

CREATE INDEX idx_agent_sessions_status ON agent_sessions(status);
CREATE INDEX idx_agent_sessions_last_seen ON agent_sessions(last_seen_at);
```

Session records are also exposed through the existing KV store at `_sessions.<id>` so dashboards/MDX pages can read them via `useData` without a new hook. The table is the source of truth; the KV view is a materialized mirror.

### 4.2 History tagging

The existing `data_history` table gains one column (see `spec.md §16`):

```sql
ALTER TABLE data_history ADD COLUMN session_id TEXT REFERENCES agent_sessions(id);
```

Every write carrying an `X-Agent-Session` header writes this column; every other write leaves it null. This makes history queryable by session (useful for the dashboard drawer in §9).

### 4.3 Bound records

Binding a record (e.g. a Kanban card) to a session is a convention, not a schema change: put a `_boundSession: "<id>"` field in the record. The dashboard knows to pick this up, and the field is preserved through merges. Any key under the `_` prefix is reserved for AgentBoard metadata.

---

## 5. REST API

Base path continues to be `/api`. All responses are JSON.

### 5.1 Create a session

```
POST /api/sessions
Content-Type: application/json

{
  "agent": "claude-code",              // optional, free-form
  "cwd": "/Users/c/dev/agentboard",    // optional
  "title": "Refactor bruno collection",// optional; becomes the UI label
  "metadata": { "branch": "main", "model": "opus-4.7" },
  "resume_cmd": "claude --resume abc123",
  "id": "abc123"                       // optional; server generates if omitted
}

→ 201 Created
{
  "id": "abc123",
  "token": "srv_tok_xxxxx",            // use for subsequent PATCHes (see §5.7)
  "status": "running",
  "started_at": "2026-04-17T10:00:00Z"
}
```

If `id` is omitted, server generates a short ULID. If `id` is supplied and already exists, returns 409 (idempotent creation handled with `PATCH` instead).

### 5.2 Update a session

```
PATCH /api/sessions/:id
Authorization: Bearer <token>    // optional, see §5.7

{
  "status": "idle",                // optional; see §7 for legal transitions
  "title": "New title",
  "metadata": { "lastPrompt": "fix naming" }
}

→ 200 OK
{ "id": "abc123", "status": "idle", ... }
```

Calling PATCH also implicitly updates `last_seen_at` (it counts as a heartbeat).

### 5.3 Heartbeat

```
POST /api/sessions/:id/heartbeat
→ 200 OK { "last_seen_at": "..." }
```

A zero-body alternative to PATCH for adapters that just need to keep the session from going `stale`.

### 5.4 End a session

```
POST /api/sessions/:id/end
{ "status": "ended" }        // or "failed", "cancelled"

→ 200 OK
{ "id": "abc123", "status": "ended", "ended_at": "..." }
```

Idempotent. Once ended, further PATCHes are 409.

### 5.5 List sessions

```
GET /api/sessions?status=running&agent=claude-code&limit=50

→ 200 OK
{
  "sessions": [ { "id": "...", "status": "running", ... }, ... ],
  "total": 3
}
```

Filters: `status`, `agent`, `project`, `since` (ISO-8601 cutoff on `last_seen_at`).

### 5.6 Get one session (with bound records and recent writes)

```
GET /api/sessions/:id

→ 200 OK
{
  "session": { "id": "...", "status": "running", ... },
  "bound": [
    { "key": "welcome.tasks", "id": "3", "title": "Ship the Bruno collection" }
  ],
  "recent_writes": [
    { "key": "welcome.tasks", "id": "3", "op": "merge", "at": "..." }
  ]
}
```

`recent_writes` is derived from `data_history.session_id = :id`, capped at 50 entries.

### 5.7 Token authorization (optional)

When a session is created, the server returns a token. Subsequent `PATCH /api/sessions/:id` and `POST /api/sessions/:id/{heartbeat,end}` calls accept `Authorization: Bearer <token>`. Writes to `/api/data/*` do *not* require the token — they accept the `X-Agent-Session: <id>` header alone. Tokens are meant to prevent accidental cross-talk between siblings, not to resist attackers. In local mode (`Authentication` disabled), tokens are optional and calls without them still work.

---

## 6. Attribution — Tagging Writes

Every write endpoint (`PUT`, `PATCH`, `POST`, `DELETE` on `/api/data/*`) accepts a new header:

```
X-Agent-Session: abc123
```

This composes with the existing `X-Agent-Source` header:

| Header                     | Meaning                                               |
|----------------------------|-------------------------------------------------------|
| `X-Agent-Source: <name>`   | *What kind of caller* wrote this (existing, see spec.md §15) |
| `X-Agent-Session: <id>`    | *Which specific session* wrote this (new)             |

Both are optional and independent. When `X-Agent-Session` is present and matches a known session, the server:
1. Writes `session_id` into the `data_history` row.
2. Touches `agent_sessions.last_seen_at` (implicit heartbeat).

If the session id is unknown, the write still succeeds but the `session_id` column is null and a `Warning` header is returned (`X-AgentBoard-Warning: unknown-session-id`).

---

## 7. Lifecycle & States

```
  ┌──────────┐   PATCH status=running/idle      ┌────────┐
  │  (none)  │──POST /api/sessions────────────▶ │running │──PATCH──▶ idle ──PATCH──▶ running
  └──────────┘                                  └───┬────┘                ▲
                                                    │                     │
                                       stale-timer  │                     │ heartbeat
                                                    ▼                     │
                                                 stale ──heartbeat ───────┘
                                                    │
                                         POST /end  │
                                                    ▼
                                                 ended / failed / cancelled
```

### Legal states

| State       | Meaning                                                       |
|-------------|---------------------------------------------------------------|
| `running`   | Actively processing (prompt in flight, tool executing)        |
| `idle`      | Registered and alive but waiting for input                    |
| `awaiting-input` | Blocked on a permission/notification (optional sub-state) |
| `stale`     | Server has not heard from this session for N seconds (default: 300s) |
| `ended`     | Normal termination                                            |
| `failed`    | Terminated with error                                         |
| `cancelled` | Killed by the user                                            |

### Stale sweep

A background goroutine runs every 60s. Any `running`/`idle`/`awaiting-input` session whose `last_seen_at` is older than `SESSION_STALE_AFTER` (default 300s, configurable via `--session-stale-after`) is transitioned to `stale`. Receiving any heartbeat/PATCH clears `stale` back to its previous state.

Terminal states (`ended`/`failed`/`cancelled`) are never swept.

---

## 8. Binding Data Records to Sessions

Binding is set by the caller, read by the dashboard:

```bash
# Agent picks up the ticket
curl -X PATCH localhost:3000/api/data/welcome.tasks/3 \
  -H "X-Agent-Session: $SESSION" \
  -d '{ "status": "doing", "_boundSession": "'$SESSION'" }'
```

The `_` prefix is reserved for AgentBoard metadata fields. The built-in `Kanban` component renders a session badge when it sees `_boundSession`; other components can opt in by the same convention. Clearing a binding is a plain PATCH: `{ "_boundSession": null }`.

**Auto-binding convenience**: if a write carries `X-Agent-Session` AND the target value is an object AND no `_boundSession` already exists, the server sets `_boundSession` automatically. Callers that *don't* want this pass `X-AgentBoard-Auto-Bind: 0`. This makes the feature cheap to adopt — agents that don't know about sessions at all just need to set the header once at startup.

---

## 9. Dashboard UX

### 9.1 New built-in component: `<Sessions>`

A list/card view of active sessions:

```mdx
<Sessions status="running,idle" />
```

Shows: status pill, title, agent, cwd, time-since-last-seen, count of bound records. Click a session → side drawer with:
- Metadata blob
- Bound records (clickable → jump to record)
- Recent writes (last 20)
- Resume command (copyable button)

### 9.2 Session badges on other components

`Kanban`, `Table`, and `List` pick up `_boundSession` on records. Badge shows:
- Green dot if session is `running`
- Grey dot if `idle`
- Yellow if `awaiting-input`
- Red outline if `stale`
- No badge if `_boundSession` refers to an ended/unknown session

Clicking the badge opens the same drawer as §9.1.

### 9.3 Realtime

Every session state transition fires an SSE event on the existing broadcaster with topic `sessions.<id>`. The `useData` hook auto-subscribes via the `_sessions.*` KV mirror, so no new frontend machinery is needed.

---

## 10. MCP Integration

Three new MCP tools ship alongside the existing 13 (per `spec.md §11`):

| Tool                     | Purpose                                          |
|--------------------------|--------------------------------------------------|
| `agentboard_register_session` | Create a session from within an MCP-connected agent. Returns `{id, token}`. |
| `agentboard_update_session`   | Update status/metadata/title.                  |
| `agentboard_end_session`      | Terminate the session.                         |

Claude agents that use the AgentBoard MCP server can self-register without touching the REST API. The MCP server injects `X-Agent-Session` into any subsequent `agentboard_set`/`agentboard_merge`/etc. tool calls automatically.

---

## 11. Claude Code Hooks Adapter (Reference)

The canonical way to wire Claude Code into AgentBoard. Ships as:

### 11.1 A template settings block

```json
{
  "hooks": {
    "SessionStart": [{ "hooks": [{ "type": "command", "command": "agentboard session start --from-env" }] }],
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "agentboard session beat --from-env --status running" }] }],
    "Stop":            [{ "hooks": [{ "type": "command", "command": "agentboard session beat --from-env --status idle" }] }],
    "Notification":    [{ "hooks": [{ "type": "command", "command": "agentboard session beat --from-env --status awaiting-input" }] }],
    "SessionEnd":      [{ "hooks": [{ "type": "command", "command": "agentboard session end --from-env" }] }]
  }
}
```

### 11.2 A CLI installer

```bash
agentboard hooks install --global       # merges into ~/.claude/settings.json
agentboard hooks install --project .    # merges into .claude/settings.json
agentboard hooks uninstall [--global|--project]
agentboard hooks status                 # shows what's installed where
```

Merging is non-destructive: existing hooks are preserved, AgentBoard entries are added (or updated in place on reinstall) under a clearly labeled block.

### 11.3 CLI subcommands backing the hooks

```
agentboard session start [--from-env] [--agent <name>] [--title <t>]
agentboard session beat  [--from-env] [--status <s>]
agentboard session end   [--from-env] [--status ended|failed|cancelled]
```

`--from-env` pulls `CLAUDE_SESSION_ID` → session id, `CLAUDE_PROJECT_DIR` → cwd, and sets `agent=claude-code`, `resume_cmd="claude --resume $CLAUDE_SESSION_ID"` automatically. The CLI talks to the locally running AgentBoard over loopback; if no server is reachable, it exits 0 silently so hooks don't break the user's Claude session.

### 11.4 Per-project filtering

A project-level `install --project` scopes the hook to just that project. For a global install, sessions are tagged with `cwd` and dashboards can filter by it.

---

## 12. Other Agent Adapters

Any tool that can shell out can integrate. Reference one-liners:

**Codex / generic CLI**:
```bash
SESSION=$(curl -s -X POST localhost:3000/api/sessions \
  -d '{"agent":"codex","cwd":"'$PWD'"}' | jq -r .id)
trap "curl -X POST localhost:3000/api/sessions/$SESSION/end" EXIT
# ... agent does work, passing X-Agent-Session: $SESSION on writes ...
```

**Cron / CI job**:
```bash
SESSION=$(curl -s -X POST localhost:3000/api/sessions \
  -d '{"agent":"cron-etl","title":"nightly-import"}' | jq -r .id)
./run-etl.sh "$SESSION"
curl -X POST localhost:3000/api/sessions/$SESSION/end -d '{"status":"ended"}'
```

**Cursor / in-editor agent**: set the session at extension start, include the header on all writes, end on editor shutdown.

We do not attempt to auto-detect or wrap these tools. We document the shape and let the agent author wire it in.

---

## 13. Implementation Phases

### Phase 1 — Core (1-2 days)
- [ ] Migration: `agent_sessions` table + `data_history.session_id` column
- [ ] REST: create, PATCH, heartbeat, end, list, get
- [ ] `X-Agent-Session` header accepted on all `/api/data/*` writes; stamps `data_history`
- [ ] Stale sweep goroutine
- [ ] Store unit tests for lifecycle + stale transitions
- [ ] Handler tests for the six session endpoints

### Phase 2 — Attribution everywhere (0.5 day)
- [ ] Auto-bind `_boundSession` on object writes (with opt-out header)
- [ ] Mirror to KV under `_sessions.<id>`
- [ ] SSE broadcast on state transitions

### Phase 3 — Dashboard (1 day)
- [ ] `<Sessions>` built-in component
- [ ] Session badges on `Kanban`, `Table`, `List`
- [ ] Session drawer (metadata + bound records + recent writes + resume command)

### Phase 4 — MCP tools (0.5 day)
- [ ] `agentboard_register_session`, `_update_session`, `_end_session`
- [ ] MCP server injects `X-Agent-Session` automatically on subsequent writes

### Phase 5 — Claude Code hooks adapter (1 day)
- [ ] `agentboard session {start,beat,end}` CLI subcommands
- [ ] `agentboard hooks {install,uninstall,status}` CLI
- [ ] Settings-merge helper (preserve existing hooks, idempotent)
- [ ] Integration test: install hook → run `claude` → verify session appears → session ends

### Phase 6 — Docs & examples (0.5 day)
- [ ] README section
- [ ] Bruno collection folder: `sessions/`
- [ ] Example MDX page wiring `<Sessions>` + a Kanban with session badges
- [ ] Adapter recipes for Codex, cron, Cursor

---

## 14. Open Questions

1. **Default auto-bind on or off?** On is better DX, off is safer. Current spec says **on** with opt-out header.
2. **Token enforcement in local mode?** Current spec says tokens are optional in local mode. Revisit if we see cross-talk bugs in practice.
3. **Multi-binding**: can one record be bound to multiple sessions at once? Current spec says no — `_boundSession` is a scalar. If we see a real pair-programming case, extend to `_boundSessions: []`.
4. **Transcript embedding**: right now we only store a pointer + resume command. Do we want a `GET /api/sessions/:id/transcript` that streams from the agent's local JSONL file? Probably phase 7+ if demand shows up.
5. **Cross-project sessions**: if the user runs two AgentBoard projects and the agent writes to both, which session entity represents it? Current spec: one session per (agent-run × AgentBoard-project). The same Claude run might register twice if it touches two projects. Revisit if this becomes common.
6. **Session GC**: ended sessions stay forever. Good for audit, bad for `GET /api/sessions` pagination over time. Add a `--session-retention-days` setting that drops sessions beyond the cutoff?
7. **Dashboard: single "sessions" system page** shipped by default (like the welcome page), or leave it to the user to mount `<Sessions />` where they want?

---

## 15. Rollout & Compatibility

- **Fully backwards-compatible.** Writes without session headers behave exactly as they do today. Dashboards without `<Sessions />` render unchanged. The `agent_sessions` table only gets populated by callers who opt in.
- **Schema migration** runs on first boot of the new binary. A no-session `data_history` row is forward-compatible with the new nullable `session_id` column.
- **Config knobs**: `--sessions-enabled` (default `true`), `--session-stale-after=300s`, `--session-retention-days=0` (0 = never GC).

---

*See `spec.md` for the architectural context, KV data model, SSE transport, and existing REST/MCP surface that this feature builds on.*
