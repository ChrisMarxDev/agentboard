# AgentBoard

Single-binary dashboard server for agent-driven workflows. Agents write data via REST; humans read live dashboards in the browser.

> **Read [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md) before making non-trivial changes.** It defines the product invariants, the stable API/MCP/component contracts, what's in vs out of Phase 1 scope, and the pre-flight checklist for risky changes. Full design is in `spec.md`.
>
> **Before widening the trust boundary** (binding to non-loopback, adding hosted mode, turning on `--allow-component-upload`, etc.) re-read [`seams_to_watch.md`](./seams_to_watch.md) — it lists the security and architectural concerns we've consciously deferred.

## Task runner

**All run commands go through [Taskfile.dev](https://taskfile.dev/).** Use `task` instead of invoking `go`, `npm`, or `make` directly. Run `task` (or `task -l`) to see every available task.

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
task test:go            # Go unit tests (data layer + API handlers)
task test:frontend      # Frontend vitest
task test:integration   # End-to-end: starts server, hits every endpoint
```

## Running

```bash
task run                        # Build and run the binary
task run -- --port 3001         # Pass flags with --
task run -- --project myproject
```

### Test coverage
- `internal/data/store_test.go` — all 7 data operations, history, schema inference, subscriptions, merge patch
- `internal/server/handlers_test.go` — all REST endpoints, MCP protocol, CORS, error cases
- `frontend/src/components/builtin/*.test.tsx` — component rendering with mocked useData
- `scripts/integration-test.sh` — 29 end-to-end API tests + optional browser tests via gstack

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

# 5. Test live data updates — in another terminal/command:
curl -X PUT http://localhost:3000/api/data/welcome.users -d '999'
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

- **Go backend**: chi router, SQLite (modernc.org/sqlite, pure Go), cobra CLI
- **Frontend**: React 18 + Vite + Tailwind CSS + recharts + @mdx-js/mdx (client-side compilation)
- **Data model**: Key-value store with dotted paths. 7 write operations (SET, MERGE, UPSERT by ID, MERGE by ID, APPEND, DELETE, DELETE by ID)
- **Realtime**: SSE broadcaster pushes data changes to all connected browsers
- **MCP**: Streamable HTTP at /mcp with 13 tools for Claude integration
- **Pages**: MDX files compiled client-side, served from project folder
- **Components**: 9 built-in (Metric, Status, Progress, Table, Chart, TimeSeries, Log, List, Kanban) + user JSX in components/

## Key directories

- `cmd/agentboard/` — CLI entry point
- `internal/data/` — SQLite data store with all operations
- `internal/server/` — HTTP handlers, SSE broadcaster
- `internal/mcp/` — MCP protocol server + 13 tool definitions
- `internal/cli/` — Cobra commands (serve, set, get, list, etc.)
- `internal/project/` — Project model, config, first-run init
- `internal/mdx/` — Page management + file watcher
- `internal/components/` — Component catalog + file watcher
- `frontend/src/` — React app, hooks (useData, SSE), 9 built-in components
- `landing/` — Astro + Tailwind 4 marketing site (separate CDN deploy, **not** embedded in the Go binary)
- `scripts/` — integration test script

## Deploying (Fly.io + GitHub Actions)

Full deploy guide, cost breakdown, and open decisions live in [`HOSTING.md`](./HOSTING.md). The short version:

- `Dockerfile` + `fly.toml` + `.github/workflows/deploy.yml` ship with the repo; push to `main` redeploys to Fly.
- State is **ephemeral** today (`AGENTBOARD_PATH=/tmp/agentboard` on a sleeping machine) — data wipes on auto-stop/restart. `HOSTING.md` covers the persistent-volume fix (~$0.15/mo) when you want state to survive.
- Auth: `AGENTBOARD_AUTH_TOKEN` secret gates every route except `GET /api/health`. Accepts `Authorization: Bearer <token>`, HTTP Basic (password=token), or `?token=<token>`. Unset → server is open (local-only assumption). See `seams_to_watch.md` §"Single-token auth gate".

## Quick API test cheatsheet

```bash
# Set data
curl -X PUT localhost:3000/api/data/mykey -d '{"count":42}'

# Get data
curl localhost:3000/api/data/mykey

# Merge
curl -X PATCH localhost:3000/api/data/mykey -d '{"status":"done"}'

# Append to array
curl -X POST localhost:3000/api/data/events -d '{"msg":"hello"}'

# List all keys
curl localhost:3000/api/data

# Schema
curl localhost:3000/api/data/schema

# MCP tools list
curl -X POST localhost:3000/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```
