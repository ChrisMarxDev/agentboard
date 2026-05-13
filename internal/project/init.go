package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// BootstrapReadmeMd is exported so serve.go can also commit it into an
// existing workspace that pre-dates the §15 convention.
//
// bootstrapReadmeMd is written at the root of every fresh workspace.
// Per CORE_GUIDELINES §15 (The workspace teaches the agent) this is
// the file the agent reads first; everything else is reachable from
// the links here. Keep it short, link-heavy, durable.
var BootstrapReadmeMd = bootstrapReadmeMd

const bootstrapReadmeMd = `---
title: README
wide: false
---

# This workspace

You are inside an **AgentBoard workspace** — a shared git repo a small
team of humans and AI agents collaborates inside. A human reads this
page in their browser; an agent reads it after ` + "`git clone`" + `.

## If you are an agent, start here

1. Read [` + "`skills/agentboard/SKILL.md`" + `](/skills/agentboard/SKILL) — it
   teaches the protocol you'll use here (the MCP tools, the conventions
   for proposing changes, how conflicts surface).
2. Look at the open work — by default that's whichever folder this
   workspace is using as its task queue. Run ` + "`agentboard_workspaces`" + `
   over MCP to confirm the workspace id; the ` + "`SKILL.md`" + ` will tell
   you where the tasks folder lives.
3. Pick something, branch (` + "`git checkout -b feature/<slug>`" + `), commit,
   push. If your push is rejected because someone else got there first,
   pull, resolve the standard ` + "`<<<<<<<`" + ` markers, push again.

## If you are a human, start here

- Click around in the left nav. Pages are MDX files in this repo;
  edits land through ` + "`git push`" + ` (an agent's) or through the web
  editor (yours).
- The home page lives at ` + "`index.md`" + `. Edit that to describe
  what this workspace is for; agents will read your description in the
  bootstrap chain too.

## Convention

- Every workspace ships with this README, a ` + "`skills/agentboard/SKILL.md`" + `,
  and an empty home page. Add a ` + "`CONVENTIONS.md`" + ` (or a section
  here) when your team has rules worth writing down.
- Two structural rules from spec §7 + CORE_GUIDELINES §14: scalars live
  inline on the page that displays them, and the only legitimate
  cross-doc reference is a folder collection (` + "`<Kanban source=\"tasks/\" />`" + `).
- Agents: read what's here before you write. The workspace usually
  knows more than your prompt does.
`

const welcomeIndexMd = `# Welcome to AgentBoard

A single-binary knowledge and dashboarding surface for agent teams. Agents write pages, skills, files, and data via REST or MCP; humans browse a live web UI. Dashboards are one content type — docs, skills, and runbooks live alongside them as equals in the same tree.

You're looking at MDX — markdown with embedded React components. The page updates live as data behind it changes.

## Connect Claude

` + "```" + `
claude mcp add agentboard http://localhost:3000/mcp
` + "```" + `

Then ask Claude to build you something:

> "Set up a kanban board for my reading list under /reading."

It'll write pages and data via the API; this site updates live.

## One namespace, many shapes

Every leaf lives at ` + "`/api/<path>`" + ` — pages, scalar values, collection items, streams, binaries. There is no separate ` + "`data/`" + ` prefix to write into; the server routes to the right backend based on path shape and content-type. Custom JSX components and skills are special-cased folders, not separate APIs.

A leaf takes one of these shapes; you pick by what you write, not where:

- **Page** — ` + "`.md`" + ` with YAML frontmatter and an MDX body. The page renders at ` + "`/<path>`" + ` in the browser.
- **Singleton / collection item** — ` + "`.md`" + ` with frontmatter only (no body), or JSON ` + "`{\"value\": …}`" + `. Reads return the structured value; ` + "`<Metric source=\"<key>\" />`" + ` resolves to it.
- **Stream** — ` + "`POST /api/<path>:append`" + ` adds an NDJSON line; reads tail the file. Used for activity feeds, logs.
- **Binary** — ` + "`agentboard_request_file_upload`" + ` mints a presigned ` + "`PUT`" + ` URL.
- **Component** — drop a ` + "`.jsx`" + ` into ` + "`<project>/components/`" + ` and the watcher registers it.
- **Skill** — ` + "`<project>/content/skills/<slug>/SKILL.md`" + ` with ` + "`name`" + ` + ` + "`description`" + ` frontmatter; appears in ` + "`GET /api/skills`" + `.

## Conventions worth knowing

- **Inline first.** A scalar shown in one place lives inline (` + "`<Metric value={14} label=\"Users\" />`" + `), not in its own leaf. If two pages need the same value, denormalize OR use a folder collection (` + "`<Kanban source=\"tasks/\" />`" + `) — per spec §7 those are the only legitimate cross-doc references.
- **No invented prefixes.** Don't write to ` + "`data/<key>`" + ` or ` + "`metrics/<key>/data/<x>`" + ` — pick the path you want the leaf to *appear* at and write there directly. The server decides whether it's a page or a singleton from the body.
- **Direct disk writes are a product violation.** They bypass auth, attribution, history, and concurrency. Always go through REST or MCP.

## Learn more

- Browse the ` + "`agentboard`" + ` skill for the full agent contract (` + "`/skills/agentboard/SKILL`" + `).
- Source: <https://github.com/ChrisMarxDev/agentboard>
`

const welcomeConfig = `title: "AgentBoard"
port: 3000
theme: auto
history_retention_days: 30
`

// skillsPageMd is seeded at content/skills.md on first-run init. It's an
// authored MDX page (not a hardcoded React route) that mounts the generic
// <ApiList/> built-in against /api/skills — see CORE_GUIDELINES §9.
const skillsPageMd = `# Skills

Anthropic-format skills hosted on this AgentBoard. Agents discover them via ` + "`GET /api/skills`" + ` (or ` + "`agentboard_list({path: \"skills/\"})`" + ` over MCP) and fetch a zip bundle from ` + "`GET /api/skills/<slug>`" + `.

A skill is any folder under ` + "`content/skills/<slug>/`" + ` containing ` + "`SKILL.md`" + ` with ` + "`name`" + ` and ` + "`description`" + ` in YAML frontmatter. Uploads go through the ` + "`/api/files/`" + ` endpoint; on disk they land in the content tree (content/ and files/ are one consolidated folder — see CORE_GUIDELINES §9). Nothing on disk is marked as special — the location + manifest are the only signal.

<Card title="Registered skills">
  <ApiList
    src="/api/skills"
    titleKey="name"
    descriptionKey="description"
    idKey="slug"
    downloadPrefix="/api/skills/"
    empty="No skills hosted yet. Write one at content/skills/<slug>/SKILL.md via PUT /api/files/skills/<slug>/SKILL.md."
    refreshOn="agentboard:file-updated"
  />
</Card>

## How to add one

<Card title="Upload via REST">

    curl -X PUT http://localhost:3000/api/files/skills/my-skill/SKILL.md \
      --data-binary @SKILL.md

</Card>

<Card title="Or via MCP">
Use ` + "`agentboard_write({items: [{path: \"skills/my-skill/SKILL\", frontmatter: {name, description}, body: \"…\"}]})`" + `. The list above refreshes automatically when the file lands.
</Card>
`

// SeededSkillManifest exposes the seeded SKILL content so admin
// tooling can re-push it into an existing workspace whenever the seed
// evolves. Mirrors BootstrapReadmeMd.
var SeededSkillManifest = seededSkillManifest

// seededSkillManifest is the SKILL.md seeded under content/skills/agentboard/
// of every fresh workspace. Per CORE_GUIDELINES §15 and spec §1.5 this is
// what the bootstrap README points the agent at — the place the agent
// learns the protocol. Keep it accurate; the §15 dogfood test is the
// canary that catches drift.
const seededSkillManifest = `---
name: agentboard
description: How to use AgentBoard as an agent — clone the workspace, follow the conventions, propose changes via git or MCP, resolve conflicts. Read this first after the README.
---

# AgentBoard for agents

You are inside an **AgentBoard workspace** — a shared git repo a team
of humans and AI agents collaborates inside. The substrate is git;
the wire format you write through is either ` + "`git`" + ` (preferred when your
runtime can shell out) or the seven ` + "`agentboard_*`" + ` MCP tools (the
git-less fallback).

## The contract in one paragraph

The workspace tells you what to do. Clone it, read README.md at the
root, follow the chain of references. Work happens on branches: you
` + "`git clone`" + `, ` + "`git checkout -b feature/<slug>`" + `, edit files, ` + "`git commit`" + `, ` + "`git push`" + `.
If your push is rejected (someone else pushed first), pull, resolve
the standard ` + "`<<<<<<<` / `=======` / `>>>>>>>`" + ` markers, push again. Conflicts
in MDX frontmatter resolve like conflicts in code — Claude is fluent
in this; treat it as ordinary work.

## Authentication

Every endpoint except ` + "`/api/health`" + ` needs a token. Format:
` + "`ab_<43 chars>`" + ` (bearer) or ` + "`oat_<…>`" + ` (audience-scoped OAuth, MCP only).
You get one by claiming an invitation URL — the operator gives you
the URL once on first use; redeem it via the browser or the
public ` + "`/api/invitations/<id>/redeem`" + ` endpoint.

For git operations the token rides as HTTP Basic auth:

` + "```bash" + `
git clone https://_:ab_<token>@<host>/git/<workspace>.git
` + "```" + `

The server advertises ` + "`Basic`" + ` on its 401 challenge so URL-embedded
creds work without an ` + "`extraHeader`" + ` workaround.

For REST + MCP, the standard form:

` + "```" + `
Authorization: Bearer ab_<token>
` + "```" + `

If you can't authenticate, **stop and report it**. Don't try to
sidestep auth by writing files directly into the worktree on disk —
that bypasses every guarantee the system makes (history, attribution,
concurrency, the SPA's live update).

## Two ways to work — pick the one that fits your runtime

### Path A — you have a ` + "`git`" + ` CLI (preferred)

` + "```bash" + `
git clone https://_:$AGENTBOARD_TOKEN@<host>/git/<workspace>.git
cd <workspace>
git checkout -b feature/<slug>
# edit files…
git add -A
git commit -m "<message>"
git push origin HEAD:main          # push-to-main mode (default)
# or, on always-PR workspaces:
git push origin HEAD:feature/<slug>
` + "```" + `

That's it. The server's working-tree mirror updates after every push;
the SPA's open browsers see your change within a second.

### Path B — your runtime can't shell out to git

Use the MCP tools. The full surface is **seven** tools; nothing else:

` + "```" + `
agentboard_workspaces            — list workspaces visible to this caller
agentboard_pull(ws, ref?)        — return the working tree as a bundle
                                   {files: [{path, frontmatter?, body, sha}]}
agentboard_propose(ws, base?,
                   branch?, files,
                   message)      — server-side branch + commit + push.
                                   files: [{path, body}]; body=null deletes.
agentboard_resolve_conflict(
   proposal, file, resolution)   — submit a resolved file body when a
                                   propose returned conflicts
agentboard_subscribe(events,
                     workspace?) — push / merge / conflict / mention events
agentboard_grab(picks)           — cross-leaf materializer; assembles
                                   the given paths into a single text blob
agentboard_fire_event(event,
                      payload?)  — emit on the webhook bus
` + "```" + `

Read tools call for single-leaf reads aren't here — ` + "`pull`" + ` is the
read primitive. If you need partial reads (just one file), filter
the bundle client-side.

## Folder collections — the most useful pattern

A folder of ` + "`.md`" + ` docs is a collection. ` + "`tasks/`" + ` cards make up the
` + "`tasks/`" + ` board. Components like ` + "`<Kanban>`, `<Sheet>`, `<List>`" + ` read
the folder directly via ` + "`source=\"tasks/\"`" + `.

**Auto-attach**: ` + "`<Kanban groupBy=\"col\" />`" + ` with no ` + "`source`" + ` prop on
a page resolves to that page's own folder. The page is the index of
its folder. Cleanest shape.

**Card frontmatter:**

` + "```yaml" + `
---
title: "Ship v2"
col: doing            # which lane
order: 1.5            # within-lane sort
assignees: [chris]
labels: [urgent]
priority: 1
---

Free-form prose body. The kanban surfaces labels, due date,
priority, and sub-task counts (cards pointing here via parent_id)
on the card face.
` + "```" + `

To move a card across lanes, edit ` + "`col`" + ` and push (or
` + "`agentboard_propose`" + ` if you can't push). One file changed; folder
structure unchanged.

**Lane config** lives in the page's frontmatter:

` + "```yaml" + `
---
title: "Marketing roadmap"
columns:
  - {id: inbox,   label: Inbox}
  - {id: review,  label: In review}
  - {id: shipped, label: Shipped this week}
---

<Kanban groupBy="col" />
` + "```" + `

## Conventions worth knowing

- **Inline first** (CORE_GUIDELINES §14). A scalar shown in one place
  lives inline in the page that displays it
  (` + "`<Metric value={14} label=\"Users\" />`" + `), not in a sibling file.
  Only folder collections may cross-reference: ` + "`<Kanban source=\"tasks/\" />`" + `.
- **No invented prefixes.** Don't write to ` + "`data/<key>`" + ` or invent
  parallel namespaces — pick the path you want the leaf to *appear*
  at and write there. The server picks the storage shape from path +
  content.
- **Read before you write.** ` + "`git pull --rebase origin main`" + ` (or
  ` + "`agentboard_pull`" + `) before you start. Conflicts that look like a
  rebase mid-air are easier to resolve than conflicts mid-push.
- **Keep prose short.** Humans glance; they don't read essays.
- **One container per content unit.** A page owns its content.

## Conflicts

Push rejected? Standard flow:

` + "```bash" + `
$ git push origin HEAD:main
! [rejected]        HEAD -> main (non-fast-forward)

$ git pull --rebase origin main
# resolve conflicts in your editor / by re-emitting the merged file…
$ git add -A
$ git rebase --continue
$ git push origin HEAD:main
` + "```" + `

The conflict markers (` + "`<<<<<<<` / `=======` / `>>>>>>>`" + `) are the repair
manual. They tell you which two versions disagreed; pick the merge
that preserves intent and push.

Via MCP, ` + "`agentboard_propose`" + ` returns a conflicts list on rejection;
call ` + "`agentboard_resolve_conflict(proposal, file, resolution)`" + ` for
each conflicted file, then the proposal retries.

## Quick examples

See ` + "`examples.md`" + ` in this skill for the common patterns (creating
a card, moving it, hosting an image, appending to a log).
`

// SeededSkillExamples exposes the seeded examples.md content so admin
// tooling can re-push it into an existing workspace whenever the seed
// evolves.
var SeededSkillExamples = seededSkillExamples

// seededSkillExamples is the companion examples.md that ships inside the
// seeded skill. Concrete recipes for the common operations, in both git
// and MCP form.
const seededSkillExamples = `# AgentBoard recipes

Every recipe below has two flavors — **git** (when your runtime can
shell out) and **MCP** (when it can't). Pick one. They do the same
thing.

## Create a card on the tasks board

### git
` + "```bash" + `
cd <clone>
mkdir -p tasks
cat > tasks/ship-v2.md <<EOF
---
title: "Ship v2"
col: todo
priority: 2
assignees: [chris]
---

What "ship v2" means: the new auth surface, the docs refresh, and the demo.
EOF
git add tasks/ship-v2.md
git commit -m "Add ship-v2 card"
git push origin HEAD:main
` + "```" + `

### MCP
` + "```" + `
agentboard_propose({
  workspace: "<ws>",
  message: "Add ship-v2 card",
  files: [{
    path: "tasks/ship-v2.md",
    body: "---\ntitle: \"Ship v2\"\ncol: todo\npriority: 2\nassignees: [chris]\n---\n\nWhat \"ship v2\" means…"
  }]
})
` + "```" + `

## Move a card across lanes

### git
Edit ` + "`tasks/ship-v2.md`" + `'s frontmatter ` + "`col:`" + ` field, commit, push. That's it.

### MCP
Pull the card via ` + "`agentboard_pull`" + `, edit the body locally to set
` + "`col: doing`" + `, propose with the new body. The whole-file write is
the unit; there is no field-level patch RPC in the git world.

## Track a metric you bump often

The whole point of CORE_GUIDELINES §14: scalars live inline on the
page that displays them.

` + "```mdx" + `
---
title: "Today"
coffee: 3
---

<Metric source="coffee" label="Cups" />
` + "```" + `

Bumping the count is a normal commit on this page's frontmatter.
Don't invent a ` + "`data/coffee.md`" + ` somewhere else — the
` + "`<Metric source=\"coffee\" />`" + ` reads the rendering page's own
frontmatter.

## Append to an activity feed

Streams are ` + "`.ndjson`" + ` files. Each push appends lines (atomic per
line). Read via ` + "`<Log source=\"deploys\" />`" + `.

### git
` + "```bash" + `
echo '{"ts":"2026-05-13T09:00Z","msg":"Shipped v1.4"}' >> deploys.ndjson
git add deploys.ndjson
git commit -m "deploy v1.4"
git push
` + "```" + `

### MCP
` + "```" + `
agentboard_propose({
  workspace: "<ws>",
  message: "deploy v1.4",
  files: [{
    path: "deploys.ndjson",
    body: "<existing-content>\n{\"ts\":\"2026-05-13T09:00Z\",\"msg\":\"Shipped v1.4\"}\n"
  }]
})
` + "```" + `

(MCP doesn't have a true append primitive yet — pull, append in
memory, propose the whole file. Git's append works concurrently;
MCP's "append" via propose is sequential.)

## Host an image / binary

Drop the file into the workspace alongside the markdown that
references it. ` + "`<Image src=\"/api/files/banner.png\" />`" + ` reads the
worktree mirror.

### git
` + "```bash" + `
cp banner.png images/banner.png
git add images/banner.png
git commit -m "Add team banner"
git push
` + "```" + `

Reference it in any page:

` + "```mdx" + `
<Image src="/api/files/images/banner.png" alt="Team banner" />
` + "```" + `

## Reacting to a teammate's push

Subscribe to events on the workspace:

` + "```" + `
agentboard_subscribe({ workspace: "<ws>" })
` + "```" + `

(In v1 this returns a snapshot; live streaming arrives in a later
cut.) For now, run ` + "`git fetch`" + ` periodically and look for new commits
on main.

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
	// Create project directory
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	// Write index.md (the page the SPA shows at /)
	indexPath := filepath.Join(projectPath, "index.md")
	if err := os.WriteFile(indexPath, []byte(welcomeIndexMd), 0644); err != nil {
		return nil, fmt.Errorf("write index.md: %w", err)
	}

	// Write README.md (the file agents read first per CORE_GUIDELINES §15
	// and spec §1.5). Distinct from index.md: README is for the bootstrap
	// chain, index.md is for the human's home page.
	readmePath := filepath.Join(projectPath, "README.md")
	if err := os.WriteFile(readmePath, []byte(bootstrapReadmeMd), 0644); err != nil {
		return nil, fmt.Errorf("write README.md: %w", err)
	}

	// Write agentboard.yaml
	configPath := filepath.Join(projectPath, "agentboard.yaml")
	if err := os.WriteFile(configPath, []byte(welcomeConfig), 0644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Load and ensure dirs
	proj, err := Load(projectPath)
	if err != nil {
		return nil, err
	}
	if err := proj.EnsureDirs(); err != nil {
		return nil, err
	}

	// Seed an example skill so the skills feature is self-documenting.
	if err := seedAgentboardSkill(projectPath); err != nil {
		return nil, fmt.Errorf("seed skill: %w", err)
	}

	// Seed the authored skills page that renders the /api/skills list.
	contentDir := filepath.Join(projectPath, "content")
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		return nil, fmt.Errorf("create content dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "skills.md"), []byte(skillsPageMd), 0644); err != nil {
		return nil, fmt.Errorf("seed content/skills.md: %w", err)
	}

	return proj, nil
}

// seedAgentboardSkill creates content/skills/agentboard/{SKILL.md, examples.md}
// with the seeded content. Called from InitProject; safe to call even if the
// folder already exists — overwrites only the two seeded files and leaves any
// other content alone.
func seedAgentboardSkill(projectPath string) error {
	skillDir := filepath.Join(projectPath, "content", "skills", "agentboard")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(seededSkillManifest), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(skillDir, "examples.md"), []byte(seededSkillExamples), 0644); err != nil {
		return err
	}
	return nil
}
