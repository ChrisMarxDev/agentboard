package html

import (
	"strings"
	"testing"
)

func TestParseTaskboard_RecognizesShape(t *testing.T) {
	raw := []byte(`{
		"title": "Q3",
		"columns": [{"id":"todo","label":"To do"},{"id":"done","label":"Done"}],
		"cards":   [{"id":"c1","title":"Ship","column":"done","priority":1,"order":0.5}]
	}`)
	tb, ok := parseTaskboard(raw)
	if !ok {
		t.Fatal("expected taskboard parse to succeed")
	}
	if tb.Title != "Q3" {
		t.Errorf("title = %q, want Q3", tb.Title)
	}
	if len(tb.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(tb.Columns))
	}
	if got := tb.Columns[1].Cards; len(got) != 1 || got[0].Title != "Ship" {
		t.Errorf("done lane cards = %+v", got)
	}
}

func TestParseTaskboard_FallsBackForArbitraryJSON(t *testing.T) {
	// No columns / cards keys — must not be recognized as a taskboard.
	raw := []byte(`{"users":[{"name":"alice"}]}`)
	if _, ok := parseTaskboard(raw); ok {
		t.Fatal("expected unrelated JSON to be rejected")
	}
}

func TestParseTaskboard_UncategorizedFallbackLane(t *testing.T) {
	// Card with a column that wasn't declared lands in an extra
	// "Uncategorized" lane — we'd rather surface it than drop it.
	raw := []byte(`{
		"columns":[{"id":"todo"}],
		"cards":[{"id":"x","title":"orphan","column":"nowhere"}]
	}`)
	tb, ok := parseTaskboard(raw)
	if !ok {
		t.Fatal("parse failed")
	}
	if n := len(tb.Columns); n != 2 {
		t.Fatalf("got %d columns, want 2 (declared + fallback)", n)
	}
	fallback := tb.Columns[1]
	if fallback.ID != "_uncategorized" || len(fallback.Cards) != 1 {
		t.Errorf("fallback lane wrong: %+v", fallback)
	}
}

func TestParseTaskboard_AcceptsColAlias(t *testing.T) {
	// Accept `col:` as a legacy alias for `column:`.
	raw := []byte(`{
		"columns":[{"id":"todo","label":"To do"}],
		"cards":[{"id":"c1","title":"A","col":"todo"}]
	}`)
	tb, ok := parseTaskboard(raw)
	if !ok {
		t.Fatal("parse failed")
	}
	if len(tb.Columns[0].Cards) != 1 {
		t.Errorf("expected card landed via `col` alias, got %+v", tb.Columns[0].Cards)
	}
}

func TestRenderTaskboardBody_EmitsExpectedMarkup(t *testing.T) {
	tb := &taskboardData{
		Title: "Smoke",
		Columns: []taskboardColumn{{
			ID:    "todo",
			Label: "To do",
			Cards: []taskboardCard{{
				ID:        "c1",
				Title:     "Ship it",
				Labels:    []string{"urgent"},
				Assignees: []string{"alice"},
			}},
		}},
	}
	got := string(renderTaskboardBody(tb))
	for _, want := range []string{
		`<h1>Smoke</h1>`,
		`<div class="taskboard">`,
		`<div class="lane">`,
		`To do`,
		`<div class="card">`,
		`Ship it`,
		`urgent`,
		`@alice`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in rendered output:\n%s", want, got)
		}
	}
}
