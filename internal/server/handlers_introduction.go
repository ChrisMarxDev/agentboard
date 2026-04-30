package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleIntroduction serves a thorough AgentBoard primer at
// GET /api/introduction.
//
// Goal: paste ONE URL to an agent and have it become productive
// immediately. No auth required. No instance-specific data. The
// response is a rich markdown walkthrough — concepts, mental model,
// common recipes, and concrete examples an agent can copy.
//
// Namespaced under /api so a user page at "/introduction" doesn't
// collide. /api/introduction is always open — registered outside the
// gated group so zero-user bootstrap also works.
//
// Content-negotiated:
//
//   - Accept: application/json → structured shape manifest (for tools)
//   - Default / Accept: text/markdown → the agent-readable primer
//
// The markdown response is the primary artifact. JSON is a tool-assist
// shape, not the main thing.
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
	// Default to markdown. Agents usually ask for markdown or pass no
	// Accept header, and a human hitting this in a browser gets readable
	// prose instead of a JSON wall.
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(introductionMarkdown()))
}

// introductionManifest returns the structured description agents
// consume when they hit the endpoint with Accept: application/json.
// Keep this in lockstep with the markdown variant.
func introductionManifest() map[string]any {
	return map[string]any{
		"product": "agentboard",
		"version": versionStr,
		"summary": "Single-binary knowledge + dashboard surface. Agents write via REST/MCP; humans browse a live web UI.",
		"primer":  "/api/introduction",
		"api": map[string]any{
			"base":        "/api",
			"auth":        "Bearer ab_<43> (header, ?token=, or HTTP Basic password)",
			"write_verbs": []string{"PUT", "PATCH", "POST", "DELETE"},
			"open_paths":  []string{"/api/health", "/api/config", "/api/introduction", "/api/setup/status"},
			"bootstrap":   "/api/setup/status returns {initialized:bool}. If false, the server prints a /invite/<id> URL on first boot — open it in a browser to claim the first admin.",
			"endpoints": []map[string]string{
				{"method": "GET", "path": "/api/health", "summary": "liveness probe"},
				{"method": "GET", "path": "/api/config", "summary": "project config + public paths"},
				{"method": "GET", "path": "/api/index", "summary": "flat catalog of every leaf in the project"},
				{"method": "GET", "path": "/api/{path}", "summary": "read a leaf — page envelope, singleton value, collection list, stream tail, or binary"},
				{"method": "PUT", "path": "/api/{path}", "summary": "write a leaf; body shape decides whether it lands as a page or a singleton; If-Match supported"},
				{"method": "PATCH", "path": "/api/{path}", "summary": "RFC-7396 merge into frontmatter and/or replace body; If-Match supported"},
				{"method": "POST", "path": "/api/{path}:append", "summary": "append one NDJSON line to a stream leaf"},
				{"method": "DELETE", "path": "/api/{path}", "summary": "delete a leaf"},
				{"method": "GET", "path": "/api/files", "summary": "list uploaded files"},
				{"method": "GET", "path": "/api/components", "summary": "list component catalog (schemas)"},
				{"method": "GET", "path": "/api/search?q=...", "summary": "substring + full-text search across every leaf"},
			},
		},
		"mcp": map[string]any{
			"transport": "streamable-http",
			"endpoint":  "/mcp",
			"protocol":  "jsonrpc 2.0",
			"methods":   []string{"initialize", "tools/list", "tools/call"},
		},
		"learn_more": map[string]string{
			"docs": "https://agentboard.org/docs",
			"repo": "https://github.com/christophermarx/agentboard",
		},
	}
}

// introductionMarkdown is the primary artifact: a self-contained
// agent primer. Reading this top-to-bottom should be enough for an
// agent to write pages, edit collections, and wire up live dashboards
// without a human walk-through.
func introductionMarkdown() string {
	return `# AgentBoard — agent primer

You are looking at the self-serve introduction to **AgentBoard**. This document is the single URL you can paste to any AI agent (or read yourself) to understand what this product is, how its surface is shaped, and exactly how to be productive inside it. There is nothing else to read first. You don't need a human walk-through.

Running AgentBoard version: ` + versionStr + `

---

## What AgentBoard is

AgentBoard is a **single-binary, self-hosted knowledge and dashboarding surface for teams that work with AI agents**. One Go binary, one SQLite file (auth + indexes only), and a tree of MDX files on disk.

The core pattern:

> **Agents write via REST/MCP. Humans read via live dashboards. Updates stream over SSE.**

What lives inside AgentBoard:

- **Pages** — ` + "`.md`" + ` files with YAML frontmatter and MDX body. The frontmatter holds structured fields (title, status, priority, anything); the body holds prose + first-party React components. A page can be a doc, a runbook, a kanban card, or a live dashboard — same shape.
- **Folders** — collections. A folder is its own collection: ` + "`content/tasks/<id>.md`" + ` is one card; ` + "`<Kanban groupBy=\"col\">`" + ` on ` + "`/tasks`" + ` reads the whole folder. There is no separate ` + "`data/`" + ` namespace.
- **Files** — uploaded binaries (images, PDFs, SVGs). Served from ` + "`/api/files/`" + `, embedded via ` + "`<Image>`" + ` / ` + "`<File>`" + `.
- **Components** — visual bricks agents drop into MDX. Run ` + "`GET /api/components`" + ` for the live catalog; built-ins include ` + "`Metric`, `Status`, `Progress`, `Kanban`, `Sheet`, `List`, `Chart`, `Log`, `Table`, `TimeSeries`, `Deck`, `Card`, `Stack`, `Markdown`, `Badge`, `Counter`, `Code`, `Mermaid`, `Image`, `File`, `Errors`, `ApiList`, `SkillInstall`, `Mention`, `RichText`, `TeamRoster`, `Inbox`, `Button`" + `.
- **Skills** — agent-consumable skill bundles at ` + "`content/skills/<slug>/SKILL.md`" + `. ` + "`GET /api/skills`" + ` lists them; ` + "`GET /api/skills/<slug>`" + ` returns a zip.

What AgentBoard is **not**: a database, a general-purpose CMS, a CI/CD tool, a chat product. It's a collaboration surface *between* agents and humans.

---

## The mental model — one tree, one URL namespace

Everything is a file under the project root, and every leaf is reachable at ` + "`/api/<path>`" + ` with the same verbs.

` + "```" + `
<project>/
  index.md                  # the home page
  content/
    handbook.md             # a regular page
    tasks.md                # a board (or omit; the folder itself is the index)
    tasks/
      ship-v2.md            # one card — frontmatter is the structured data, body is prose
      hire-engineer.md
    skills/
      kanban/
        SKILL.md            # the manifest (URL: /skills/kanban)
        examples.md         # supporting file (URL: /skills/kanban/examples)
` + "```" + `

A leaf takes one of these shapes; you don't pick a directory, you pick a body:

- **Page** — ` + "`PUT`" + ` markdown (` + "`text/markdown`" + ` or a body that starts with ` + "`---`" + ` / ` + "`#`" + `). Renders at ` + "`/<path>`" + ` in the browser.
- **Singleton** — ` + "`PUT`" + ` JSON ` + "`{\"value\": <anything>}`" + `. ` + "`<Metric source=\"<path>\" />`" + ` resolves to it.
- **Collection item** — same JSON envelope but at ` + "`<key>/<id>`" + `; the parent ` + "`<key>`" + ` becomes the collection.
- **Stream** — ` + "`POST /api/<path>:append`" + ` with one NDJSON line; reads tail the file. Used for activity feeds.
- **Binary** — ` + "`POST /api/files/request-upload`" + ` mints a presigned URL; you ` + "`PUT`" + ` the bytes there.

There is no separate ` + "`data/`" + ` namespace to write into. Path collisions are real — once a leaf exists at ` + "`metrics/dau`" + `, re-writes match its existing shape, and you can't have both a page and a singleton at the same path. Pick the path you want to *appear* at and write there directly.

**Rule of thumb**: if you'd want to navigate to it in the sidebar, write a page; if it's a number a component reads via ` + "`source=`" + `, write a singleton at the same path the component points at.

---

## Authentication

One token per user, format ` + "`ab_<43 chars>`" + `. Pass it in any of:

- Header: ` + "`Authorization: Bearer ab_...`" + `
- Basic: password=token, username ignored
- Query: ` + "`?token=ab_...`" + ` (last resort; logs the token to access logs)

Three user kinds: ` + "`admin`" + `, ` + "`member`" + `, ` + "`bot`" + `. Admins additionally unlock ` + "`/api/admin/*`" + `.

**If the board is unclaimed**, the server prints a ` + "`/invite/<id>`" + ` URL on first boot. Open it in a browser, pick a username, get the first admin token. ` + "`GET /api/setup/status`" + ` returns ` + "`{initialized: bool}`" + ` so you can detect this state programmatically.

If you see 401, ask your operator for a token. **Never** route around auth by editing files on disk — every write goes through the API, period.

---

## Core API — by example

All URLs are relative to this board's origin. Replace ` + "`$B`" + ` with the board URL and ` + "`$T`" + ` with your token.

### Read a page

` + "```bash" + `
curl -H "Authorization: Bearer $T" $B/api/handbook
# → { "path": "/handbook", "source": "# Handbook\\n…", "frontmatter": {...}, "etag": "...", ... }
#   NOTE: "source" is the body only; frontmatter travels in the "frontmatter" map.
` + "```" + `

### Write a page (full replace)

Body is raw MDX source — frontmatter (` + "`---`" + ` block) plus body. Content-Type: ` + "`text/markdown`" + `.

` + "```bash" + `
curl -X PUT -H "Authorization: Bearer $T" -H "Content-Type: text/markdown" \
  --data-binary @- $B/api/handbook <<'EOF'
---
title: Handbook
tags: [intro, onboarding]
---

# Handbook

Body prose lives here.
EOF
` + "```" + `

Optimistic concurrency:

` + "```bash" + `
curl -X PUT -H 'If-Match: "<etag>"' ...   # 200 on match, 412 with current etag on mismatch
` + "```" + `

### Patch a page (frontmatter merge / body replace)

` + "```bash" + `
# Move a kanban card without rewriting the doc.
curl -X PATCH -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  $B/api/tasks/ship-v2 \
  -d '{"frontmatter_patch": {"col": "done", "shipped": "2026-04-28"}}'

# Replace just the body, frontmatter preserved.
curl -X PATCH -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  $B/api/handbook \
  -d '{"body": "# Handbook\\n\\nFresh prose."}'

# Delete a frontmatter key (RFC-7396 null = remove).
curl -X PATCH -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  $B/api/tasks/ship-v2 \
  -d '{"frontmatter_patch": {"deprecated_field": null}}'
` + "```" + `

` + "`PATCH`" + ` honours ` + "`If-Match`" + ` the same way as PUT.

### Build a kanban board (the canonical folder pattern)

The board page IS the folder index. ` + "`<Kanban>`" + ` with no ` + "`source`" + ` auto-attaches.

` + "```mdx" + `
---
title: Intake
---

# Intake

<Kanban groupBy="col" columns={["todo","doing","done"]} />
` + "```" + `

Then PUT cards under the same folder:

` + "```bash" + `
curl -X PUT -H "Authorization: Bearer $T" -H "Content-Type: text/markdown" \
  --data-binary @- $B/api/intake/triage <<'EOF'
---
title: Triage support mailbox
col: todo
owner: alice
priority: 2
---

# Triage support mailbox
Tickets from the long weekend.
EOF
` + "```" + `

Move a card with one PATCH:

` + "```bash" + `
curl -X PATCH -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  $B/api/intake/triage -d '{"frontmatter_patch": {"col": "doing"}}'
` + "```" + `

` + "`<Sheet>`" + ` and ` + "`<List>`" + ` follow the same auto-attach rule. See the ` + "`kanban`" + ` skill at ` + "`GET /api/skills/kanban`" + ` for a full worked example.

### Set a data value

` + "```bash" + `
curl -X PUT $B/api/team.uptime -d '99.98' -H "Authorization: Bearer $T"

curl -X PATCH $B/api/team.status -H "Authorization: Bearer $T" \
  -d '{"detail":"one small blip"}'

curl -X POST $B/api/app.log -H "Authorization: Bearer $T" \
  -d '{"ts":"2026-04-28T12:00:00Z","level":"info","message":"deploy started"}'
` + "```" + `

Useful for log streams, counters, and ad-hoc state. **Use pages for anything that has structure or deserves a URL.**

### Discover what components exist on THIS board

` + "```bash" + `
curl -H "Authorization: Bearer $T" $B/api/components
# → [{ name: "Metric", meta: { description, props: {...} } }, ... ]
` + "```" + `

### Full-text search

` + "```bash" + `
curl -H "Authorization: Bearer $T" "$B/api/search/pages?q=login+timeout"
` + "```" + `

---

## MCP

Prefer MCP over REST if you are a Claude-family agent. JSON-RPC 2.0 over HTTP at ` + "`POST /mcp`" + `. Methods: ` + "`initialize`, `tools/list`, `tools/call`" + `. Tool names use the prefix ` + "`agentboard_*`" + ` — page operations (` + "`agentboard_read_page`, `agentboard_write_page`, `agentboard_list_pages`, `agentboard_delete_page`" + `), data operations (` + "`agentboard_read`, `agentboard_set`, `agentboard_merge`, `agentboard_append`, `agentboard_delete`" + `), discovery (` + "`agentboard_list_components`, `agentboard_list_skills`, `agentboard_get_skill`" + `), and more. Always run ` + "`tools/list`" + ` first — that's the source of truth for what this specific board advertises.

---

## Error envelope

Every error is JSON ` + "`{code, error}`" + ` with the appropriate HTTP status:

- **400** — bad request shape (` + "`INVALID_VALUE`, `INVALID_KEY`" + `).
- **401** — missing or invalid token (` + "`UNAUTHORIZED`" + `). Ask the operator.
- **403** — authenticated but per-user rules or admin gating denied (` + "`FORBIDDEN`, `ADMIN_REQUIRED`" + `).
- **404** — path doesn't exist (` + "`NOT_FOUND`, `ROUTE_NOT_FOUND`" + `). For pages, ` + "`PUT`" + ` to create.
- **409** — conflict (e.g. move destination already exists).
- **412** — ` + "`If-Match`" + ` etag mismatch (` + "`STALE_WRITE`" + `). Body includes ` + "`current`" + ` so you can re-base in one round-trip.
- **429** — rate-limited; ` + "`Retry-After`" + ` header tells you when to retry.

---

## Discovery endpoints (agents land here first)

- ` + "`GET /api/introduction`" + ` → *this document*
- ` + "`GET /api/config`" + ` → project title, theme, public paths
- ` + "`GET /api/components`" + ` → what JSX tags this board knows
- ` + "`GET /api/index`" + ` → flat catalog of every leaf (pages + data + streams)
- ` + "`GET /api/skills`" + ` → installable skill registry
- ` + "`GET /api/setup/status`" + ` → ` + "`{initialized}`" + ` flag for unclaimed-board detection
- ` + "`POST /mcp`" + ` with ` + "`{method:\"tools/list\"}`" + ` → MCP tool catalog

---

## Two invariants to memorise

1. **Writes always require auth.** The public-routes config can open *reads* on specific paths; it cannot open writes. Writing without a token is always 401.
2. **Never edit files on disk directly.** Even though the project is a tree of plain ` + "`.md`" + ` files, direct disk writes bypass auth, optimistic concurrency, the activity log, mention-dispatch, and ref-graph updates. Always go through the REST/MCP API.

---

## Next steps

- ` + "`GET /api/skills/kanban`" + ` if you've been asked to build a board
- ` + "`GET /api/skills/agentboard`" + ` for the broader skill that ships on every board
- ` + "`GET /api/index`" + ` to see what's already authored here
- ` + "`GET /api/components`" + ` to learn what JSX tags are valid

That's it. You know enough to start.
`
}
