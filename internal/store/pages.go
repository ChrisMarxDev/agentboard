package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/christophermarx/agentboard/internal/project"
	"gopkg.in/yaml.v3"
)

// ErrPageStale is returned by WritePageIfMatch / DeletePageIfMatch when
// the caller's expected version doesn't match the current page. Handlers
// translate it to HTTP 412 with the current page in the body.
var ErrPageStale = errors.New("store: stale page write — If-Match did not match current _meta.version")

// ErrPageNotFoundForMatch signals that an If-Match was set but the page
// doesn't exist. 412 with `current: null`.
var ErrPageNotFoundForMatch = errors.New("store: page not found for If-Match check")

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
	// Summary is a short, agent-authored description pulled from the page's
	// YAML frontmatter (`summary:` field). Used by search to pre-expand
	// semantic surface — an agent writing the page knows its use cases
	// and mentions them here so search finds the page later.
	Summary string `json:"summary"`
	// Tags are authored in frontmatter (`tags: [foo, bar]`) and let search
	// filter by topic in addition to full-text matching.
	Tags []string `json:"tags"`
	// Version is the page's `_meta.version` — an RFC3339Nano timestamp
	// stamped by the server on every write. Handlers echo it as the
	// HTTP ETag header; callers send it back in If-Match for optimistic
	// concurrency. Spec §3: pages and data both CAS via `_meta.version`.
	Version string `json:"version"`
	// Etag is the legacy field name kept on the JSON wire for one
	// release so SPA + Bruno consumers don't break in lock-step with
	// this rename. Identical to Version. Drop after Cut 6.
	Etag  string `json:"etag"`
	Order int    `json:"order"`
	// Frontmatter is the full YAML frontmatter as a generic map. Used
	// by the view broker to splat user-authored fields into the page
	// bundle so `<Status source="title">` resolves against the page's
	// own frontmatter without an external store lookup. _meta is
	// stripped during parse — server-owned, never agent-readable here.
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
}

// PageManager manages MDX pages for a project. Pages share the same
// monotonic-version generator as the rest of the store so CAS works
// uniformly across pages and data leaves (spec §3).
type PageManager struct {
	project  *project.Project
	versions *VersionGen
	mu       sync.RWMutex
	pages    map[string]*PageInfo // path -> page
}

// NewPageManager creates a new page manager. The version generator is
// seeded from the highest `_meta.version` already on disk so a fresh
// boot never hands out a version that sorts before an existing leaf.
func NewPageManager(proj *project.Project) *PageManager {
	pm := &PageManager{
		project:  proj,
		versions: NewVersionGen(time.Time{}),
		pages:    make(map[string]*PageInfo),
	}
	pm.ScanPages()
	// Push the high-water mark to the latest observed page version.
	for _, p := range pm.pages {
		if p.Version != "" {
			pm.versions.Observe(p.Version)
		}
	}
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
		fm, source := parseFrontmatter(string(content))
		title := fm.Title
		if title == "" {
			title = "Home"
		}
		pm.pages["index"] = &PageInfo{
			Path:        "/",
			File:        "index.md",
			Title:       title,
			Source:      source,
			Summary:     fm.Summary,
			Tags:        fm.Tags,
			Frontmatter: fm.All,
			Version:     fm.Version,
			Etag:        fm.Version,
			Order:       0,
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

		// Anthropic-style skill manifest: when a folder contains `SKILL.md`,
		// treat it as the folder's index page. `content/skills/bruno/SKILL.md`
		// renders at `/skills/bruno` (not `/skills/bruno/SKILL`) — the file
		// name is a format requirement from the skill zip, not a URL.
		if filepath.Base(pagePath) == "SKILL" {
			parent := filepath.ToSlash(filepath.Dir(pagePath))
			if parent != "." && parent != "" {
				pagePath = parent
			}
		}
		urlPath := "/" + pagePath

		fm, source := parseFrontmatter(string(content))
		title := fm.Title
		if title == "" {
			title = titleCase(strings.ReplaceAll(filepath.Base(pagePath), "-", " "))
		}

		collected = append(collected, scanned{
			pagePath: pagePath,
			info: &PageInfo{
				Path:        urlPath,
				File:        filepath.Join("content", relPath),
				Title:       title,
				Source:      source,
				Summary:     fm.Summary,
				Tags:        fm.Tags,
				Frontmatter: fm.All,
				Version:     fm.Version,
				Etag:        fm.Version,
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

// WritePageIfMatch is WritePage with an optimistic-concurrency
// precondition expressed as `_meta.version`.
//
// expectedVersion semantics (spec §3 + §5):
//   - "" — no precondition. New leaves write through; existing leaves
//     are overwritten last-write-wins. (Spec §5: "PUT to a path with
//     no existing leaf MUST succeed without If-Match.")
//   - "*" — force-overwrite. Skips the version check entirely.
//   - any other value — strict CAS. ErrPageStale on mismatch,
//     ErrPageNotFoundForMatch when the page is gone.
//
// On every successful write the server stamps a fresh
// `_meta.version` into the frontmatter via pm.versions.Next() so the
// next reader observes the new monotonic timestamp.
func (pm *PageManager) WritePageIfMatch(pagePath, source, expectedVersion string) error {
	var filePath string
	normalized := pagePath
	if pagePath == "index" || pagePath == "index.md" {
		filePath = pm.project.IndexFile()
		normalized = "index"
	} else {
		normalized = strings.TrimSuffix(pagePath, ".md")
		filePath = filepath.Join(pm.project.ContentDir(), normalized+".md")
	}

	if expectedVersion != "" && expectedVersion != "*" {
		pm.mu.RLock()
		current := pm.pages[normalized]
		pm.mu.RUnlock()
		if current == nil {
			return ErrPageNotFoundForMatch
		}
		if current.Version != expectedVersion {
			return ErrPageStale
		}
	}

	// Stamp `_meta.version` into the frontmatter. The author-supplied
	// source may already carry an `_meta` block (echoed from a prior
	// read); strip everything except the version we're about to mint
	// so agents can't forge `created_at` / `modified_by`.
	stamped, err := stampPageVersion(source, pm.versions.Next())
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(filePath, []byte(stamped), 0644); err != nil {
		return err
	}

	// Re-scan to pick up the new version + any frontmatter changes.
	pm.ScanPages()
	return nil
}

// DeletePage removes a page file (last-write-wins).
func (pm *PageManager) DeletePage(pagePath string) error {
	return pm.DeletePageIfMatch(pagePath, "")
}

// DeletePageIfMatch is DeletePage with an optimistic-concurrency
// precondition. Empty expectedVersion or "*" force the delete; any
// other value must match the current `_meta.version`.
func (pm *PageManager) DeletePageIfMatch(pagePath, expectedVersion string) error {
	normalized := strings.TrimSuffix(pagePath, ".md")
	filePath := filepath.Join(pm.project.ContentDir(), normalized+".md")

	if expectedVersion != "" && expectedVersion != "*" {
		pm.mu.RLock()
		current := pm.pages[normalized]
		pm.mu.RUnlock()
		if current == nil {
			return ErrPageNotFoundForMatch
		}
		if current.Version != expectedVersion {
			return ErrPageStale
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

// NormalizePagePath turns a URL path or file path into the canonical key
// the PageManager indexes under: trailing `.md` stripped, and a
// `<folder>/SKILL` collapsed to `<folder>` so the Anthropic skill manifest
// convention round-trips with the page tree. Handlers that look up a
// freshly-written page (write/patch/delete) MUST go through this helper —
// otherwise SKILL.md writes silently miss every post-write hook
// (PageMeta, Search, PageRefs, etag echo, mention dispatch).
func NormalizePagePath(pagePath string) string {
	pagePath = strings.TrimSuffix(pagePath, ".md")
	if filepath.Base(pagePath) == "SKILL" {
		parent := filepath.ToSlash(filepath.Dir(pagePath))
		if parent != "." && parent != "" {
			return parent
		}
	}
	return pagePath
}

// AssemblePageSource builds an MDX file source from a frontmatter map and
// a body. Empty frontmatter writes no `---` block. Used by PATCH handlers
// that need to round-trip a structured frontmatter edit back to disk.
//
// Round-trip note: marshalling through map[string]any loses key order and
// comments. The body is passed through verbatim so prose whitespace is
// preserved exactly.
func AssemblePageSource(fm map[string]any, body string) (string, error) {
	if len(fm) == 0 {
		return body, nil
	}
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("---\n")
	out.Write(fmBytes)
	out.WriteString("---\n")
	if body != "" {
		if !strings.HasPrefix(body, "\n") {
			out.WriteString("\n")
		}
		out.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			out.WriteString("\n")
		}
	}
	return out.String(), nil
}

// frontmatter captures the authored metadata at the top of a page. Fields
// are YAML-unmarshaled from the block between two `---` lines. Missing
// fields stay at their zero value — agents can write a page without any
// of them and the server won't reject it (per #8); search quality just
// degrades for that page until a summary is filled in.
type frontmatter struct {
	Title   string   `yaml:"title"`
	Summary string   `yaml:"summary"`
	Tags    []string `yaml:"tags"`
	// Version is read from `_meta.version` in frontmatter. Used as the
	// canonical CAS token for pages (spec §3).
	Version string         `yaml:"-"`
	All     map[string]any `yaml:"-"` // full frontmatter for component source-resolution
}

// parseFrontmatter extracts the YAML frontmatter block and returns the
// remaining source. Unrecognized fields are ignored at the typed-struct
// layer but preserved in `All` (full map[string]any) so components like
// `<Status source="col" />` can resolve fields the page authored
// without forcing every field into the typed struct.
//
// Malformed YAML leaves the returned struct zero-valued and passes the
// raw content through as source so the page still renders.
func parseFrontmatter(content string) (fm frontmatter, source string) {
	if !strings.HasPrefix(content, "---\n") {
		return fm, content
	}

	end := strings.Index(content[4:], "\n---\n")
	if end == -1 {
		return fm, content
	}

	block := content[4 : 4+end]
	source = content[4+end+5:] // skip the closing ---\n

	// Typed parse for the well-known fields.
	_ = yaml.Unmarshal([]byte(block), &fm)
	// Untyped parse so the broker can splat user-authored fields into
	// the page bundle. _meta is dropped from the agent-readable map
	// (server-owned), but its `version` is pulled out separately so
	// callers can echo it for CAS.
	all := map[string]any{}
	if err := yaml.Unmarshal([]byte(block), &all); err == nil {
		if rawMeta, ok := all["_meta"].(map[string]any); ok {
			if v, ok := rawMeta["version"].(string); ok {
				fm.Version = v
			}
		}
		delete(all, "_meta")
		fm.All = all
	}
	return fm, source
}

// stampPageVersion takes a raw page source (frontmatter + body) and
// returns the same source with `_meta.version` set to `version` —
// stripping any agent-supplied `_meta` fields other than version
// itself. Server-owned: agents may echo version for CAS but cannot
// forge `_meta.modified_by` / `_meta.created_at` (spec §3).
//
// A source with no frontmatter gets one. A source with a frontmatter
// block that's missing `_meta` gets one inserted at the top of the
// block. Existing user-authored keys are preserved verbatim — only the
// `_meta` map is rewritten.
func stampPageVersion(source, version string) (string, error) {
	fm := map[string]any{}
	body := source
	if strings.HasPrefix(source, "---\n") {
		end := strings.Index(source[4:], "\n---\n")
		if end >= 0 {
			block := source[4 : 4+end]
			body = source[4+end+5:]
			if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
				return "", err
			}
		}
	}
	// Carry over only `created_at` from the agent-supplied _meta so
	// first-create vs subsequent-update can be distinguished by readers
	// that scrape it from disk; everything else is server-owned and
	// scrubbed here.
	createdAt := ""
	if existing, ok := fm["_meta"].(map[string]any); ok {
		if c, ok := existing["created_at"].(string); ok && c != "" {
			createdAt = c
		}
	}
	if createdAt == "" {
		createdAt = version
	}
	fm["_meta"] = map[string]any{
		"version":    version,
		"created_at": createdAt,
	}
	return AssemblePageSource(fm, body)
}
