package files

import (
	"strings"
	"testing"
)

func TestParseFrontmatter_Valid(t *testing.T) {
	data := []byte("---\nname: my-skill\ndescription: Does things\n---\n\n# Body\n")
	fm := parseFrontmatter(data)
	if fm == nil {
		t.Fatal("expected frontmatter map, got nil")
	}
	if fm["name"] != "my-skill" {
		t.Errorf("name = %v, want my-skill", fm["name"])
	}
	if fm["description"] != "Does things" {
		t.Errorf("description = %v, want 'Does things'", fm["description"])
	}
}

func TestParseFrontmatter_NoMarker(t *testing.T) {
	data := []byte("# Just a heading\n\nno frontmatter here\n")
	if fm := parseFrontmatter(data); fm != nil {
		t.Errorf("expected nil, got %v", fm)
	}
}

func TestParseFrontmatter_Unclosed(t *testing.T) {
	data := []byte("---\nname: foo\ndescription: bar\nthis never closes\n")
	if fm := parseFrontmatter(data); fm != nil {
		t.Errorf("expected nil for unclosed frontmatter, got %v", fm)
	}
}

func TestParseFrontmatter_Malformed(t *testing.T) {
	data := []byte("---\nthis is not: valid: yaml: at: all\n  : :\n---\n")
	fm := parseFrontmatter(data)
	// yaml.v3 is tolerant; this may parse OR fail. Either way the result must be
	// either nil or a map — never panic.
	if fm != nil {
		// If it parsed, the keys shouldn't include our expected skill fields.
		if _, ok := fm["name"]; ok {
			t.Errorf("malformed YAML accidentally parsed a name")
		}
	}
}

func TestParseFrontmatter_Empty(t *testing.T) {
	data := []byte{}
	if fm := parseFrontmatter(data); fm != nil {
		t.Errorf("expected nil for empty input, got %v", fm)
	}
}

func TestParseFrontmatter_OnlyMarkers(t *testing.T) {
	data := []byte("---\n---\n")
	if fm := parseFrontmatter(data); fm != nil {
		t.Errorf("expected nil for empty frontmatter body, got %v", fm)
	}
}

func TestParseFrontmatter_CRLF(t *testing.T) {
	data := []byte("---\r\nname: winfile\r\ndescription: crlf skill\r\n---\r\n\r\nbody\r\n")
	fm := parseFrontmatter(data)
	if fm == nil {
		t.Fatal("expected frontmatter, got nil")
	}
	if fm["name"] != "winfile" {
		t.Errorf("name = %v, want winfile", fm["name"])
	}
}

func TestParseFrontmatter_RespectsScanCap(t *testing.T) {
	// Build a file whose closing --- lives past the scan cap. The parser must
	// refuse to find it rather than reading the whole file.
	padding := strings.Repeat("x\n", frontmatterScanBytes)
	data := []byte("---\nname: late\ndescription: late\n" + padding + "---\n")
	if fm := parseFrontmatter(data); fm != nil {
		t.Errorf("expected nil when closing marker is past scan cap, got %v", fm)
	}
}

func TestParseFrontmatter_NestedTypes(t *testing.T) {
	data := []byte("---\nname: complex\ndescription: nested\ntags: [a, b, c]\nmeta:\n  key: value\n---\n")
	fm := parseFrontmatter(data)
	if fm == nil {
		t.Fatal("expected map, got nil")
	}
	tags, ok := fm["tags"].([]any)
	if !ok {
		t.Fatalf("tags type = %T, want []any", fm["tags"])
	}
	if len(tags) != 3 {
		t.Errorf("tags length = %d, want 3", len(tags))
	}
}
