package grab

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Format turns a materialized section list into agent-ready text in the
// requested format. Unknown format defaults to markdown.
func Render(sections []Section, format Format) string {
	switch format {
	case FormatXML:
		return renderXML(sections)
	case FormatJSON:
		return renderJSON(sections)
	default:
		return renderMarkdown(sections)
	}
}

func renderMarkdown(sections []Section) string {
	if len(sections) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Context from AgentBoard\n\n")
	for _, s := range sections {
		if s.Missing != "" {
			fmt.Fprintf(&b, "<!-- skipped: %s%s (%s) -->\n\n", s.Page, sectionAnchor(s), s.Missing)
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", sectionHeading(s))
		if mdx := strings.TrimSpace(stripLayoutTags(s.MDXSource)); mdx != "" {
			b.WriteString(mdx)
			b.WriteString("\n\n")
		}
		for _, c := range s.Components {
			b.WriteString(markdownFenceFor(c))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// stripLayoutTags removes AgentBoard-specific MDX chrome from a section body
// so the agent-facing output is plain markdown + code fences. Three classes:
//
//  1. Layout wrappers (Card/Deck/Stack): open+close tags removed, children
//     preserved. Content survives, the chrome doesn't.
//  2. Self-closing data-bound components (Metric, Counter, …, Mermaid, …):
//     removed entirely — their resolved values are emitted separately via
//     markdownFenceFor, so keeping the JSX tag would duplicate the payload.
//  3. Self-closing discovery components (ApiList, Errors): removed entirely
//     — they fetch REST endpoints at render time and don't have a
//     materializable static value.
//
// Fence-aware: tags inside ```code``` blocks are left alone so example MDX in
// docs doesn't get mangled. Tags can appear anywhere on a line — inline
// `<Card title="Alpha"><Counter /></Card>` gets reduced to the inner content.
// Multiline tags (e.g. <ApiList src=… /> spread across several lines) are
// handled too.
func stripLayoutTags(body string) string {
	// Pre-pass: strip multiline self-closing tags (e.g. a <ApiList ... /> with
	// newlines between attrs) in fence-aware chunks.
	body = stripOutsideFences(body, selfClosingMultilineRe)

	var b strings.Builder
	inFence := false
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(line)
			continue
		}
		if !inFence {
			line = layoutInlineOpen.ReplaceAllString(line, "")
			line = layoutInlineClose.ReplaceAllString(line, "")
			line = selfClosingRe.ReplaceAllString(line, "")
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	out := b.String()
	// Collapse runs of 3+ newlines that emptied lines leave behind.
	out = blankRunRe.ReplaceAllString(out, "\n\n")
	return out
}

// stripOutsideFences applies a regex replacement across the body but skips any
// text inside ```fenced blocks``` so examples stay intact.
func stripOutsideFences(body string, re *regexp.Regexp) string {
	parts := strings.Split(body, "```")
	// Even indices are outside fences, odd indices are inside. Walk, replace
	// only outside; then rejoin with the fence markers we split on.
	for i := 0; i < len(parts); i += 2 {
		parts[i] = re.ReplaceAllString(parts[i], "")
	}
	return strings.Join(parts, "```")
}

var (
	layoutInlineOpen  = regexp.MustCompile(`<(Card|Deck|Stack)\b[^>]*>`)
	layoutInlineClose = regexp.MustCompile(`</(Card|Deck|Stack)>`)
	selfClosingRe     = regexp.MustCompile(
		`<(Metric|Counter|Status|Progress|Badge|Chart|TimeSeries|Table|List|Log|Kanban|Mermaid|Markdown|Code|Image|File|ApiList|Errors)\b[^>]*/>`,
	)
	selfClosingMultilineRe = regexp.MustCompile(
		`(?s)<(Metric|Counter|Status|Progress|Badge|Chart|TimeSeries|Table|List|Log|Kanban|Mermaid|Markdown|Code|Image|File|ApiList|Errors)\b.*?/>`,
	)
	blankRunRe = regexp.MustCompile(`\n{3,}`)
)

// sectionHeading is the human-readable label for one section in markdown
// output. Includes provenance (page path) and the section identity (card
// title, heading text, or page title for whole-page picks).
func sectionHeading(s Section) string {
	switch s.Kind {
	case KindHeading:
		if s.HeadingText != "" {
			return s.Page + " — " + s.HeadingText
		}
		return s.Page
	case KindPage:
		if s.PageTitle != "" {
			return s.Page + " — " + s.PageTitle + " (full page)"
		}
		return s.Page + " (full page)"
	default: // KindCard
		if s.CardTitle != "" {
			return s.Page + " — " + s.CardTitle
		}
		return s.Page
	}
}

// sectionAnchor is the "#<id>" suffix used in skipped-comment references.
func sectionAnchor(s Section) string {
	switch s.Kind {
	case KindHeading:
		return "#" + s.HeadingSlug
	case KindPage:
		return ""
	default:
		return "#" + s.CardID
	}
}

// markdownFenceFor renders one resolved component as a markdown fragment.
// Scalars (Metric/Counter/Status/Progress/Badge) render as a one-line bullet
// because a JSON fence around a single value is scaffolding that eats reader
// attention without adding signal. Multi-line text types (Mermaid/Code) keep
// their fenced block; structured collections (Table/Kanban/Chart/…) render
// as a pretty JSON fence.
func markdownFenceFor(c Component) string {
	switch c.Type {
	case "Mermaid":
		code := stringValue(c.Value)
		// Unwrap {code: "..."} if that's the source shape.
		if obj, ok := c.Value.(map[string]any); ok {
			if s, ok := obj["code"].(string); ok {
				code = s
			}
		}
		return fmt.Sprintf("**[%s source=%s]**\n```mermaid\n%s\n```", c.Type, c.SourceKey, code)
	case "Code":
		lang := c.Language
		code := stringValue(c.Value)
		if obj, ok := c.Value.(map[string]any); ok {
			if s, ok := obj["code"].(string); ok {
				code = s
			}
			if l, ok := obj["language"].(string); ok && lang == "" {
				lang = l
			}
		}
		return fmt.Sprintf("**[%s source=%s]**\n```%s\n%s\n```", c.Type, c.SourceKey, lang, code)
	case "Markdown":
		// Unwrap the { text: "..." } shape so we emit raw markdown, not
		// JSON-encoded (which turns `<` into `<`).
		text := markdownText(c.Value)
		return fmt.Sprintf("**[%s source=%s]**\n\n%s", c.Type, c.SourceKey, text)
	case "Image", "File":
		// Expect {file, alt?, label?} or a plain string.
		ref := stringValue(c.Value)
		if obj, ok := c.Value.(map[string]any); ok {
			if f, ok := obj["file"].(string); ok {
				ref = f
			}
		}
		return fmt.Sprintf("**[%s source=%s]** → `/api/files/%s`", c.Type, c.SourceKey, ref)
	case "Metric", "Counter", "Status", "Progress", "Badge":
		return fmt.Sprintf("- **%s** (`%s`): %s", c.Type, c.SourceKey, scalarInline(c.Value))
	default:
		// Structured collections (Chart, TimeSeries, Table, List, Log, Kanban).
		pretty, _ := json.MarshalIndent(c.Value, "", "  ")
		return fmt.Sprintf("**[%s source=%s]**\n```json\n%s\n```", c.Type, c.SourceKey, string(pretty))
	}
}

// markdownText extracts the raw markdown string from a Markdown component's
// resolved value. Supports plain-string shape and {text: "..."} object shape.
// Returns empty string for anything else (the component doesn't have a sane
// string representation we can emit without re-encoding to JSON).
func markdownText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if obj, ok := v.(map[string]any); ok {
		if s, ok := obj["text"].(string); ok {
			return s
		}
	}
	return ""
}

// scalarInline returns a compact one-line representation of a scalar
// component's value. Handles the common shapes:
//   - a plain number/string/bool → stringified directly
//   - {value: …}                 → uses the value
//   - {state, label, detail?}    → "state — label (detail)" for Status
//   - {value, max, label?}       → "value/max" for Progress
//   - {text, variant?}           → "text" for Badge
func scalarInline(v any) string {
	if v == nil {
		return "—"
	}
	if obj, ok := v.(map[string]any); ok {
		// Status
		if state, ok := obj["state"].(string); ok {
			label, _ := obj["label"].(string)
			detail, _ := obj["detail"].(string)
			out := state
			if label != "" {
				out = label
			}
			if detail != "" {
				out += " (" + detail + ")"
			}
			return out
		}
		// Progress
		if val, hasVal := obj["value"]; hasVal {
			if max, hasMax := obj["max"]; hasMax {
				return fmt.Sprintf("%s/%s", stringValue(val), stringValue(max))
			}
			return stringValue(val)
		}
		// Badge
		if txt, ok := obj["text"].(string); ok {
			return txt
		}
		if label, ok := obj["label"].(string); ok {
			return label
		}
	}
	return stringValue(v)
}

func renderXML(sections []Section) string {
	var b strings.Builder
	b.WriteString("<agentboard_context")
	fmt.Fprintf(&b, ` generated_at="%s"`, time.Now().UTC().Format(time.RFC3339))
	b.WriteString(">\n")
	for _, s := range sections {
		if s.Missing != "" {
			fmt.Fprintf(&b, `  <skipped page=%q anchor=%q reason=%q/>`+"\n", s.Page, sectionAnchor(s), s.Missing)
			continue
		}
		title := s.CardTitle
		if s.Kind == KindHeading {
			title = s.HeadingText
		} else if s.Kind == KindPage {
			title = s.PageTitle
		}
		fmt.Fprintf(&b, `  <section kind=%q page=%q title=%q>`+"\n", string(s.Kind), s.Page, title)
		if mdx := strings.TrimSpace(s.MDXSource); mdx != "" {
			b.WriteString("    <mdx><![CDATA[")
			b.WriteString(mdx)
			b.WriteString("]]></mdx>\n")
		}
		for _, c := range s.Components {
			fmt.Fprintf(&b, `    <component type=%q source=%q>`, c.Type, c.SourceKey)
			b.WriteString("<![CDATA[")
			// For structured values, inline JSON is the least lossy shape
			// inside XML. For strings, raw text.
			switch v := c.Value.(type) {
			case string:
				b.WriteString(v)
			default:
				j, _ := json.MarshalIndent(v, "", "  ")
				b.Write(j)
			}
			b.WriteString("]]></component>\n")
		}
		b.WriteString("  </section>\n")
	}
	b.WriteString("</agentboard_context>\n")
	return b.String()
}

func renderJSON(sections []Section) string {
	payload := map[string]interface{}{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"sections":     sections,
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b) + "\n"
}

// stringValue best-effort stringifies a resolved value for markdown rendering.
func stringValue(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	// Numbers / bools / arrays / objects: JSON representation beats %v for agents.
	b, _ := json.Marshal(v)
	return string(b)
}
