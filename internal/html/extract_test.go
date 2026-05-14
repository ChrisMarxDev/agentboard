package html

import "testing"

func TestExtractHTMLTitle(t *testing.T) {
	cases := map[string]string{
		`<!doctype html><title>Hello</title><p>x</p>`:                  "Hello",
		`<!doctype html><title class="x">  spaced  </title>`:           "spaced",
		`<!doctype html><html><head><title>Deep</title></head></html>`: "Deep",
		`<p>no title</p>`:                                              "",
	}
	for in, want := range cases {
		if got := extractHTMLTitle(in); got != want {
			t.Errorf("extractHTMLTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractHTMLBody_FullDocStripsChrome(t *testing.T) {
	in := `<!doctype html>
<html>
<head><title>X</title><style>p{color:red}</style></head>
<body><h1>Hi</h1><p>real content</p></body>
</html>`
	got := extractHTMLBody(in)
	if !contains(got, `<h1>Hi</h1>`) || !contains(got, `<p>real content</p>`) {
		t.Errorf("body = %q, expected to retain h1+p", got)
	}
	if contains(got, "<head>") || contains(got, "<title>") || contains(got, "<!doctype") {
		t.Errorf("body still contains chrome: %q", got)
	}
}

func TestExtractHTMLBody_FragmentPassesThrough(t *testing.T) {
	in := `<h1>Title</h1><p>body content</p>`
	if got := extractHTMLBody(in); got != in {
		t.Errorf("fragment got mangled: %q → %q", in, got)
	}
}

func TestExtractHTMLBody_DoctypeNoBody_StripsHeadOnly(t *testing.T) {
	// Common shape agents will produce: just <!doctype html><title>…</title><style>…</style><actual content>
	in := `<!doctype html><title>X</title><style>p{margin:0}</style><h1>Hi</h1>`
	got := extractHTMLBody(in)
	if contains(got, "<title>") || contains(got, "<!doctype") {
		t.Errorf("chrome leaked: %q", got)
	}
	if !contains(got, "<h1>Hi</h1>") {
		t.Errorf("real content dropped: %q", got)
	}
	if !contains(got, "<style>p{margin:0}</style>") {
		t.Errorf("inline style dropped: %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
