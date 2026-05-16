# AgentBoard

**Single-binary git server** that hosts the shared workspace humans and AI agents collaborate inside. Agents clone, branch, commit, push — git's standard concurrency model handles parallel work, and conflicts surface as standard merge markers that Claude-class agents already know how to resolve. Humans read a live web dashboard the server renders directly from the working tree.

> Evidence.dev for normal people, where AI agents are the authors and the workspace is the source of truth.

---

## What it is

- **Single binary, zero dependencies.** No Node, Python, Docker, or system services to run it. Download, execute, done.
- **Self-host anywhere.** Your laptop, a Raspberry Pi, your own VPS — €3/mo Hetzner is fine. AgentBoard is the software; the deployment is yours.
- **Git is the substrate.** Every change is a commit. Every commit is reversible. Workspaces are real git repos with smart-HTTP push and pull. No proprietary store.
- **Files are the API.** HTML renders as expressive pages (full layout control, no build step). Markdown renders through goldmark. JSON with `columns` + `cards` renders as a kanban. SVG / PNG / PDF / CSV render with the right content-type or a preview affordance. Whatever the team can author is what the dashboard shows.
- **AI-first authoring.** Six MCP tools (`agentboard_workspaces`, `_pull`, `_propose`, `_resolve_conflict`, `_subscribe`, `_fire_event`) for agents whose runtime can't shell out to git, plus the standard git endpoint for agents that can.
- **Human-first reading.** The rendered dashboard is a polished, readable document with a Finder-style sidebar, in-browser edit form, page history, and full-text search.
- **Realtime by default.** Pushes broadcast over SSE; the dashboard pops a Reload toast when a page you're looking at changes.

Read the product principles in [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md), the design contract in [`spec.md`](./spec.md), and the auth design in [`AUTH.md`](./AUTH.md).

---

## Quickstart

```bash
agentboard                         # boots on http://localhost:3000
agentboard --project ./my-board    # use a specific project folder
agentboard --port 8080 --no-open   # custom port, don't pop a browser
```

First run creates a `.agentboard/` folder, initializes the `default` workspace as a git repo with a starter `index.html`, and prints a `/invite/<id>` URL to stdout (also written to `<project>/.agentboard/first-admin-invite.url`). Open that URL in a browser, pick a username + password, and you're the first admin.

From there:

```bash
TOKEN=ab_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Clone the workspace and start working
git clone http://_:$TOKEN@localhost:3000/git/default.git
cd default
echo '# Hello' > pages/hello.md
git add . && git commit -m "first page" && git push

# Reload http://localhost:3000/pages/hello.md — your page is there.

# Or sign in as a human via the dashboard
curl -c jar.txt -X POST -H 'Content-Type: application/json' \
  http://localhost:3000/_api/auth/login \
  -d '{"username":"alice","password":"…"}'
curl -b jar.txt http://localhost:3000/_api/auth/me

# Or talk to MCP if your runtime can't shell out to git
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  http://localhost:3000/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agentboard_workspaces","arguments":{}}}'
```

---

## Build from source

**Requirements:** Go 1.25+, [Task](https://taskfile.dev/) (`brew install go-task`).

```bash
git clone https://github.com/christophermarx/agentboard.git
cd agentboard

task build          # compiles the Go binary → ./agentboard
./agentboard        # run it
```

Common tasks:

```bash
task                 # list every task
task build           # compile (embeds internal/html/assets)
task dev             # run with --dev so templates reload from disk
task run             # run the built binary
task test            # run Go tests
task test:dogfood    # boot ephemeral instance + run an AI agent through the bootstrap prompt
task lint            # gofmt check + go vet
task fmt             # apply gofmt
task clean           # remove build artifacts
```

The binary is fully static (CGO disabled, pure-Go SQLite via `modernc.org/sqlite`) — copy it to any Linux / macOS / Windows machine of the same architecture and run it.

### Cross-compiling

```bash
GOOS=linux   GOARCH=amd64 task build     # Linux x86_64
GOOS=linux   GOARCH=arm64 task build     # Linux ARM (e.g. Raspberry Pi 4/5)
GOOS=darwin  GOARCH=arm64 task build     # Apple Silicon
GOOS=windows GOARCH=amd64 task build     # Windows x86_64
```

---

## Self-hosting

The binary is designed to run anywhere — see [`HOSTING.md`](./HOSTING.md) for supported deployment paths, cost breakdowns, and first-time setup. The current production reference is a Hetzner CAX11 running [Coolify](https://coolify.io) so multiple boards share one box for ~€3/mo.

- **Hetzner / DigitalOcean / your homelab.** `scripts/deploy-vps.sh` does a one-shot install behind Caddy with Let's Encrypt. ~€3–4/mo.
- **Multi-tenant on one VPS.** Install Coolify, then `scripts/new-board.sh` provisions per-friend boards with isolated containers + volumes.
- **Render / Railway / Koyeb.** Point at the Dockerfile; they'll build + run it.
- **Raspberry Pi.** Cross-compile for `linux/arm64`, `scp` the binary, run it.

Auth has two credential paths, both per-user (no shared admin token):

- **Bearer tokens** (`ab_…`, plus `oat_…` audience-scoped tokens minted via OAuth 2.1 + DCR for browser-driven MCP clients) — used by agents, CLI, MCP, and git smart-HTTP.
- **Browser sessions** (`agentboard_session` HttpOnly cookie + `agentboard_csrf` companion, double-submit CSRF) — used by humans.

Full design in [`AUTH.md`](./AUTH.md). Trust-boundary deferrals in [`seams_to_watch.md`](./seams_to_watch.md).

---

## Project goals & non-goals

**Goals**

- A self-hostable single binary for shared human/agent workspaces that users own end-to-end.
- Git as the only substrate. No bespoke storage layer, no custom protocol.
- A minimal MCP surface (six tools) for agents whose runtime can't shell out to git, plus full git smart-HTTP for the ones that can.
- Composability through files: pages, boards, decks, briefs are artifacts in the repo, not configuration in a database.

**Non-goals (today)**

- We do **not** operate a hosted AgentBoard service. You deploy your own.
- No multi-tenant accounts, no billing. The trust boundary is "you control the machine and your credentials."
- No SQL query panels, no admin UI for a data plane. There is no data plane. Files are it.

A managed cloud service is a possible future direction but explicitly undecided. It will not compromise the self-host-first design.

---

## Architecture (one paragraph)

A Go binary mounts a chi router. Three layers above it: an HTTP gateway (`/_api/*` for REST + auth, `/git/<workspace>.git` for smart-HTTP git, `/mcp` for the agent tool surface), an HTML renderer (`internal/html/`) that serves the workspace's working tree as a browsable dashboard, and `internal/gitserver/` which manages the bare repos + worktrees on disk. Pure-Go SQLite (modernc.org/sqlite) holds auth/sessions/invitations only — content lives in git. Pushes fire the post-receive event bus; subscribers re-render and the dashboard sends a Reload toast over SSE. Six MCP tools cover the agent surface for runtimes without shell access. The binary is fully static — no Node, no React, no MDX compiler, no separate frontend build.

Full design: [`spec.md`](./spec.md). Key directories:

```
cmd/agentboard/        CLI entry point
internal/auth/         users, tokens, passwords, sessions, OAuth, middleware
internal/gitserver/    bare-repo + worktree manager, smart-HTTP, post-receive hub
internal/html/         server-rendered HTML dashboard + embedded design-system
internal/server/       /_api/* handlers, admin UI, SSE broadcaster
internal/mcp/          JSON-RPC protocol + six agentboard_* tools
internal/search/       SQLite FTS5 index over the worktree
internal/cli/          Cobra commands (serve, admin, deploy, ...)
```

---

## Contributing

Contributions are welcome. See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for dev setup, test expectations, and how to open a good PR.

Before proposing a non-trivial change, read [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md) — the 15 product principles that shape what belongs in core vs. what should be a workspace convention or an external connector.

---

## License

AgentBoard is open source under the [MIT License](./LICENSE).
