---
name: agentboard
description: Run and manage the local AgentBoard dashboard server. Use when the user asks to start, open, or stop AgentBoard, check if their dashboard is running, author an AgentBoard page, or demo the product. Triggers — "start agentboard", "open my dashboard", "set me up with agentboard", "is agentboard running", "stop agentboard", "show me my agentboard", "add an agentboard page".
---

# AgentBoard

You run and manage a local AgentBoard server for the user. AgentBoard is a single Go binary that exposes a live dashboard at `http://localhost:<port>` and an MCP server that lets you write data, author pages, and manage components.

## What is already installed

The AgentBoard installer has already run and placed everything on disk:

| Path | Purpose |
|---|---|
| `~/.agentboard/bin/agentboard` | the binary |
| `~/.agentboard/data/` | project data, MDX pages, SQLite store |
| `~/.agentboard/server.pid` | PID of the running server (if any) |
| `~/.agentboard/server.log` | server stdout+stderr |

**Always use the full path** `~/.agentboard/bin/agentboard` rather than `agentboard` — the binary may not be on the user's PATH.

## When to use which tool

- **MCP tools** (`agentboard:set`, `agentboard:merge`, `agentboard:write_page`, etc.) — prefer these for data writes and page authoring once the server is running. They are strongly typed and safer.
- **Bash via the binary** — for starting, stopping, and inspecting the server process itself (MCP can't do that — it *is* the server).

If MCP tools don't appear to be available, the server is not running or Claude Desktop hasn't been restarted since install. Start the server first, then ask the user to restart Claude Desktop if they're on Desktop.

---

## Starting the server

Use this exact sequence when the user says anything like "start AgentBoard" / "open my dashboard" / "set me up":

1. **Verify the binary exists.**
   ```bash
   ~/.agentboard/bin/agentboard --version
   ```
   If this fails, the installer didn't run (or was partial). Tell the user:
   > AgentBoard isn't installed yet. Run this in a terminal:
   > `curl -fsSL https://agentboard.dev/install.sh | sh`

2. **Check whether the server is already running.**
   ```bash
   curl -fsS http://localhost:3000/api/health 2>/dev/null
   ```
   If this returns 200, skip to step 5 (open the browser).

3. **Start the server in the background.**
   ```bash
   nohup ~/.agentboard/bin/agentboard serve \
     --project default \
     --port 3000 \
     --no-open \
     > ~/.agentboard/server.log 2>&1 &
   echo $! > ~/.agentboard/server.pid
   ```

4. **Wait for the server to respond.** Poll health up to 10 times at 0.5s intervals:
   ```bash
   for i in 1 2 3 4 5 6 7 8 9 10; do
     curl -fsS http://localhost:3000/api/health && break
     sleep 0.5
   done
   ```
   If it never responds, read `~/.agentboard/server.log` and surface the last 20 lines to the user.

5. **Open the dashboard in the browser.**
   - macOS: `open http://localhost:3000`
   - Linux: `xdg-open http://localhost:3000 2>/dev/null || true`
   - If neither works, print the URL for the user to click.

6. **Report success** in one sentence: "AgentBoard is live at http://localhost:3000 — I'll update your dashboards as we go."

---

## Stopping the server

When the user says "stop AgentBoard" / "shut it down":

1. If `~/.agentboard/server.pid` exists, `kill "$(cat ~/.agentboard/server.pid)"` and remove the file.
2. If that fails, `pkill -f '^.*/\.agentboard/bin/agentboard serve'`.
3. Confirm the port is free: `curl -fsS http://localhost:3000/api/health` should fail.
4. Say: "Stopped."

Don't remove `~/.agentboard/data/` unless the user explicitly says "delete my data" or "reset".

---

## First-time welcome experience

If the server has just started AND there is no `welcome.mdx` page yet, optionally seed a welcome demo:

1. Use the MCP tool `agentboard:set` to initialize:
   - `welcome.events` = `0`
   - `welcome.log` = `[]`

2. Use `agentboard:write_page` to create `welcome.mdx` with:
   ```mdx
   # Welcome to AgentBoard

   You're live. This page updates in real time as agents write to your data.

   <Metric path="welcome.events" label="Events" />

   <Log path="welcome.log" />
   ```

3. Open `http://localhost:3000/welcome` in the browser.

4. Demonstrate realtime: call `agentboard:append` on `welcome.log` with `"AgentBoard started"`, and tell the user their dashboard just updated live.

Do this only once. If the page already exists, don't overwrite it — the user may have edited it.

---

## Adding a page

When the user says "add a page for X" / "make a dashboard for Y":

1. Ask them 1–2 questions if critical data is missing (what data should it show? what's the page for?).
2. Use `agentboard:list_components` to see what's available (Metric, Chart, TimeSeries, Table, Kanban, List, Log, Status, Progress — but confirm via the tool).
3. Use `agentboard:write_page` with a thoughtful MDX composition. Bind each component to a reasonable data path.
4. Seed any data the page needs with `agentboard:set` or `agentboard:merge` so the page isn't empty on first view.
5. Open `http://localhost:3000/<slug>` in the browser.

---

## Checking status

When the user asks "is agentboard running?" / "what's on my dashboard?":

1. `curl -fsS http://localhost:3000/api/health` — reports up/down.
2. If up, use `agentboard:list_pages` and `agentboard:list_keys` to summarize what's live.
3. Report in one paragraph, not a wall of text.

---

## Error handling

| Symptom | Response |
|---|---|
| Binary not found | Point user at `curl \| sh` installer; do not try to install it yourself. |
| Port 3000 in use by something else | Offer to start on 3001, 3002, ... (update the URL everywhere you report it). |
| Server starts but `/api/health` never 200s | Surface `~/.agentboard/server.log` tail to the user. |
| MCP tools unavailable | Server isn't running, or (on Claude Desktop) user needs to restart the Desktop app. |

Never silently swallow errors. Always tell the user what went wrong and what you tried.

---

## What this skill does **not** do

- **Does not install the binary.** That's the installer's job (`install.sh`). If the binary is missing, stop and point the user at the installer URL.
- **Does not modify the user's code or dotfiles** beyond the paths listed at the top.
- **Does not delete data** unless explicitly asked with words like "reset" or "delete my data".
- **Does not run remote commands** — everything happens on `localhost`.
