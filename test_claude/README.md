# test_claude

Clean-room integration playground for AgentBoard. A sibling-folder inside the repo where we roleplay the end-to-end user journey — install, start, connect MCP, build a dashboard — using a fresh Claude Code session.

This is where we catch friction a real user would hit.

## Use it

```bash
cd test_claude
task install         # builds parent repo, copies binary to ./bin/agentboard
task start:bg        # starts the server on :3000, logs -> ./agentboard.log
task mcp:add         # claude mcp add agentboard http://localhost:3000/mcp
```

Start a **new** Claude Code session with cwd `test_claude/`, then paste a scenario from `prompts/` into it verbatim:

- `prompts/01-coffee-tracker.md` — minimal first dashboard (set + write_page)
- `prompts/02-sprint-board.md` — collections, kanban, table components
- `prompts/03-live-updates.md` — append + merge + SSE live updates

## Teardown

```bash
task stop
task reset           # wipe ./bin, ./agentboard-data, ./agentboard.log
```

## Layout

```
.
├── Taskfile.yml         # install / start / stop / mcp:add / reset / doctor
├── CLAUDE.md            # instructions loaded by the test Claude session
├── .claude/             # minimal permissions allowlist
├── prompts/             # copy-paste scenarios for the test session
├── bin/                 # installed binary (gitignored)
└── agentboard-data/     # project data — pages, components, sqlite (gitignored)
```

## Why

Dev loop (`task dev`, `task run` at repo root) assumes you're a contributor. This folder assumes you're a user who just ran `curl | bash` and wants a dashboard. The `install` task is the prototype for the eventual public installer at `agentboard.dev`.
