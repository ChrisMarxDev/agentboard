package project

import (
	"fmt"
	"os"
	"path/filepath"
)

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

## Where things live

- **Pages** — ` + "`<project>/content/<path>.md`" + ` (MDX with YAML frontmatter)
- **Data** — ` + "`<project>/data/<key>.{md,ndjson}`" + `, or a folder of pages for collections
- **Files (binary)** — ` + "`<project>/content/files/<name>`" + `; mint upload URLs via ` + "`agentboard_request_file_upload`" + `
- **Custom components** — drop a ` + "`.jsx`" + ` into ` + "`<project>/components/`" + `; the watcher registers it on the next render
- **Skills** — ` + "`<project>/content/skills/<slug>/SKILL.md`" + `; agents discover them via ` + "`GET /api/skills`" + `

## Conventions worth knowing

- **Inline first.** A scalar shown in one place lives inline (` + "`<Metric value={14} label=\"Users\" />`" + `), not in its own page. Per spec §7, only folder collections may cross-reference (` + "`<Kanban source=\"tasks/\" />`" + `).
- **One namespace.** ` + "`/api/<path>`" + ` covers pages, singletons, collection items, streams, and binaries. The 10-tool MCP surface dispatches the same way.
- **Direct disk writes are a product violation** — they bypass auth, attribution, history, and concurrency. Always go through REST or MCP.

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

// seededSkillManifest is the SKILL.md seeded under content/skills/agentboard/
// on first-run project init. It documents the skill-hosting convention by
// being an example of it, and teaches any agent that fetches it how to
// interact with AgentBoard. Format mirrors Anthropic's skill spec: YAML
// frontmatter with name + description, followed by free-form markdown.
const seededSkillManifest = `---
name: agentboard
description: How to use AgentBoard as an agent — authenticate, write data, author pages, manage files, and host skills. Fetch this skill first when asked to use an AgentBoard instance so you know the available surfaces, auth, and conventions.
---

# AgentBoard for agents

AgentBoard is a content surface for agent teams. You write — dashboards, docs, files, skills — humans read. Every write goes through the REST API or MCP. Never write directly to disk files even if the filesystem is reachable: direct writes bypass auth, rate limits, activity attribution, content_history, and optimistic concurrency.

## Authentication — do this first

Every request except ` + "`GET /api/health`" + ` requires a token. You get one by:

1. **First admin on a new board** — on first ` + "`agentboard serve`" + `, the server prints a
   ` + "`/invite/<id>`" + ` URL. Open it in a browser, pick a username, and receive the
   first admin token.
2. **Additional users** — an admin creates an invitation at ` + "`/admin`" + `, shares the
   ` + "`/invite/<id>`" + ` URL, and the invitee claims their account.
3. **Rotation** — ` + "`agentboard --project <name> admin rotate <username> <label>`" + ` mints
   a fresh token value for an existing token slot.

Pass it on every request:

` + "```" + `
Authorization: Bearer ab_<43 chars>
` + "```" + `

No token / revoked token / deactivated user → ` + "`401 Unauthorized`" + `. Don't retry without a fresh token. Don't fall through to disk writes. If you can't authenticate, stop and report it — that's a configuration problem for the human to fix, not something to route around.

## The write contract

- **REST or MCP only.** Use the ` + "`agentboard_*`" + ` MCP tools or POST/PUT/PATCH/DELETE to ` + "`/api/*`" + `. Period.
- **Optimistic concurrency** (when available): read first, get the ` + "`ETag`" + ` or ` + "`version`" + ` field, send it back as ` + "`If-Match`" + ` on the write. ` + "`412 Precondition Failed`" + ` means someone else wrote meanwhile — re-read, merge semantically, retry.
- **Never edit files under ` + "`<project>/content/`" + ` directly.** The file watcher would accept the edit and the UI would update, but the write has no actor, no history row, no activity entry, and can clobber concurrent edits silently. This is a product violation.

## MCP surface (10 tools, generic over paths)

The MCP server exposes a single CRUD surface that dispatches by path — pages, data singletons, streams, and folders all live in the same ` + "`/api/<path>`" + ` namespace. Always-plural batch shape: every call takes an array, partial success per item, native JSON values.

` + "```" + `
agentboard_read(paths)              — paths: [string]; returns one envelope per path
agentboard_list(path)               — folder children + frontmatter snippets
agentboard_search(q, scope?)        — full-text + substring across the tree
agentboard_write(items)             — items: [{path, frontmatter?, body?, version?}]
agentboard_patch(items)             — items: [{path, frontmatter_patch?, body?, version?}]
agentboard_append(path, items)      — items: [any]; one stream per call; race-free
agentboard_delete(items)            — items: [{path, version?}]
agentboard_request_file_upload(items) — items: [{name, size_bytes}]; returns presigned PUTs
agentboard_grab(picks)              — cross-page materializer; assembles agent-ready text
agentboard_fire_event(event, payload?) — emit on the webhook bus
` + "```" + `

REST equivalents are available at ` + "`PUT/PATCH/GET/DELETE /api/<path>`" + ` if you can't speak MCP.

**Single-item operations wrap in a one-element array** — there is no singular form. ` + "`agentboard_write({items: [{path, frontmatter}]})`" + ` for one write; same call shape for twenty.

**Admin operations are NOT on MCP.** Webhook subscribe/revoke/list, page locks, team management, user invitations all live on ` + "`/api/admin/*`" + ` and the ` + "`agentboard admin`" + ` CLI. MCP is the agent realm.

## Folder collections (the most useful pattern)

A folder of ` + "`.md`" + ` docs IS a collection. ` + "`content/tasks/<id>.md`" + ` cards make up the ` + "`tasks/`" + ` board. The components ` + "`<Kanban>`, `<Sheet>`, `<List>`" + ` read the folder directly.

**Auto-attach**: ` + "`<Kanban groupBy=\"col\" />`" + ` with **no source prop** on a page resolves to that page's own folder. The page is then the folder's index. This is the cleanest shape.

**Card shape.** Each card is a ` + "`.md`" + ` file under the board's folder. Frontmatter holds the structured fields the kanban groups / sorts / displays by; the MDX body is the free-form description.

` + "```yaml" + `
---
title: "Ship v2"
col: doing            # which lane
order: 1.5            # within-lane sort (floats avoid renumbering)
assignees: [chris]
---

Free-form description goes here. The card's detail-pane editor reads
and writes this body via ` + "`PATCH /api/content/<path>` `{body: \"...\"}`" + `.
Frontmatter and body are independent — patches to one don't touch the
other.
` + "```" + `

To move a card across lanes, ` + "`PATCH`" + ` only the field that changed:

` + "```bash" + `
curl -X PATCH "$B/api/content/tasks/ship-v2" \
  -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  -d '{"frontmatter_patch": {"col": "done"}}'
` + "```" + `

The body and every other frontmatter field are preserved. ` + "`null`" + ` deletes a key (RFC-7396).

**Lane configuration.** Default lanes are TODO / DOING / DONE. Override per-board by setting ` + "`columns`" + ` in the *page's* frontmatter (not the cards'):

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

Renames are pure presentation: change the ` + "`label`" + ` and existing cards keep their ` + "`col`" + ` ids. Adding a lane = appending a ` + "`{id, label}`" + ` object via ` + "`PATCH /api/content/<board> {frontmatter_patch:{columns:[...]}}`" + `.

If you've been asked to build a board, fetch ` + "`GET /api/skills/kanban`" + ` for a fully worked recipe.

## Hosting a skill

A skill is a folder under ` + "`content/skills/<slug>/`" + `. The folder must contain ` + "`SKILL.md`" + ` with YAML frontmatter:

` + "```yaml" + `
---
name: my-skill
description: One sentence explaining when to use this skill.
---
` + "```" + `

Supporting files (examples, scripts, references) go in the same folder. AgentBoard indexes skills by folder name; the frontmatter ` + "`name`" + ` is the human-readable title.

Share a skill with a teammate by sending them the URL ` + "`<server>/api/skills/<slug>`" + ` — it returns a zip they can unpack.

## Conventions

- **Data changes often, pages rarely.** Push live values to data-store keys; let page MDX reference them via ` + "`source`" + ` props.
- **Inline first.** A scalar that's only displayed in one place lives inline (` + "`<Metric value={14} label=\"Appointments\" />`" + `), not in its own page. Per spec §7, the only cross-doc ` + "`source`" + ` reference is a folder collection (e.g. ` + "`<Kanban source=\"tasks/\" />`" + `). Same-page frontmatter and data-store keys are also fine.
- **Keep prose short.** Humans glance; they don't read essays.
- **Use components for visualizations**, prose for context. The full catalog is documented in the AgentBoard repo's component reference.
- **Errors surface on the dashboard** via the errors beacon — broken diagrams, missing images, bad data refs show up without anyone asking.

## Quick examples

See ` + "`examples.md`" + ` in this skill for common patterns (building a dashboard, appending to a log, hosting an image).
`

// seededSkillExamples is the companion examples.md that ships inside the
// seeded skill to demonstrate that skills can carry supporting files alongside
// the manifest.
const seededSkillExamples = `# AgentBoard examples

Every example uses the ` + "`agentboard_*`" + ` MCP tools. REST equivalents at ` + "`/api/<path>`" + ` work the same.

## Track a counter

Create the page with the value inline (preferred for one-off scalars):

` + "```" + `
agentboard_write({
  items: [
    {
      path: "coffee",
      frontmatter: { title: "Coffee", today: 3 },
      body: "# Coffee\n\n<Metric source=\"today\" label=\"Cups\" />"
    }
  ]
})
` + "```" + `

The ` + "`<Metric source=\"today\" />`" + ` reads the page's own frontmatter — no cross-page lookup, no orphan singletons.

To bump it later:

` + "```" + `
agentboard_patch({
  items: [{ path: "coffee", frontmatter_patch: { today: 4 } }]
})
` + "```" + `

## Append to a log

Streams live in the same path namespace; ` + "`agentboard_append`" + ` writes one NDJSON line atomically.

` + "```" + `
agentboard_append({
  path: "deploys",
  items: [{ ts: "2026-04-21T10:00Z", msg: "Shipped v1.4" }]
})
` + "```" + `

Render it:

` + "```mdx" + `
<Log source="deploys" />
` + "```" + `

## Host an image

Files take a two-step upload — request a presigned PUT, then upload the bytes.

` + "```" + `
const { items } = await agentboard_request_file_upload({
  items: [{ name: "banner.png", size_bytes: 24576 }]
})
// items[0].url is a presigned PUT — upload the bytes via fetch().
` + "```" + `

Reference it:

` + "```mdx" + `
<Image src="/api/files/banner.png" alt="Team banner" />
` + "```" + `
`

// InitProject creates a new project from the welcome template.
func InitProject(projectPath string) (*Project, error) {
	// Create project directory
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	// Write index.md
	indexPath := filepath.Join(projectPath, "index.md")
	if err := os.WriteFile(indexPath, []byte(welcomeIndexMd), 0644); err != nil {
		return nil, fmt.Errorf("write index.md: %w", err)
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
