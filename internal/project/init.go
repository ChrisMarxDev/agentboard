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
- Add pages to pages/
- Add custom components to components/
- Docs: https://agentboard.dev/docs
`

const welcomeConfig = `title: "AgentBoard"
port: 3000
theme: auto
history_retention_days: 30
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

	return proj, nil
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
