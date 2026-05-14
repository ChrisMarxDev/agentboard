package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// Seed content for a fresh workspace. Per CORE_GUIDELINES §15 the
// workspace teaches the agent — these constants are the chain an agent
// walks on a cold boot: README → SKILL → examples.

// BootstrapReadmeMd is the file an agent reads first inside a fresh
// workspace. Linked by the SKILL chain; kept short, link-heavy, and
// durable enough to outlive surface tweaks.
var BootstrapReadmeMd = bootstrapReadmeMd

const bootstrapReadmeMd = `---
title: README
---

# This workspace

You are inside an **AgentBoard workspace** — a shared git repo a team
of humans and AI agents collaborate inside. The substrate is git: the
working tree you see on disk is the same tree the dashboard renders.
There is no separate database, no envelope format, no “singletons vs
streams” distinction — every leaf is a real file.

## Connecting (one-time setup)

The agent's current working directory IS the workspace. Wire it up:

` + "```bash" + `
# in the directory you want to bind to this workspace
git init -q
git remote add origin http://<user>:$AGENTBOARD_TOKEN@<host>/git/<workspace>.git
git fetch -q origin
git checkout -B main origin/main
` + "```" + `

The bearer token rides as the HTTP Basic password. Username is ignored
by the git endpoint but ` + "`<user>`" + ` is a convenient label in your shell
history.

(If your cwd is empty, ` + "`git clone <url> .`" + ` is the one-shot form.)

## If you are an agent, start here

1. Read [` + "`skills/agentboard/SKILL.md`" + `](/skills/agentboard/SKILL.md) — it teaches
   the protocol you'll use here: the six MCP tools, the conventions
   for proposing changes, conflict resolution.
2. Skim the rest of the tree. Whatever folder this workspace uses as
   its task queue (the SKILL says where) is where the open work lives.
3. Pick something, branch (` + "`git checkout -b feature/<slug>`" + `), commit,
   push. If the push is rejected, pull, resolve standard
   ` + "`<<<<<<<`" + ` / ` + "`=======`" + ` / ` + "`>>>>>>>`" + ` markers, push again.

## If you are a human, start here

- Browse the dashboard at the live URL. Markdown is rendered with
  goldmark; HTML files render inside a sandboxed iframe; JSON files
  display as JSON.
- The home page lives at ` + "`index.md`" + `. Edit it to describe what
  this workspace is for. Agents read it during the bootstrap chain.

## Conventions

- Files are what they are. A ` + "`.md`" + ` is markdown; a ` + "`.json`" + ` is JSON; a
  ` + "`.html`" + ` is sandboxed HTML. There is no transcoded schema.
- Inline first (CORE_GUIDELINES §14): scalars live on the page that
  displays them. Only folder collections may legitimately cross-reference.
- Read what's already here before you write. The workspace usually
  knows more than your prompt does.
- Don't bypass the server. Even though the working tree is a real git
  tree on disk, direct writes to the mirror skip auth, the activity
  log, and the post-receive event fan-out. Push through git or MCP.
`

// welcomeIndexMd is the home page seeded at the root of a fresh
// workspace. Short, points at the README + SKILL.
const welcomeIndexMd = `---
title: Welcome
---

# Welcome to AgentBoard

This workspace is the dashboard you're looking at, and a real git repo
agents can clone and contribute to. The two views are the same files.

## Quick links

- [README](/README.md) — the bootstrap chain, conventions, how to
  connect.
- [skills/agentboard/SKILL.md](/skills/agentboard/SKILL.md) — the agent
  contract: six MCP tools, propose/resolve conflicts, recipes.

## Connect Claude

` + "```" + `
claude mcp add agentboard http://localhost:3000/mcp
` + "```" + `

Then ask Claude to do real work — write notes, refactor a file, build
out a folder. Pushes show up here within a second.
`

// welcomeConfig is the agentboard.yaml seeded next to the workspace
// tree. Fields the server actually reads today; older v0.13 keys have
// been removed.
const welcomeConfig = `title: "AgentBoard"
port: 3000
theme: auto
history_retention_days: 30
`

// SeededSkillManifest is the SKILL.md seeded under
// skills/agentboard/ of every fresh workspace. Per CORE_GUIDELINES §15
// and spec §1.5 this is where the bootstrap README points the agent —
// the canonical agent contract. The §15 dogfood test is the canary
// that catches drift.
var SeededSkillManifest = seededSkillManifest

const seededSkillManifest = `---
name: agentboard
description: How to use AgentBoard as an agent — clone the workspace, follow the conventions, propose changes via git or MCP, resolve conflicts. Read this first after the README.
---

# AgentBoard for agents

You are inside an **AgentBoard workspace** — a shared git repo a team
of humans and AI agents collaborate inside. The substrate is git; the
wire format you write through is either ` + "`git`" + ` (when your runtime can
shell out) or the six ` + "`agentboard_*`" + ` MCP tools (the git-less fallback).

## The contract in one paragraph

The workspace tells you what to do. Clone it, read ` + "`README.md`" + ` at
the root, follow the chain of references. Work happens on commits:
edit files, ` + "`git commit`" + `, ` + "`git push`" + `. If your push is rejected, pull,
resolve the standard conflict markers, push again. Conflicts in
markdown frontmatter resolve the same way conflicts in code do —
treat them as ordinary work.

## Authentication

Every endpoint except ` + "`/_api/health`" + ` needs a credential. The format
is ` + "`ab_<43 chars>`" + ` for personal tokens, or ` + "`oat_<…>`" + ` for
audience-scoped OAuth tokens minted via the MCP onboarding flow.

For git the token rides as HTTP Basic auth:

` + "```bash" + `
git clone http://<user>:ab_<token>@<host>/git/<workspace>.git
` + "```" + `

Username is ignored by the git endpoint; pick anything that helps you
recognize the credential in your shell history.

For REST + MCP:

` + "```" + `
Authorization: Bearer ab_<token>
` + "```" + `

If you can't authenticate, **stop and report it**. Don't sidestep
auth by writing to the working-tree mirror on disk — direct writes
bypass auth, attribution, history, and the post-receive event bus.

## Two ways to work

### Path A — your runtime has git (preferred)

The agent's working directory IS the workspace. Don't clone into a
second directory; bind your cwd to the workspace remote:

` + "```bash" + `
git init -q
git remote add origin http://<user>:$AGENTBOARD_TOKEN@<host>/git/<workspace>.git
git fetch -q origin
git checkout -B main origin/main
` + "```" + `

Normal git flow from there:

` + "```bash" + `
git checkout -b feature/<slug>
# edit files...
git add -A
git commit -m "<message>"
git push origin HEAD:main          # push-to-main mode (default)
# or, on always-PR workspaces:
git push origin HEAD:feature/<slug>
` + "```" + `

The server materializes its working-tree mirror after every push;
the dashboard reflects your change within a second.

### Path B — no git available

Use the six MCP tools. Nothing else exists on the wire:

` + "```" + `
agentboard_workspaces             — list workspaces visible to this caller
agentboard_pull(ws, ref?)         — return the working tree as
                                    {files: [{path, frontmatter?, body, sha}]}
agentboard_propose(ws, files,
                   message,
                   base?, branch?) — server-side branch + commit + push
                                    files: [{path, body}]; body=null deletes
agentboard_resolve_conflict(
  proposal, file, resolution)     — submit a resolved file body when a
                                    propose returned conflicts
agentboard_subscribe(events,
                     workspace?,
                     cursor?)     — long-poll push / merge / conflict events
agentboard_fire_event(event,
                      payload?)   — emit on the webhook bus
` + "```" + `

` + "`pull`" + ` is the read primitive; partial reads filter the bundle
client-side. There is no field-level patch RPC — write the whole file
body via ` + "`propose`" + `.

## Conventions

- **Inline first** (CORE_GUIDELINES §14). A scalar shown in one place
  lives on the page that displays it, in YAML frontmatter or in the
  markdown body. Cross-doc references are reserved for folder
  collections.
- **No invented prefixes.** Pick the path you want the leaf to *appear*
  at and write there. Don't invent parallel namespaces.
- **Read before you write.** ` + "`git pull --rebase`" + ` (or ` + "`agentboard_pull`" + `)
  before you start. Mid-air rebase beats mid-push conflict.
- **Keep prose short.** The dashboard is a glance surface.

## Conflicts

Push rejected?

` + "```bash" + `
$ git push origin HEAD:main
! [rejected]        HEAD -> main (non-fast-forward)

$ git pull --rebase origin main
# resolve conflicts in your editor / by re-emitting the merged file…
$ git add -A
$ git rebase --continue
$ git push origin HEAD:main
` + "```" + `

The ` + "`<<<<<<<`" + ` / ` + "`=======`" + ` / ` + "`>>>>>>>`" + ` markers tell you which two
versions disagreed. Pick the merge that preserves intent and push.

Via MCP: ` + "`agentboard_propose`" + ` returns a conflicts list on rejection;
call ` + "`agentboard_resolve_conflict(proposal, file, resolution)`" + ` once
per conflicted file, and the proposal retries.

## Quick examples

See [examples.md](/skills/agentboard/examples.md) for concrete recipes
— writing a doc, hosting a binary, replying to a teammate's push.
`

// SeededSkillExamples ships at skills/agentboard/examples.md. Concrete
// recipes for the common operations, in both git and MCP form.
var SeededSkillExamples = seededSkillExamples

const seededSkillExamples = `# AgentBoard recipes

Every recipe below has two flavors — **git** (when your runtime can
shell out) and **MCP** (when it can't). They are the same operation.

## Create a doc

### git
` + "```bash" + `
mkdir -p notes
cat > notes/2026-05-13.md <<'EOF'
---
title: "Smoke test results"
author: agent
---

# Smoke test results

Everything from the bootstrap walk passed.
EOF
git add notes/2026-05-13.md
git commit -m "Add smoke-test note"
git push origin HEAD:main
` + "```" + `

### MCP
` + "```" + `
agentboard_propose({
  workspace: "<ws>",
  message: "Add smoke-test note",
  files: [{
    path: "notes/2026-05-13.md",
    body: "---\ntitle: \"Smoke test results\"\nauthor: agent\n---\n\n# Smoke test results\n\nEverything from the bootstrap walk passed.\n"
  }]
})
` + "```" + `

## Update a doc

### git
Edit the file, commit, push. That's it.

### MCP
` + "`agentboard_pull`" + ` the file, change the body locally, ` + "`agentboard_propose`" + `
the new body. Whole-file writes only; there is no field-level patch.

## Build a taskboard

A JSON file whose top-level shape has ` + "`columns`" + ` and ` + "`cards`" + ` arrays
renders as a kanban board (the **Taskboard typed view**) instead of as
pretty-printed JSON. Drop it anywhere — convention is ` + "`taskboards/`" + `
but the path doesn't matter.

### git
` + "```bash" + `
mkdir -p taskboards
cat > taskboards/sprint.json <<'EOF'
{
  "title": "Sprint 14",
  "columns": [
    {"id": "todo",  "label": "To do"},
    {"id": "doing", "label": "In progress"},
    {"id": "done",  "label": "Done"}
  ],
  "cards": [
    {"id": "c1", "title": "Ship Taskboard", "column": "doing",
     "labels": ["substrate"], "assignees": ["alice"],
     "priority": 1, "order": 1.0},
    {"id": "c2", "title": "Add OAuth", "column": "done",
     "labels": ["mcp"], "priority": 2}
  ]
}
EOF
git add taskboards/sprint.json
git commit -m "Add sprint 14 board"
git push
` + "```" + `

To move a card, edit its ` + "`column`" + ` field and push. To add a card,
append to the ` + "`cards`" + ` array. Whole-file rewrites are the unit —
there is no field-level patch RPC.

The dashboard renders ` + "`/taskboards/sprint.json`" + ` as a kanban board.
The JSON file remains the source of truth; cloning the workspace
gives you the bytes you can edit offline.

## Host a binary

Commit binary files directly into the workspace alongside the doc
that references them. The HTML renderer serves them with the right
content-type by extension.

### git
` + "```bash" + `
mkdir -p images
cp banner.png images/banner.png
git add images/banner.png
git commit -m "Add team banner"
git push
` + "```" + `

Reference it from any markdown file:

` + "```markdown" + `
![Team banner](/images/banner.png)
` + "```" + `

## Reply to a teammate's push

Poll for events on the workspace:

` + "```" + `
agentboard_subscribe({
  workspace: "<ws>",
  events: ["push", "conflict"],
  cursor: "<last-cursor>"     // omit on first call
})
` + "```" + `

The response carries any events since the cursor plus a fresh cursor
to use next time. For git-capable runtimes, ` + "`git fetch`" + ` periodically
and look for new commits on main.

## Notify downstream subscribers

` + "```" + `
agentboard_fire_event({
  event: "ship.v2.ready",
  payload: { branch: "main", commit: "abc1234" }
})
` + "```" + `

Webhook subscribers receive ` + "`{name: \"ship.v2.ready\", at, data: …}`" + `.
`

// Seeded demo content is now HTML, not markdown. The substrate pivot
// (spec-filesystem-substrate.md) made HTML the primary expressive
// primitive: agents already know it from training data, it renders
// without a runtime, and it ships zero proprietary syntax. We keep
// markdown for two conventional surfaces — the bootstrap README and
// Anthropic-format SKILL.md — and use HTML for everything else
// authored. Typed views (Taskboard, etc.) stay structured JSON.

// SeededIndexHTML is the home page seeded at index.html. HTML, not
// markdown, because the substrate pivot named HTML as the primary
// expressive surface for read-once content authored by agents.
var SeededIndexHTML = seededIndexHTML

const seededIndexHTML = `<!doctype html>
<title>AgentBoard</title>
<style>
  .hero { padding: 2rem 0 1rem; border-bottom: 1px solid var(--border); margin-bottom: 1.5rem; }
  .hero h1 { font-size: 2.25rem; margin: 0 0 .5rem; letter-spacing: -.02em; }
  .hero p  { font-size: 1.05rem; color: var(--text-secondary); margin: 0; max-width: 60ch; }

  .grid {
    display: grid;
    gap: 1rem;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    margin: 1.5rem 0 2rem;
  }
  .card-link {
    display: block; padding: 1rem 1.25rem;
    background: var(--bg); border: 1px solid var(--border);
    border-radius: var(--ab-radius); text-decoration: none; color: inherit;
    transition: border-color .12s ease, transform .12s ease;
  }
  .card-link:hover { border-color: var(--accent); transform: translateY(-1px); }
  .card-link .eyebrow { font-size: .7rem; text-transform: uppercase;
    letter-spacing: .05em; color: var(--text-secondary); margin-bottom: .35rem; }
  .card-link .label { font-weight: 600; color: var(--text); margin-bottom: .25rem; }
  .card-link .desc  { font-size: .85rem; color: var(--text-secondary); }

  .connect { background: var(--bg-secondary); border: 1px solid var(--border);
    border-radius: var(--ab-radius); padding: 1.25rem 1.5rem; margin-top: 1rem; }
  .connect h2 { margin-top: 0; font-size: 1rem; text-transform: uppercase;
    letter-spacing: .05em; color: var(--text-secondary); }
  .connect pre { margin: .75rem 0 0; }
</style>

<section class="hero">
  <h1>AgentBoard</h1>
  <p>A shared workspace — a git repo a small team of humans and AI agents
     collaborate inside. Every file in this tree is served as-is by the
     dashboard: HTML renders as expressive pages, JSON with
     <code>columns</code>+<code>cards</code> renders as a kanban,
     markdown renders through goldmark.</p>
</section>

<section class="grid">
  <a class="card-link" href="/pages/getting-started.html">
    <div class="eyebrow">Read</div>
    <div class="label">Getting started</div>
    <div class="desc">Authored HTML — what an expressive page looks like.</div>
  </a>
  <a class="card-link" href="/taskboards/sprint.json">
    <div class="eyebrow">Typed view</div>
    <div class="label">Sprint board</div>
    <div class="desc">A JSON file rendered as a live kanban.</div>
  </a>
  <a class="card-link" href="/pages/changelog.html">
    <div class="eyebrow">Activity</div>
    <div class="label">Changelog</div>
    <div class="desc">Short, glance-friendly log of recent ships.</div>
  </a>
  <a class="card-link" href="/pages/roadmap.html">
    <div class="eyebrow">Plan</div>
    <div class="label">Roadmap</div>
    <div class="desc">Quality-of-life features the board could grow into next.</div>
  </a>
  <a class="card-link" href="/README.md">
    <div class="eyebrow">Convention</div>
    <div class="label">README</div>
    <div class="desc">How this workspace is wired and how to connect.</div>
  </a>
  <a class="card-link" href="/skills/agentboard/SKILL.md">
    <div class="eyebrow">Agent contract</div>
    <div class="label">SKILL</div>
    <div class="desc">The protocol agents read on a cold boot.</div>
  </a>
</section>

<section class="connect">
  <h2>Connect</h2>
  <p>Open the invite URL printed in the server log to claim an admin
     account. Then bind a working directory to this workspace:</p>
  <pre><code>git init -q
git remote add origin http://&lt;user&gt;:$AGENTBOARD_TOKEN@&lt;host&gt;/git/dogfood.git
git fetch -q origin
git checkout -B main origin/main</code></pre>
  <p style="margin-top:.75rem">Or, over MCP:</p>
  <pre><code>claude mcp add agentboard https://&lt;host&gt;/mcp</code></pre>
</section>
`

// SeededGettingStartedHTML is the demo "what authoring HTML looks
// like" page. Embedded styles, design-system tokens, real content
// shape — read it for ideas about what agents can ship as HTML.
var SeededGettingStartedHTML = seededGettingStartedHTML

const seededGettingStartedHTML = `<!doctype html>
<title>Getting started</title>
<style>
  h1 { letter-spacing: -.015em; }
  .lede { font-size: 1.05rem; color: var(--text-secondary);
          max-width: 65ch; margin: 0 0 1.5rem; }
  .callout {
    display: grid; grid-template-columns: auto 1fr; gap: 1rem;
    align-items: start;
    background: var(--accent-light); border-left: 3px solid var(--accent);
    border-radius: var(--ab-radius); padding: 1rem 1.25rem; margin: 1.25rem 0;
    font-size: .95rem; color: var(--text);
  }
  .callout .icon {
    width: 28px; height: 28px; border-radius: 50%; background: var(--accent);
    color: white; font-weight: 700; display: grid; place-items: center;
  }
  .step-grid { display: grid; gap: 1rem;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    margin: 1.5rem 0; }
  .step { padding: 1rem 1.25rem; background: var(--bg-secondary);
    border: 1px solid var(--border); border-radius: var(--ab-radius); }
  .step .n { color: var(--accent); font-weight: 700; font-size: 1.25rem; }
  .step h3 { margin: .25rem 0 .5rem; font-size: 1rem; }
  .step p  { margin: 0; font-size: .9rem; color: var(--text-secondary); }
  table.compact { width: 100%; }
  table.compact th { text-align: left; }
</style>

<h1>Getting started</h1>

<p class="lede">
  This page is a real <code>.html</code> file in the workspace. The server
  inlines its body inside the dashboard shell so it can use the same
  design-system tokens (<code>--ab-accent</code>, <code>--ab-radius</code>, …)
  the chrome uses. No build step. No JSX. Just HTML and CSS, the way agents
  learn them from training data.
</p>

<div class="callout">
  <div class="icon">i</div>
  <div>
    <strong>HTML is the primary expressive primitive.</strong> Reach for it
    for read-once content that wants a layout. Reach for <em>markdown</em>
    only for conventional docs (READMEs, SKILL.md). Reach for <em>typed
    JSON</em> when the content has queryable structure (taskboards now;
    more types coming).
  </div>
</div>

<h2>The three primitives</h2>

<div class="step-grid">
  <div class="step">
    <div class="n">01</div>
    <h3>HTML pages</h3>
    <p>Free-form layout, embedded CSS, design-system tokens. The body
       inlines into the dashboard shell — drop a <code>&lt;style&gt;</code>
       block and go.</p>
  </div>
  <div class="step">
    <div class="n">02</div>
    <h3>Typed views</h3>
    <p>Structured JSON the server renders as a typed surface. A file
       with <code>columns</code>+<code>cards</code> renders as a
       <a href="/taskboards/sprint.json">kanban board</a>.</p>
  </div>
  <div class="step">
    <div class="n">03</div>
    <h3>Markdown</h3>
    <p>For conventional docs — READMEs, change logs, agent skills.
       Rendered through goldmark with GitHub-flavored extensions.</p>
  </div>
</div>

<h2>How edits land</h2>

<p>Either <code>git push</code> (the preferred path) or the MCP
<code>agentboard_propose</code> tool. The server makes a real commit,
updates the worktree mirror, and the dashboard re-renders on the next
request.</p>

<pre><code># push a page
git add pages/notes.html
git commit -m "Add notes"
git push

# push a typed view
git add taskboards/sprint.json
git commit -m "Update sprint board"
git push</code></pre>

<h2>Comparison</h2>

<table class="compact">
  <thead>
    <tr><th>Use case</th><th>Pick</th><th>Why</th></tr>
  </thead>
  <tbody>
    <tr><td>Landing page, dashboard, demo</td><td><code>.html</code></td>
        <td>Full layout control, custom CSS, design-system tokens.</td></tr>
    <tr><td>Kanban board, structured data</td><td><code>.json</code></td>
        <td>Typed view renders + stays queryable for typed APIs.</td></tr>
    <tr><td>README, skill, change log prose</td><td><code>.md</code></td>
        <td>Convention. Short prose; humans glance.</td></tr>
    <tr><td>Image, PDF, font</td><td>(binary)</td>
        <td>Commit the bytes; the renderer serves the right content-type.</td></tr>
  </tbody>
</table>

<p style="margin-top:2rem;color:var(--text-secondary);font-size:.85rem">
  Need to react to a teammate's push? Use
  <code>agentboard_subscribe</code> (long-poll) or <code>git fetch</code>
  on a timer.
</p>
`

// SeededChangelogHTML is a tight, visual activity log. Demonstrates
// what a "stream-like" page looks like as authored HTML — no need for
// a separate stream primitive when a styled <ol> does the job.
var SeededChangelogHTML = seededChangelogHTML

const seededChangelogHTML = `<!doctype html>
<title>Changelog</title>` + changelogStyles + `

<h1>Changelog</h1>
<p class="ab-muted">Short, glance-friendly. One entry per shipping moment. Newest first.</p>

<ol class="timeline">
  <li>
    <time>2026-05-14</time>
    <h3>Full-text search + header search box</h3>
    <p><span class="pill">qol</span>
       SQLite FTS5 index over the working tree, rebuilt on every push.
       <code>/?q=needle</code> renders <code>&lt;mark&gt;</code>-highlighted
       hits with click-through to the source file. Header gets a search
       input on tablet+ viewports. Snippet HTML safely escaped before
       sentinel-swapping for marks.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>Unclaimed-board → /invite redirect</h3>
    <p><span class="pill">auth</span>
       Visiting <code>/login</code> on a board with zero users now
       short-circuits to the active bootstrap invitation. Cleaner
       first-touch UX for fresh deploys.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>Live page-changed toast (SSE)</h3>
    <p><span class="pill">qol</span>
       Push events broadcast over <code>/_api/events</code>; the
       dashboard shell pops a non-modal "Reload" toast when the workspace
       changes under your feet. EventSource-based; graceful degradation
       on browsers without it.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>Diff viewer + clickable history</h3>
    <p><span class="pill">qol</span>
       <code>?diff=&lt;sha&gt;</code> or <code>?diff=&lt;from&gt;..&lt;to&gt;</code> renders
       a colored unified diff. History rows now link straight into the
       diff for that commit. Backed by <code>git diff --no-color</code>
       on the bare repo.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>Sidebar nested folders</h3>
    <p><span class="pill">qol</span>
       <code>buildTree</code> now recurses three levels. Folders render
       as <code>&lt;details&gt;</code> with a rotating chevron — no JS,
       keyboard-toggleable. The folder containing the active page
       auto-expands.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>Page history viewer</h3>
    <p><span class="pill">qol</span>
       <code>?history=1</code> renders the file's git log as a styled
       list. The shell shows a "(history)" link in the page-actions
       strip on every real file page.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>Roadmap page + quality-of-life plan</h3>
    <p><span class="pill">docs</span>
       <a href="/pages/roadmap.html">/pages/roadmap.html</a> is the
       living plan: history (shipped), search, typed views, editing,
       presence, substrate work. Each item carries a rough size
       estimate.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>Human-facing auth UI</h3>
    <p><span class="pill">auth</span>
       <code>/login</code> and <code>/invite/&lt;id&gt;</code> are real HTML
       forms now; <code>/logout</code> hops home. The header shows
       <code>@username</code> + a sign-out link when a session cookie is
       set. Self-tested by <code>test/dogfood/auth-e2e.sh</code> — 21
       assertions covering anonymous probes, login, invite redemption,
       cookie persistence, and bad-credentials handling.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>Mobile-responsive shell</h3>
    <p><span class="pill">ux</span>
       Hamburger toggle, drawer-style sidebar overlay, larger tap targets,
       horizontal-scroll behavior on tables and code blocks.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>HTML is the primary expressive primitive</h3>
    <p><span class="pill substrate">substrate</span>
       Seeded examples shipped as <code>.md</code> in the first cut — wrong
       reflex. Switched to authored <code>.html</code>: home,
       getting-started, changelog, roadmap. Markdown stays for READMEs and
       SKILL.md per convention.</p>
  </li>
  <li>
    <time>2026-05-13</time>
    <h3>Taskboard typed view (substrate cut E)</h3>
    <p><span class="pill substrate">substrate</span>
       JSON files with <code>columns</code>+<code>cards</code> render as
       kanban boards. See <a href="/taskboards/sprint.json">/taskboards/sprint.json</a>.</p>
  </li>
  <li>
    <time>2026-05-13</time>
    <h3>Wholesale rewrite — cuts C–H</h3>
    <p><span class="pill substrate">substrate</span>
       React SPA + v0.13 file store retired. Substrate is now git, dashboard
       is server-rendered HTML. ~46k lines deleted; ~1k added.</p>
  </li>
  <li>
    <time>2026-04-30</time>
    <h3>v0.13 → git substrate pivot begins</h3>
    <p><span class="pill substrate">substrate</span>
       Cuts 1–10 retired the v0.13 substrate. Workspace = git repo;
       dashboard reads the working-tree mirror.</p>
  </li>
</ol>
`

// changelogStyles is broken out so the roadmap page can reuse the
// timeline styling without duplicating the CSS.
const changelogStyles = `
<style>
  .timeline { list-style: none; padding: 0; margin: 1.5rem 0; }
  .timeline > li { position: relative; padding: 0 0 1.5rem 1.5rem;
    border-left: 2px solid var(--border); margin-left: 4px; }
  .timeline > li:last-child { border-left-color: transparent; }
  .timeline > li::before {
    content: ""; position: absolute; left: -7px; top: .35rem;
    width: 12px; height: 12px; border-radius: 50%;
    background: var(--accent); border: 2px solid var(--bg);
  }
  .timeline time { font-size: .8rem; color: var(--text-secondary);
    font-variant-numeric: tabular-nums; }
  .timeline h3 { margin: .15rem 0 .25rem; font-size: 1rem; }
  .timeline p { margin: 0; color: var(--text-secondary); font-size: .9rem; }
  .pill { display: inline-block; padding: .05rem .45rem; border-radius: 9999px;
    font-size: .7rem; font-weight: 500; margin-right: .35rem;
    background: var(--bg-secondary); color: var(--text-secondary);
    border: 1px solid var(--border); }
  .pill.substrate { background: var(--accent-light); color: var(--accent);
    border-color: transparent; }
</style>
`

// SeededRoadmapHTML is the quality-of-life roadmap page seeded at
// pages/roadmap.html. Lives on the dashboard as a living document
// agents can update. The categories below were chosen as the natural
// next moves once the substrate cuts stabilize.
var SeededRoadmapHTML = seededRoadmapHTML

const seededRoadmapHTML = `<!doctype html>
<title>Roadmap</title>` + changelogStyles + `
<style>
  .category { margin: 2rem 0 1rem; padding-bottom: .35rem;
    border-bottom: 1px solid var(--border); font-size: .8rem;
    text-transform: uppercase; letter-spacing: .05em;
    color: var(--text-secondary); }
  .pill.size-s { background: rgba(34,197,94,.10); color: var(--success);
    border-color: transparent; }
  .pill.size-m { background: var(--accent-light); color: var(--accent);
    border-color: transparent; }
  .pill.size-l { background: rgba(245,158,11,.15); color: var(--warning);
    border-color: transparent; }
</style>

<h1>Roadmap</h1>
<p class="ab-muted">
  Quality-of-life features the board could grow into next. Each item
  carries a rough size estimate (S/M/L) — these are nudges, not
  commitments. Reorder freely.
</p>

<div class="category">History &amp; versioning</div>
<ol class="timeline">
  <li>
    <time>v0.2</time>
    <h3>Page history viewer</h3>
    <p><span class="pill size-m">M</span>
       <code>GET /&lt;path&gt;?history=1</code> lists every commit that
       touched the file with author, date, and a one-line summary. Backed
       by <code>git log --follow</code>; cheap to serve.</p>
  </li>
  <li>
    <time>v0.2</time>
    <h3>Diff between revisions</h3>
    <p><span class="pill size-m">M</span>
       Side-by-side diff at <code>/&lt;path&gt;?diff=&lt;sha&gt;..&lt;sha&gt;</code>.
       Rendered server-side via <code>git diff --color-words</code> →
       HTML; agents can deep-link to a diff URL.</p>
  </li>
  <li>
    <time>v0.3</time>
    <h3>Annotated blame view</h3>
    <p><span class="pill size-l">L</span>
       Per-line author + commit hover. Useful for "who put this status
       to <em>done</em>?" without leaving the page. Cache the blame
       computation per (path, sha) — expensive without caching.</p>
  </li>
  <li>
    <time>v0.3</time>
    <h3>Restore-from-history</h3>
    <p><span class="pill size-s">S</span>
       One-click "restore this version" on history rows — opens a
       confirmed POST that round-trips through <code>agentboard_propose</code>
       with the old body as the new commit.</p>
  </li>
</ol>

<div class="category">Discovery &amp; navigation</div>
<ol class="timeline">
  <li>
    <time>v0.2</time>
    <h3>Full-text search</h3>
    <p><span class="pill size-m">M</span>
       SQLite FTS5 index over the working tree, rebuilt on the
       post-receive hook. <code>/?q=needle</code> as a top-level query;
       results show the path + matching line context. Headers, frontmatter
       fields, and code blocks are searchable equally.</p>
  </li>
  <li>
    <time>v0.2</time>
    <h3>Sidebar nested folders</h3>
    <p><span class="pill size-s">S</span>
       Today the tree is one level deep. Recurse into subfolders, lazy-
       expand on click, remember open state in <code>localStorage</code>.
       Critical once the tree grows past 20-ish entries.</p>
  </li>
  <li>
    <time>v0.2</time>
    <h3>Recently-edited surface</h3>
    <p><span class="pill size-s">S</span>
       Home-page card "recent activity" listing the 10 most-recently
       committed files. Pulls from git log; useful for jumping back into
       wherever the team was working.</p>
  </li>
</ol>

<div class="category">Typed views (more)</div>
<ol class="timeline">
  <li>
    <time>v0.2</time>
    <h3>Mention typed view</h3>
    <p><span class="pill size-m">M</span>
       <code>@username</code> tokens in any file get indexed; per-user
       <code>/mentions/&lt;user&gt;</code> renders the inbox as a list.
       Materializer runs on every commit; cheap incremental update.</p>
  </li>
  <li>
    <time>v0.3</time>
    <h3>Metric typed view</h3>
    <p><span class="pill size-s">S</span>
       <code>metrics/*.json</code> with <code>{value, label, trend}</code>
       renders as a styled metric card with a big number + a delta arrow.
       Trivial to add once we agree on a shape.</p>
  </li>
  <li>
    <time>v0.3</time>
    <h3>User typed view</h3>
    <p><span class="pill size-m">M</span>
       <code>/users/&lt;username&gt;</code> is the canonical per-user page —
       avatar, display name, recent activity, tokens (admin-only). Replaces
       the today's per-user admin pages that live under <code>/_api/admin</code>.</p>
  </li>
</ol>

<div class="category">Editing &amp; collaboration</div>
<ol class="timeline">
  <li>
    <time>v0.2</time>
    <h3>In-browser edit</h3>
    <p><span class="pill size-l">L</span>
       A <strong>(edit)</strong> link in the meta-bar opens a textarea
       view of the file with monospaced styling; saving POSTs through the
       cookie + CSRF and round-trips into a git commit. Optimistic
       concurrency via the existing CAS check.</p>
  </li>
  <li>
    <time>v0.3</time>
    <h3>Presence: who's looking now</h3>
    <p><span class="pill size-m">M</span>
       Long-poll <code>/_api/presence/&lt;path&gt;</code> reports recent
       viewers; the meta-bar shows avatars of anyone currently on the
       page. Helps avoid edit-collisions before they happen.</p>
  </li>
  <li>
    <time>v0.3</time>
    <h3>Live page-changed toasts</h3>
    <p><span class="pill size-s">S</span>
       Reuse the existing SSE broadcaster: when a push touches the page
       you're viewing, a non-modal toast offers "reload". Already wired —
       just needs the post-receive hook to broadcast.</p>
  </li>
</ol>

<div class="category">Substrate</div>
<ol class="timeline">
  <li>
    <time>v0.2</time>
    <h3>Split-origin sandbox</h3>
    <p><span class="pill size-l">L</span>
       Serve user-authored HTML on <code>usercontent.&lt;host&gt;</code>
       inside a sandbox iframe so untrusted page CSS / JS can't reach the
       dashboard chrome. Spec-filesystem-substrate.md §security covers the
       deployment plan.</p>
  </li>
  <li>
    <time>v0.2</time>
    <h3>Multiple workspaces per board</h3>
    <p><span class="pill size-m">M</span>
       Today every board has one workspace (<code>dogfood</code>). The
       gitserver already supports n workspaces; the dashboard and MCP need
       a workspace-picker in the header + a route prefix.</p>
  </li>
  <li>
    <time>v0.3</time>
    <h3>Always-PR mode</h3>
    <p><span class="pill size-l">L</span>
       Per-workspace policy: <code>push-to-main</code> (today) vs
       <code>always-PR</code> (server creates a branch, holds the merge
       until reviewer approval). Surface PRs as a typed view under
       <code>/proposals/</code>.</p>
  </li>
</ol>

<p class="ab-muted" style="margin-top:2.5rem;font-size:.85rem">
  Ideas welcome — propose new entries via
  <code>git push</code> to <code>pages/roadmap.html</code> or via
  <code>agentboard_propose</code>.
</p>
`

// SeededSprintTaskboardJSON is a working Taskboard example. Shape
// matches what the renderer recognizes: top-level ` + "`columns`" + ` and
// ` + "`cards`" + ` arrays. Cards span all three lanes so a visitor lands on
// a realistic board, not an empty one.
var SeededSprintTaskboardJSON = seededSprintTaskboardJSON

const seededSprintTaskboardJSON = `{
  "kind": "taskboard",
  "title": "Sprint 14",
  "columns": [
    {"id": "backlog", "label": "Backlog"},
    {"id": "todo",    "label": "To do"},
    {"id": "doing",   "label": "In progress"},
    {"id": "done",    "label": "Done"}
  ],
  "cards": [
    {
      "id": "c-done-auth-ui",
      "title": "Human-facing auth UI",
      "column": "done",
      "body": "/login, /logout, /invite/<id> as real HTML forms. Self-tested by test/dogfood/auth-e2e.sh.",
      "labels": ["auth", "ux"],
      "assignees": ["claude"],
      "priority": 1,
      "order": 0.5
    },
    {
      "id": "c-done-mobile-shell",
      "title": "Mobile-responsive dashboard shell",
      "column": "done",
      "body": "Collapsible sidebar overlay, larger tap targets.",
      "labels": ["ux", "mobile"],
      "assignees": ["claude"],
      "priority": 1,
      "order": 1.0
    },
    {
      "id": "c-done-html-primitive",
      "title": "HTML as primary expressive primitive",
      "column": "done",
      "body": "Seeded examples switched from .md to .html. Markdown stays for READMEs / SKILLs.",
      "labels": ["substrate"],
      "assignees": ["claude"],
      "priority": 1,
      "order": 2.0
    },
    {
      "id": "c-done-taskboard",
      "title": "Taskboard typed view",
      "column": "done",
      "body": "JSON with columns+cards renders as kanban (you're looking at it).",
      "labels": ["substrate", "typed-view"],
      "assignees": ["claude"],
      "priority": 2,
      "order": 3.0
    },
    {
      "id": "c-done-history",
      "title": "Page history viewer",
      "column": "done",
      "body": "?history=1 lists commits via git log --follow on the bare repo.",
      "labels": ["history", "qol"],
      "assignees": ["claude"],
      "priority": 1,
      "order": 4.0
    },
    {
      "id": "c-done-nested-tree",
      "title": "Sidebar nested folders",
      "column": "done",
      "body": "Recurses 3 levels via <details>/<summary>. No JS dependency.",
      "labels": ["ux", "qol"],
      "assignees": ["claude"],
      "priority": 2,
      "order": 5.0
    },
    {
      "id": "c-done-diff",
      "title": "Diff between revisions",
      "column": "done",
      "body": "?diff=<sha> renders colored unified diff. History rows link in.",
      "labels": ["history", "qol"],
      "assignees": ["claude"],
      "priority": 1,
      "order": 6.0
    },
    {
      "id": "c-done-sse-toast",
      "title": "Live page-changed toast (SSE)",
      "column": "done",
      "body": "Workspace pushes broadcast over /_api/events; dashboard pops a Reload toast.",
      "labels": ["collab", "qol"],
      "assignees": ["claude"],
      "priority": 2,
      "order": 7.0
    },
    {
      "id": "c-done-search",
      "title": "Full-text search",
      "column": "done",
      "body": "SQLite FTS5 over the working tree; rebuilt on push. /?q=needle renders hits.",
      "labels": ["discovery", "qol"],
      "assignees": ["claude"],
      "priority": 2,
      "order": 8.0
    },
    {
      "id": "c-done-unclaimed-redirect",
      "title": "Unclaimed-board → /invite redirect",
      "column": "done",
      "body": "/login short-circuits to the bootstrap invitation on fresh deploys.",
      "labels": ["auth", "ux"],
      "assignees": ["claude"],
      "priority": 3,
      "order": 9.0
    },
    {
      "id": "c-doing-self-loop",
      "title": "Self-checking dev loop",
      "column": "doing",
      "body": "Auto-iterate on auth + content + QOL features under unsupervised 3-hour window.",
      "labels": ["dogfood", "process"],
      "assignees": ["claude"],
      "priority": 1,
      "order": 0.5
    },
    {
      "id": "c-backlog-mention",
      "title": "Mention typed view",
      "column": "backlog",
      "body": "Materialize @mentions across the worktree into /mentions/<user>.",
      "labels": ["typed-view"],
      "priority": 2,
      "order": 1.0
    },
    {
      "id": "c-backlog-edit",
      "title": "In-browser edit",
      "column": "backlog",
      "body": "Textarea view + POST-to-commit round-trip. Optimistic concurrency.",
      "labels": ["editing", "qol"],
      "priority": 2,
      "order": 2.0
    },
    {
      "id": "c-backlog-presence",
      "title": "Presence: who's looking now",
      "column": "backlog",
      "body": "Long-poll viewer list; avatars in the meta-bar.",
      "labels": ["collab", "qol"],
      "priority": 3,
      "order": 3.0
    },
    {
      "id": "c-backlog-pr-mode",
      "title": "Always-PR workspace policy",
      "column": "backlog",
      "body": "Server creates a branch + holds the merge until approval.",
      "labels": ["substrate"],
      "priority": 3,
      "order": 4.0
    }
  ]
}
`

// InitProject creates a new project from the welcome template.
func InitProject(projectPath string) (*Project, error) {
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	indexPath := filepath.Join(projectPath, "index.md")
	if err := os.WriteFile(indexPath, []byte(welcomeIndexMd), 0o644); err != nil {
		return nil, fmt.Errorf("write index.md: %w", err)
	}

	readmePath := filepath.Join(projectPath, "README.md")
	if err := os.WriteFile(readmePath, []byte(bootstrapReadmeMd), 0o644); err != nil {
		return nil, fmt.Errorf("write README.md: %w", err)
	}

	configPath := filepath.Join(projectPath, "agentboard.yaml")
	if err := os.WriteFile(configPath, []byte(welcomeConfig), 0o644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	proj, err := Load(projectPath)
	if err != nil {
		return nil, err
	}
	if err := proj.EnsureDirs(); err != nil {
		return nil, err
	}

	if err := seedAgentboardSkill(projectPath); err != nil {
		return nil, fmt.Errorf("seed skill: %w", err)
	}

	return proj, nil
}

// seedAgentboardSkill writes skills/agentboard/{SKILL.md, examples.md}.
// The path is workspace-root-relative — no `content/` namespace, matching
// the git-substrate layout.
func seedAgentboardSkill(projectPath string) error {
	skillDir := filepath.Join(projectPath, "skills", "agentboard")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(seededSkillManifest), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(skillDir, "examples.md"), []byte(seededSkillExamples), 0o644); err != nil {
		return err
	}
	return nil
}
