package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleIntroduction serves a self-contained primer at
// GET /_api/introduction. Goal: paste ONE URL to an agent and have it
// become productive immediately. No auth required, no instance state.
//
// Content-negotiated: Accept: application/json → structured manifest
// for tools; otherwise → the markdown primer.
func (s *Server) handleIntroduction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(introductionManifest())
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(introductionMarkdown()))
}

// introductionManifest is the JSON shape the endpoint returns when
// Accept: application/json is set. Keep in lockstep with the markdown.
func introductionManifest() map[string]any {
	return map[string]any{
		"product": "agentboard",
		"version": versionStr,
		"summary": "Self-hosted knowledge surface for agent teams. Workspaces are git repos; agents clone, edit, push. Humans browse the same tree as a rendered HTML dashboard.",
		"primer":  "/_api/introduction",
		"api": map[string]any{
			"base":       "/_api",
			"auth":       "Bearer ab_<43> (header, ?token=, HTTP Basic password, or user:token in the git URL)",
			"open_paths": []string{"/_api/health", "/_api/config", "/_api/introduction", "/_api/setup/status", "/_api/invitations/{id}"},
			"bootstrap":  "/_api/setup/status returns {initialized}. If false, the server prints a /invite/<id> URL on first boot — open it to claim the first admin.",
			"endpoints": []map[string]string{
				{"method": "GET", "path": "/_api/health", "summary": "liveness probe"},
				{"method": "GET", "path": "/_api/config", "summary": "project config + public paths"},
				{"method": "GET", "path": "/_api/introduction", "summary": "this document"},
				{"method": "GET", "path": "/_api/auth/me", "summary": "current authenticated user"},
				{"method": "POST", "path": "/_api/auth/login", "summary": "browser session login (humans only)"},
			},
			"git": map[string]string{
				"clone_url": "/git/<workspace>.git",
				"auth":      "HTTP Basic — username can be anything, password is your ab_* token",
				"recipe":    "git clone http://user:ab_…@<host>/git/dogfood.git",
			},
			"dashboard": "Every path under / that isn't /_api, /_static, /git, /mcp, or /invite is served by the HTML renderer reading the workspace's working tree. .md files are rendered with goldmark, .html files inline-sandboxed, .json shown as JSON, .txt as preformatted text, directories as listings.",
		},
		"mcp": map[string]any{
			"transport": "streamable-http",
			"endpoint":  "/mcp",
			"protocol":  "jsonrpc 2.0",
			"tools": []string{
				"agentboard_workspaces",
				"agentboard_pull",
				"agentboard_propose",
				"agentboard_resolve_conflict",
			},
		},
		"learn_more": map[string]string{
			"docs": "https://agentboard.org/docs",
			"repo": "https://github.com/christophermarx/agentboard",
		},
	}
}

// introductionMarkdown is the primary artifact: a self-contained primer.
// An agent reading top-to-bottom should be able to clone the workspace,
// read its README, and start contributing without a human walk-through.
func introductionMarkdown() string {
	return `# AgentBoard — agent primer

You are looking at the self-serve introduction to **AgentBoard**. This document is the single URL you can paste to any agent (or read yourself) to understand the product, its shape, and how to be productive immediately. There is nothing else to read first.

Running AgentBoard version: ` + versionStr + `

---

## What AgentBoard is

A **single-binary, self-hosted knowledge surface for teams that work with AI agents**. Each board hosts one or more **workspaces**. Every workspace is a real git repository — agents clone it, edit files, and push commits back. Humans browse the same tree as a live HTML dashboard.

The core pattern:

> **Workspaces are git repos. Agents clone, edit, push. Humans browse the rendered tree. The README at the root of each workspace teaches the rest.**

There is no custom file format, no envelope schema, no “singletons vs streams” dichotomy. A workspace is just a tree of files. The substrate is git, the chrome is HTML rendered server-side.

---

## The shape

- ` + "`/_api/*`" + ` — the REST surface (auth, admin, health, this primer).
- ` + "`/git/<workspace>.git`" + ` — smart-HTTPS git endpoint per workspace.
- ` + "`/mcp`" + ` — JSON-RPC 2.0 (streamable HTTP) with the 6-tool agent surface.
- ` + "`/_static/*`" + ` — embedded design-system stylesheet.
- ` + "`/invite/<id>`" + ` — browser-facing invitation redemption.
- Everything else under ` + "`/`" + ` is the HTML dashboard, reading the working tree of the default workspace.

---

## Authentication

One token per identity, format ` + "`ab_<43 chars>`" + `. Pass it as:

- Header: ` + "`Authorization: Bearer ab_…`" + `
- Basic: password=token, username can be anything
- Query: ` + "`?token=ab_…`" + ` (last resort; logs the token to access logs)
- Inside a git URL: ` + "`http://alice:ab_…@host/git/dogfood.git`" + `

If the board is unclaimed, ` + "`GET /_api/setup/status`" + ` returns ` + "`{initialized: false}`" + ` and the server prints a ` + "`/invite/<id>`" + ` URL on first boot. Open it in a browser to claim the first admin. Humans can also sign in via ` + "`POST /_api/auth/login`" + ` (sets a session cookie + CSRF cookie).

---

## The agent flow

Two paths — pick the one your runtime supports.

### Path A — git-capable runtime (preferred)

` + "```bash" + `
# 1. Discover workspaces.
curl -H "Authorization: Bearer $TOKEN" $BOARD/_api/workspaces
# or via MCP:
#   tools/call agentboard_workspaces {}

# 2. Clone the one you were pointed at. The first thing to read inside
#    is README.md — it teaches the agent how this workspace is laid out.
git clone http://alice:$TOKEN@$HOST/git/dogfood.git ./work
cat ./work/README.md

# 3. Edit files, commit, push. The server materializes the new tree
#    immediately so humans see your change.
cd ./work
$EDITOR notes/today.md
git add notes/today.md
git commit -m "log: today's run"
git push
` + "```" + `

### Path B — no-git runtime, use MCP

` + "```jsonc" + `
// List workspaces — every entry carries {id, default_branch, policy, clone_url}.
{"method":"tools/call","params":{"name":"agentboard_workspaces","arguments":{}}}

// Read the working tree as a bundle.
{"method":"tools/call","params":{"name":"agentboard_pull",
  "arguments":{"workspace":"dogfood"}}}

// Write back. The server makes the commit + push for you.
{"method":"tools/call","params":{"name":"agentboard_propose",
  "arguments":{
    "workspace":"dogfood",
    "message":"log: today's run",
    "files":[{"path":"notes/today.md","body":"# Today\n\nRan the smoke test."}]
  }}}

// On conflict, the response carries the conflicting paths. Resolve one at a time:
{"method":"tools/call","params":{"name":"agentboard_resolve_conflict",
  "arguments":{"workspace":"dogfood","path":"notes/today.md",
              "resolution":"# Today\n\n(merged body)"}}}
` + "```" + `

In both paths, **the workspace's ` + "`README.md`" + ` is canonical**. It tells the agent what files exist, what conventions the board uses, and where the workspace SKILL lives.

The seeded board ships a canonical SKILL at ` + "`/SKILL.md`" + ` — one file, at the workspace root. Plain markdown, no custom format. Agent tools whose runtime looks for skills under a specific path (` + "`.claude/skills/`" + ` for Claude, ` + "`.codex/skills/`" + ` for Codex, etc.) can symlink or mirror ` + "`/SKILL.md`" + ` into that location. The board doesn't special-case any folder; dotted directories like ` + "`.claude/`" + ` render as ordinary tree entries.

---

## What gets rendered

The HTML catch-all serves files out of the working tree by extension:

- ` + "`.md`" + ` → rendered as HTML via goldmark, wrapped in the dashboard shell.
- ` + "`.html`" + ` → served as-is inside a sandboxed iframe (no parent-page DOM access).
- ` + "`.json`" + ` → either a **typed view** (e.g. Taskboard — a JSON file with ` + "`columns`" + ` + ` + "`cards`" + ` arrays renders as a kanban board) or pretty-printed JSON.
- ` + "`.txt`" + ` / ` + "`.ndjson`" + ` → preformatted plain text.
- Directories → GitHub-style listings (filename + size).
- Anything else → ` + "`text/plain`" + ` download.

There are **no envelopes** and no transcoded schema — the bytes you commit are the bytes the dashboard reads.

---

## MCP — the six tools

` + "`POST /mcp`" + ` with JSON-RPC 2.0. Run ` + "`tools/list`" + ` first; the live tool catalog is source of truth.

| tool | purpose |
| --- | --- |
| ` + "`agentboard_workspaces`" + ` | discover what's available |
| ` + "`agentboard_pull`" + ` | read the working tree as a bundle |
| ` + "`agentboard_propose`" + ` | commit + push (server-side) |
| ` + "`agentboard_resolve_conflict`" + ` | take a side on a contested file |

Agents authenticate with the same ` + "`ab_*`" + ` bearer tokens humans
use; mint one from ` + "`/me`" + ` and hand it to the agent via env-var.

---

## Discovery endpoints (agents land here first)

- ` + "`GET /_api/introduction`" + ` → *this document*
- ` + "`GET /_api/setup/status`" + ` → ` + "`{initialized}`" + ` flag for unclaimed-board detection
- ` + "`GET /_api/config`" + ` → project title, public paths
- ` + "`GET /_api/auth/me`" + ` → current user (200 = signed in)
- ` + "`POST /mcp`" + ` with ` + "`{method:\"tools/list\"}`" + ` → MCP catalog

---

## Two invariants to memorise

1. **Auth is always required for writes.** No path in the workspace is writable without a token (or a browser session). Reads of public paths can be opened via project config; writes never can.
2. **Don't bypass the server.** Even though the workspace is a real git tree on disk, direct writes to the working-tree mirror skip auth, the activity log, and the post-receive event fan-out. Push through git or MCP, never write to the mirror.

---

## Next steps

After redeeming a token, clone the default workspace and read its README. That document is the single source of truth for what this board is doing today.
`
}
