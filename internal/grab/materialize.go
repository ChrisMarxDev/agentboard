// Package grab collects Card sections across MDX pages into a single
// agent-ready payload. See spec-grab.md for the full design.
//
// The materializer is deliberately regex-based: AgentBoard authors already
// follow a narrow <Card title="..."> convention, a full MDX parser would
// bring in an AST dep for a single feature, and stale data is worse than
// slightly permissive matching. If a pick can't resolve, the formatter emits
// a comment and carries on — never blocks the whole copy.
package grab

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/christophermarx/agentboard/internal/data"
	"github.com/christophermarx/agentboard/internal/mdx"
)

// Pick identifies one Card to include. Matches the frontend/MCP input shape.
type Pick struct {
	Page   string `json:"page"`    // URL path, e.g. "/features/files"
	CardID string `json:"card_id"` // Slug(Card.title)
}

// Section is one materialized pick.
type Section struct {
	Page       string      `json:"page"`
	PageTitle  string      `json:"page_title"`
	CardTitle  string      `json:"card_title"`
	CardID     string      `json:"card_id"`
	MDXSource  string      `json:"mdx_source"`
	Components []Component `json:"components"`
	Missing    string      `json:"missing,omitempty"` // set when the pick couldn't resolve
}

// Component describes one data-bound component found inside the Card.
type Component struct {
	Type      string      `json:"type"`              // e.g. "Mermaid", "Code"
	SourceKey string      `json:"source_key"`        // data key the component reads
	Value     interface{} `json:"value"`             // resolved current value
	Language  string      `json:"language,omitempty"`
}

// Format is one of the three output shapes.
type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatXML      Format = "xml"
	FormatJSON     Format = "json"
)

// Materializer resolves picks using a PageManager + data store.
type Materializer struct {
	Pages *mdx.PageManager
	Store data.DataStore
}

// Materialize returns the list of resolved sections for the given picks.
// Sections appear in pick order. Missing picks are included with Missing set.
func (m *Materializer) Materialize(picks []Pick) []Section {
	out := make([]Section, 0, len(picks))
	for _, p := range picks {
		out = append(out, m.materializeOne(p))
	}
	return out
}

func (m *Materializer) materializeOne(p Pick) Section {
	// URL path "/features/files" → PageManager key "features/files"; "/" → "index".
	key := strings.TrimPrefix(p.Page, "/")
	if key == "" {
		key = "index"
	}
	page := m.Pages.GetPage(key)
	if page == nil {
		return Section{Page: p.Page, CardID: p.CardID, Missing: "page not found"}
	}

	title, body, ok := findCardByID(page.Source, p.CardID)
	if !ok {
		return Section{
			Page: p.Page, PageTitle: page.Title, CardID: p.CardID,
			Missing: "card id not found on page",
		}
	}

	comps := m.resolveComponents(body)
	return Section{
		Page:       p.Page,
		PageTitle:  page.Title,
		CardTitle:  title,
		CardID:     p.CardID,
		MDXSource:  strings.TrimSpace(body),
		Components: comps,
	}
}

// cardRegex matches a top-level <Card …>…</Card> block. Non-greedy body so
// multiple Cards on a page don't merge. Does NOT handle nested <Card> (rare
// in our authoring convention; documented limitation in spec-grab.md).
var cardRegex = regexp.MustCompile(`(?s)<Card\s([^>]*?)>(.*?)</Card>`)

// titleAttrRegex pulls the title="..." attr out of Card's attribute list.
// Intentionally permissive: order-independent, tolerates other attrs, tolerates
// single or double quotes.
var titleAttrRegex = regexp.MustCompile(`title=["']([^"']+)["']`)

// findCardByID walks every <Card title="..."> block in source, slugifies the
// title, and returns the block whose slug matches cardID. Returns the raw title,
// the inner content, and ok=true when found.
func findCardByID(source, cardID string) (title, body string, ok bool) {
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
			return t, inner, true
		}
	}
	return "", "", false
}

// componentRegex matches self-closing and open/close tags of known data-bound
// built-ins with a source="..." attribute. We deliberately allow-list the
// component names instead of matching any capitalized tag, so prose that
// happens to contain JSX-like syntax doesn't get picked up as components.
var componentRegex = regexp.MustCompile(
	`<(Metric|Counter|Status|Progress|Badge|Chart|TimeSeries|Table|List|Log|Kanban|Mermaid|Markdown|Code|Image|File)\b([^>]*?)/?>`,
)

var sourceAttrRegex = regexp.MustCompile(`source=["']([^"']+)["']`)
var languageAttrRegex = regexp.MustCompile(`language=["']([^"']+)["']`)

// resolveComponents walks the Card body, finds every known component with a
// source= attribute, and resolves the current value from the data store.
func (m *Materializer) resolveComponents(body string) []Component {
	var out []Component
	seen := make(map[string]bool) // dedupe by type|source within one card
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
		if m.Store != nil {
			if raw, err := m.Store.Get(source); err == nil && raw != nil {
				var v interface{}
				if err := json.Unmarshal(raw, &v); err == nil {
					c.Value = v
				}
			}
		}
		out = append(out, c)
	}
	return out
}
