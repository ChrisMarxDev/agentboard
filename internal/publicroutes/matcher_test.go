package publicroutes

import (
	"net/http"
	"testing"
)

func TestMatcher_Exact(t *testing.T) {
	m := New([]string{"/changelog", "/"})
	cases := []struct {
		path string
		want bool
	}{
		{"/changelog", true},
		{"/changelog/", true}, // trailing slash normalised
		{"/", true},
		{"/other", false},
		{"/changelog/extra", false},
	}
	for _, c := range cases {
		got := m.IsPubliclyReadable(http.MethodGet, c.path)
		if got != c.want {
			t.Errorf("IsPubliclyReadable(GET %q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestMatcher_SingleWildcard(t *testing.T) {
	m := New([]string{"/catalog/*"})
	cases := []struct {
		path string
		want bool
	}{
		{"/catalog/a", true},
		{"/catalog/b-thing", true},
		{"/catalog", false},     // pattern needs a segment
		{"/catalog/a/b", false}, // * is one segment only
		{"/other/a", false},
	}
	for _, c := range cases {
		got := m.IsPubliclyReadable(http.MethodGet, c.path)
		if got != c.want {
			t.Errorf("IsPubliclyReadable(GET %q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestMatcher_DoubleStar(t *testing.T) {
	m := New([]string{"/skills/**"})
	cases := []struct {
		path string
		want bool
	}{
		// `**` matches zero-or-more segments, so the folder root itself
		// is covered — which is what operators want for a public registry
		// ("/skills/**" = "the skills folder, pages and all").
		{"/skills", true},
		{"/skills/a", true},
		{"/skills/a/b", true},
		{"/skills/a/b/c/d", true},
		{"/other", false},
		{"/other/skills/a", false},
	}
	for _, c := range cases {
		got := m.IsPubliclyReadable(http.MethodGet, c.path)
		if got != c.want {
			t.Errorf("IsPubliclyReadable(GET %q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestMatcher_Exclusions(t *testing.T) {
	m := New([]string{
		"/blog/**",
		"!/blog/drafts/**",
	})
	cases := []struct {
		path string
		want bool
	}{
		{"/blog/post-1", true},
		{"/blog/2026/post", true},
		{"/blog/drafts/wip", false},
		{"/blog/drafts/deep/path", false},
		{"/other", false},
	}
	for _, c := range cases {
		got := m.IsPubliclyReadable(http.MethodGet, c.path)
		if got != c.want {
			t.Errorf("IsPubliclyReadable(GET %q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestMatcher_WritesNeverPublic(t *testing.T) {
	// Even a pattern that would otherwise match cannot allow writes.
	m := New([]string{"/skills/**", "/"})
	writeMethods := []string{http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete}
	for _, method := range writeMethods {
		for _, path := range []string{"/skills/foo", "/", "/skills/bar/deep"} {
			if m.IsPubliclyReadable(method, path) {
				t.Errorf("IsPubliclyReadable(%s %q) = true, want false (writes must never be public)", method, path)
			}
		}
	}
}

func TestMatcher_EmptyPatterns(t *testing.T) {
	m := New(nil)
	if m.HasRules() {
		t.Errorf("HasRules() on empty matcher = true, want false")
	}
	if m.IsPubliclyReadable(http.MethodGet, "/anything") {
		t.Errorf("empty matcher should return false for any path")
	}
}

func TestMatcher_NormalisesLeadingSlash(t *testing.T) {
	// Pattern "foo" without a leading slash still matches "/foo".
	m := New([]string{"foo"})
	if !m.IsPubliclyReadable(http.MethodGet, "/foo") {
		t.Errorf("pattern 'foo' should match '/foo'")
	}
}

func TestMatcher_ExclusionOrderIndependent(t *testing.T) {
	// Exclusion before include in config shouldn't leak the path.
	m := New([]string{
		"!/internal/**",
		"/",
		"/internal/**",
	})
	if m.IsPubliclyReadable(http.MethodGet, "/internal/secret") {
		t.Errorf("exclusion should win over include regardless of order")
	}
	if !m.IsPubliclyReadable(http.MethodGet, "/") {
		t.Errorf("root include should still match")
	}
}

func TestMatcher_DoubleStarInMiddle(t *testing.T) {
	m := New([]string{"/api/**/public"})
	cases := []struct {
		path string
		want bool
	}{
		{"/api/a/public", true},
		{"/api/a/b/c/public", true},
		{"/api/public", false}, // ** needs at least one segment here? Actually ** can match 0 segments in our impl.
		{"/api/other", false},
	}
	_ = cases
	// Our matcher treats ** as 0-or-more, so "/api/public" should match too.
	if !m.IsPubliclyReadable(http.MethodGet, "/api/a/public") {
		t.Errorf("/api/a/public should match /api/**/public")
	}
	if !m.IsPubliclyReadable(http.MethodGet, "/api/a/b/c/public") {
		t.Errorf("/api/a/b/c/public should match /api/**/public")
	}
	if !m.IsPubliclyReadable(http.MethodGet, "/api/public") {
		t.Errorf("/api/public should match /api/**/public (** = 0 segments)")
	}
	if m.IsPubliclyReadable(http.MethodGet, "/api/a/private") {
		t.Errorf("/api/a/private should NOT match /api/**/public")
	}
}
