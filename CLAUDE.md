# AgentBoard

**Single-binary git server** that hosts the shared workspace AI agents collaborate inside, plus a live web UI that humans use to read what the agents are doing. Agents clone, branch, commit, push — git's standard concurrency model handles parallel work, and conflicts surface as standard merge markers that Claude-class agents already know how to resolve.

> **Source of truth — read all three before any non-trivial change:**
>
> - **[`spec.md`](./spec.md)** — the design contract. Workspace model, concurrency policy, MCP surface.
> - **[`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md)** — the product principles. §15 (workspace teaches the agent) is the principle the whole architecture exists to serve.
> - **[`ROADMAP.md`](./ROADMAP.md)** — what ships next.
>
> **Domain contracts:** [`AUTH.md`](./AUTH.md) (tokens + browser sessions), [`HOSTING.md`](./HOSTING.md) + [`SCALE.md`](./SCALE.md) (deploy), [`spec-plugins.md`](./spec-plugins.md) (workspace-extension contract), [`seams_to_watch.md`](./seams_to_watch.md) (consciously-deferred security/architectural concerns — read before widening the trust boundary).
>
> **Historical context:** [`ISSUES.md`](./ISSUES.md) is the live bug list. Earlier rewrite snapshots and aspirational drafts live under [`docs/archive/`](./docs/archive/) — historical only.

## Architecture in one paragraph

A Go binary mounts a chi router. Three layers above it: an HTTP gateway (`/_api/*` for the REST + auth surface, `/git/<workspace>.git` for the smart-HTTP git endpoint, `/mcp` for the agent tool surface), an HTML renderer (`internal/html/`) that serves the workspace's working tree as a browsable dashboard, and `internal/gitserver/` which manages the bare repos + worktrees on disk. Each workspace is one git repo, with a worktree mirror the renderer reads from. Pages are real files in the worktree — HTML renders as expressive pages with full layout control, Markdown renders through goldmark, JSON with `columns`+`cards` renders as a kanban, anything else gets a preview or download affordance based on its extension. There is no MDX compiler, no React frontend, no `/api/<path>` data store with singletons/streams/binaries — those were retired during the substrate pivot. The workspace is the filesystem and the filesystem is git.

## Substrate

- **Git is the database.** Every change is a commit. Every commit is reversible. Conflict resolution is the standard tooling humans already know. There is no separate "store" layer.
- **Files are the API.** Whatever the team can author is the same thing the dashboard renders. Pages don't compile through a build step.
- **Per-actor tokens.** Humans and agents authenticate the same way: a personal token in `Authorization: Bearer …`. Activity is attributed to whoever's token signed the commit.

## We dogfood ourselves

The repo runs its **own** AgentBoard instance at `https://agentboard.hextorical.com` (Cloudflare Tunnel → `localhost:3000` on the Hetzner box). A demo workspace at `/agency/` shows what a real team workspace looks like — campaigns, briefs, kanban boards, decks. When you change the product, refresh that demo or the workspace's `pages/changelog.html` so the demo keeps matching the live behavior.

The canonical agent primer is `/SKILL.md` at the workspace root. Update it in the same commit when you change something agents see (a new MCP tool, a renamed endpoint, a new file-type rendering rule). A stale skill means the next agent builds against outdated assumptions.

## Task runner

All run commands go through [Taskfile.dev](https://taskfile.dev/). Use `task` instead of invoking `go` directly. `task` or `task -l` lists available targets.

```bash
task build              # Compile the Go binary (embeds internal/html/assets)
task dev                # Run the server with --dev (templates reload from disk)
task run                # Run the built binary (pass flags with --)
task test               # Go unit tests
task test:dogfood       # Boot ephemeral instance + run a Claude agent through the bootstrap prompt
task lint               # gofmt check + go vet
task fmt                # Apply gofmt
task deploy:vps         # Install/update on a Debian/Ubuntu VPS over SSH
```

## Go tooling for agents

Reach for `go doc` and `gopls` before grepping source — they're faster and more accurate:

- `go doc <pkg>` / `go doc <pkg>.<symbol>` — authoritative API + doc comments.
- `gopls symbols <file>` — every symbol with line numbers.
- `gopls references <file>:<line>:<col>` — every call site of a symbol.
- `gopls definition <file>:<line>:<col>` — jump to definition.
- `go vet ./...` and `go list -m all` — bug-catch + dependency graph.

Allowlisted in `.claude/settings.json` so they run without prompts. Read-only.

## Key directories

- `cmd/agentboard/` — CLI entry point.
- `internal/auth/` — users, tokens, passwords, sessions, OAuth, middleware.
- `internal/gitserver/` — bare-repo + worktree management, smart-HTTP git endpoint, post-receive event hub.
- `internal/html/` — server-rendered HTML dashboard. `renderFile`, `renderDirectory`, `renderMarkdown`, kanban renderer, embedded design-system assets at `/_static/*`.
- `internal/server/` — HTTP handlers for the `/_api/*` surface (auth, admin UI, invitations, setup, edit form, restore, OAuth) + SSE broadcaster.
- `internal/mcp/` — JSON-RPC protocol + the six git-aware MCP tools (`agentboard_workspaces`, `_pull`, `_propose`, `_resolve_conflict`, `_subscribe`, `_fire_event`).
- `internal/cli/` — Cobra commands (`serve`, `admin`, `deploy`, etc.).
- `internal/project/` — project lifecycle: first-run init, default workspace seeding, paths.
- `internal/search/` — SQLite FTS5 index over the worktree, rebuilt on push.
- `internal/db/`, `internal/embed/`, `internal/invitations/`, `internal/webhooks/` — supporting infrastructure.
- `landing/` — Astro marketing site at `agentboard.dev` (separate CDN deploy, **not** embedded in the Go binary).
- `scripts/` — VPS deploy + smoke tests.
- `test/dogfood/` — end-to-end "agent bootstrap from cold start" self-test.

## Deploying (Hetzner + Coolify)

Full deploy guide in [`HOSTING.md`](./HOSTING.md). Short version:

- Production runs on a Hetzner CAX11 behind a Cloudflare Tunnel. The `Dockerfile` is a single-stage pure-Go build; Coolify redeploys each board on push to `main`.
- Auth: three user kinds (`admin`, `member`, `bot`), each carrying zero-or-more bearer tokens **and** an optional password. Full design in [`AUTH.md`](./AUTH.md).
  - **Bearer tokens** (`ab_*`, `oat_*`) authenticate non-human callers — agents, CLI, MCP, git smart-HTTP. Members manage their own via `/me`, admins manage anyone's via `/_admin`. Every gated route accepts `Authorization: Bearer …`, HTTP Basic with password=token, or `?token=…`.
  - **Browser sessions** (`agentboard_session` HttpOnly cookie + `agentboard_csrf` companion cookie) authenticate humans. `POST /_api/auth/login` mints them; `POST /_api/auth/logout` revokes. Cookie-authenticated state-changing requests carry `X-CSRF-Token` (double-submit cookie pattern). The Secure flag follows `X-Forwarded-Proto: https` so cookies survive proxy hops like Cloudflare Tunnel.
  - **Admin-kind credentials** additionally unlock `/_admin/*`. Member and bot don't.
  - **Bootstrap order matters.** A fresh instance has zero users; first boot prints `/invite/<id>` to stdout and writes it to `<project>/.agentboard/first-admin-invite.url`. Open it, pick a username + password, you're in.
  - **Lockout recovery (filesystem access):**
    - `agentboard admin rotate <user> [label]` — mint a fresh token slot.
    - `agentboard admin set-password <user>` — reset the browser password.
    - `agentboard admin revoke-sessions <user>` — kill every active cookie session.
    - `agentboard admin invite [--role …]` — print a fresh `/invite/<id>` URL.

## Quick wire test

```bash
TOKEN=ab_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Health (no auth)
curl localhost:3000/_api/health

# Sign in to a session cookie (humans)
curl -c jar.txt -X POST -H 'Content-Type: application/json' \
  localhost:3000/_api/auth/login \
  -d '{"username":"alice","password":"…"}'
curl -b jar.txt localhost:3000/_api/auth/me

# List workspaces visible to this caller
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  localhost:3000/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agentboard_workspaces","arguments":{}}}'

# Clone a workspace via git (HTTP Basic with token as the password)
git clone http://_:$TOKEN@localhost:3000/git/dogfood.git

# Read a page rendered by the dashboard
curl -H "Authorization: Bearer $TOKEN" localhost:3000/pages/getting-started.html
```

There is no `/api/<path>` namespace — every URL maps to a file in the workspace's working tree. Writes go through git (clone + commit + push) or the MCP tool surface; **never** to the working-tree mirror on disk directly. Direct disk writes bypass auth, attribution, content history, and the post-receive event bus.
