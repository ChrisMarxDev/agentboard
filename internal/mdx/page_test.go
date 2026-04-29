package mdx

import (
	"reflect"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantTitle   string
		wantSummary string
		wantTags    []string
		wantSource  string
	}{
		{
			name:       "no frontmatter",
			in:         "# Hello\n\nbody",
			wantSource: "# Hello\n\nbody",
		},
		{
			name:       "title only",
			in:         "---\ntitle: My Page\n---\n# Body\n",
			wantTitle:  "My Page",
			wantSource: "# Body\n",
		},
		{
			name: "title, summary, tags",
			in: `---
title: Social & Blog Voice
summary: Voice and tone for blog posts, social, marketing copy.
tags:
  - voice
  - content
  - writing
---
# Guide
`,
			wantTitle:   "Social & Blog Voice",
			wantSummary: "Voice and tone for blog posts, social, marketing copy.",
			wantTags:    []string{"voice", "content", "writing"},
			wantSource:  "# Guide\n",
		},
		{
			name: "tags inline list",
			in: `---
title: T
tags: [a, b, c]
---
body`,
			wantTitle:  "T",
			wantTags:   []string{"a", "b", "c"},
			wantSource: "body",
		},
		{
			name:       "malformed yaml falls back gracefully",
			in:         "---\ntitle: ok\nbroken: [unterminated\n---\nbody",
			wantSource: "body",
			// title may or may not be set depending on where the parser gave up;
			// what matters is the source still passes through.
		},
		{
			name:       "unterminated frontmatter passes through raw",
			in:         "---\ntitle: no close\nbody continues",
			wantSource: "---\ntitle: no close\nbody continues",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fm, source := parseFrontmatter(tc.in)
			if tc.wantTitle != "" && fm.Title != tc.wantTitle {
				t.Errorf("title: got %q, want %q", fm.Title, tc.wantTitle)
			}
			if fm.Summary != tc.wantSummary {
				t.Errorf("summary: got %q, want %q", fm.Summary, tc.wantSummary)
			}
			if len(tc.wantTags) > 0 && !reflect.DeepEqual(fm.Tags, tc.wantTags) {
				t.Errorf("tags: got %v, want %v", fm.Tags, tc.wantTags)
			}
			if source != tc.wantSource {
				t.Errorf("source: got %q, want %q", source, tc.wantSource)
			}
		})
	}
}
