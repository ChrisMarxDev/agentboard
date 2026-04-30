package mcp

import (
	"strings"
	"testing"
)

// TestMCPToolsDoNotExposeAdminPlane is a forever-invariant: no MCP tool
// must ever be able to mint, mutate, rotate, revoke, or inspect identities,
// sessions, bootstrap codes, passwords, or any other admin-plane capability.
// MCP is the agent realm; admins manage the system through the browser.
//
// If you add a new MCP tool that trips this test, either:
//   - rename it if the match was a false positive (audit review required), or
//   - recognize that the capability belongs on /api/admin/* instead.
//
// See AUTH.md §"MCP invariant".
func TestMCPToolsDoNotExposeAdminPlane(t *testing.T) {
	// forbiddenSubstrings is matched case-insensitively against the tool
	// Name AND Description. Substrings are deliberately broad so a
	// creatively-named admin tool can't slip by — false positives here are
	// strictly better than a real regression.
	forbiddenSubstrings := []string{
		"identity", "identities",
		"admin", "administrator",
		"session", "cookie",
		"password", "credential",
		"bootstrap", "setup-code",
		"token", "rotate",
		"revoke",
		"allowlist", "blocklist", "access mode",
	}

	// Whitelist lets us allow specific tool names/descriptions that match a
	// forbidden substring but are legitimate. Keep this list as short as
	// possible — adding entries is how the invariant erodes.
	whitelist := map[string]bool{
		// None today. If you add one, annotate with why and who reviewed it.
	}

	// Construct a minimal Server so toolDefinitions() can run. The tool
	// catalog is static — it doesn't actually query any of the backends.
	s := &Server{}
	tools := s.toolDefinitions()
	if len(tools) == 0 {
		t.Fatal("expected at least one MCP tool; got zero (did the catalog move?)")
	}

	for _, tool := range tools {
		if whitelist[tool.Name] {
			continue
		}
		haystack := strings.ToLower(tool.Name + " " + tool.Description)
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(haystack, forbidden) {
				t.Errorf("MCP tool %q (%q) contains forbidden admin-plane term %q — admin capabilities must NEVER be exposed via MCP. See AUTH.md §\"MCP invariant\".",
					tool.Name, tool.Description, forbidden)
			}
		}
	}
}
