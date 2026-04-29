# AgentBoard Bruno Collection

Headless contract test suite for AgentBoard. Every public surface
that an agent or browser hits is asserted here against a running
server. Run with `task test:bruno` (or per-folder with `bru run`).

The legacy "manual walkthrough" demo collection (welcome-dashboard,
all-seven-ops, showcase, etc.) was removed during the files-first
cleanup — it was tied to the SQLite KV demo data that no longer
exists. The dogfood instance at `~/.agentboard/agentboard-dev/`
plays the same role for live demos now.

## Setup

```bash
# 1. Build + boot a server with a known auth token
cd ..
task build
AGENTBOARD_AUTH_TOKEN=test-token ./agentboard --no-open --port 3000

# 2. Open the collection in Bruno (https://www.usebruno.com/)
#    File → Open Collection → select the `bruno/` folder
#    Switch the environment dropdown (top-right) to "Local"
```

Authentication: every request inherits a `Authorization: Bearer
{{process.env.AGENTBOARD_AUTH_TOKEN}}` header from the env file.
Copy `.env.example` to `.env` and paste your token (the file is
gitignored).

## Suite layout (`tests/`)

| Folder | What it covers |
| --- | --- |
| `00-setup/` | DELETEs any lingering `test.*` keys before the run. |
| `02-content/` | Page CRUD, nested paths, `Accept: text/markdown` returning raw source, index-delete 400. |
| `03-components/` | Catalog includes every built-in with correct `type` + `meta.props`, bundle serves as JS, upload-disabled 403. |
| `04-mcp/` | `initialize`, `tools/list` returns the live tool set, a `tools/call` per core tool, unknown-tool + unknown-method JSON-RPC errors. |
| `05-meta/` | Health, config shape, CORS preflight, skill file. |
| `06-files/` | Upload PNG, fetch back, ETag 304 round-trip, nested path, path-traversal reject, dotfile reject, oversize (55 MB → 413), delete lifecycle. |
| `07-errors/` | Render-error beacons (component reports a render exception, server records it, list/clear). |
| `08-grab/` | Materializer end-to-end: pick a page + a data key, prove the merged text comes back. |
| `09-skills/` | Skill catalog + per-slug fetch. |
| `10-data/` | Files-first store: singleton CAS + 412 conflict, deep-merge PATCH, collection upsert + list, stream append + read, wrong-shape 409, history + activity. |
| `99-teardown/` | Cleans up `test.*` keys after the run. |

Every request asserts more than just status: response bodies are
inspected (array shapes, field presence, value round-trips), side
effects are verified (write → read back), and error codes on the
body are checked.

## Running headless via CLI

```bash
npm install -g @usebruno/cli

# Whole suite (iterates every sub-folder in order, fails on first
# broken subfolder so you see the real failure, not an abort).
task test:bruno

# Or one folder at a time:
bru run tests/04-mcp  --env Local
bru run tests/10-data --env Local
```

> **SSE coverage note:** The `/api/events` stream is not in Bruno
> (Bruno blocks on the long-lived connection). It's covered by
> `scripts/integration-test.sh`.

## Running against a remote instance

The `Hosted` env in `environments/Hosted.yml` points at a public
AgentBoard instance — today the Hetzner/Coolify deploy at
`agentboard.hextorical.com`. The token never touches git:

```bash
cp .env.example .env
# edit .env, paste your token. .env is gitignored.

bru run tests --env Hosted
```

To point at a different deployment, edit `baseUrl` in
`environments/Hosted.yml`.
