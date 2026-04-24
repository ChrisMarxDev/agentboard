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
			"base":         "/api",
			"auth":         "Bearer ab_<43> (header, ?token=, or HTTP Basic password)",
			"write_verbs":  []string{"PUT", "PATCH", "POST", "DELETE"},
			"open_paths":   []string{"/api/health", "/api/config", "/api/introduction"},
			"bootstrap":    "/api/setup/status returns {claimed:bool}; if false, POST /api/setup claims the board.",
			"endpoints": []map[string]string{
				{"method": "GET", "path": "/api/health", "summary": "liveness probe"},
				{"method": "GET", "path": "/api/config", "summary": "project config + public paths"},
				{"method": "GET", "path": "/api/content", "summary": "list all pages"},
				{"method": "GET", "path": "/api/content/{path}", "summary": "read one page (markdown+etag)"},
				{"method": "PUT", "path": "/api/content/{path}", "summary": "write a page (MDX source); If-Match supported"},
				{"method": "GET", "path": "/api/data", "summary": "list data keys"},
				{"method": "GET", "path": "/api/data/{key}", "summary": "read a value"},
				{"method": "PUT", "path": "/api/data/{key}", "summary": "set a value"},
				{"method": "PATCH", "path": "/api/data/{key}", "summary": "deep-merge into a value"},
				{"method": "POST", "path": "/api/data/{key}", "summary": "append to an array value"},
				{"method": "DELETE", "path": "/api/data/{key}", "summary": "delete a key"},
				{"method": "GET", "path": "/api/files", "summary": "list uploaded files"},
				{"method": "GET", "path": "/api/components", "summary": "list component catalog (schemas)"},
				{"method": "GET", "path": "/api/search?q=...", "summary": "full-text search over pages"},
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
// agent to write pages, set data, and wire up live dashboards without
// a human walk-through.
func introductionMarkdown() string {
	return `# AgentBoard — agent primer

You are looking at the self-serve introduction to **AgentBoard**. This document is the single URL you can paste to any AI agent (or read yourself) to understand what this product is, how its surface is shaped, and exactly how to be productive inside it. There is nothing else to read first. You don't need a human walk-through.

Running AgentBoard version: ` + versionStr + `

---

## What AgentBoard is

AgentBoard is a **single-binary, self-hosted knowledge and dashboarding surface for teams that work with AI agents**. One Go binary, one SQLite file, one web UI.

The core pattern is:

> **Agents write via REST/MCP. Humans read via live dashboards. Updates stream in real time over SSE.**

What lives inside AgentBoard:

- **Pages** — MDX documents. Any mix of Markdown + first-party React components. The normal stuff a team keeps — docs, runbooks, project briefs, meeting notes — but also **live dashboards** because any component can bind to a data key and update as data changes.
- **Data** — a key/value store with dotted-path keys. Seven write operations (set, merge, append, upsert-by-id, merge-by-id, delete, delete-by-id). Every write broadcasts to every connected browser via SSE.
- **Files** — uploaded artifacts (images, PDFs, SVGs, zips). Referenced by pages via ` + "`<Image>`" + ` / ` + "`<File>`" + `.
- **Components** — the vocabulary of visual bricks agents can drop into pages. Built-ins: ` + "`Metric`, `Status`, `Progress`, `Kanban`, `Chart`, `Log`, `List`, `Table`, `TimeSeries`, `Deck`, `Card`, `Markdown`, `Badge`, `Counter`, `Code`, `Mermaid`, `Image`, `File`, `Errors`, `ApiList`, `SkillInstall`, `Mention`, `RichText`" + `.
- **Skills** — agent-consumable skill bundles hosted under ` + "`skills/<slug>`" + `. Every skill page auto-gets an install card.

What AgentBoard is **not**: a database replacement, a general-purpose CMS, a CI/CD tool, or a chat product. It's a collaboration surface *between* agents and humans.

---

## The mental model

Three things to keep separate in your head.

1. **Pages are content.** They're MDX. When you edit a page, the content of a paragraph changes. You don't edit a page to change a live metric.
2. **Data is signal.** It lives in the key/value store. You edit data to change what a dashboard component renders.
3. **Rendering is one-way.** A page mounts a ` + "`<Metric source=\"team.uptime\"/>`" + ` and the browser subscribes to that data key. Every write to ` + "`team.uptime`" + ` is reflected in every open browser within ~100ms.

This separation means you can edit a page once and then drive the dashboard by just setting data — no page re-edit, no redeploy. Your rule of thumb: **if the number changes hourly, it's data; if it changes weekly, it's prose.**

---

## Authentication

One token per user, one format: ` + "`ab_<43 chars>`" + `. Passed in any of:

- Header: ` + "`Authorization: Bearer ab_...`" + `
- Basic: password=token, username ignored
- Query: ` + "`?token=ab_...`" + `

Two kinds of tokens:

- **agent** — scoped by per-user rules
- **admin** — additionally unlocks ` + "`/api/admin/*`" + `

If you see 401, ask your operator for a token. If the board is **unclaimed** (no users yet), ` + "`POST /api/setup`" + ` with ` + "`{username, display_name?}`" + ` mints the first admin + returns a token.

---

## Core API — by example

All URLs are relative to this board's origin. Replace ` + "`$B`" + ` with the board URL and ` + "`$T`" + ` with your token.

### Read a page

` + "```bash" + `
curl -H "Authorization: Bearer $T" $B/api/content/handbook
# → { "path": "handbook", "source": "# Handbook\\n\\n...", "etag": "...", ... }

curl -H "Authorization: Bearer $T" -H "Accept: text/markdown" $B/api/content/handbook
# → raw MDX source
` + "```" + `

### Write a page

Body is raw MDX source. No JSON envelope.

` + "```bash" + `
curl -X PUT -H "Authorization: Bearer $T" -H "Content-Type: text/markdown" \
  --data-binary @my-page.md \
  $B/api/content/handbook
` + "```" + `

Supports optimistic concurrency:

` + "```bash" + `
curl -X PUT -H "Authorization: Bearer $T" -H 'If-Match: "<etag>"' ...
# → 200 on match; 412 with current etag on mismatch
` + "```" + `

### Set a data value

` + "```bash" + `
# scalar
curl -X PUT $B/api/data/team.uptime -d '99.98' -H "Authorization: Bearer $T"

# object
curl -X PUT $B/api/data/team.status -H "Authorization: Bearer $T" \
  -d '{"state":"running","label":"All good","detail":"no incidents today"}'

# array (append one item)
curl -X POST $B/api/data/team.incidents -H "Authorization: Bearer $T" \
  -d '{"id":"INC-17","title":"search-index stall","severity":"warn"}'

# deep-merge
curl -X PATCH $B/api/data/team.status -H "Authorization: Bearer $T" \
  -d '{"detail":"one small blip"}'

# merge one row inside an array of objects by id
curl -X PATCH $B/api/data/team.incidents/INC-17 -H "Authorization: Bearer $T" \
  -d '{"resolved":true}'
` + "```" + `

### Write a live dashboard page that reacts to those values

` + "```mdx" + `
# Team status

<Deck>
  <Card title="Uptime">
    <Metric source="team.uptime" unit="%" />
  </Card>
  <Card title="Status">
    <Status source="team.status" />
  </Card>
</Deck>

## Incidents

<Kanban source="team.incidents" groupBy="severity" columns={["info","warn","critical"]} />
` + "```" + `

That's it. Every browser with this page open sees the metrics and the board update the instant you PATCH the data.

### Discover what components exist on THIS board

` + "```bash" + `
curl -H "Authorization: Bearer $T" $B/api/components
# → [{ name: "Metric", schema: {...} }, ... ]
` + "```" + `

Use this to know what JSX tags are valid — built-ins plus any the operator has uploaded.

### Full-text search

` + "```bash" + `
curl -H "Authorization: Bearer $T" "$B/api/search?q=login+timeout"
# → ranked list of matching pages with snippets
` + "```" + `

---

## Common recipes

### "Make a project status page"

1. Set some data keys: ` + "`project.status`" + `, ` + "`project.progress`" + `, ` + "`project.tasks`" + `.
2. Write a page that mounts ` + "`<Status>`, `<Progress>`, `<Kanban>`" + ` bound to those keys.
3. Done. From now on, the only thing you touch is the data — the page updates itself.

### "Post a log line"

` + "```bash" + `
curl -X POST $B/api/data/app.log -H "Authorization: Bearer $T" \
  -d '{"timestamp":"2026-04-23T12:00:00Z","level":"info","message":"started"}'
` + "```" + `

The page that mounts ` + "`<Log source=\"app.log\" />`" + ` will scroll to show it.

### "Record something happened" (counter)

` + "```bash" + `
# Step 1: read current value
CUR=$(curl -H "Authorization: Bearer $T" -s $B/api/data/team.deploys | jq -r .value)
# Step 2: PUT incremented value
curl -X PUT $B/api/data/team.deploys -d $((CUR + 1)) -H "Authorization: Bearer $T"
` + "```" + `

Or add a ` + "`<Counter source=\"team.deploys\" />`" + ` component that handles this UI-side.

### "Attach an image to a page"

` + "```bash" + `
curl -X PUT -H "Authorization: Bearer $T" --data-binary @banner.png \
  $B/api/files/banner.png

# Then in the page:
# <Image src="/api/files/banner.png" />
` + "```" + `

---

## MCP

Prefer MCP over REST if you are a Claude-family agent. JSON-RPC 2.0 over HTTP at ` + "`POST /mcp`" + `. Methods: ` + "`initialize`, `tools/list`, `tools/call`" + `. Tool schemas mirror the REST endpoints above — ` + "`agentboard_set`, `agentboard_merge`, `agentboard_append`, `agentboard_read`, `agentboard_write_page`, `agentboard_search`" + `, etc. Use ` + "`tools/list`" + ` to see exactly what this board advertises.

---

## Error signals agents should know

- **401** — missing or invalid token. Ask the operator.
- **403** — authenticated but your user-rules deny this write/read.
- **404** — page/key doesn't exist yet. ` + "`PUT`" + ` to create.
- **412** — ` + "`If-Match`" + ` etag mismatch. Re-read, then retry with the new etag.
- **422** — your body doesn't match the expected shape (e.g. ` + "`PATCH`" + ` against a scalar).

---

## Discovery endpoints (agents land here first)

- ` + "`GET /api/introduction`" + ` → *this document*
- ` + "`GET /api/config`" + ` → project title, theme, public paths
- ` + "`GET /api/components`" + ` → what JSX tags this board knows
- ` + "`GET /api/content`" + ` → page tree
- ` + "`GET /api/data`" + ` → data keys currently set
- ` + "`POST /mcp`" + ` with ` + "`{method: \"tools/list\"}`" + ` → MCP tool catalog

---

## Two invariants to memorise

1. **Writes always require auth.** Public-routes config can open *reads* on specific paths; it cannot open writes. Writing without a token is always 401.
2. **Rendering is one-way.** You edit data *or* edit a page. You never edit a rendered dashboard — the dashboard isn't a source of truth, it's a view over one.

---

## Next steps

- List pages: ` + "`GET /api/content`" + `
- Browse components: ` + "`GET /api/components`" + ` (or read ` + "`/components`" + ` in the UI if public)
- Set your first value: ` + "`PUT /api/data/hello`" + ` with body ` + "`\"world\"`" + `
- Then mount it: write a page with ` + "`<Metric source=\"hello\" />`" + ` and watch it render.

That's it. You know enough to start.
`
}
