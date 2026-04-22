package mdx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/christophermarx/agentboard/internal/project"
)

// ErrStale is returned by WritePageIfMatch / DeletePageIfMatch when the
// caller's expected etag doesn't match the current page source. Handlers
// translate it to HTTP 412 with the current page in the body.
var ErrStale = errors.New("mdx: stale write — If-Match did not match current etag")

// ErrNotFoundForMatch signals that an If-Match was set but the page doesn't
// exist. 412 with `current: null`.
var ErrNotFoundForMatch = errors.New("mdx: page not found for If-Match check")

// pageEtag derives a content-addressed etag from the raw source. First 16
// hex chars of sha256 — short enough for headers, long enough to rule out
// accidental collisions within a project.
func pageEtag(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])[:16]
}

// titleCase uppercases the first rune of each space-separated word.
// Replaces strings.Title (deprecated in Go 1.18) for the narrow filename-slug
// case: ASCII words separated by spaces, already lowercased upstream.
func titleCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	atStart := true
	for _, r := range s {
		if unicode.IsSpace(r) {
			b.WriteRune(r)
			atStart = true
			continue
		}
		if atStart {
			b.WriteRune(unicode.ToUpper(r))
			atStart = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PageInfo represents a page in the project.
type PageInfo struct {
	Path   string `json:"path"`
	File   string `json:"file"`
	Title  string `json:"title"`
	Source string `json:"source"`
	// Etag is a content-addressed version identifier — sha256(source) prefix.
	// Handlers echo it as the HTTP ETag header; callers send it back in
	// If-Match for optimistic concurrency.
	Etag  string `json:"etag"`
	Order int    `json:"order"`
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
			Etag:   pageEtag(source),
			Order:  0,
		}
	}

	// Scan content/ directory — collect first, then assign orders by
	// hierarchical path sort so `/foo` precedes `/foo/bar` (the parent
	// index page comes before its subtree, not after).
	contentDir := pm.project.ContentDir()
	type scanned struct {
		pagePath string
		info     *PageInfo
	}
	var collected []scanned
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
			title = titleCase(strings.ReplaceAll(filepath.Base(pagePath), "-", " "))
		}

		collected = append(collected, scanned{
			pagePath: pagePath,
			info: &PageInfo{
				Path:   urlPath,
				File:   filepath.Join("content", relPath),
				Title:  title,
				Source: source,
				Etag:   pageEtag(source),
			},
		})
		return nil
	})

	sort.Slice(collected, func(i, j int) bool {
		return lessHierarchical(collected[i].info.Path, collected[j].info.Path)
	})
	for i, s := range collected {
		s.info.Order = i + 1
		pm.pages[s.pagePath] = s.info
	}
}

// lessHierarchical orders URL paths so a parent index page comes before
// any of its descendants (e.g. /features before /features/auth,
// /features/components before /features/components/badge). Segments are
// compared lexicographically; when one path is a prefix of the other,
// the shorter path wins.
func lessHierarchical(a, b string) bool {
	as := strings.Split(strings.TrimPrefix(a, "/"), "/")
	bs := strings.Split(strings.TrimPrefix(b, "/"), "/")
	n := min(len(as), len(bs))
	for i := range n {
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
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

// WritePage creates or updates a page (last-write-wins).
func (pm *PageManager) WritePage(pagePath string, source string) error {
	return pm.WritePageIfMatch(pagePath, source, "")
}

// WritePageIfMatch is WritePage with an optimistic-concurrency precondition.
// When expectedEtag is non-empty, returns ErrStale if the current page etag
// doesn't match, or ErrNotFoundForMatch if the page doesn't exist yet.
func (pm *PageManager) WritePageIfMatch(pagePath, source, expectedEtag string) error {
	var filePath string
	normalized := pagePath
	if pagePath == "index" || pagePath == "index.md" {
		filePath = pm.project.IndexFile()
		normalized = "index"
	} else {
		normalized = strings.TrimSuffix(pagePath, ".md")
		filePath = filepath.Join(pm.project.ContentDir(), normalized+".md")
	}

	if expectedEtag != "" {
		pm.mu.RLock()
		current := pm.pages[normalized]
		pm.mu.RUnlock()
		if current == nil {
			return ErrNotFoundForMatch
		}
		if current.Etag != expectedEtag {
			return ErrStale
		}
	}

	// Ensure parent directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(filePath, []byte(source), 0644); err != nil {
		return err
	}

	// Re-scan to pick up changes (including the new etag)
	pm.ScanPages()
	return nil
}

// DeletePage removes a page file (last-write-wins).
func (pm *PageManager) DeletePage(pagePath string) error {
	return pm.DeletePageIfMatch(pagePath, "")
}

// DeletePageIfMatch is DeletePage with an optimistic-concurrency precondition.
func (pm *PageManager) DeletePageIfMatch(pagePath, expectedEtag string) error {
	normalized := strings.TrimSuffix(pagePath, ".md")
	filePath := filepath.Join(pm.project.ContentDir(), normalized+".md")

	if expectedEtag != "" {
		pm.mu.RLock()
		current := pm.pages[normalized]
		pm.mu.RUnlock()
		if current == nil {
			return ErrNotFoundForMatch
		}
		if current.Etag != expectedEtag {
			return ErrStale
		}
	}

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
