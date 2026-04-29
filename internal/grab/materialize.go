// Package grab collects Card, heading, or whole-page sections across MDX
// pages into a single agent-ready payload. See docs/archive/spec-grab.md for the full
// design.
//
// The materializer is deliberately regex-based: AgentBoard authors already
// follow narrow conventions (<Card title="…">, ATX headings, MDX pages), a
// full MDX parser would bring in an AST dep for a single feature, and stale
// data is worse than slightly permissive matching. If a pick can't resolve,
// the formatter emits a comment and carries on — never blocks the whole copy.
package grab

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/christophermarx/agentboard/internal/mdx"
	"github.com/christophermarx/agentboard/internal/store"
)

// PickKind identifies which slicing strategy a Pick uses.
type PickKind string

const (
	KindCard    PickKind = "card"
	KindHeading PickKind = "heading"
	KindPage    PickKind = "page"
)

// Pick identifies one section to include. `Kind` routes to the slicer;
// legacy payloads without a Kind default to "card" for backward compatibility.
type Pick struct {
	Kind         PickKind `json:"kind"`
	Page         string   `json:"page"` // URL path, e.g. "/features/files"
	CardID       string   `json:"card_id,omitempty"`
	HeadingSlug  string   `json:"heading_slug,omitempty"`
	HeadingLevel int      `json:"heading_level,omitempty"`
}

// Section is one materialized pick.
type Section struct {
	Kind         PickKind    `json:"kind"`
	Page         string      `json:"page"`
	PageTitle    string      `json:"page_title"`
	CardTitle    string      `json:"card_title,omitempty"`
	CardID       string      `json:"card_id,omitempty"`
	HeadingText  string      `json:"heading_text,omitempty"`
	HeadingSlug  string      `json:"heading_slug,omitempty"`
	HeadingLevel int         `json:"heading_level,omitempty"`
	MDXSource    string      `json:"mdx_source"`
	Components   []Component `json:"components"`
	Missing      string      `json:"missing,omitempty"` // set when the pick couldn't resolve
}

// Component describes one data-bound component found inside the section.
type Component struct {
	Type      string      `json:"type"`       // e.g. "Mermaid", "Code"
	SourceKey string      `json:"source_key"` // data key the component reads
	Value     interface{} `json:"value"`      // resolved current value
	Language  string      `json:"language,omitempty"`
}

// Format is one of the three output shapes.
type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatXML      Format = "xml"
	FormatJSON     Format = "json"
)

// Materializer resolves picks using a PageManager + the files-first
// content store. The data lookup for component sources is best-effort:
// the store reads the same dotted-key path the agent wrote (singleton
// or collection-flattened) and unwraps the envelope before stitching
// the value into the materialised section.
type Materializer struct {
	Pages     *mdx.PageManager
	FileStore *store.Store
}

// Materialize returns the list of resolved sections for the given picks.
// Sections appear in pick order. Missing picks are included with Missing set.
// Picks whose byte range is fully contained in another same-page pick's
// range are dropped — see dedupe rules in materialize §Dedupe.
func (m *Materializer) Materialize(picks []Pick) []Section {
	// Resolve all picks first so we know their byte ranges.
	type resolved struct {
		section Section
		page    string
		start   int
		end     int
		keep    bool
	}
	items := make([]resolved, len(picks))
	pageCollision := false
	for i, p := range picks {
		s, start, end := m.materializeOne(p)
		items[i] = resolved{section: s, page: p.Page, start: start, end: end, keep: true}
		if !pageCollision {
			for j := 0; j < i; j++ {
				if items[j].page == p.Page {
					pageCollision = true
					break
				}
			}
		}
	}

	// Dedupe: sort indices by (page, start ASC, length DESC) so parents come
	// before children within a page. Walk; mark any item whose [start,end) is
	// fully contained within an earlier kept item on the same page as dropped.
	// Overlap is only possible between picks that share a page, so agents
	// gathering picks from distinct pages skip the sort + O(n²) walk entirely.
	if pageCollision {
		order := make([]int, len(items))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(i, j int) bool {
			a, b := items[order[i]], items[order[j]]
			if a.page != b.page {
				return a.page < b.page
			}
			if a.start != b.start {
				return a.start < b.start
			}
			return (a.end - a.start) > (b.end - b.start)
		})
		for ii, oi := range order {
			if !items[oi].keep || items[oi].section.Missing != "" || items[oi].end <= items[oi].start {
				continue
			}
			for jj := 0; jj < ii; jj++ {
				oj := order[jj]
				if !items[oj].keep || items[oj].page != items[oi].page {
					continue
				}
				if items[oj].start <= items[oi].start && items[oj].end >= items[oi].end {
					items[oi].keep = false
					break
				}
			}
		}
	}

	// Emit in original pick order, skipping dropped.
	out := make([]Section, 0, len(items))
	for _, it := range items {
		if it.keep {
			out = append(out, it.section)
		}
	}
	return out
}

// materializeOne resolves a single pick. Returns the section plus the byte
// range [start, end) the section occupies in the page's source, or (0, 0)
// when the pick is missing or applies to the whole page with no useful range
// for dedupe.
func (m *Materializer) materializeOne(p Pick) (Section, int, int) {
	// Tolerate legacy payloads without a kind: default to card.
	kind := p.Kind
	if kind == "" {
		kind = KindCard
	}

	key := strings.TrimPrefix(p.Page, "/")
	if key == "" {
		key = "index"
	}
	page := m.Pages.GetPage(key)
	if page == nil {
		return Section{
			Kind: kind, Page: p.Page,
			CardID: p.CardID, HeadingSlug: p.HeadingSlug,
			Missing: "page not found",
		}, 0, 0
	}

	switch kind {
	case KindCard:
		title, body, start, end, ok := findCardByID(page.Source, p.CardID)
		if !ok {
			return Section{
				Kind: kind, Page: p.Page, PageTitle: page.Title, CardID: p.CardID,
				Missing: "card id not found on page",
			}, 0, 0
		}
		return Section{
			Kind:       KindCard,
			Page:       p.Page,
			PageTitle:  page.Title,
			CardTitle:  title,
			CardID:     p.CardID,
			MDXSource:  strings.TrimSpace(body),
			Components: m.resolveComponents(body),
		}, start, end

	case KindHeading:
		text, body, level, start, end, ok := findHeadingBySlug(page.Source, p.HeadingSlug)
		if !ok {
			return Section{
				Kind: kind, Page: p.Page, PageTitle: page.Title,
				HeadingSlug: p.HeadingSlug,
				Missing:     "heading not found on page",
			}, 0, 0
		}
		return Section{
			Kind:         KindHeading,
			Page:         p.Page,
			PageTitle:    page.Title,
			HeadingText:  text,
			HeadingSlug:  p.HeadingSlug,
			HeadingLevel: level,
			MDXSource:    strings.TrimSpace(body),
			Components:   m.resolveComponents(body),
		}, start, end

	case KindPage:
		body := page.Source
		return Section{
			Kind:       KindPage,
			Page:       p.Page,
			PageTitle:  page.Title,
			MDXSource:  strings.TrimSpace(body),
			Components: m.resolveComponents(body),
		}, 0, len(body)
	}

	return Section{
		Kind: kind, Page: p.Page, PageTitle: page.Title,
		Missing: "unknown pick kind",
	}, 0, 0
}

// cardRegex matches a top-level <Card …>…</Card> block. Non-greedy body so
// multiple Cards on a page don't merge. Does NOT handle nested <Card> (rare
// in our authoring convention; documented limitation in docs/archive/spec-grab.md).
var cardRegex = regexp.MustCompile(`(?s)<Card\s([^>]*?)>(.*?)</Card>`)

// titleAttrRegex pulls the title="..." attr out of Card's attribute list.
var titleAttrRegex = regexp.MustCompile(`title=["']([^"']+)["']`)

// findCardByID walks every <Card title="..."> block in source, slugifies the
// title, and returns the block whose slug matches cardID. Returns the raw
// title, inner content, and byte range [start, end) of the whole <Card>…</Card>
// in source.
func findCardByID(source, cardID string) (title, body string, start, end int, ok bool) {
	matches := cardRegex.FindAllStringSubmatchIndex(source, -1)
	for _, m := range matches {
		attrs := source[m[2]:m[3]]
		inner := source[m[4]:m[5]]
		tm := titleAttrRegex.FindStringSubmatch(attrs)
		if len(tm) < 2 {
			continue
		}
		t := tm[1]
		if Slug(t) == cardID {
			return t, inner, m[0], m[1], true
		}
	}
	return "", "", 0, 0, false
}

// headingRegex matches ATX markdown headings at levels 1-3. Anchored to line
// start via multi-line flag. Excludes headings inside fenced code blocks —
// see isInsideFence().
var headingRegex = regexp.MustCompile(`(?m)^(#{1,3})\s+(.+?)\s*$`)

// findHeadingBySlug locates a heading whose slug matches headingSlug, then
// returns the text, body (heading line through the last line before the next
// heading of equal-or-higher level), the level, and the byte range.
//
// Fenced code blocks on an authoring page are rare, so we avoid copying the
// match slice up front just to exclude fenced headings — a single substring
// probe lets the common fence-free path skip every fence check entirely.
func findHeadingBySlug(source, headingSlug string) (text, body string, level, start, end int, ok bool) {
	matches := headingRegex.FindAllStringSubmatchIndex(source, -1)
	hasFence := strings.Contains(source, "```")

	for i, m := range matches {
		if hasFence && isInsideFence(source, m[0]) {
			continue
		}
		lvl := m[3] - m[2] // length of the '#' group
		t := strings.TrimSpace(source[m[4]:m[5]])
		if Slug(t) != headingSlug {
			continue
		}
		sectionStart := m[0]
		sectionEnd := len(source)
		for j := i + 1; j < len(matches); j++ {
			if hasFence && isInsideFence(source, matches[j][0]) {
				continue
			}
			nlvl := matches[j][3] - matches[j][2]
			if nlvl <= lvl {
				sectionEnd = matches[j][0]
				break
			}
		}
		return t, source[sectionStart:sectionEnd], lvl, sectionStart, sectionEnd, true
	}
	return "", "", 0, 0, 0, false
}

// isInsideFence reports whether byte offset off falls within a ```fenced
// code block``` in source. Simple toggle-counter: every ``` at column 0
// (outside strings) flips the state.
func isInsideFence(source string, off int) bool {
	inside := false
	pos := 0
	for pos < off {
		nl := strings.IndexByte(source[pos:], '\n')
		var line string
		if nl < 0 {
			line = source[pos:]
			pos = len(source)
		} else {
			line = source[pos : pos+nl]
			pos += nl + 1
		}
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") {
			inside = !inside
		}
	}
	return inside
}

// componentRegex matches self-closing and open/close tags of known data-bound
// built-ins with a source="..." attribute.
var componentRegex = regexp.MustCompile(
	`<(Metric|Counter|Status|Progress|Badge|Chart|TimeSeries|Table|List|Log|Kanban|Mermaid|Markdown|Code|Image|File|ApiList|Errors)\b([^>]*?)/?>`,
)

var sourceAttrRegex = regexp.MustCompile(`source=["']([^"']+)["']`)
var languageAttrRegex = regexp.MustCompile(`language=["']([^"']+)["']`)

// resolveComponents walks the section body, finds every known component with
// a source= attribute, and resolves the current value from the data store.
func (m *Materializer) resolveComponents(body string) []Component {
	var out []Component
	seen := make(map[string]bool)
	matches := componentRegex.FindAllStringSubmatch(body, -1)
	for _, match := range matches {
		typ := match[1]
		attrs := match[2]
		sm := sourceAttrRegex.FindStringSubmatch(attrs)
		if len(sm) < 2 {
			continue
		}
		source := sm[1]
		sig := typ + "|" + source
		if seen[sig] {
			continue
		}
		seen[sig] = true

		c := Component{Type: typ, SourceKey: source}
		if lm := languageAttrRegex.FindStringSubmatch(attrs); len(lm) >= 2 {
			c.Language = lm[1]
		}
		if m.FileStore != nil {
			if env, err := m.FileStore.ReadSingleton(source); err == nil && env != nil {
				var v interface{}
				if err := json.Unmarshal(env.Value, &v); err == nil {
					c.Value = v
				}
			}
		}
		out = append(out, c)
	}
	return out
}
