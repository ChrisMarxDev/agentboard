package grab

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Simple":                "simple",
		"Two Words":             "two-words",
		"  leading/trailing  ":  "leading-trailing",
		"Numbers 123":           "numbers-123",
		"Punct! Punct? Punct.":  "punct-punct-punct",
		"München":               "münchen",
		"Parse error on line 2": "parse-error-on-line-2",
		"":                      "",
		"---dashes---":          "dashes",
	}
	for input, want := range cases {
		got := Slug(input)
		if got != want {
			t.Errorf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFindCardByID(t *testing.T) {
	src := `
# Heading

<Card title="Alpha Beta">
  first card body
</Card>

<Card title="Gamma" span={2}>
  second card body
</Card>
`
	title, body, _, _, ok := findCardByID(src, "alpha-beta")
	if !ok || title != "Alpha Beta" || !strings.Contains(body, "first card body") {
		t.Fatalf("alpha-beta: ok=%v title=%q body=%q", ok, title, body)
	}
	title, body, _, _, ok = findCardByID(src, "gamma")
	if !ok || title != "Gamma" || !strings.Contains(body, "second card body") {
		t.Fatalf("gamma: ok=%v title=%q body=%q", ok, title, body)
	}
	_, _, _, _, ok = findCardByID(src, "nope")
	if ok {
		t.Fatal("nonexistent id should return ok=false")
	}
}

func TestFindHeadingBySlug(t *testing.T) {
	src := `# Top

intro paragraph

## First Section

body of first
multiline

### Nested

nested content

## Second Section

body of second
`
	t.Run("section spans to next same-level heading", func(t *testing.T) {
		text, body, level, _, _, ok := findHeadingBySlug(src, "first-section")
		if !ok || text != "First Section" || level != 2 {
			t.Fatalf("got text=%q level=%d ok=%v", text, level, ok)
		}
		if !strings.Contains(body, "body of first") || !strings.Contains(body, "Nested") {
			t.Fatalf("H2 section should include its H3 child; body=%q", body)
		}
		if strings.Contains(body, "Second Section") {
			t.Fatalf("H2 section should NOT include the next H2; body=%q", body)
		}
	})
	t.Run("H3 stops at next H3 or higher", func(t *testing.T) {
		_, body, level, _, _, ok := findHeadingBySlug(src, "nested")
		if !ok || level != 3 {
			t.Fatalf("level=%d ok=%v", level, ok)
		}
		if !strings.Contains(body, "nested content") {
			t.Fatal("missing body")
		}
		if strings.Contains(body, "Second Section") {
			t.Fatal("H3 should stop at the next H2")
		}
	})
	t.Run("H1 includes everything after it", func(t *testing.T) {
		_, body, level, _, _, ok := findHeadingBySlug(src, "top")
		if !ok || level != 1 {
			t.Fatalf("level=%d ok=%v", level, ok)
		}
		if !strings.Contains(body, "Second Section") {
			t.Fatal("H1 should include the last H2")
		}
	})
	t.Run("missing slug returns ok=false", func(t *testing.T) {
		_, _, _, _, _, ok := findHeadingBySlug(src, "nope")
		if ok {
			t.Fatal("nonexistent slug should be !ok")
		}
	})
	t.Run("headings inside fenced code blocks are skipped", func(t *testing.T) {
		fenced := "## Real\n\n```\n## Fake\n```\n\n## Next\n"
		text, body, _, _, _, ok := findHeadingBySlug(fenced, "real")
		if !ok || text != "Real" {
			t.Fatalf("real: ok=%v text=%q", ok, text)
		}
		// Section boundary should skip the fake heading and stop at "Next".
		if strings.Contains(body, "Next") {
			t.Fatalf("section should stop at the next real H2; body=%q", body)
		}
		if _, _, _, _, _, ok := findHeadingBySlug(fenced, "fake"); ok {
			t.Fatal("fenced heading should not be discoverable as a target")
		}
	})
}

func TestIsInsideFence(t *testing.T) {
	src := "before\n```\ninside fence\n```\nafter\n"
	cases := []struct {
		offset int
		want   bool
	}{
		{0, false},
		{strings.Index(src, "inside"), true},
		{strings.Index(src, "after"), false},
	}
	for _, c := range cases {
		got := isInsideFence(src, c.offset)
		if got != c.want {
			t.Errorf("isInsideFence(off=%d) = %v, want %v", c.offset, got, c.want)
		}
	}
}
