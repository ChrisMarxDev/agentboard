package store

import (
	"database/sql"
	"regexp"
	"sort"
	"strings"
)

// RefSet is the static dependency graph of a page: every data key and
// file URL the page references via literal attribute bindings.
//
// Extraction is deliberately conservative — only literal string
// attributes ("foo.bar" / 'foo.bar') are captured. Dynamic expressions
// ({expr}) are invisible; they're documented as unsupported for share
// visitors. Authenticated users hit data directly (for now) and don't
// rely on this extraction.
type RefSet struct {
	Data  []string // data keys: e.g. "dev.principles", "welcome.tasks"
	Files []string // file paths: e.g. "/api/files/banner.svg"
}

// Empty reports whether the set has no references at all.
func (r RefSet) Empty() bool {
	return len(r.Data) == 0 && len(r.Files) == 0
}

// Regexes compiled once at package load. source/src attribute forms we
// capture:
//
//	source="key"     source='key'
//	src="/api/files/name.svg"
//
// We reject the `source={expr}` JSX-expression form by anchoring on a
// quote character immediately after the `=`. Same for src.
var (
	reDataDQ = regexp.MustCompile(`\bsource\s*=\s*"([^"{}<>]+)"`)
	reDataSQ = regexp.MustCompile(`\bsource\s*=\s*'([^'{}<>]+)'`)
	reFileDQ = regexp.MustCompile(`\bsrc\s*=\s*"(/api/files/[^"{}<>]+)"`)
	reFileSQ = regexp.MustCompile(`\bsrc\s*=\s*'(/api/files/[^'{}<>]+)'`)
)

// folderAutowireTags lists JSX components that auto-attach to the
// rendering page's own folder collection when no `source` attribute
// is given. Any `<Kanban>` / `<List>` / `<Sheet>` without `source=`
// is treated as `source="<page-path>/"`.
var folderAutowireTags = []string{"Kanban", "List", "Sheet"}

// reFolderAutowire matches the open tag of an autowire-eligible
// component. We reconstruct the alternation at package load so adding a
// new component just means appending to folderAutowireTags. The body
// captures everything inside the tag up to the closing `>` so the
// caller can probe for an explicit `source=` and skip auto-attach.
var reFolderAutowire = regexp.MustCompile(
	`<(?:` + strings.Join(folderAutowireTags, "|") + `)\b([^>]*)>`,
)
var reExplicitSource = regexp.MustCompile(`\bsource\s*=`)
var reKanbanTag = regexp.MustCompile(`<Kanban\b`)

// ExtractRefs walks the MDX source of a page and returns the unique
// set of data-key and file references.
//
// `pagePath` is the page's normalised path (e.g. "tasks", "roadmap")
// used to compute auto-attach refs for `<Kanban>` / `<List>` tags
// without an explicit `source=`. Empty pagePath disables auto-attach.
func ExtractRefs(source, pagePath string) RefSet {
	dataSet := map[string]struct{}{}
	fileSet := map[string]struct{}{}
	collect := func(re *regexp.Regexp, bag map[string]struct{}) {
		for _, m := range re.FindAllStringSubmatch(source, -1) {
			v := strings.TrimSpace(m[1])
			if v == "" {
				continue
			}
			bag[v] = struct{}{}
		}
	}
	collect(reDataDQ, dataSet)
	collect(reDataSQ, dataSet)
	collect(reFileDQ, fileSet)
	collect(reFileSQ, fileSet)

	// Auto-attach: any folder-aware tag without an explicit source=
	// resolves to the page's own folder. Powers the no-arg
	// `<Kanban groupBy="col" />` form on a folder-index page.
	//
	// Kanban also reads its lane configuration from the page's own
	// frontmatter.columns (so users can rename + add lanes inline,
	// persisted to the page). Add `columns` as an implicit data key
	// whenever <Kanban> appears so the broker's frontmatter splat
	// surfaces it in the bundle.
	if pagePath != "" {
		for _, m := range reFolderAutowire.FindAllStringSubmatch(source, -1) {
			body := m[1]
			if reExplicitSource.MatchString(body) {
				continue
			}
			dataSet[pagePath+"/"] = struct{}{}
			break // one auto-attach key is enough; the page is its own folder
		}
		if reKanbanTag.MatchString(source) {
			dataSet["columns"] = struct{}{}
		}
	}

	out := RefSet{
		Data:  keysSorted(dataSet),
		Files: keysSorted(fileSet),
	}
	return out
}

func keysSorted(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// RefStore persists page_refs (kind, target) per page. Writes replace
// the whole row set for a page atomically.
type RefStore struct {
	db *sql.DB
}

// NewRefStore creates (and migrates) the page_refs table.
func NewRefStore(db *sql.DB) (*RefStore, error) {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS page_refs (
		page_path  TEXT NOT NULL,
		kind       TEXT NOT NULL CHECK (kind IN ('data','file','page')),
		target     TEXT NOT NULL,
		PRIMARY KEY (page_path, kind, target)
	) STRICT;
	CREATE INDEX IF NOT EXISTS idx_page_refs_target ON page_refs(kind, target);
	`)
	if err != nil {
		return nil, err
	}
	return &RefStore{db: db}, nil
}

// Record replaces the ref set for a page. Called on every page write.
func (s *RefStore) Record(pagePath string, refs RefSet) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM page_refs WHERE page_path = ?`, pagePath); err != nil {
		return err
	}
	for _, k := range refs.Data {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO page_refs (page_path, kind, target) VALUES (?, 'data', ?)`,
			pagePath, k,
		); err != nil {
			return err
		}
	}
	for _, f := range refs.Files {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO page_refs (page_path, kind, target) VALUES (?, 'file', ?)`,
			pagePath, f,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Delete removes all refs for a page. Called when a page is deleted.
func (s *RefStore) Delete(pagePath string) error {
	_, err := s.db.Exec(`DELETE FROM page_refs WHERE page_path = ?`, pagePath)
	return err
}

// GetForPage returns the refs for a single page.
func (s *RefStore) GetForPage(pagePath string) (RefSet, error) {
	return s.getForPrefix(pagePath, pagePath)
}

// GetForSubtree returns the union of refs for a page AND all its
// descendants. The share/public scope uses this directly — sharing
// /handbook covers /handbook/**.
func (s *RefStore) GetForSubtree(rootPath string) (RefSet, []string, error) {
	// Normalise: strip trailing slash, handle root.
	root := strings.TrimSuffix(rootPath, "/")
	// Match the root itself AND anything under it via the / separator.
	// Using (path = root OR path LIKE root/%) keeps the index happy.
	rows, err := s.db.Query(
		`SELECT DISTINCT page_path, kind, target
		 FROM page_refs
		 WHERE page_path = ? OR page_path LIKE ?`,
		root, root+"/%",
	)
	if err != nil {
		return RefSet{}, nil, err
	}
	defer rows.Close()
	dataSet := map[string]struct{}{}
	fileSet := map[string]struct{}{}
	pageSet := map[string]struct{}{}
	for rows.Next() {
		var pp, kind, target string
		if err := rows.Scan(&pp, &kind, &target); err != nil {
			return RefSet{}, nil, err
		}
		pageSet[pp] = struct{}{}
		switch kind {
		case "data":
			dataSet[target] = struct{}{}
		case "file":
			fileSet[target] = struct{}{}
		}
	}
	pages := keysSorted(pageSet)
	// Always include the root itself even if it had no refs rows.
	if _, ok := pageSet[root]; !ok {
		// Confirm the page actually exists before advertising it.
		// Caller can filter; we err on inclusive.
		pages = append([]string{root}, pages...)
	}
	return RefSet{Data: keysSorted(dataSet), Files: keysSorted(fileSet)}, pages, rows.Err()
}

// getForPrefix is a private helper kept for future single-page queries.
func (s *RefStore) getForPrefix(root, pattern string) (RefSet, error) {
	_ = pattern
	rows, err := s.db.Query(
		`SELECT kind, target FROM page_refs WHERE page_path = ?`, root,
	)
	if err != nil {
		return RefSet{}, err
	}
	defer rows.Close()
	dataSet := map[string]struct{}{}
	fileSet := map[string]struct{}{}
	for rows.Next() {
		var kind, target string
		if err := rows.Scan(&kind, &target); err != nil {
			return RefSet{}, err
		}
		switch kind {
		case "data":
			dataSet[target] = struct{}{}
		case "file":
			fileSet[target] = struct{}{}
		}
	}
	return RefSet{Data: keysSorted(dataSet), Files: keysSorted(fileSet)}, rows.Err()
}
