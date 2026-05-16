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

1. Read [` + "`SKILL.md`" + `](/SKILL.md) at the workspace root — the
   canonical AgentBoard primer (the six MCP tools, conventions, recipes).
   If your runtime expects skills under a specific path
   (` + "`.claude/skills/`" + `, ` + "`.codex/skills/`" + `, etc.), symlink or
   mirror ` + "`SKILL.md`" + ` there. The file is plain markdown — no
   custom format, no board-side special-casing.
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
- **One canonical SKILL.md at the workspace root.** Agent tools can
  symlink or mirror it into their own convention folder (` + "`.claude/`" + `,
  ` + "`.codex/`" + `, etc.) — the board renders these dotted folders like
  any other; no special handling. Convention over configuration.
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

// SeededRootSkill is the single canonical SKILL.md seeded at the
// workspace root. Merges the manifest + examples into one document so
// the README's bootstrap chain has exactly one target. Agent tools
// that look in their own convention folders (.claude/skills/,
// .codex/skills/, etc.) can symlink or mirror this file in — we don't
// pre-fork the seed into n directories.
var SeededRootSkill = seededSkillManifest + "\n\n" + seededSkillExamples

// SeededSkillManifest is kept exported for the admin sync-seed
// command; its content is the first half of SeededRootSkill.
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

Every endpoint except ` + "`/_api/health`" + ` needs a credential. The
format is ` + "`ab_<43 chars>`" + ` for personal tokens. Mint them from
` + "`/me`" + ` once you've claimed an admin or member account via an
invite link.

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

## Styling the workspace

The dashboard supports one-file workspace-wide theming. Drop a file
named ` + "`theme.css`" + ` at the workspace root and the shell loads it
**last** in the document ` + "`<head>`" + ` — after the embedded design
system, after the shell's own scaffolding. Anything you write there
overrides both.

The most useful surface is the CSS custom properties (` + "`--ab-*`" + ` and
unprefixed tokens like ` + "`--bg`, `--text`, `--accent`, `--border`" + `).
Every built-in component reads them, so a single token change
ripples through ` + "`.ab-card`, `.ab-badge`," + ` etc.

For a starting point, fetch the well-commented reference at
[` + "`/_static/theme.default.css`" + `](/_static/theme.default.css) — it
documents every token, shows example overrides (warm accent, custom
typography, header logo via background-image, dark-mode tweaks), and
lists which tokens each built-in component reads. Copy it to
` + "`/theme.css`" + ` and edit.

### git
` + "```bash" + `
curl -o theme.css http://<host>/_static/theme.default.css
$EDITOR theme.css   # adjust --ab-accent, fonts, etc.
git add theme.css
git commit -m "Theme: warmer palette"
git push
` + "```" + `

### MCP
` + "```" + `
agentboard_propose({
  workspace: "<ws>",
  message: "Theme: warmer palette",
  files: [{ path: "theme.css", body: ":root { --accent: #d97706; ... }" }]
})
` + "```" + `

Open tabs receive an SSE "Reload" toast after the push. No deploy step.

## Where this skill lives

This skill ships at ` + "`/SKILL.md`" + ` — the workspace root, one file,
the only canonical copy. The board doesn't special-case the path; if
your agent tool expects a skill under a specific directory
(` + "`.claude/skills/`" + `, ` + "`.codex/skills/`" + `, etc.), symlink or
mirror this file in. Plain markdown, no custom format.

## Quick examples
`

// SeededSkillExamples ships at skills/agentboard/examples.md. Concrete
// recipes for the common operations, in both git and MCP form.
var SeededSkillExamples = seededSkillExamples

// seededSkillExamples is appended after the manifest body when forming
// SeededRootSkill. Its h1 is omitted on purpose: the parent SKILL.md
// already uses an h1 and this is a continuation section under it.
const seededSkillExamples = `Every recipe below has two flavors — **git** (when your runtime can
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

` + "`git fetch`" + ` periodically and look for new commits on the default
branch — that's the discover-changes primitive. Browser tabs get a
non-modal "reload" toast via SSE on ` + "`/_api/events`" + ` when a push lands.

`

// Seeded demo content is now HTML, not markdown. The substrate pivot
// (spec-filesystem-substrate.md) made HTML the primary expressive
// primitive: agents already know it from training data, it renders
// without a runtime, and it ships zero proprietary syntax. We keep
// markdown for two conventional surfaces — the bootstrap README and
// Anthropic-format SKILL.md — and use HTML for everything else
// authored. Structured data ships as JSON or NDJSON without a typed
// renderer — agents read the raw bytes when they need the shape.

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
     dashboard: HTML renders as expressive pages, markdown renders
     through goldmark, JSON renders pretty-printed.</p>
</section>

<section class="grid">
  <a class="card-link" href="/pages/getting-started.html">
    <div class="eyebrow">Read</div>
    <div class="label">Getting started</div>
    <div class="desc">Authored HTML — what an expressive page looks like.</div>
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
  <a class="card-link" href="/pages/file-types.html">
    <div class="eyebrow">Reference</div>
    <div class="label">File types</div>
    <div class="desc">What renders how: markdown, HTML, JSON, images, CSV, text.</div>
  </a>
  <a class="card-link" href="/README.md">
    <div class="eyebrow">Convention</div>
    <div class="label">README</div>
    <div class="desc">How this workspace is wired and how to connect.</div>
  </a>
  <a class="card-link" href="/SKILL.md">
    <div class="eyebrow">Agent contract</div>
    <div class="label">SKILL</div>
    <div class="desc">The canonical AgentBoard primer at the workspace root — agent tools symlink it into their own convention folder.</div>
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
    only for conventional docs (READMEs, SKILL.md). Reach for <em>JSON
    or NDJSON</em> when agents need to query the bytes.
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

# push structured data
git add data/sprint-14.csv
git commit -m "Update sprint metrics"
git push</code></pre>

<h2>Comparison</h2>

<table class="compact">
  <thead>
    <tr><th>Use case</th><th>Pick</th><th>Why</th></tr>
  </thead>
  <tbody>
    <tr><td>Landing page, dashboard, demo</td><td><code>.html</code></td>
        <td>Full layout control, custom CSS, design-system tokens.</td></tr>
    <tr><td>Kanban, status board, table</td><td><code>.html</code></td>
        <td>CSS grid + one section per column; no typed renderer required.</td></tr>
    <tr><td>Structured data agents will query</td><td><code>.json</code></td>
        <td>Stays queryable; renders pretty-printed for humans.</td></tr>
    <tr><td>README, skill, change log prose</td><td><code>.md</code></td>
        <td>Convention. Short prose; humans glance.</td></tr>
    <tr><td>Image, PDF, font</td><td>(binary)</td>
        <td>Commit the bytes; the renderer serves the right content-type.</td></tr>
  </tbody>
</table>

<p style="margin-top:2rem;color:var(--text-secondary);font-size:.85rem">
  Need to react to a teammate's push? Run <code>git fetch</code> on a
  timer; the browser tab refreshes via SSE on its own.
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
    <h3>Restore-from-history (one-click)</h3>
    <p><span class="pill">qol</span>
       Each history row gains a <code>(restore)</code> button for
       signed-in users. POSTs to <code>/_api/restore</code> with the
       sha; server re-commits the file's content at that sha as a new
       commit. Closes the history → diff → restore triad.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>One canonical SKILL.md at the workspace root</h3>
    <p><span class="pill">substrate</span>
       Retired the <code>skills/agentboard/{SKILL.md, examples.md}</code>
       split in favor of a single <code>/SKILL.md</code> at the
       workspace root. Agent tools that look in their own convention
       folder (<code>.claude/skills/</code>, <code>.codex/skills/</code>)
       symlink or mirror this file in. The dashboard renders dotted
       folders too — <code>.git</code> + <code>.agentboard</code> stay
       server-internal; everything else is workspace content.</p>
  </li>
  <li>
    <time>2026-05-14</time>
    <h3>In-browser edit</h3>
    <p><span class="pill">qol</span>
       Signed-in users get an <code>(edit)</code> link in the
       page-actions strip. POSTs to <code>/_api/edit</code> with CSRF
       (double-submit cookie + form field). Round-trips through
       <code>gitserver.PutFile</code> — full git history captured.</p>
  </li>
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
  carries a rough size estimate (S/M/L). Items marked
  <span class="ab-badge accent">shipped</span> already landed —
  kept here as a record of what was on this list when planned.
</p>

<div class="category">History &amp; versioning</div>
<ol class="timeline">
  <li>
    <time>shipped</time>
    <h3>Page history viewer <span class="ab-badge accent">shipped</span></h3>
    <p><span class="pill size-m">M</span>
       <code>?history=1</code> on any file lists commits via
       <code>git log --follow</code>.</p>
  </li>
  <li>
    <time>shipped</time>
    <h3>Diff between revisions <span class="ab-badge accent">shipped</span></h3>
    <p><span class="pill size-m">M</span>
       <code>?diff=&lt;sha&gt;</code> renders colored unified diff. History
       rows link in.</p>
  </li>
  <li>
    <time>shipped</time>
    <h3>Restore-from-history <span class="ab-badge accent">shipped</span></h3>
    <p><span class="pill size-s">S</span>
       <code>(restore)</code> button on every history row for signed-in
       users. Re-commits the file's content at the chosen sha.</p>
  </li>
  <li>
    <time>v0.3</time>
    <h3>Annotated blame view</h3>
    <p><span class="pill size-l">L</span>
       Per-line author + commit hover. Useful for "who put this status
       to <em>done</em>?" without leaving the page. Cache the blame
       computation per (path, sha) — expensive without caching.</p>
  </li>
</ol>

<div class="category">Discovery &amp; navigation</div>
<ol class="timeline">
  <li>
    <time>shipped</time>
    <h3>Full-text search <span class="ab-badge accent">shipped</span></h3>
    <p><span class="pill size-m">M</span>
       SQLite FTS5 index, rebuilt on every push. <code>/?q=needle</code>
       renders <code>&lt;mark&gt;</code>-highlighted hits.</p>
  </li>
  <li>
    <time>shipped</time>
    <h3>Sidebar nested folders <span class="ab-badge accent">shipped</span></h3>
    <p><span class="pill size-s">S</span>
       Recurses 3 levels via <code>&lt;details&gt;</code>. Active folder
       auto-expands.</p>
  </li>
  <li>
    <time>v0.2</time>
    <h3>Recently-edited surface</h3>
    <p><span class="pill size-s">S</span>
       Home-page card "recent activity" listing the 10 most-recently
       committed files. Cheap addition on top of the existing
       <code>git log</code> machinery.</p>
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
    <time>shipped</time>
    <h3>In-browser edit <span class="ab-badge accent">shipped</span></h3>
    <p><span class="pill size-l">L</span>
       <code>(edit)</code> link in the meta-bar; CSRF + cookie-gated
       POST round-trips through gitserver.PutFile. Includes
       create-new-file via the directory listing's "+ new file" form.</p>
  </li>
  <li>
    <time>shipped</time>
    <h3>Live page-changed toasts <span class="ab-badge accent">shipped</span></h3>
    <p><span class="pill size-s">S</span>
       Workspace pushes broadcast over SSE; dashboard pops a Reload
       toast.</p>
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
    <h3>Optimistic concurrency on save</h3>
    <p><span class="pill size-m">M</span>
       Edit form embeds the file's current sha; the POST handler
       rejects with a conflict UI if the sha changed under the
       editor's feet. Standard CAS pattern on top of the existing
       PutFile flow.</p>
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

// SeededLogoSVG is a tiny demo logo committed at /assets/logo.svg.
// Demonstrates how the dashboard serves binary-ish files (SVG is text
// underneath but renders as an image): the renderer sets the right
// content-type and the page embeds it via <img src="/assets/logo.svg">.
// Authored as inline SVG so the seed stays text-only — no binary
// blobs in the constants table.
var SeededLogoSVG = seededLogoSVG

const seededLogoSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 60" role="img" aria-label="AgentBoard logo">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#2563eb"/>
      <stop offset="1" stop-color="#a855f7"/>
    </linearGradient>
  </defs>
  <rect x="4" y="4" width="52" height="52" rx="10" fill="url(#g)"/>
  <text x="14" y="38" font-family="ui-monospace,monospace" font-size="22" font-weight="700" fill="white">ab</text>
  <text x="70" y="38" font-family="system-ui,sans-serif" font-size="20" font-weight="600" fill="#1a1d24">AgentBoard</text>
</svg>
`

// SeededChartSVG is a small bar-chart-style demo at /assets/chart.svg.
// Shows that data viz works fine as authored SVG — no extra component
// system needed.
var SeededChartSVG = seededChartSVG

const seededChartSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 240 140" role="img" aria-label="Sprint outcome bar chart">
  <style>
    .bar { fill: #2563eb; }
    .bar.warn { fill: #d97706; }
    .label { font: 10px ui-sans-serif, system-ui, sans-serif; fill: #5c6470; }
    .axis { stroke: #e1e4ea; stroke-width: 1; }
  </style>
  <line class="axis" x1="40" y1="110" x2="230" y2="110"/>
  <line class="axis" x1="40" y1="20" x2="40" y2="110"/>
  <rect class="bar"      x="55"  y="40" width="28" height="70"/>
  <rect class="bar"      x="93"  y="55" width="28" height="55"/>
  <rect class="bar warn" x="131" y="80" width="28" height="30"/>
  <rect class="bar"      x="169" y="25" width="28" height="85"/>
  <text class="label" x="69"  y="125" text-anchor="middle">auth</text>
  <text class="label" x="107" y="125" text-anchor="middle">qol</text>
  <text class="label" x="145" y="125" text-anchor="middle">infra</text>
  <text class="label" x="183" y="125" text-anchor="middle">docs</text>
  <text class="label" x="20"  y="25"  text-anchor="end">100</text>
  <text class="label" x="20"  y="113" text-anchor="end">0</text>
</svg>
`

// SeededFlowSVG is a small node-and-arrow flow diagram demonstrating
// authored SVG for architecture docs. Lives at /assets/flow.svg.
var SeededFlowSVG = seededFlowSVG

const seededFlowSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 380 140" role="img" aria-label="Request flow: browser → server → worktree">
  <defs>
    <marker id="arr" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
      <path d="M0,0 L10,5 L0,10 z" fill="#5c6470"/>
    </marker>
    <style>
      .node { fill: #f7f8fa; stroke: #2563eb; stroke-width: 1.5; }
      .label { font: 12px ui-sans-serif, system-ui, sans-serif; fill: #1a1d24; text-anchor: middle; }
      .small { font: 10px ui-monospace, monospace; fill: #5c6470; }
      .edge { stroke: #5c6470; stroke-width: 1.5; fill: none; }
    </style>
  </defs>
  <rect class="node" x="10"  y="50" width="90" height="40" rx="6"/>
  <text class="label" x="55"  y="74">browser</text>
  <rect class="node" x="145" y="50" width="90" height="40" rx="6"/>
  <text class="label" x="190" y="74">server</text>
  <rect class="node" x="280" y="50" width="90" height="40" rx="6"/>
  <text class="label" x="325" y="74">worktree</text>
  <path class="edge" d="M100,70 L143,70" marker-end="url(#arr)"/>
  <path class="edge" d="M235,70 L278,70" marker-end="url(#arr)"/>
  <text class="small" x="121" y="62" text-anchor="middle">GET /file</text>
  <text class="small" x="256" y="62" text-anchor="middle">read</text>
  <path class="edge" d="M278,90 Q190,130 100,90" marker-end="url(#arr)"/>
  <text class="small" x="190" y="125" text-anchor="middle">rendered HTML</text>
</svg>
`

// SeededAvatarSVG is a generic circular-gradient avatar placeholder
// at /assets/avatar.svg, demoing image use beyond logos + charts.
var SeededAvatarSVG = seededAvatarSVG

const seededAvatarSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" role="img" aria-label="Avatar placeholder">
  <defs>
    <radialGradient id="a" cx="35%" cy="35%" r="70%">
      <stop offset="0" stop-color="#fcd34d"/>
      <stop offset="1" stop-color="#d97706"/>
    </radialGradient>
  </defs>
  <circle cx="32" cy="32" r="30" fill="url(#a)"/>
  <text x="32" y="40" text-anchor="middle" font-family="ui-sans-serif, system-ui" font-size="22" font-weight="600" fill="#1a1d24">A</text>
</svg>
`

// SeededSampleCSV is a small CSV demonstrating how the dashboard
// serves data files: rendered as preformatted text inline. Agents
// authoring CSV reports can drop them in alongside their docs.
var SeededSampleCSV = seededSampleCSV

const seededSampleCSV = `# Sprint 14 — outcome by category (illustrative data)
category,planned,shipped
auth,3,3
qol,7,7
substrate,4,4
docs,2,2
`

// SeededSampleTXT is a plain-text README-style note showing that
// arbitrary .txt files render as preformatted text. Useful for log
// excerpts, paste-from-terminal output, etc.
var SeededSampleTXT = seededSampleTXT

const seededSampleTXT = `AgentBoard files-as-files demo
==============================

Plain .txt files render as preformatted text inside the shell.
Useful for:

  - terminal output pasted from a script
  - tab-separated tables you don't want HTML-formatted
  - log excerpts you want preserved verbatim
  - quick notes-to-self that don't deserve a markdown file

No syntax highlighting — that's the trade-off. For code samples or
prose, prefer .md or .html.
`

// SeededFilesDemoHTML walks visitors through the file-type matrix:
// markdown, HTML, JSON, images, CSV, plain text, history/diff/restore
// on any of them. Linked from the home page as "what files render how".
var SeededFilesDemoHTML = seededFilesDemoHTML

const seededFilesDemoHTML = `<!doctype html>
<title>File types</title>
<style>
  .matrix { width: 100%; border-collapse: collapse; margin: 1.5rem 0;
    font-size: .9rem; }
  .matrix th { text-align: left; font-size: .75rem;
    text-transform: uppercase; letter-spacing: .05em;
    color: var(--text-secondary); padding: .5rem .65rem;
    border-bottom: 1px solid var(--border); }
  .matrix td { padding: .65rem; vertical-align: top;
    border-bottom: 1px solid var(--border); }
  .matrix td:first-child { font-family: var(--ab-mono, monospace);
    color: var(--accent); white-space: nowrap; }
  .matrix tr:last-child td { border-bottom: 0; }
  .demo-img { background: var(--bg-secondary);
    border: 1px solid var(--border); border-radius: var(--ab-radius);
    padding: 1rem; text-align: center; margin: 1rem 0; }
  .demo-img img { max-width: 240px; height: auto; }
</style>

<h1>File types</h1>
<p class="ab-muted">Every file in the workspace is served as-is — the
renderer picks behavior from the extension. Authors drop bytes; the
dashboard surfaces them with the right content-type and the right
chrome.</p>

<h2>Rendered demos</h2>

<p class="ab-muted">Each tile below uses a different embed pattern so
you can see how the renderer handles image fetches in different
situations.</p>

<div class="img-grid">

  <div class="demo-img">
    <img src="/assets/logo.svg" alt="AgentBoard logo">
    <div class="caption">
      <code>&lt;img src="/assets/logo.svg"&gt;</code><br>
      Bare URL — modern browsers set <code>Sec-Fetch-Dest: image</code>
      so the server returns raw bytes automatically.
    </div>
  </div>

  <div class="demo-img">
    <img src="/assets/chart.svg" alt="Bar chart">
    <div class="caption">
      <code>&lt;img src="/assets/chart.svg"&gt;</code><br>
      Authored SVG can carry its own styles + data — no chart library
      required.
    </div>
  </div>

  <div class="demo-img">
    <img src="/assets/flow.svg?raw=1" alt="Request flow">
    <div class="caption">
      <code>?raw=1</code> — explicit override. Forces raw bytes even
      if the request looks like a top-level navigation. Useful in
      background-image CSS where headers aren't reliable.
    </div>
  </div>

  <div class="demo-img">
    <img src="/assets/avatar.svg" alt="Avatar" style="max-width:96px">
    <div class="caption">
      Avatar placeholder. Small SVG, same content-type, embeds at any
      size the page wants.
    </div>
  </div>

  <div class="demo-img">
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 60" role="img" style="max-width:200px">
      <rect width="200" height="60" rx="6" fill="#2563eb"/>
      <text x="100" y="38" text-anchor="middle" font-family="ui-monospace,monospace" font-size="14" fill="white">inline &lt;svg&gt;</text>
    </svg>
    <div class="caption">
      Inline <code>&lt;svg&gt;</code> in HTML — no network fetch, no
      content-type negotiation, fastest to render. Pick this for tiny
      glyphs and badges.
    </div>
  </div>

  <div class="demo-img" style="background:none;border:0;padding:0;text-align:left">
    <div style="background:linear-gradient(135deg, #2563eb, #a855f7);height:120px;border-radius:8px;display:flex;align-items:center;justify-content:center;color:white;font-weight:600">
      CSS gradient
    </div>
    <div class="caption">
      Sometimes you don't need an image at all. CSS gradients +
      pseudo-elements cover most "decorative tile" use cases.
    </div>
  </div>

</div>

<style>
  .img-grid { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); margin: 1.5rem 0; }
  .demo-img .caption { margin-top: .65rem; font-size: .8rem; color: var(--text-secondary); line-height: 1.4; }
  .demo-img .caption code { font-size: .85em; }
</style>

<h2>What renders how</h2>

<table class="matrix">
  <thead>
    <tr><th>Extension</th><th>Rendered as</th><th>Try it</th></tr>
  </thead>
  <tbody>
    <tr>
      <td>.md</td>
      <td>Markdown → HTML via goldmark, wrapped in the shell.
          Headings, tables, code blocks, GFM.</td>
      <td><a href="/README.md">/README.md</a></td>
    </tr>
    <tr>
      <td>.html</td>
      <td>Authored HTML — body inlined into the shell. <code>&lt;title&gt;</code>
          extracted; <code>&lt;style&gt;</code> blocks honored; design-system
          tokens available.</td>
      <td><a href="/pages/getting-started.html">/pages/getting-started.html</a></td>
    </tr>
    <tr>
      <td>.json</td>
      <td>Pretty-printed inside a <code>&lt;pre&gt;</code>. For a kanban,
          write an HTML page with CSS grid — same visual outcome,
          fewer rules.</td>
      <td>—</td>
    </tr>
    <tr>
      <td>.svg / .png / .jpg</td>
      <td>Image content-type. Embed via
          <code>&lt;img src="/assets/logo.svg"&gt;</code> or link
          directly.</td>
      <td><a href="/assets/logo.svg">/assets/logo.svg</a></td>
    </tr>
    <tr>
      <td>.csv</td>
      <td>Served as <code>text/plain</code>; preformatted inside the
          shell. Drop spreadsheet-style data here for at-a-glance
          inspection.</td>
      <td><a href="/data/sprint-14.csv">/data/sprint-14.csv</a></td>
    </tr>
    <tr>
      <td>.txt / .ndjson</td>
      <td>Plain text or NDJSON, served verbatim. Useful for log
          excerpts and tab-separated tables.</td>
      <td><a href="/data/scratch.txt">/data/scratch.txt</a></td>
    </tr>
    <tr>
      <td>(directory)</td>
      <td>Directory listing with files + subfolders. If
          <code>index.html</code> or <code>index.md</code> exists,
          serves that instead.</td>
      <td><a href="/pages/">/pages/</a></td>
    </tr>
    <tr>
      <td>anything else</td>
      <td><code>text/plain</code> fallback. Download to inspect.</td>
      <td>—</td>
    </tr>
  </tbody>
</table>

<h2>History + edit on every file</h2>

<p>Every file gains <code>(history)</code> and <code>(edit)</code>
links in the page-actions strip (signed-in users only). The history
view links into diffs and offers one-click restore. Editing commits
through <code>git push</code> under the hood — full provenance, no
ceremony.</p>

<p class="ab-muted" style="margin-top:2rem;font-size:.85rem">
  To add a new file type to the renderer, edit
  <code>internal/html/server.go</code> — the <code>renderFile</code>
  switch is the contract.
</p>
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
