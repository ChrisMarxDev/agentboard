# AgentBoard Bruno Collection

Two layers:

1. **Manual walkthrough** — top-level folders (`welcome-dashboard/`, `content/`, `component-demos/`, …) are designed to be clicked through in the Bruno UI while watching the dashboard in a browser. Every write flips a visible component within ~100 ms via SSE. Good for dogfooding and demos.

2. **Contract test suite** — `tests/` is a headless, assertion-driven regression suite covering every public surface (all 7 data ops + 2 error paths, content CRUD + protected index, components catalog, 13 MCP tools + 2 error paths, meta endpoints, CORS). 46 requests, ~90 assertions. Run with `task test:bruno`.

## Setup

```bash
# 1. Build and start the server (Terminal 1)
cd ..
task build
task run -- --no-open --port 3000

# 2. Open the dashboard in your browser
open http://localhost:3000

# 3. Open this collection in Bruno (https://www.usebruno.com/)
#    - File → Open Collection → select the `bruno/` folder
#    - Switch the environment dropdown (top-right) to "Local"
```

## Folder walkthrough

Folders are ordered via `seq` in each `folder.yml`. Run them in order — each builds on the previous.

| Folder | What it does | What to watch |
| --- | --- | --- |
| `reset/` | Restores `welcome.*` to seed values + clears `demo.*` keys (server stays up, dashboard SSE-flips back) | Run any time you want a clean slate |
| `Health/` | Sanity checks the server is up | Bruno response panel: `{"ok":true,...}` |
| `welcome-dashboard/` | Mutates the seeded `welcome.*` keys | The four components on `/` (Metric, Progress, Status, Kanban) — each animates as you fire requests |
| `all-seven-ops/` | Demonstrates every write operation against `demo.*` keys | Inspect with `reads/list-keys` between calls |
| `component-demos/` | Seeds data for the 5 components not on the welcome page (Chart, TimeSeries, Log, Table, List) | Nothing yet — these need a page to render on |
| `content/` | Creates `/demo` page that mounts all 9 components against the seeded data | After `create-demo-page`, navigate to `http://localhost:3000/demo` |
| `reads/` | Read-only coverage: list keys, get value, get-by-id, schema, components list, components.js bundle, `/skill` | Bruno response panel |
| `mcp/` | Exercises the JSON-RPC `/mcp` endpoint — initialize, tools/list, and a `tools/call` for every registered tool | Bruno response panel |
| `component-upload/` | Uploads a custom JSX component, lists it, tests rejection paths, deletes it | **Requires `--allow-component-upload`** — start the server with that flag (or `allow_component_upload: true` in `agentboard.yaml`). Without it every request returns 403. |
| `file-upload/` | Uploads an SVG banner, a PDF, and a CSV; lists + fetches them; wires them into `demo.files.*` data keys; creates `/files` page showing `<Image>` + `<File>`; deletes one file at the end | Run in order → open `http://localhost:3000/files` in the browser. The inline SVG banner renders and the PDF/CSV become download cards. Fire `8-delete-csv` to see the file-updated SSE remove the CSV live. |
| `errors/` | Negative paths: 404 on unknown key/id/content, 400 blocking index-page deletion | Bruno response panel — every request is expected to return a non-2xx status |
| `showcase/` | One-click visual regression: seeds rich data for **every** built-in (Metric, Counter, Status, Badge, Progress, Chart, TimeSeries, Table, List, Log, Kanban, Markdown, Code, Mermaid, Image, File, Deck, Card, Stack), uploads sample PNG/PDF/CSV files, and writes a `/showcase` page that renders all 19 in themed sections | Run the folder once → open `http://localhost:3000/showcase`. Re-fire `03 Pulse counter` to watch Counter flash. |
| `hosted/` | Exercises the token-gated public instance (Fly.io). Covers `Authorization: Bearer` and `?token=` auth paths, the 401 negative case, MCP tool list, and a full write/read/delete cycle. Uses the **Hosted** environment. See "Running against the hosted instance" below. (Basic Auth is proven by the Go unit test — Bruno's script API can't inject the header reliably.) | Bruno response panel |

> **SSE coverage note:** The `/api/events` stream is not in Bruno (Bruno blocks on the long-lived connection). It's covered by `scripts/integration-test.sh`.

## Running against the hosted instance

The `hosted/` folder points at the public Fly.io deploy (or any AgentBoard instance started with `AGENTBOARD_AUTH_TOKEN` set). The token never touches git — Bruno reads it from a local dotenv:

```bash
# One-time
cp bruno/.env.example bruno/.env
# edit bruno/.env and paste your token (bruno/.env is gitignored)
```

Then:

```bash
# GUI: switch env dropdown to "Hosted", run the hosted/ folder
# CLI:
cd bruno && bru run hosted --env Hosted
```

Requests reference `{{process.env.AGENTBOARD_AUTH_TOKEN}}`, which Bruno auto-loads from `bruno/.env` (GUI and CLI). To point at a different deployment, edit `baseUrl` in `environments/Hosted.yml`.

## Running headless via CLI

```bash
npm install -g @usebruno/cli

# Run the contract test suite (recommended — iterates every sub-folder in order,
# fails on first broken subfolder so you see the real failure, not an abort).
task test:bruno

# Or manually, one folder at a time:
bru run tests/01-data --env Local
bru run tests/04-mcp  --env Local

# Or a single manual-walkthrough folder:
bru run welcome-dashboard --env Local
```

### Test suite layout (`tests/`)

| Folder | What it covers |
| --- | --- |
| `00-setup/` | DELETEs any lingering `test.*` keys before the run |
| `01-data/` | All 7 data write ops + MERGE RFC-7396 semantics (deep, null-removes), 404, 400, schema, list |
| `02-content/` | Page CRUD, nested paths, `Accept: text/markdown` returning raw source, index-delete 400 |
| `03-components/` | Catalog includes all 17 built-ins with correct `type` + `meta.props`, bundle serves as JS, upload-disabled 403 |
| `04-mcp/` | Initialize, `tools/list` returns all 16 core tools, a `tools/call` per tool, unknown-tool + unknown-method JSON-RPC errors |
| `05-meta/` | Health, config shape, CORS preflight, skill file |
| `06-files/` | Upload PNG, fetch back, ETag 304 round-trip, nested path (`exports/…`), path-traversal reject, dotfile reject, oversize (55 MB → 413), delete lifecycle |
| `99-teardown/` | Cleans up `test.*` keys after the run |

Every request asserts more than just status — response bodies are inspected (array shapes, field presence, value round-trips), side effects are verified (write → read back), and error codes on the body are checked.

## Resetting between runs

**Soft reset (recommended)** — server stays up, welcome dashboard flips back to its seed state via SSE:

In Bruno: right-click the `reset/` folder → Run Folder.
From CLI:

```bash
bru run reset --env Local
```

This restores `welcome.users`, `welcome.progress`, `welcome.status`, `welcome.tasks` to their original values and DELETEs every `demo.*` key the rest of the collection created.

**Nuclear reset** — wipe the SQLite db and re-init from scratch (kills server first):

```bash
rm -rf ~/.agentboard/default
task run -- --no-open --port 3000
```
