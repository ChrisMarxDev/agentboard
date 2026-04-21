package grab

import (
	"encoding/json"
	"fmt"
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
			fmt.Fprintf(&b, "<!-- skipped: %s#%s (%s) -->\n\n", s.Page, s.CardID, s.Missing)
			continue
		}
		fmt.Fprintf(&b, "### %s — %s\n\n", s.Page, s.CardTitle)
		if mdx := strings.TrimSpace(s.MDXSource); mdx != "" {
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

// markdownFenceFor renders one resolved component as a markdown fragment.
// Multi-line text types get a fenced block; scalars go inline.
func markdownFenceFor(c Component) string {
	switch c.Type {
	case "Mermaid":
		return fmt.Sprintf("**[%s source=%s]**\n```mermaid\n%s\n```", c.Type, c.SourceKey, stringValue(c.Value))
	case "Code":
		lang := c.Language
		code := stringValue(c.Value)
		// Support {code, language} object shape.
		if obj, ok := c.Value.(map[string]interface{}); ok {
			if s, ok := obj["code"].(string); ok {
				code = s
			}
			if l, ok := obj["language"].(string); ok && lang == "" {
				lang = l
			}
		}
		return fmt.Sprintf("**[%s source=%s]**\n```%s\n%s\n```", c.Type, c.SourceKey, lang, code)
	case "Markdown":
		return fmt.Sprintf("**[%s source=%s]**\n\n%s", c.Type, c.SourceKey, stringValue(c.Value))
	case "Image", "File":
		// Expect {file, alt?, label?} or a plain string.
		ref := stringValue(c.Value)
		if obj, ok := c.Value.(map[string]interface{}); ok {
			if f, ok := obj["file"].(string); ok {
				ref = f
			}
		}
		return fmt.Sprintf("**[%s source=%s]** → `/api/files/%s`", c.Type, c.SourceKey, ref)
	default:
		// Structured values (Chart, TimeSeries, Table, List, Log, Kanban, …)
		// and scalars (Metric, Counter, Status, Progress, Badge) render as JSON.
		pretty, _ := json.MarshalIndent(c.Value, "", "  ")
		return fmt.Sprintf("**[%s source=%s]**\n```json\n%s\n```", c.Type, c.SourceKey, string(pretty))
	}
}

func renderXML(sections []Section) string {
	var b strings.Builder
	b.WriteString("<agentboard_context")
	fmt.Fprintf(&b, ` generated_at="%s"`, time.Now().UTC().Format(time.RFC3339))
	b.WriteString(">\n")
	for _, s := range sections {
		if s.Missing != "" {
			fmt.Fprintf(&b, `  <skipped page=%q card_id=%q reason=%q/>`+"\n", s.Page, s.CardID, s.Missing)
			continue
		}
		fmt.Fprintf(&b, `  <section page=%q title=%q>`+"\n", s.Page, s.CardTitle)
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
