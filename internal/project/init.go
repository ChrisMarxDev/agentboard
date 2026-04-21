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

// seededSkillManifest is the SKILL.md seeded under files/skills/agentboard/
// on first-run project init. It documents the skill-hosting convention by
// being an example of it, and teaches any agent that fetches it how to
// interact with AgentBoard. Format mirrors Anthropic's skill spec: YAML
// frontmatter with name + description, followed by free-form markdown.
const seededSkillManifest = `---
name: agentboard
description: How to use AgentBoard as an agent — write data, author pages, manage files, and host skills. Fetch this skill first when asked to use an AgentBoard instance so you know the available surfaces and conventions.
---

# AgentBoard for agents

AgentBoard is a content surface for agent teams. You write — dashboards, docs, files, skills — humans read. Everything lives under the project folder and is served over REST and MCP.

## Surfaces

| What | How to write | How to read |
|---|---|---|
| Data (key/value) | ` + "`agentboard_set`, `agentboard_merge`, `agentboard_append`" + ` | ` + "`agentboard_get`, `agentboard_list_keys`" + ` |
| Pages (MDX) | ` + "`agentboard_write_page`" + ` | ` + "`agentboard_read_page`, `agentboard_list_pages`" + ` |
| Files (binary) | ` + "`agentboard_write_file`" + ` | ` + "`agentboard_list_files`, GET /api/files/<name>" + ` |
| Skills | write a folder under ` + "`files/skills/<slug>/`" + ` with ` + "`SKILL.md`" + ` | ` + "`agentboard_list_skills`, `agentboard_get_skill`" + ` |

## Hosting a skill

A skill is a folder under ` + "`files/skills/<slug>/`" + `. The folder must contain ` + "`SKILL.md`" + ` with YAML frontmatter:

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

	return proj, nil
}

// seedAgentboardSkill creates files/skills/agentboard/{SKILL.md, examples.md}
// with the seeded content. Called from InitProject; safe to call even if the
// folder already exists — overwrites only the two seeded files and leaves any
// other content alone.
func seedAgentboardSkill(projectPath string) error {
	skillDir := filepath.Join(projectPath, "files", "skills", "agentboard")
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
