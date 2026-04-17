# AgentBoard Bruno Collection

Manual test collection for the AgentBoard server. Designed to be clicked through while watching the dashboard in a browser — every write request flips a visible component within ~100ms via SSE.

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
| `pages/` | Creates `/demo` page that mounts all 9 components against the seeded data | After `create-demo-page`, navigate to `http://localhost:3000/demo` |
| `reads/` | Read-only inspection (list keys, get value, JSON schema, list components) | Bruno response panel |
| `mcp/` | Exercises the JSON-RPC `/mcp` endpoint (the same surface Claude uses) | Bruno response panel |
| `component-upload/` | Uploads a custom JSX component, lists it, tests rejection paths, deletes it | **Requires `--allow-component-upload`** — start the server with that flag (or `allow_component_upload: true` in `agentboard.yaml`). Without it every request returns 403. |

## Running headless via CLI

```bash
npm install -g @usebruno/cli
bru run --env Local              # entire collection
bru run welcome-dashboard --env Local   # one folder
```

Every request has a `tests` block asserting `res.status === 200`, so `bru run` doubles as a smoke-test sweep.

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
