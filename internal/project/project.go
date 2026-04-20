package project

import (
	"log"
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

// ContentDir returns the content/ directory path — where MDX dashboards and
// knowledge docs live.
func (p *Project) ContentDir() string {
	return filepath.Join(p.Path, "content")
}

// PagesDir returns the legacy pages/ directory path.
//
// Deprecated: Use ContentDir(). Kept for one release cycle so EnsureDirs can
// detect and migrate projects created before the rename.
func (p *Project) PagesDir() string {
	return filepath.Join(p.Path, "pages")
}

// MigrateLegacyPagesDir renames pages/ → content/ if content/ does not already
// exist. Safe to call on every startup: returns (false, nil) when there's
// nothing to do. See spec-knowledge.md §5.
func (p *Project) MigrateLegacyPagesDir() (bool, error) {
	contentDir := p.ContentDir()
	pagesDir := p.PagesDir()

	if _, err := os.Stat(contentDir); err == nil {
		if _, err := os.Stat(pagesDir); err == nil {
			log.Printf("agentboard: both pages/ and content/ exist in %s — using content/, ignoring pages/", p.Path)
		}
		return false, nil
	}
	if _, err := os.Stat(pagesDir); os.IsNotExist(err) {
		return false, nil
	}
	if err := os.Rename(pagesDir, contentDir); err != nil {
		return false, err
	}
	log.Printf("agentboard: migrated %s → %s", pagesDir, contentDir)
	return true, nil
}

// ComponentsDir returns the components/ directory path.
func (p *Project) ComponentsDir() string {
	return filepath.Join(p.Path, "components")
}

// FilesDir returns the files/ directory path — where user-uploaded images,
// PDFs, exports, etc. live. Served via /api/files/*.
func (p *Project) FilesDir() string {
	return filepath.Join(p.Path, "files")
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
// Runs the pages/ → content/ migration first so callers never see the old layout.
func (p *Project) EnsureDirs() error {
	if _, err := p.MigrateLegacyPagesDir(); err != nil {
		return err
	}
	dirs := []string{
		p.DataDir(),
		p.BuildDir(),
		p.ContentDir(),
		p.ComponentsDir(),
		p.FilesDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
