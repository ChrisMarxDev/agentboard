package auth

import "testing"

func TestPathMatches(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// exact
		{"/api/health", "/api/health", true},
		{"/api/health", "/api/health/x", false},

		// single-star
		{"/api/data/*", "/api/data/foo", true},
		{"/api/data/*", "/api/data/foo.bar", true},
		{"/api/data/*", "/api/data/foo/bar", false}, // * doesn't cross /
		{"/api/data/dev.*", "/api/data/dev.metrics", true},
		{"/api/data/dev.*", "/api/data/devops", false},

		// double-star
		{"/api/data/**", "/api/data/foo", true},
		{"/api/data/**", "/api/data/foo/bar", true},
		{"/api/data/**", "/api/data/dev.metrics.users", true},
		{"/api/data/dev.**", "/api/data/dev.metrics.users", true},
		{"/api/data/dev.**", "/api/data/other", false},

		// Trailing slash handling
		{"/api/content/private/**", "/api/content/private/report", true},
		{"/api/content/private/**", "/api/content/public/report", false},
	}
	for _, c := range cases {
		got := pathMatches(c.pattern, c.path)
		if got != c.want {
			t.Errorf("pathMatches(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestMethodMatches(t *testing.T) {
	if !methodMatches([]string{"*"}, "GET") {
		t.Error("wildcard should match GET")
	}
	if !methodMatches([]string{"GET", "POST"}, "POST") {
		t.Error("explicit POST should match")
	}
	if methodMatches([]string{"GET"}, "POST") {
		t.Error("GET-only should reject POST")
	}
	if !methodMatches([]string{}, "GET") {
		t.Error("empty methods is treated as wildcard for ergonomics")
	}
}

func TestAuthorize_AllowAll_Default(t *testing.T) {
	// No rules, allow_all → every request allowed.
	if !Authorize(ModeAllowAll, nil, "GET", "/api/data/foo") {
		t.Error("allow_all with no rules should allow")
	}
}

func TestAuthorize_RestrictToList_Default(t *testing.T) {
	// No rules, restrict_to_list → every request denied.
	if Authorize(ModeRestrictToList, nil, "GET", "/api/data/foo") {
		t.Error("restrict_to_list with no rules should deny")
	}
}

func TestAuthorize_DenyPattern_WithinAllowAll(t *testing.T) {
	rules := []Rule{
		Deny("/api/data/secrets.**"),
	}
	if Authorize(ModeAllowAll, rules, "GET", "/api/data/secrets.api_key") {
		t.Error("deny rule should block the match")
	}
	if !Authorize(ModeAllowAll, rules, "GET", "/api/data/public.visitors") {
		t.Error("non-matching path should fall through to allow_all")
	}
}

func TestAuthorize_AllowPattern_WithinRestrict(t *testing.T) {
	rules := []Rule{
		Allow("/api/data/marketing.**"),
		Allow("/api/content/marketing/**"),
	}
	if !Authorize(ModeRestrictToList, rules, "PUT", "/api/data/marketing.leads") {
		t.Error("explicit allow should permit")
	}
	if Authorize(ModeRestrictToList, rules, "GET", "/api/data/finance.revenue") {
		t.Error("unmatched path in restrict mode should deny")
	}
}

func TestAuthorize_FirstMatchWins(t *testing.T) {
	// Rules earlier in the list override later ones.
	rules := []Rule{
		Deny("/api/data/secrets.**"),
		Allow("/api/data/**"),
	}
	if Authorize(ModeRestrictToList, rules, "GET", "/api/data/secrets.x") {
		t.Error("earlier deny should beat later allow")
	}
	if !Authorize(ModeRestrictToList, rules, "GET", "/api/data/public.x") {
		t.Error("falls through to second rule")
	}
}

func TestAuthorize_ReadOnlyViewer(t *testing.T) {
	// The "viewer" recipe from AUTH.md.
	rules := []Rule{
		Allow("/api/data/**", "GET"),
		Allow("/api/content/**", "GET"),
	}
	if !Authorize(ModeRestrictToList, rules, "GET", "/api/data/anything") {
		t.Error("viewer should read data")
	}
	if Authorize(ModeRestrictToList, rules, "PUT", "/api/data/anything") {
		t.Error("viewer must NOT write")
	}
}
