# AgentBoard

**A single-binary dashboard server for agent-driven workflows.** Agents push data over REST; humans read a live dashboard in the browser. Pages are MDX, components are JSX, storage is SQLite — all baked into one Go binary.

> Evidence.dev for normal people, where AI agents are the authors and the data source.

---

## What it is

- **Single binary, zero dependencies.** No Node, Python, Docker, or system services to run it. Download, execute, done.
- **Self-host anywhere.** Your laptop, a Raspberry Pi, a $0.15/month Fly machine, your own VPS. AgentBoard is the software — the deployment is yours.
- **AI-first authoring.** REST endpoints, MCP tools, and component props are shaped so an LLM can write pages, emit data, and add new visualizations without human ergonomics getting in the way.
- **Human-first reading.** The rendered dashboard is a polished, readable document. No SQL panels, no jargon, no "advanced" toggles.
- **Extensible via files.** New pages are `.mdx` files. New components are `.jsx` files. Drop them in a folder; hot reload picks them up.
- **Realtime by default.** SSE pushes every data change to connected browsers. No polling, no page refreshes.

Read the product principles in [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md) and the full spec in [`spec.md`](./spec.md).

---

## Quickstart

> **Coming soon.** One-line installers are planned — the commands below are placeholders and don't work yet. See [Build from source](#build-from-source) for the current path.

```bash
# Homebrew (planned)
brew install agentboard

# Install script (planned)
curl -fsSL https://agentboard.dev/install.sh | bash
```

Once installed:

```bash
agentboard                         # boots on http://localhost:3000
agentboard --project ./my-board    # use a specific project folder
agentboard --port 8080 --no-open   # custom port, don't pop a browser
```

On first run, AgentBoard creates a `.agentboard/` folder with a starter page and a SQLite database. Open the URL in your browser and start pushing data:

```bash
curl -X PUT  localhost:3000/api/data/users.count  -d '42'
curl -X POST localhost:3000/api/data/events       -d '{"msg":"deploy started"}'
curl      localhost:3000/api/data
```

---

## Build from source

**Requirements:** Go 1.25+, Node 20+, [Task](https://taskfile.dev/) (`brew install go-task`).

```bash
git clone https://github.com/christophermarx/agentboard.git
cd agentboard

task build          # builds the frontend, then compiles the Go binary → ./agentboard
./agentboard        # run it
```

Or step by step:

```bash
task install:frontend    # npm install in frontend/
task build:frontend      # Vite build → frontend/dist (gets embedded into the binary)
task build               # go build → ./agentboard
```

Other common tasks:

```bash
task              # list every task
task dev          # Vite HMR + Go dev server in parallel (fast feedback loop)
task test         # run Go tests + frontend vitest suite
task test:integration    # end-to-end: starts server, hits every endpoint
task clean        # remove build artifacts
```

The binary is fully static (CGO disabled, pure-Go SQLite via `modernc.org/sqlite`) — you can copy it to any Linux/macOS/Windows machine of the same architecture and run it.

### Cross-compiling

```bash
GOOS=linux   GOARCH=amd64 task build     # Linux x86_64
GOOS=linux   GOARCH=arm64 task build     # Linux ARM (e.g. Raspberry Pi 4/5)
GOOS=darwin  GOARCH=arm64 task build     # Apple Silicon
GOOS=windows GOARCH=amd64 task build     # Windows x86_64
```

---

## Self-hosting

The binary is designed to run anywhere — see [`HOSTING.md`](./HOSTING.md) for the supported deployment paths, cost breakdowns, and first-time setup. The current production reference is a Hetzner CAX11 running [Coolify](https://coolify.io) so multiple boards share one box for ~€3/mo.

Pick whatever host fits:

- **Hetzner / DigitalOcean / your homelab**: `scripts/deploy-vps.sh` does a one-shot install behind Caddy with Let's Encrypt. ~€3–4/mo.
- **Multi-tenant on one VPS**: install Coolify, then `scripts/new-board.sh` provisions per-friend boards with isolated containers + volumes.
- **Render / Railway / Koyeb**: point at the Dockerfile; they'll build + run it.
- **Raspberry Pi**: cross-compile for `linux/arm64`, `scp` the binary, run it.

Auth for non-localhost deployments is the user-token system documented in [`AUTH.md`](./AUTH.md): per-user tokens, plus an OAuth 2.1 + DCR surface for browser-driven MCP clients (e.g. Claude.ai Custom Connectors). See [`seams_to_watch.md`](./seams_to_watch.md) for the security model and what's explicitly deferred.

---

## Project goals & non-goals

**Goals**

- A self-hostable single binary for agent-driven dashboards that users own end-to-end.
- A stable, minimal REST + MCP surface that any agent (Claude, scripts, CI jobs) can push data into.
- Composability through files: pages and components are artifacts, not configuration.

**Non-goals (today)**

- We do **not** operate a hosted AgentBoard service. You deploy your own.
- No multi-tenant auth, no per-user accounts, no billing. The trust boundary is "you control the machine and the token."
- No SQL query panels, no admin UI. Agents write data; pages render data. That's the whole surface.

A managed cloud service is a possible *future* direction but explicitly undecided. It will not compromise the self-host-first design.

---

## Architecture (one paragraph)

A Go backend (chi router, pure-Go SQLite, cobra CLI) embeds the Vite-built React frontend and serves it from a single binary. The frontend compiles MDX in the browser (via `@mdx-js/mdx`) and subscribes to an SSE broadcaster for live data updates. Pages live as `.mdx` files, custom components as `.jsx` files — both hot-reload from disk. An MCP server is mounted at `/mcp` for Claude integration. Data is a key-value store with dotted paths and seven write operations (SET, MERGE, UPSERT by ID, MERGE by ID, APPEND, DELETE, DELETE by ID).

Full design: [`spec.md`](./spec.md). Key directories:

```
cmd/agentboard/        CLI entry point
internal/data/         SQLite data store
internal/server/       HTTP handlers, SSE broadcaster
internal/mcp/          MCP protocol server + 13 tool definitions
internal/mdx/          Page management + file watcher
internal/components/   Component catalog + file watcher
frontend/src/          React app + 9 built-in components
```

---

## Contributing

Contributions are welcome. See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for dev setup, test expectations, and how to open a good PR.

Before proposing a non-trivial change, read [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md) — the eight product principles that shape what belongs in core vs. what should be a plugin, component, or external connector.

---

## License

AgentBoard is open source under the [MIT License](./LICENSE).
