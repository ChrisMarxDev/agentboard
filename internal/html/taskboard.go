package html

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
)

// Taskboard typed view (spec-filesystem-substrate.md §"first-class types").
//
// A JSON file whose top-level shape matches the taskboard schema gets
// rendered as a kanban board instead of as pretty-printed JSON. The
// schema is intentionally lax — anything resembling a board renders;
// anything else falls back to the JSON view.
//
// Schema (all fields optional unless noted):
//
//	{
//	  "kind": "taskboard",       // optional explicit kind marker
//	  "title": "Q3 work",
//	  "columns": [
//	    {"id": "todo",  "label": "To do"},
//	    {"id": "doing", "label": "In progress"},
//	    {"id": "done",  "label": "Done"}
//	  ],
//	  "cards": [
//	    {"id": "c1", "title": "Ship v2", "column": "todo",
//	     "body": "What 'ship v2' means: …",
//	     "labels": ["urgent"], "assignees": ["alice"],
//	     "priority": 1, "order": 1.5}
//	  ]
//	}
//
// Recognition rule: any object that has both `columns` (array) and
// `cards` (array). The optional `kind: "taskboard"` marker disambiguates
// in case some other file happens to use those keys.

// taskboardData is the in-memory shape the renderer works with.
type taskboardData struct {
	Title   string
	Columns []taskboardColumn
}

type taskboardColumn struct {
	ID    string
	Label string
	Cards []taskboardCard
}

type taskboardCard struct {
	ID        string
	Title     string
	BodyHTML  template.HTML
	Labels    []string
	Assignees []string
	Priority  *int
	Order     float64
}

// parseTaskboard reads the raw JSON and returns either a structured
// taskboard or (nil, false) when the shape doesn't match.
func parseTaskboard(raw []byte) (*taskboardData, bool) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, false
	}
	rawCols, hasCols := top["columns"].([]any)
	rawCards, hasCards := top["cards"].([]any)
	if !hasCols || !hasCards {
		return nil, false
	}

	tb := &taskboardData{}
	if s, ok := top["title"].(string); ok {
		tb.Title = s
	}

	colByID := map[string]int{}
	for _, c := range rawCols {
		obj, ok := c.(map[string]any)
		if !ok {
			continue
		}
		col := taskboardColumn{
			ID:    asString(obj["id"]),
			Label: asString(obj["label"]),
		}
		if col.ID == "" {
			continue
		}
		if col.Label == "" {
			col.Label = col.ID
		}
		colByID[col.ID] = len(tb.Columns)
		tb.Columns = append(tb.Columns, col)
	}

	// Fallback lane for cards whose `column` doesn't match any declared
	// column. Better to surface them than to drop them.
	uncategorized := -1
	addCardToLane := func(card taskboardCard, col string) {
		if idx, ok := colByID[col]; ok {
			tb.Columns[idx].Cards = append(tb.Columns[idx].Cards, card)
			return
		}
		if uncategorized == -1 {
			tb.Columns = append(tb.Columns, taskboardColumn{ID: "_uncategorized", Label: "Uncategorized"})
			uncategorized = len(tb.Columns) - 1
		}
		tb.Columns[uncategorized].Cards = append(tb.Columns[uncategorized].Cards, card)
	}

	for _, c := range rawCards {
		obj, ok := c.(map[string]any)
		if !ok {
			continue
		}
		card := taskboardCard{
			ID:    asString(obj["id"]),
			Title: asString(obj["title"]),
		}
		if body := asString(obj["body"]); body != "" {
			// Cards bodies are short prose — rendered as plain text inside
			// a <p>. Skipping goldmark keeps the surface predictable; the
			// full markdown surface still lives at .md files.
			card.BodyHTML = template.HTML("<p>" + template.HTMLEscapeString(body) + "</p>")
		}
		if labels, ok := obj["labels"].([]any); ok {
			for _, l := range labels {
				if s := asString(l); s != "" {
					card.Labels = append(card.Labels, s)
				}
			}
		}
		if assignees, ok := obj["assignees"].([]any); ok {
			for _, a := range assignees {
				if s := asString(a); s != "" {
					card.Assignees = append(card.Assignees, s)
				}
			}
		}
		if p, ok := asNumber(obj["priority"]); ok {
			v := int(p)
			card.Priority = &v
		}
		if o, ok := asNumber(obj["order"]); ok {
			card.Order = o
		}
		col := asString(obj["column"])
		if col == "" {
			col = asString(obj["col"]) // accept legacy alias
		}
		addCardToLane(card, col)
	}

	// Stable lane sort: by `order` if set, then by title.
	for i := range tb.Columns {
		lane := &tb.Columns[i]
		sort.SliceStable(lane.Cards, func(a, b int) bool {
			ca, cb := lane.Cards[a], lane.Cards[b]
			if ca.Order != cb.Order {
				return ca.Order < cb.Order
			}
			return ca.Title < cb.Title
		})
	}
	return tb, true
}

// renderTaskboardBody returns the inner HTML for a taskboard. The
// caller is responsible for wrapping it in the dashboard shell.
func renderTaskboardBody(tb *taskboardData) template.HTML {
	var buf bytes.Buffer
	if tb.Title != "" {
		fmt.Fprintf(&buf, "<h1>%s</h1>\n", template.HTMLEscapeString(tb.Title))
	}
	buf.WriteString(`<div class="taskboard">`)
	for _, lane := range tb.Columns {
		fmt.Fprintf(&buf, `<div class="lane"><h3>%s <span class="ab-muted">(%d)</span></h3>`,
			template.HTMLEscapeString(lane.Label), len(lane.Cards))
		for _, card := range lane.Cards {
			buf.WriteString(`<div class="card">`)
			if card.Title != "" {
				fmt.Fprintf(&buf, `<div class="title">%s</div>`,
					template.HTMLEscapeString(card.Title))
			}
			if card.BodyHTML != "" {
				buf.WriteString(string(card.BodyHTML))
			}
			if len(card.Labels) > 0 || len(card.Assignees) > 0 || card.Priority != nil {
				buf.WriteString(`<div class="meta">`)
				for _, l := range card.Labels {
					fmt.Fprintf(&buf, `<span class="ab-badge">%s</span>`,
						template.HTMLEscapeString(l))
				}
				for _, a := range card.Assignees {
					fmt.Fprintf(&buf, `<span class="ab-badge accent">@%s</span>`,
						template.HTMLEscapeString(a))
				}
				if card.Priority != nil {
					cls := "ab-badge"
					if *card.Priority <= 1 {
						cls = "ab-badge warn"
					}
					fmt.Fprintf(&buf, `<span class="%s">p%d</span>`, cls, *card.Priority)
				}
				buf.WriteString(`</div>`)
			}
			buf.WriteString(`</div>`)
		}
		buf.WriteString(`</div>`)
	}
	buf.WriteString(`</div>`)
	return template.HTML(buf.String())
}

// ----- small JSON-value helpers -----

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return fmt.Sprintf("%v", t)
	}
	return ""
}

func asNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case int:
		return float64(t), true
	}
	return 0, false
}
