package inbox

import (
	"reflect"
	"testing"
)

func TestExtractMentions(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"no mention here", nil},
		{"@alice wrote the doc", []string{"alice"}},
		{"ping @bob and @charlie tomorrow", []string{"bob", "charlie"}},
		{"@alice @alice @alice", []string{"alice"}},                    // dedupe
		{"email user@example.com", nil},                                // mid-word @ not a mention
		{"(@alice) got this", []string{"alice"}},                       // paren-prefixed OK
		{"see @Alice_01 for details", nil},                             // uppercase letter → invalid start
		{"@bob.chris should not merge", []string{"bob"}},               // bob.c... → bob followed by `.` → match bob only
		{"@alice, @bob; @charlie:", []string{"alice", "bob", "charlie"}}, // comma/semicolon/colon endings
	}
	for _, c := range cases {
		got := ExtractMentions(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ExtractMentions(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestExtractMentionsInAny(t *testing.T) {
	// Nested structure — kanban-card shaped.
	v := []any{
		map[string]any{
			"id":    "t1",
			"title": "Review @alice's PR",
			"notes": map[string]any{"comment": "also ping @bob"},
		},
		map[string]any{"id": "t2", "title": "@charlie work"},
		"another @alice line at the top level",
	}
	got := ExtractMentionsInAny(v)
	// Order depends on walk; we assert the set.
	want := map[string]bool{"alice": true, "bob": true, "charlie": true}
	if len(got) != 3 {
		t.Fatalf("count = %d, got %v", len(got), got)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected name %q", name)
		}
	}
}

func TestDiffAssigneesAny(t *testing.T) {
	cases := []struct {
		prev, next any
		want       []string
	}{
		{nil, []any{"alice"}, []string{"alice"}},
		{[]any{"alice"}, []any{"alice", "bob"}, []string{"bob"}},
		{[]any{"alice", "bob"}, []any{"bob"}, nil},
		{[]any{"Alice"}, []any{"alice"}, nil}, // case-insensitive match → nothing added
		{[]any{"alice"}, []any{"alice", "alice"}, nil}, // dedupe
		{[]any{}, []any{"alice", "bob"}, []string{"alice", "bob"}},
		{"not an array", []any{"alice"}, []string{"alice"}},
	}
	for _, c := range cases {
		got := DiffAssigneesAny(c.prev, c.next)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("DiffAssignees(%v → %v) = %v, want %v", c.prev, c.next, got, c.want)
		}
	}
}
