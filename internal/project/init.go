package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/christophermarx/agentboard/internal/data"
)

const welcomeIndexMd = `# Welcome to AgentBoard

This dashboard updates live as your agents work.
You're looking at MDX — markdown with embedded components.

## At a glance

<Deck>
  <Card title="Users">
    <Metric source="welcome.users" />
  </Card>
  <Card title="Status">
    <Status source="welcome.status" />
  </Card>
  <Card title="Onboarding">
    <Progress source="welcome.progress" />
  </Card>
</Deck>

## Try it

Run this in your terminal:

    agentboard set welcome.users 42

The number above updates in real time.

## Tasks

<Card title="Backlog" span={2}>
  <Kanban source="welcome.tasks" groupBy="status" />
</Card>

## Connect Claude

    claude mcp add agentboard http://localhost:3000/mcp

Then ask Claude to build you something. For example:

  "Track my daily reading progress with a chart."

## Learn more

- Edit this page: open index.md in your project folder
- Add pages and docs to content/
- Add custom components to components/
- Docs: https://agentboard.dev/docs
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

Anthropic-format skills hosted on this AgentBoard. Agents discover them via ` + "`GET /api/skills`" + ` (or the ` + "`agentboard_list_skills`" + ` MCP tool) and fetch a zip bundle from ` + "`GET /api/skills/<slug>`" + `.

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
Use ` + "`agentboard_write_file`" + ` with path ` + "`skills/my-skill/SKILL.md`" + ` and the SKILL.md body as content. The list above refreshes automatically when the file lands.
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

Every request except ` + "`GET /api/health`" + ` requires a token. You receive it from a human operator who minted it with:

` + "```" + `
agentboard --project <name> admin mint-admin <username>      # for admins
agentboard --project <name> admin rotate <username> <label>  # additional tokens
` + "```" + `

Pass it on every request:

` + "```" + `
Authorization: Bearer ab_<43 chars>
` + "```" + `

No token / revoked token / deactivated user → ` + "`401 Unauthorized`" + `. Don't retry without a fresh token. Don't fall through to disk writes. If you can't authenticate, stop and report it — that's a configuration problem for the human to fix, not something to route around.

## The write contract

- **REST or MCP only.** Use the ` + "`agentboard_*`" + ` MCP tools or POST/PUT/PATCH/DELETE to ` + "`/api/*`" + `. Period.
- **Optimistic concurrency** (when available): read first, get the ` + "`ETag`" + ` or ` + "`version`" + ` field, send it back as ` + "`If-Match`" + ` on the write. ` + "`412 Precondition Failed`" + ` means someone else wrote meanwhile — re-read, merge semantically, retry.
- **Never edit files under ` + "`<project>/content/`" + ` directly.** The file watcher would accept the edit and the UI would update, but the write has no actor, no history row, no activity entry, and can clobber concurrent edits silently. This is a product violation.

## Surfaces

| What | How to write | How to read |
|---|---|---|
| Data (key/value) | ` + "`agentboard_set`, `agentboard_merge`, `agentboard_append`" + ` | ` + "`agentboard_get`, `agentboard_list_keys`" + ` |
| Pages (MDX) | ` + "`agentboard_write_page`" + ` | ` + "`agentboard_read_page`, `agentboard_list_pages`" + ` |
| Files (binary) | ` + "`agentboard_write_file`" + ` | ` + "`agentboard_list_files`, GET /api/files/<name>" + ` |
| Skills | write a folder under ` + "`content/skills/<slug>/`" + ` with ` + "`SKILL.md`" + ` via the file API | ` + "`agentboard_list_skills`, `agentboard_get_skill`" + ` |

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

- **Data changes often, pages rarely.** Push live values to data keys; let page MDX reference them via ` + "`source`" + ` props.
- **Keep prose short.** Humans glance; they don't read essays.
- **Use components for visualizations**, prose for context. See ` + "`agentboard_list_components`" + ` for the catalog.
- **Errors surface on the dashboard** via the errors beacon — broken diagrams, missing images, bad data refs show up without anyone asking.

## Quick examples

See ` + "`examples.md`" + ` in this skill for common patterns (building a dashboard, appending to a log, hosting an image).
`

// seededSkillExamples is the companion examples.md that ships inside the
// seeded skill to demonstrate that skills can carry supporting files alongside
// the manifest.
const seededSkillExamples = `# AgentBoard examples

## Track a counter

` + "```" + `
agentboard_set({ key: "coffee.today", value: 0 })
agentboard_write_page({
  path: "coffee",
  source: "# Coffee\n\n<Metric source=\"coffee.today\" label=\"Cups\" />"
})
` + "```" + `

Later:
` + "```" + `
agentboard_merge({ key: "coffee", value: { today: 3 } })
` + "```" + `

## Append to a log

` + "```" + `
agentboard_append({ key: "deploys", item: { ts: "2026-04-21T10:00Z", msg: "Shipped v1.4" } })
` + "```" + `

Render it:

` + "```mdx" + `
<Log source="deploys" />
` + "```" + `

## Host an image

Upload binary content:
` + "```" + `
agentboard_write_file({ name: "banner.png", content_base64: "..." })
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

// SeedSampleData populates the welcome sample data.
func SeedSampleData(store data.DataStore) error {
	samples := map[string]interface{}{
		"welcome.users": 42,
		"welcome.progress": map[string]interface{}{
			"value": 3,
			"max":   10,
			"label": "Getting Started",
		},
		"welcome.status": map[string]interface{}{
			"state":  "running",
			"label":  "AgentBoard",
			"detail": "All systems go",
		},
		"welcome.tasks": []map[string]interface{}{
			{"id": "1", "title": "Install AgentBoard", "status": "done"},
			{"id": "2", "title": "Connect Claude", "status": "in_progress"},
			{"id": "3", "title": "Build your first dashboard", "status": "todo"},
		},
	}

	for key, val := range samples {
		jsonVal, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("marshal sample %s: %w", key, err)
		}
		if err := store.Set(key, jsonVal, "agentboard"); err != nil {
			return fmt.Errorf("seed %s: %w", key, err)
		}
	}

	return nil
}
