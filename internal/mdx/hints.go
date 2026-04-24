package mdx

import (
	"strings"
)

// Hint is a poka-yoke nudge returned alongside a successful write. Agents
// read these from the response, interpret the `code`, and self-correct
// on the next call. Codes are stable snake_case across versions — once a
// code ships, it doesn't rename (per principle #12).
type Hint struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Example string `json:"example,omitempty"`
}

// PageHints inspects a freshly-written PageInfo and returns any guidance
// we have for the author. Today this is narrow: flag pages without a
// summary so they stay discoverable in agentboard_search. Additional
// signals (typo'd tags, tag taxonomy drift, suspiciously short bodies)
// can land here as the product learns what to coach on.
//
// Returns nil when everything looks good — the common case should be
// silent.
func PageHints(p *PageInfo) []Hint {
	if p == nil {
		return nil
	}
	var out []Hint
	if strings.TrimSpace(p.Summary) == "" {
		out = append(out, Hint{
			Code: "missing_summary",
			Message: "Page has no `summary:` in its frontmatter. " +
				"Agents searching the knowledge base rely on title + summary + tags to find relevant pages " +
				"— a missing summary means this page is only findable when someone already knows the exact words in its title or body. " +
				"Add a 1–2 sentence summary describing what this page covers and when an agent should reach for it.",
			Example: exampleFrontmatter,
		})
	}
	return out
}

const exampleFrontmatter = `---
title: Social Voice Guidelines
summary: Tone, voice, and style for blog posts, social media, marketing copy, and newsletters. Covers brand personality, word choice, and common pitfalls.
tags: [voice, content, writing]
---

# Body goes here
`
