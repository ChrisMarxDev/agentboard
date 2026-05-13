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
