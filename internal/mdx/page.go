package mdx

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/christophermarx/agentboard/internal/project"
)

// PageInfo represents a page in the project.
type PageInfo struct {
	Path      string `json:"path"`
	File      string `json:"file"`
	Title     string `json:"title"`
	Source    string  `json:"source"`
	Order     int    `json:"order"`
}

// PageManager manages MDX pages for a project.
type PageManager struct {
	project *project.Project
	mu      sync.RWMutex
	pages   map[string]*PageInfo // path -> page
}

// NewPageManager creates a new page manager.
func NewPageManager(proj *project.Project) *PageManager {
	pm := &PageManager{
		project: proj,
		pages:   make(map[string]*PageInfo),
	}
	pm.ScanPages()
	return pm
}

// ScanPages reads all .md files from the project and builds the page index.
func (pm *PageManager) ScanPages() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.pages = make(map[string]*PageInfo)

	// Scan index.md
	indexPath := pm.project.IndexFile()
	if content, err := os.ReadFile(indexPath); err == nil {
		title, source := parseFrontmatter(string(content))
		if title == "" {
			title = "Home"
		}
		pm.pages["index"] = &PageInfo{
			Path:   "/",
			File:   "index.md",
			Title:  title,
			Source: source,
			Order:  0,
		}
	}

	// Scan content/ directory
	contentDir := pm.project.ContentDir()
	order := 1
	filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(contentDir, path)
		pagePath := strings.TrimSuffix(relPath, ".md")
		urlPath := "/" + pagePath

		title, source := parseFrontmatter(string(content))
		if title == "" {
			title = strings.Title(strings.ReplaceAll(filepath.Base(pagePath), "-", " "))
		}

		pm.pages[pagePath] = &PageInfo{
			Path:   urlPath,
			File:   filepath.Join("content", relPath),
			Title:  title,
			Source: source,
			Order:  order,
		}
		order++
		return nil
	})
}

// ListPages returns all pages sorted by order.
func (pm *PageManager) ListPages() []PageInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pages := make([]PageInfo, 0, len(pm.pages))
	for _, p := range pm.pages {
		pages = append(pages, *p)
	}
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].Order < pages[j].Order
	})
	return pages
}

// GetPage returns a page by its path.
func (pm *PageManager) GetPage(pagePath string) *PageInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.pages[pagePath]
}

// WritePage creates or updates a page.
func (pm *PageManager) WritePage(pagePath string, source string) error {
	var filePath string
	if pagePath == "index" || pagePath == "index.md" {
		filePath = pm.project.IndexFile()
	} else {
		pagePath = strings.TrimSuffix(pagePath, ".md")
		filePath = filepath.Join(pm.project.ContentDir(), pagePath+".md")
	}

	// Ensure parent directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(filePath, []byte(source), 0644); err != nil {
		return err
	}

	// Re-scan to pick up changes
	pm.ScanPages()
	return nil
}

// DeletePage removes a page file and re-scans.
func (pm *PageManager) DeletePage(pagePath string) error {
	pagePath = strings.TrimSuffix(pagePath, ".md")
	filePath := filepath.Join(pm.project.ContentDir(), pagePath+".md")

	if err := os.Remove(filePath); err != nil {
		return err
	}

	pm.ScanPages()
	return nil
}

// MovePage renames/moves a page file. Returns os.ErrNotExist if the source
// doesn't exist, and os.ErrExist if the destination already does — so callers
// can map those to 404 / 409 without inspecting the file system themselves.
func (pm *PageManager) MovePage(from, to string) error {
	from = strings.TrimSuffix(from, ".md")
	to = strings.TrimSuffix(to, ".md")

	srcPath := filepath.Join(pm.project.ContentDir(), from+".md")
	dstPath := filepath.Join(pm.project.ContentDir(), to+".md")

	if _, err := os.Stat(srcPath); err != nil {
		return err
	}
	if _, err := os.Stat(dstPath); err == nil {
		return os.ErrExist
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	if err := os.Rename(srcPath, dstPath); err != nil {
		return err
	}

	pm.ScanPages()
	return nil
}

// parseFrontmatter extracts title from YAML frontmatter and returns the remaining content.
func parseFrontmatter(content string) (title string, source string) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content
	}

	end := strings.Index(content[4:], "\n---\n")
	if end == -1 {
		return "", content
	}

	frontmatter := content[4 : 4+end]
	source = content[4+end+5:] // skip the closing ---\n

	// Simple title extraction from frontmatter
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "title:") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "title:"))
			title = strings.Trim(title, `"'`)
			break
		}
	}

	return title, source
}
