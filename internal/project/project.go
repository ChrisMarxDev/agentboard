package project

import (
	"os"
	"path/filepath"
)

// Project represents an AgentBoard project on disk.
type Project struct {
	Path   string
	Config *Config
}

// DefaultProjectDir returns the default project directory.
func DefaultProjectDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agentboard", "default")
}

// NamedProjectDir returns the directory for a named project.
func NamedProjectDir(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agentboard", name)
}

// Load opens an existing project at the given path.
func Load(path string) (*Project, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return &Project{Path: path, Config: cfg}, nil
}

// DataDir returns the .agentboard runtime data directory.
func (p *Project) DataDir() string {
	return filepath.Join(p.Path, ".agentboard")
}

// DatabasePath returns the SQLite database path.
func (p *Project) DatabasePath() string {
	return filepath.Join(p.DataDir(), "data.sqlite")
}

// PagesDir returns the pages/ directory path.
func (p *Project) PagesDir() string {
	return filepath.Join(p.Path, "pages")
}

// ComponentsDir returns the components/ directory path.
func (p *Project) ComponentsDir() string {
	return filepath.Join(p.Path, "components")
}

// IndexFile returns the path to index.md.
func (p *Project) IndexFile() string {
	return filepath.Join(p.Path, "index.md")
}

// BuildDir returns the .agentboard/build/ directory.
func (p *Project) BuildDir() string {
	return filepath.Join(p.DataDir(), "build")
}

// EnsureDirs creates all required directories for the project.
func (p *Project) EnsureDirs() error {
	dirs := []string{
		p.DataDir(),
		p.BuildDir(),
		p.PagesDir(),
		p.ComponentsDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
