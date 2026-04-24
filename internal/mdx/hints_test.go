package mdx

import "testing"

func TestPageHints_MissingSummary(t *testing.T) {
	p := &PageInfo{
		Path:  "/voice",
		Title: "Voice Guide",
	}
	hints := PageHints(p)
	if len(hints) != 1 {
		t.Fatalf("expected 1 hint for missing summary, got %d", len(hints))
	}
	if hints[0].Code != "missing_summary" {
		t.Errorf("expected code missing_summary, got %q", hints[0].Code)
	}
	if hints[0].Example == "" {
		t.Errorf("hint should include a corrected example")
	}
}

func TestPageHints_WhitespaceOnlySummary(t *testing.T) {
	// Don't let "summary: \"   \"" slip past — whitespace isn't a summary.
	p := &PageInfo{Path: "/a", Title: "A", Summary: "   "}
	hints := PageHints(p)
	if len(hints) != 1 || hints[0].Code != "missing_summary" {
		t.Fatalf("whitespace summary should trigger missing_summary, got %+v", hints)
	}
}

func TestPageHints_WellFormedPageSilent(t *testing.T) {
	p := &PageInfo{
		Path:    "/voice",
		Title:   "Voice Guide",
		Summary: "Tone for blogs, social, newsletters.",
		Tags:    []string{"voice"},
	}
	hints := PageHints(p)
	if len(hints) != 0 {
		t.Errorf("well-formed page should produce no hints, got %+v", hints)
	}
}

func TestPageHints_NilPage(t *testing.T) {
	if hints := PageHints(nil); hints != nil {
		t.Errorf("nil page should return nil hints, got %+v", hints)
	}
}
