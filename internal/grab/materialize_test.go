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
	title, body, ok := findCardByID(src, "alpha-beta")
	if !ok || title != "Alpha Beta" || !strings.Contains(body, "first card body") {
		t.Fatalf("alpha-beta: ok=%v title=%q body=%q", ok, title, body)
	}
	title, body, ok = findCardByID(src, "gamma")
	if !ok || title != "Gamma" || !strings.Contains(body, "second card body") {
		t.Fatalf("gamma: ok=%v title=%q body=%q", ok, title, body)
	}
	_, _, ok = findCardByID(src, "nope")
	if ok {
		t.Fatal("nonexistent id should return ok=false")
	}
}
