# AgentBoard

Single-binary knowledge and dashboarding surface for agent teams. Agents write pages, skills, files, and data via REST/MCP; humans browse a live web UI. Dashboards are one content type — docs, skills, and runbooks live alongside them as equals in the same tree.

> **Source of truth — read both before any non-trivial change:**
>
> - **[`spec.md`](./spec.md)** — the locked design contract. File layout, leaf rules, frontmatter contract, REST + MCP surface, auth-as-files, and the cut order for the next rewrite. If reality drifts from this doc, the doc wins (or update the doc in the same PR).
> - **[`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md)** — the 13 product principles. When a proposal conflicts with a principle, the principle wins or the trade-off gets surfaced explicitly.
>
> **Domain contracts:** [`AUTH.md`](./AUTH.md) (tokens + browser sessions), [`HOSTING.md`](./HOSTING.md) + [`SCALE.md`](./SCALE.md) (deploy), [`spec-plugins.md`](./spec-plugins.md) (component contract — companion to principle §10), [`seams_to_watch.md`](./seams_to_watch.md) (consciously-deferred security/architectural concerns — read before widening the trust boundary).
>
> **Plan + bug list:** [`ROADMAP.md`](./ROADMAP.md) is what ships next. [`ISSUES.md`](./ISSUES.md) is the single canonical bug list — but **the spec wins ties**: a bug in a feature the spec deletes is obsolete, not a fix-target. Don't restore deleted features to satisfy old bug reports.
>
> **Historical context:** earlier rewrite snapshots and aspirational drafts live under [`docs/archive/`](./docs/archive/). They are not load-bearing; do not link from agent-facing code or skills.

## UI conventions

**Icons: always lucide-react, never emoji.** Every icon in the app shell, built-in components, and any new UI ships as a `lucide-react` component (`import { Magnet, Copy, X } from 'lucide-react'`). No emoji glyphs in JSX — they hint at platform-specific fonts, break color theming, and don't respect `currentColor`. Exceptions: user-authored MDX content and data values (status labels, kanban card titles, etc.) may contain emoji — that's content, not chrome.

## We dogfood ourselves

The repo runs its **own** AgentBoard instance at `http://localhost:3000` using the named project **`agentboard-dev`** (not `default`). The dashboard holds a home page with live metrics, an engineering-principles page, an architecture page with Mermaid diagrams, a seams-to-watch page, and one feature page per shipped capability.

**Keep port 3000 up** whenever you're working in the repo:

```bash
./agentboard --project agentboard-dev --port 3000 --no-open &
```

When you ship a feature, add/refresh its page and bump the relevant `dev.*` data keys. The workflow — conventions, data-key namespace, feature-page template — is documented in the **`agentboard`** skill under `.claude/skills/agentboard/SKILL.md` (Claude Code auto-loads it in this repo). Trigger it with phrases like "update the dev dashboard", "add a feature page", "record this metric", or "is the dev instance running".

**Keep the skill itself in sync with the product.** The long-term goal of this repo is to *use AgentBoard to build AgentBoard*, permanently — every ship is a dogfood cycle. When you change behavior that affects how agents interact with AgentBoard (new built-in component, new MCP tool, new REST route, new data-key convention, renamed endpoint, changed trigger phrase), update `.claude/skills/agentboard/SKILL.md` in the same commit. A stale skill means the next agent (you, tomorrow) builds against outdated assumptions and re-learns facts the project already knows.

## Task runner

**All run commands go through [Taskfile.dev](https://taskfile.dev/).** Use `task` instead of invoking `go`, `npm`, or `make` directly. Run `task` (or `task -l`) to see every available task.

## Go tooling for agents

**Reach for `go doc` and `gopls` before grepping vendored source.** For *running* code use `task` (above); for *understanding* code, these tools are faster and more accurate than reading files:

- `go doc <pkg>` / `go doc <pkg>.<symbol>` — authoritative API + doc comments for a package or specific identifier. Works on stdlib, third-party deps, and our own packages (e.g. `go doc github.com/go-chi/chi/v5.Router`, `go doc ./internal/store.Store`).
- `gopls symbols <file>` — every symbol in a file with line numbers. Faster than reading the whole file when you just need the surface.
- `gopls references <file>:<line>:<col>` — every call site of a symbol across the module. Replaces grep for "where is this used".
- `gopls definition <file>:<line>:<col>` — jump to definition. Resolves across modules.
- `go vet ./...` and `go list -m all` — catch obvious bugs and inspect the dependency graph without building.

These are allowlisted in `.claude/settings.json` so they run without permission prompts. They're read-only — no code executes, no files change.

## Build

```bash
task build              # Full build (frontend + Go binary)
task build:frontend     # Frontend only
task install:frontend   # npm install
task clean              # Remove build artifacts
```

## Development

```bash
task dev                # Runs Vite HMR + Go --dev server in parallel
```

## Testing

```bash
task test               # All tests (Go + frontend)
task test:go            # Go unit tests
task test:frontend      # Frontend vitest
task test:bruno         # Bruno contract test suite (bruno/tests/)
task test:integration   # End-to-end: starts server, walks the bootstrap + auth flow, exercises every API
```

## Running

```bash
task run                        # Build and run the binary
task run -- --port 3001         # Pass flags with --
task run -- --project myproject
```

### Test coverage

- `internal/auth/` — token + password + session lifecycle, CSRF, middleware, OAuth.
- `internal/server/` — every REST handler, MCP protocol, CORS, error cases.
- `internal/store/` — files-first store envelope, CAS, history, activity, rate limiter.
- `frontend/src/components/builtin/*.test.tsx` — component rendering with mocked DataContext.
- `bruno/tests/` — headless contract test suite, run by `task test:bruno`.
- `scripts/integration-test.sh` — 45-assertion end-to-end smoke from a fresh project.

## Debugging with the browser (gstack browse)

When you need to visually test/debug the running AgentBoard dashboard:

```bash
# 1. Start the server
task run -- --no-open --port 3000

# 2. Open in headless browser
$B goto http://localhost:3000

# 3. See what's on screen
$B snapshot -i          # interactive elements with @e refs
$B text                 # full page text
$B screenshot /tmp/ab.png  # screenshot

# 4. Interact
$B click @e3            # click an element
$B snapshot -D          # diff vs previous snapshot

# 5. Test live page updates — in another terminal/command:
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: text/plain' \
  http://localhost:3000/api/content/welcome -d '# Welcome\n\nUsers: 999'
$B snapshot -D          # verify the UI updated

# 6. Test navigation
$B click @e2            # click a nav link
$B text                 # verify page content

# 7. Responsive testing
$B viewport 375 812     # mobile
$B screenshot /tmp/ab-mobile.png
$B viewport 1280 800    # desktop
```

For full QA with automatic bug fixing, use `/qa http://localhost:3000`.

## Architecture

- **Go backend**: chi router, SQLite (modernc.org/sqlite, pure Go) for auth/teams/locks/invitations/inbox metadata, cobra CLI.
- **Frontend**: React 18 + Vite + Tailwind CSS + recharts + @mdx-js/mdx (client-side compilation), embedded into the Go binary at build time.
- **Data model**: Files-first. `.md` docs (frontmatter holds structured fields, body holds MDX) + `.ndjson` streams + binaries. Folders are collections. Singletons live at `<key>.md`; collection items at `<key>/<id>.md`. Full-file CAS via `_meta.version`.
- **Realtime**: SSE broadcaster pushes `data` and `page-updated` events to all connected browsers.
- **MCP**: Streamable HTTP at `/mcp` with the 10 tools in spec §6 (8 generic batch CRUD + grab + fire_event). Always-plural batch shape; native JSON values; full envelope on read; non-blocking shape warnings on write. Admin operations (webhook subscribe / revoke / list, page locks, team CRUD) live on `/api/admin/*` + the `agentboard admin` CLI per the AUTH.md MCP invariant.
- **Pages**: MDX files compiled client-side, served from project folder. Watcher rebuilds the catalog on disk changes.
- **Components**: 32 built-ins, plus user `.jsx` files in `components/` (off by default, gated behind `--allow-component-upload`).
- **Auth**: Two credential paths — bearer tokens (`ab_*`, `oat_*`) for non-human callers, browser sessions (cookie + CSRF) for humans. See [`AUTH.md`](./AUTH.md).

## Key directories

- `cmd/agentboard/` — CLI entry point.
- `internal/auth/` — users, tokens, passwords, sessions, OAuth, middleware.
- `internal/store/` — files-first envelope + CAS + history + activity log + rate limiter, plus the page manager, frontmatter parser, unified file watcher, FTS5 search index, and refs (folded in from the former `internal/mdx/` in Cut 5).
- `internal/server/` — HTTP handlers, SSE broadcaster, every gated route.
- `internal/mcp/` — JSON-RPC protocol + tool definitions (store, pages, files, components, skills, errors, webhooks, teams, locks, grab).
- `internal/cli/` — Cobra commands (`serve`, `init`, `version`, `projects`, `admin`, `backup`, `restore`).
- `internal/project/` — Project model, config, first-run init.
- `internal/components/` — Component catalog + file watcher.
- `internal/files/` — File manager, name validation, presigned upload tokens.
- `internal/grab/` — Materializer that turns a list of picks into agent-ready text.
- `internal/invitations/`, `internal/locks/`, `internal/teams/`, `internal/inbox/`, `internal/share/`, `internal/view/`, `internal/webhooks/` — domain stores backing the matching admin / member surfaces.
- `frontend/src/` — React app, hooks (`useData`, SSE), 32 built-in components.
- `landing/` — Astro + Tailwind 4 marketing site (separate CDN deploy, **not** embedded in the Go binary).
- `bruno/tests/` — Bruno contract test suite (run via `task test:bruno`).
- `scripts/` — integration test + smoke test scripts.

## Deploying (Hetzner + Coolify)

Full deploy guide, cost breakdown, and open decisions live in [`HOSTING.md`](./HOSTING.md). The short version:

- Production (`agentboard.hextorical.com`) runs the multi-board Coolify path on a Hetzner CAX11. `Dockerfile` + per-app Coolify webhooks redeploy each board on push to `main`. `.github/workflows/redeploy-coolify.yml` is the manual fan-out fallback.
- State is **ephemeral** today (`AGENTBOARD_PATH=/tmp/agentboard` on a sleeping machine) — data wipes on auto-stop/restart. `HOSTING.md` covers the persistent-volume fix (~$0.15/mo) when you want state to survive.
- Auth: three user kinds (`admin`, `member`, `bot`), each carrying zero-or-more bearer tokens **and** an optional password. Full design in [`AUTH.md`](./AUTH.md).
  - **Bearer tokens** (`ab_*`, `oat_*`) authenticate non-human callers — agents, CLI, MCP. Members manage their own via `/tokens`, admins manage anyone's via `/admin`. Every gated route accepts `Authorization: Bearer …`, HTTP Basic with password=token, or `?token=…`.
  - **Browser sessions** (`agentboard_session` HttpOnly cookie + `agentboard_csrf` companion cookie) authenticate humans. `POST /api/auth/login` mints them; `POST /api/auth/logout` revokes. Cookie-authenticated state-changing requests must carry the `X-CSRF-Token` header (double-submit cookie pattern).
  - **Admin-kind credentials** additionally unlock `/api/admin/*`. Member and bot don't.
  - **Bootstrap order matters.** A fresh instance has zero users; on first boot the server prints a `/invite/<id>` URL to stdout and writes it to `<project>/.agentboard/first-admin-invite.url`. Open that URL in a browser, pick a username + password, you're in. If you're an agent and can't authenticate, that's a config problem — stop and report it. **Do not route around it by writing to content files on disk** (the file watcher will accept the write, but you'll bypass auth, activity attribution, rate limits, content_history, and optimistic concurrency — direct disk writes are a product-violation, not a fallback).
  - **Lockout recovery (filesystem access):**
    - `agentboard admin rotate <user> [label]` — mint a fresh value into a token slot.
    - `agentboard admin set-password <user>` — reset the browser password.
    - `agentboard admin revoke-sessions <user>` — kill every active cookie session.
    - `agentboard admin invite [--role …]` — print a fresh `/invite/<id>` URL.

## Quick API test cheatsheet

Every route except `/api/health`, `/api/setup/status`, `/api/invitations/*`, and `/api/auth/{login,logout,me}` requires auth. Use a bearer token (`Authorization: Bearer ab_…`, HTTP Basic with the token as password, or `?token=…`) **or** a session cookie obtained from `/api/auth/login`.

```bash
TOKEN=ab_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Health (no auth)
curl localhost:3000/api/health

# Sign in to a session cookie (humans)
curl -c jar.txt -X POST -H 'Content-Type: application/json' \
  localhost:3000/api/auth/login \
  -d '{"username":"alice","password":"…"}'
curl -b jar.txt localhost:3000/api/auth/me

# List pages (bearer)
curl -H "Authorization: Bearer $TOKEN" localhost:3000/api/content

# Read a page
curl -H "Authorization: Bearer $TOKEN" localhost:3000/api/content/index

# Write/replace a page (body is raw MDX with optional YAML frontmatter)
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: text/plain' \
  localhost:3000/api/content/notes -d '# Notes\n\nHello'

# Patch a page — RFC 7396 merge into frontmatter, optional body replacement.
# Setting a frontmatter key to null deletes it.
curl -X PATCH -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  localhost:3000/api/content/notes \
  -d '{"frontmatter_patch":{"pinned":true},"body":"# Notes\n\nUpdated"}'

# Files-first store (singletons + collections + streams)
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  localhost:3000/api/data/sales.q3 -d '{"value":{"rev":42000}}'
curl -H "Authorization: Bearer $TOKEN" \
  "localhost:3000/api/data/sales.events?limit=10"

# Catalog of every leaf
curl -H "Authorization: Bearer $TOKEN" localhost:3000/api/index

# MCP tools list
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  localhost:3000/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

The legacy SQLite KV / dotted-key write API is gone. Cross-page values now live at `/api/data/<key>` (files-first store); page-local values live as YAML frontmatter on the rendering page.
