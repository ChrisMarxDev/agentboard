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
// nothing to do. See docs/archive/spec-knowledge.md §5.
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

// FilesDir returns the directory where uploaded assets live. Historically this
// was <project>/files/ — it now points at ContentDir() so pages and assets share
// one tree (see CORE_GUIDELINES §9). MigrateLegacyFilesDir handles the one-time
// move for projects that predate the consolidation.
func (p *Project) FilesDir() string {
	return p.ContentDir()
}

// LegacyFilesDir returns the pre-consolidation files/ directory path. Used only
// by the migration path; new code should use ContentDir().
func (p *Project) LegacyFilesDir() string {
	return filepath.Join(p.Path, "files")
}

// MigrateLegacyFilesDir merges <project>/files/* into <project>/content/* and
// removes the old files/ directory. Safe to call on every startup: returns
// (false, nil) when there's nothing to do. If a path collision occurs (same
// relative path exists in both), the content/ version wins and the files/
// version is logged and left in place with a `.bak` suffix.
func (p *Project) MigrateLegacyFilesDir() (bool, error) {
	legacy := p.LegacyFilesDir()
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		return false, nil
	}

	contentDir := p.ContentDir()
	if err := os.MkdirAll(contentDir, 0755); err != nil {
		return false, err
	}

	moved := false
	err := filepath.Walk(legacy, func(src string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(legacy, src)
		if err != nil {
			return err
		}
		dst := filepath.Join(contentDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		if _, err := os.Stat(dst); err == nil {
			bak := src + ".bak"
			log.Printf("agentboard: files/%s collides with content/%s — leaving legacy copy at %s", rel, rel, bak)
			return os.Rename(src, bak)
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
		moved = true
		return nil
	})
	if err != nil {
		return moved, err
	}

	// Remove now-empty legacy tree, skipping any .bak files that survived.
	_ = filepath.Walk(legacy, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || path == legacy {
			return nil
		}
		if info.IsDir() {
			_ = os.Remove(path) // only removes if empty
		}
		return nil
	})
	if entries, err := os.ReadDir(legacy); err == nil && len(entries) == 0 {
		_ = os.Remove(legacy)
	}

	if moved {
		log.Printf("agentboard: migrated %s/* → %s/*", legacy, contentDir)
	}
	return moved, nil
}

// IndexFile returns the path to index.md.
func (p *Project) IndexFile() string {
	return filepath.Join(p.Path, "index.md")
}

// BuildDir returns the .agentboard/build/ directory.
func (p *Project) BuildDir() string {
	return filepath.Join(p.DataDir(), "build")
}

// EnsureDirs creates all required directories for the project. Runs legacy
// migrations first so callers never see the old layout:
//   - pages/  → content/    (pre-knowledge-spec rename)
//   - files/* → content/*   (pages + assets consolidated per CORE_GUIDELINES §9)
func (p *Project) EnsureDirs() error {
	if _, err := p.MigrateLegacyPagesDir(); err != nil {
		return err
	}
	if _, err := p.MigrateLegacyFilesDir(); err != nil {
		return err
	}
	dirs := []string{
		p.DataDir(),
		p.BuildDir(),
		p.ContentDir(),
		p.ComponentsDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
