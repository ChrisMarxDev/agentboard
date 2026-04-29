# AgentBoard

**A single-binary dashboard server for agent-driven workflows.** Agents push pages, files, and data over REST or MCP; humans read a live dashboard in the browser. Pages are MDX, components are JSX, storage is plain `.md` + `.ndjson` + binaries on disk — all baked into one Go binary.

> Evidence.dev for normal people, where AI agents are the authors and the data source.

---

## What it is

- **Single binary, zero dependencies.** No Node, Python, Docker, or system services to run it. Download, execute, done.
- **Self-host anywhere.** Your laptop, a Raspberry Pi, your own VPS — €3/mo Hetzner is fine. AgentBoard is the software; the deployment is yours.
- **AI-first authoring.** REST endpoints, MCP tools, and component props are shaped so an LLM can write pages, emit data, and add new visualizations without human ergonomics getting in the way.
- **Human-first reading.** The rendered dashboard is a polished, readable document. No SQL panels, no jargon, no "advanced" toggles.
- **Files-first storage.** Pages, collection items, and singletons are `.md` files with YAML frontmatter; streams are `.ndjson`; binaries are files. Everything lives in one tree under the project root. Folders are collections.
- **Realtime by default.** SSE pushes every page and data change to connected browsers. No polling, no page refreshes.

Read the product principles in [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md), the locked rewrite contract in [`spec-rework.md`](./spec-rework.md), and the auth design in [`AUTH.md`](./AUTH.md).

---

## Quickstart

> **Coming soon.** One-line installers are planned — the commands below are placeholders. See [Build from source](#build-from-source) for the current path.

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

On first run, AgentBoard creates a `.agentboard/` folder, writes a starter page, and prints a `/invite/<id>` URL to stdout (also written to `<project>/.agentboard/first-admin-invite.url`). Open that URL in a browser, pick a username + password, and you're the first admin. From then on:

```bash
TOKEN=ab_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Browser session login mints a cookie — for humans
curl -c jar.txt -X POST -H 'Content-Type: application/json' \
  http://localhost:3000/api/auth/login \
  -d '{"username":"alice","password":"…"}'

# Bearer auth — for agents, MCP, CLI
curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/api/content

# Push a page (MDX with optional YAML frontmatter)
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: text/plain' \
  http://localhost:3000/api/content/notes -d '# Notes\n\nFirst entry.'

# Push a singleton value into the files-first store
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  http://localhost:3000/api/data/sales.q3 -d '{"value":{"rev":42000}}'
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
task                     # list every task
task dev                 # Vite HMR + Go dev server in parallel (fast feedback loop)
task test                # run Go tests + frontend vitest suite
task test:bruno          # Bruno contract test suite (bruno/tests/)
task test:integration    # end-to-end: bootstrap a fresh project, walk auth + every API
task clean               # remove build artifacts
```

The binary is fully static (CGO disabled, pure-Go SQLite via `modernc.org/sqlite`) — you can copy it to any Linux / macOS / Windows machine of the same architecture and run it.

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

Auth has two credential paths, both per-user (no shared admin token):

- **Bearer tokens** (`ab_…`, plus `oat_…` audience-scoped tokens minted via OAuth 2.1 + DCR for browser-driven MCP clients like Claude.ai Custom Connectors) — used by agents, CLI, and MCP.
- **Browser sessions** (`agentboard_session` HttpOnly cookie + `agentboard_csrf` companion, double-submit CSRF) — used by humans.

Full design in [`AUTH.md`](./AUTH.md). Trust-boundary deferrals in [`seams_to_watch.md`](./seams_to_watch.md).

---

## Project goals & non-goals

**Goals**

- A self-hostable single binary for agent-driven dashboards that users own end-to-end.
- A stable, minimal REST + MCP surface that any agent (Claude, scripts, CI jobs) can push pages and data into.
- Composability through files: pages, data, and components are artifacts, not configuration.

**Non-goals (today)**

- We do **not** operate a hosted AgentBoard service. You deploy your own.
- No multi-tenant accounts, no billing. The trust boundary is "you control the machine and your credentials."
- No SQL query panels, no admin UI for the data plane. Agents write data; pages render data. That's the whole surface.

A managed cloud service is a possible *future* direction but explicitly undecided. It will not compromise the self-host-first design.

---

## Architecture (one paragraph)

A Go backend (chi router, pure-Go SQLite for auth/teams/locks/invitations metadata, cobra CLI) embeds the Vite-built React frontend and serves it from a single binary. The frontend compiles MDX in the browser (via `@mdx-js/mdx`) and subscribes to an SSE broadcaster for live data + page updates. Pages and data live as `.md` files with YAML frontmatter on disk; folders are collections (`tasks/<id>.md` cards make up the `tasks/` board). Streams are `.ndjson`. Custom components are `.jsx` files — both pages and components hot-reload from disk. An MCP server is mounted at `/mcp` (~40 tools across pages, files, store, components, skills, errors, webhooks, teams, locks, grab) for Claude integration.

Full design: [`spec.md`](./spec.md). Key directories:

```
cmd/agentboard/        CLI entry point
internal/auth/         users, tokens, passwords, sessions, OAuth, middleware
internal/store/        files-first store envelope + CAS + history + activity
internal/server/       HTTP handlers, SSE broadcaster
internal/mcp/          MCP protocol server + tool definitions
internal/mdx/          Page management + frontmatter parser + file watcher + FTS5
internal/components/   Component catalog + file watcher
frontend/src/          React app + 32 built-in components
```

---

## Contributing

Contributions are welcome. See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for dev setup, test expectations, and how to open a good PR.

Before proposing a non-trivial change, read [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md) — the 12 product principles that shape what belongs in core vs. what should be a plugin, component, or external connector.

---

## License

AgentBoard is open source under the [MIT License](./LICENSE).
