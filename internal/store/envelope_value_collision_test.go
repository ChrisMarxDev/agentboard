package store

// Regression: writing a singleton whose user-supplied object includes a
// top-level `value:` key used to clobber the inner field on read. The
// MarshalDoc splat treated `value` as a regular field; UnmarshalDoc
// then dropped it because "object form wins when both `value:` and
// other keys are present". Net: round-trip lost the inner value.
//
// Cut 9 fix (envelope.go): when the object collides with `value`,
// nest the whole object under `value:` instead of splatting. The
// frontmatter is slightly less natural to read but the round-trip is
// symmetric.
//
// See `ISSUES.md` "Data singleton drops user-supplied `value:` field
// on read round-trip `[needs-decision]`" — option (c).

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEnvelope_ValueFieldCollisionRoundTrips(t *testing.T) {
	cases := []struct {
		name string
		in   any
	}{
		{
			name: "value-only object",
			in:   map[string]any{"value": float64(42)},
		},
		{
			name: "value plus siblings (the canonical bug)",
			in: map[string]any{
				"label": "DAU",
				"value": float64(42),
			},
		},
		{
			name: "value as a nested object",
			in: map[string]any{
				"label": "API status",
				"value": map[string]any{"latency_ms": float64(120), "ok": true},
			},
		},
		{
			name: "value as an array",
			in: map[string]any{
				"label":  "uptime samples",
				"value":  []any{float64(99.9), float64(99.8), float64(99.95)},
				"window": "7d",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := json.Marshal(c.in)
			if err != nil {
				t.Fatalf("seed marshal: %v", err)
			}
			env := &Envelope{Value: raw}
			doc, err := MarshalDoc(env)
			if err != nil {
				t.Fatalf("MarshalDoc: %v", err)
			}
			parsed, err := UnmarshalDoc(doc)
			if err != nil {
				t.Fatalf("UnmarshalDoc: %v\n%s", err, doc)
			}
			var got any
			if err := json.Unmarshal(parsed.Value, &got); err != nil {
				t.Fatalf("decode round-trip value: %v\nraw=%s", err, parsed.Value)
			}
			if !reflect.DeepEqual(got, c.in) {
				t.Errorf("round-trip dropped field(s):\n  in:  %+v\n  out: %+v\n  doc:\n%s",
					c.in, got, doc)
			}
		})
	}
}

// TestEnvelope_NoValueKeySplatsTopLevel — the splat path remains the
// default for objects without a `value` collision so frontmatter
// stays human-readable in the common case.
func TestEnvelope_NoValueKeySplatsTopLevel(t *testing.T) {
	env := &Envelope{
		Value: json.RawMessage(`{"title":"Hi","priority":2,"tags":["a","b"]}`),
	}
	doc, err := MarshalDoc(env)
	if err != nil {
		t.Fatalf("MarshalDoc: %v", err)
	}
	// Splatted objects expose `title:` etc. at the top of the YAML
	// block. A nested form would write `value:` as the only top-level
	// user key; that's not what we want here.
	s := string(doc)
	if !contains(s, "\ntitle: Hi\n") {
		t.Errorf("expected splatted top-level title in doc:\n%s", s)
	}
	if contains(s, "\nvalue:") {
		t.Errorf("non-colliding object should NOT nest under value: in doc:\n%s", s)
	}
	parsed, err := UnmarshalDoc(doc)
	if err != nil {
		t.Fatalf("UnmarshalDoc: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(parsed.Value, &got)
	if got["title"] != "Hi" {
		t.Errorf("title round-trip lost: %+v", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) <= len(haystack) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
