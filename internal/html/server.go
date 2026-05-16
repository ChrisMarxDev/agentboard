// Package html is the server-rendered dashboard surface introduced by
// the substrate pivot (spec-filesystem-substrate.md). Replaces the
// React SPA + MDX renderer with Go templates over the workspace's
// working-tree mirror.
//
// What it does:
//
//   - GET /<path> serves the file at <worktree>/<path>:
//       .md  → renders via goldmark, wrapped in the shell.
//       .html → served as-is (sandboxed iframe in a future cut for
//               untrusted-by-design content; for now inline).
//       .json → pretty-printed with a small "structured data" wrapper
//               (typed views will replace this for known kinds in a
//               later cut).
//       binary → served with the file's content-type, no chrome.
//
//   - GET /<dir>/ either serves index.html if present or auto-renders
//     a GitHub-style directory listing.
//
//   - GET /_static/* serves the embedded design-system.css + future
//     dashboard assets.
//
//   - GET /<file> for an unknown extension falls back to text/plain.
//
// Auth: the shell shows the current user in the top-right. Reads are
// open per the existing public-routes model; admin-only / signed-in
// content stays gated upstream.
package html

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed assets/*
var assetsFS embed.FS

// isServerInternal reports whether a top-level directory entry is
// AgentBoard's own state and must not be exposed via the dashboard.
// Everything else, dotted or not, is legitimate workspace content —
// `.claude`, `.codex`, `.github`, `.gitignore`, etc. are agent-tool
// homes that the dashboard should surface so users can navigate them
// like any other folder.
func isServerInternal(name string) bool {
	return name == ".git" || name == ".agentboard"
}

// CommitInfo mirrors gitserver.CommitInfo so this package doesn't
// take a hard dep on gitserver — keeps the renderer testable in
// isolation. The shape is identical; cli/serve.go bridges the two
// when wiring HistoryFn.
type CommitInfo struct {
	SHA     string
	Short   string
	Author  string
	When    string
	Subject string
}

// SearchHit mirrors search.Hit; same isolation reasoning as CommitInfo.
type SearchHit struct {
	Path        string
	SnippetHTML string // already-escaped HTML with <mark> markers
}

// Server renders the workspace's working tree as HTML. Construct one
// per workspace via New(); cli/serve.go mounts it as the catch-all
// after /_api/ and /git/ are registered.
type Server struct {
	WorktreeRoot string // absolute path to <project>/.agentboard/worktrees/<workspace>/
	Workspace    string // workspace id; shown in the header
	Branch       string // default branch; shown in the header

	// UserResolver returns the current user's name for the request, or
	// empty string for anonymous. Optional — defaults to anonymous.
	UserResolver func(r *http.Request) string

	// IsAdminFn reports whether the current request's user is an
	// admin. Optional; defaults to false. Used to gate the /admin
	// link in the header.
	IsAdminFn func(r *http.Request) bool

	// HistoryFn returns commits that touched `path` on the workspace's
	// default branch, newest first. Set by cli/serve.go; if nil the
	// ?history=1 view falls back to a "not available" message.
	HistoryFn func(path string, limit int) ([]CommitInfo, error)

	// DiffFn returns the textual diff of `path` between `from` and `to`
	// revisions. `from` empty means "the parent of to". Used by the
	// ?diff=<from>..<to> view.
	DiffFn func(path, from, to string) (string, error)

	// SearchFn queries the full-text index. Set by cli/serve.go; if
	// nil the search input + ?q=… view fall back to "not available".
	SearchFn func(q string, limit int) ([]SearchHit, error)

	once     sync.Once
	tmpl     *template.Template
	md       goldmark.Markdown
	assetsMu http.FileSystem
}

func (s *Server) lazyInit() {
	s.once.Do(func() {
		t := template.New("").Funcs(template.FuncMap{
			"short": func(v string) string {
				if len(v) > 12 {
					return v[:12]
				}
				return v
			},
		})
		// Walk the embedded templates and parse them all.
		_ = fs.WalkDir(templateFS, "templates", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, rerr := templateFS.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			_, perr := t.Parse(string(data))
			return perr
		})
		s.tmpl = t

		s.md = goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithRendererOptions(),
		)

		sub, err := fs.Sub(assetsFS, "assets")
		if err == nil {
			s.assetsMu = http.FS(sub)
		}
	})
}

// ServeHTTP dispatches reads. Layout:
//
//   - /_static/*  → embedded assets (design-system.css, etc.)
//   - /          → render index.md / index.html if any, else dir listing
//   - everything else → file under worktree
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.lazyInit()
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	urlPath := r.URL.Path
	if strings.HasPrefix(urlPath, "/_static/") {
		s.serveStatic(w, r, strings.TrimPrefix(urlPath, "/_static/"))
		return
	}

	// ?history=1 — render git log for this path instead of the file.
	if r.URL.Query().Get("history") != "" {
		s.renderHistory(w, r, urlPath)
		return
	}
	// ?diff=<from>..<to> or ?diff=<sha> (parent-of-sha implicit) — render
	// the diff for this path between the two revisions.
	if d := r.URL.Query().Get("diff"); d != "" {
		s.renderDiff(w, r, urlPath, d)
		return
	}
	// ?q=needle — full-text search results page. Top-level only, so
	// "/foo?q=bar" still pulls the file at /foo.
	if q := r.URL.Query().Get("q"); q != "" && (urlPath == "/" || urlPath == "") {
		s.renderSearch(w, r, q)
		return
	}
	// ?edit=1 — render an edit form for the file (cookie-auth required).
	// The companion POST handler lives at /_api/edit so non-GET traffic
	// goes through the normal gated stack.
	if r.URL.Query().Get("edit") != "" {
		s.renderEdit(w, r, urlPath)
		return
	}

	rel := strings.TrimPrefix(urlPath, "/")
	abs, ok := s.resolvePath(rel)
	if !ok {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Auto-suffix: try .md or .html for extension-less paths.
			if filepath.Ext(abs) == "" {
				for _, ext := range []string{".html", ".md"} {
					if i, ierr := os.Stat(abs + ext); ierr == nil && !i.IsDir() {
						s.renderFile(w, r, urlPath, abs+ext, i)
						return
					}
				}
			}
			s.render404(w, r, urlPath)
			return
		}
		http.Error(w, "stat: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		s.renderDirectory(w, r, urlPath, abs)
		return
	}
	s.renderFile(w, r, urlPath, abs, info)
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request, sub string) {
	if s.assetsMu == nil {
		http.Error(w, "static assets unavailable", http.StatusInternalServerError)
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + sub
	http.FileServer(s.assetsMu).ServeHTTP(w, r2)
}

// StaticHandler returns a handler that serves the embedded /_static/*
// asset bundle (design-system.css, theme.default.css, etc.). Mounted
// at /_static/* by the server outside the auth gate so the login
// page can pull design-system.css while anonymous.
func (s *Server) StaticHandler() http.Handler {
	s.lazyInit()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/_static/")
		s.serveStatic(w, r, sub)
	})
}

// resolvePath joins `rel` to the worktree root and rejects anything
// that escapes via .. or symlinks. Returns the cleaned absolute path
// (which may not exist on disk).
func (s *Server) resolvePath(rel string) (string, bool) {
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return s.WorktreeRoot, true
	}
	// Reject .. anywhere; reject absolute paths.
	if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	joined := filepath.Join(s.WorktreeRoot, rel)
	// Ensure joined is still under worktree.
	root, _ := filepath.Abs(s.WorktreeRoot)
	clean, _ := filepath.Abs(joined)
	if !strings.HasPrefix(clean, root) {
		return "", false
	}
	return clean, true
}

// renderDirectory serves <dir>/index.html if it exists; otherwise
// builds a directory listing.
func (s *Server) renderDirectory(w http.ResponseWriter, r *http.Request, urlPath, abs string) {
	for _, idx := range []string{"index.html", "index.md"} {
		candidate := filepath.Join(abs, idx)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			s.renderFile(w, r, urlPath, candidate, info)
			return
		}
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		http.Error(w, "readdir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	type entry struct {
		Name  string
		Href  string
		IsDir bool
	}
	var visible []entry
	for _, e := range entries {
		name := e.Name()
		if isServerInternal(name) {
			continue
		}
		href := path.Join(urlPath, name)
		if e.IsDir() {
			href += "/"
		}
		visible = append(visible, entry{Name: name, Href: href, IsDir: e.IsDir()})
	}
	sort.Slice(visible, func(i, j int) bool {
		if visible[i].IsDir != visible[j].IsDir {
			return visible[i].IsDir
		}
		return visible[i].Name < visible[j].Name
	})
	var body bytes.Buffer
	_ = s.tmpl.ExecuteTemplate(&body, "dir", map[string]any{
		"Title":   strings.TrimSuffix(urlPath, "/"),
		"Entries": visible,
	})
	// Signed-in users get a tiny "new file" form below the listing.
	// The form fires a GET that lands on /<dir>/<name>?edit=1 which
	// the editor handler turns into a create-on-save.
	if s.userIsSignedIn(r) {
		dirPrefix := strings.TrimSuffix(urlPath, "/")
		if dirPrefix == "" {
			dirPrefix = ""
		}
		fmt.Fprintf(&body, `<form class="new-file" action="" method="get" onsubmit="
  var name=this.elements['name'].value.trim();
  if(!name){return false}
  if(name.startsWith('/')){name=name.slice(1)}
  window.location='%s/'+name+'?edit=1';return false">
<style>
  .new-file{margin-top:1.5rem;padding-top:1rem;border-top:1px solid var(--border);display:flex;gap:.5rem;align-items:center;font-size:.85rem;color:var(--text-secondary)}
  .new-file input{flex:1;max-width:320px;padding:.4rem .65rem;border:1px solid var(--border);border-radius:6px;background:var(--bg);color:var(--text);font-family:var(--ab-mono,monospace);font-size:.85rem}
  .new-file button{padding:.4rem .85rem;background:var(--accent);color:#fff;border:0;border-radius:6px;cursor:pointer;font-size:.85rem}
</style>
+ <input name="name" placeholder="new-file.html, notes/today.md, …" autocomplete="off">
<button type="submit">Create</button>
</form>`, template.HTMLEscapeString(dirPrefix))
	}
	s.renderShell(w, r, urlPath, "", template.HTML(body.String()), nil, false)
}

// renderFile picks a renderer based on extension and serves the file.
//
// Rich types (.md, .html, .json) render inline with their own
// formatters. Everything else routes through a preview wrapper that
// shows the file with metadata + a download button:
//   - images embedded via <img>
//   - text-ish files in a <pre>
//   - everything else as a metadata-only "Download" page
//
// Three escape hatches let the wrapper get out of the way:
//   - ?raw=1                — write the raw bytes with the proper
//                             content-type. Used by <img src> embeds.
//   - ?download=1           — same, plus Content-Disposition:
//                             attachment. Used by the Download button.
//   - Accept header missing "text/html" — the caller is a curl-style
//                             tool or a resource fetcher (<img>,
//                             <link>, <script>). Send raw bytes.
//
// .md/.html/.json always render rich; the Accept-based escape doesn't
// apply to them because those are meant for browser consumption.
func (s *Server) renderFile(w http.ResponseWriter, r *http.Request, urlPath, abs string, info os.FileInfo) {
	data, err := os.ReadFile(abs)
	if err != nil {
		http.Error(w, "read: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ext := strings.ToLower(filepath.Ext(abs))

	// Rich-render paths short-circuit. They never use ?raw=1.
	switch ext {
	case ".md", ".mdx":
		s.renderMarkdown(w, r, urlPath, data)
		return
	case ".html", ".htm":
		s.renderHTML(w, r, urlPath, data)
		return
	case ".json":
		s.renderJSON(w, r, urlPath, data)
		return
	}

	wantRaw := r.URL.Query().Get("raw") != ""
	wantDownload := r.URL.Query().Get("download") != ""
	// Preview vs raw — two signals, combined defensively because no
	// single header is reliable through proxies (Cloudflare Tunnel
	// strips Sec-Fetch-Dest in some configs).
	//
	//   Sec-Fetch-Dest (modern browsers): "document"/"iframe" =
	//     top-level navigation → preview. Anything else (image,
	//     script, style, font, ...) = resource fetch → raw.
	//   Accept header: legacy fallback. text/html present → preview.
	//     Otherwise (image/*, application/json, */*) → raw.
	//
	// Preview wins only when at least one signal says so. <img src>
	// embeds without Sec-Fetch-Dest still send Accept: image/* — the
	// Accept check correctly routes them to raw.
	dest := r.Header.Get("Sec-Fetch-Dest")
	acceptHTML := strings.Contains(r.Header.Get("Accept"), "text/html")
	preview := false
	switch {
	case dest == "document" || dest == "iframe":
		preview = true
	case dest != "" && dest != "empty":
		preview = false // explicit resource fetch
	default:
		preview = acceptHTML
	}
	if wantRaw || wantDownload || !preview {
		ct := contentTypeFor(ext)
		w.Header().Set("Content-Type", ct)
		if wantDownload {
			w.Header().Set("Content-Disposition",
				`attachment; filename="`+filepath.Base(abs)+`"`)
		}
		_, _ = w.Write(data)
		return
	}

	// Preview-shell paths.
	switch {
	case isImageExt(ext):
		s.renderImagePreview(w, r, urlPath, abs, info, ext)
	case isTextishExt(ext):
		s.renderTextPreview(w, r, urlPath, abs, info, string(data))
	default:
		s.renderBinaryPreview(w, r, urlPath, abs, info, ext)
	}
}

// contentTypeFor maps an extension to its served-as content-type.
// Used by the raw / download path; the preview wrappers don't need it.
func contentTypeFor(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".bmp":
		return "image/bmp"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".ndjson":
		return "application/x-ndjson"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".tsv":
		return "text/tab-separated-values; charset=utf-8"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".txt", ".log", ".yaml", ".yml", ".toml", ".ini":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

func isImageExt(ext string) bool {
	switch ext {
	case ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".bmp":
		return true
	}
	return false
}

func isTextishExt(ext string) bool {
	switch ext {
	case ".txt", ".log", ".csv", ".tsv", ".ndjson",
		".yaml", ".yml", ".toml", ".ini",
		".css", ".js", ".sh", ".env":
		return true
	}
	return false
}

// splitFrontmatter pulls a YAML frontmatter block off a markdown / HTML
// file. Returns the body and the parsed map; empty map when no
// frontmatter is present.
func splitFrontmatter(raw []byte) (map[string]any, string) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return map[string]any{}, s
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return map[string]any{}, s
	}
	fmText := s[4 : 4+end]
	body := s[4+end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	fm := map[string]any{}
	_ = yaml.Unmarshal([]byte(fmText), &fm)
	return fm, body
}

func (s *Server) renderMarkdown(w http.ResponseWriter, r *http.Request, urlPath string, raw []byte) {
	fm, body := splitFrontmatter(raw)
	var out bytes.Buffer
	if err := s.md.Convert([]byte(body), &out); err != nil {
		http.Error(w, "markdown: "+err.Error(), http.StatusInternalServerError)
		return
	}
	title := titleFromFrontmatter(fm)
	if title == "" {
		title = pathLabel(urlPath)
	}
	wide := isWide(fm)
	s.renderShell(w, r, urlPath, title, template.HTML(out.String()), metaBarFrom(fm), wide)
}

func (s *Server) renderHTML(w http.ResponseWriter, r *http.Request, urlPath string, raw []byte) {
	// For trusted authored HTML we extract the page <title> + the
	// <body> contents (if the file is a full HTML document) and inline
	// the result into the shell. Agents writing partial fragments work
	// too — those pass through unchanged. A future cut serves arbitrary
	// HTML on usercontent.<host> via a sandbox iframe; until that
	// origin exists, inline-into-shell is the chosen rendering.
	//
	// Frontmatter still works (YAML `---` block at the very top) for
	// agents who prefer it; full HTML docs win when both are present.
	fm, rest := splitFrontmatter(raw)
	title := titleFromFrontmatter(fm)
	if t := extractHTMLTitle(rest); t != "" {
		title = t
	}
	body := extractHTMLBody(rest)
	if title == "" {
		title = pathLabel(urlPath)
	}
	wide := isWide(fm)
	s.renderShell(w, r, urlPath, title, template.HTML(body), metaBarFrom(fm), wide)
}

// extractHTMLTitle pulls the inner text out of the first <title>…</title>
// in a document. Tolerant of attributes and whitespace; returns "" when
// no title is present.
func extractHTMLTitle(s string) string {
	lower := strings.ToLower(s)
	start := strings.Index(lower, "<title")
	if start < 0 {
		return ""
	}
	gt := strings.Index(lower[start:], ">")
	if gt < 0 {
		return ""
	}
	inner := start + gt + 1
	end := strings.Index(lower[inner:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(s[inner : inner+end])
}

// extractHTMLBody returns the inside of the first <body>…</body> if the
// document has one. Files written as fragments (no <html>/<body>) pass
// through unchanged. Drops doctype, <head>, and the <body>'s own tag
// when present so the shell's chrome doesn't get nested inside another
// document.
func extractHTMLBody(s string) string {
	lower := strings.ToLower(s)
	start := strings.Index(lower, "<body")
	if start < 0 {
		// Strip a leading doctype if the agent wrote a full doc without
		// a <body> wrapper (which is valid HTML — the body element is
		// optional). Also strip any <title>/<head> tags since the shell
		// owns the document <head>.
		out := stripTag(s, "head")
		out = stripTag(out, "title")
		out = stripDoctype(out)
		return out
	}
	gt := strings.Index(lower[start:], ">")
	if gt < 0 {
		return s
	}
	bodyStart := start + gt + 1
	end := strings.LastIndex(lower, "</body>")
	if end < bodyStart {
		return s[bodyStart:]
	}
	return s[bodyStart:end]
}

// stripDoctype removes a leading <!doctype …> declaration (case-
// insensitive). No-op if absent.
func stripDoctype(s string) string {
	t := strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(strings.ToLower(t), "<!doctype") {
		return s
	}
	gt := strings.Index(t, ">")
	if gt < 0 {
		return s
	}
	return strings.TrimLeft(t[gt+1:], "\r\n")
}

// stripTag removes the first <name …>…</name> block (case-insensitive)
// from s. No-op if not found. Used to scrub <head> and <title> when
// they appear outside a <body> wrapper — the shell owns the document
// <head> and shouldn't nest a stray one inside <main>.
func stripTag(s, name string) string {
	lower := strings.ToLower(s)
	open := "<" + name
	close := "</" + name + ">"
	start := strings.Index(lower, open)
	if start < 0 {
		return s
	}
	end := strings.Index(lower[start:], close)
	if end < 0 {
		return s
	}
	return s[:start] + s[start+end+len(close):]
}

func (s *Server) renderJSON(w http.ResponseWriter, r *http.Request, urlPath string, raw []byte) {
	// Typed view: if the JSON's shape matches a taskboard, render it
	// as a kanban board. Otherwise fall back to pretty-printed JSON.
	if tb, ok := parseTaskboard(raw); ok {
		title := tb.Title
		if title == "" {
			title = pathLabel(urlPath)
		}
		s.renderShell(w, r, urlPath, title, renderTaskboardBody(tb), nil, true)
		return
	}

	pretty := indentJSON(raw)
	body := fmt.Sprintf(`<h1>%s</h1><pre><code>%s</code></pre>`,
		template.HTMLEscapeString(pathLabel(urlPath)),
		template.HTMLEscapeString(pretty))
	s.renderShell(w, r, urlPath, pathLabel(urlPath), template.HTML(body), nil, false)
}

func (s *Server) renderHistory(w http.ResponseWriter, r *http.Request, urlPath string) {
	rel := strings.Trim(strings.TrimPrefix(urlPath, "/"), "/")
	title := pathLabel(urlPath)
	if title == "" || title == "Home" {
		title = "Workspace history"
	} else {
		title = "History — " + title
	}

	var body bytes.Buffer
	fmt.Fprintf(&body, `<h1>%s</h1>`, template.HTMLEscapeString(title))
	fmt.Fprintf(&body, `<p class="ab-muted">Commits that touched <code>%s</code>, newest first. `+
		`<a href="%s">Back to the file</a>.</p>`,
		template.HTMLEscapeString("/"+rel),
		template.HTMLEscapeString("/"+rel))

	if s.HistoryFn == nil {
		body.WriteString(`<p class="ab-muted">History is not available on this instance.</p>`)
		s.renderShell(w, r, urlPath, title, template.HTML(body.String()), nil, false)
		return
	}
	commits, err := s.HistoryFn(rel, 100)
	if err != nil {
		fmt.Fprintf(&body, `<p class="ab-muted">Could not load history: %s</p>`, template.HTMLEscapeString(err.Error()))
		s.renderShell(w, r, urlPath, title, template.HTML(body.String()), nil, false)
		return
	}
	if len(commits) == 0 {
		body.WriteString(`<p class="ab-muted">No commits yet for this path.</p>`)
		s.renderShell(w, r, urlPath, title, template.HTML(body.String()), nil, false)
		return
	}
	body.WriteString(`<style>
  .history { list-style: none; padding: 0; margin: 1.5rem 0; }
  .history > li { padding: .75rem 0; border-bottom: 1px solid var(--border);
    display: grid; grid-template-columns: auto 1fr auto; gap: 1rem;
    align-items: baseline; font-size: .9rem; }
  .history .sha { font-family: var(--ab-mono, monospace);
    color: var(--accent); font-size: .85rem; }
  .history .subject { color: var(--text); }
  .history .meta { color: var(--text-secondary); font-size: .75rem;
    text-align: right; font-variant-numeric: tabular-nums; }
</style>`)
	// Signed-in users get a one-click restore button per row. Looks
	// like a small grey link; submits a tiny inline form that POSTs
	// to /_api/restore with the file's CSRF token.
	canRestore := s.userIsSignedIn(r)
	csrf := ""
	if canRestore {
		if c, err := r.Cookie("agentboard_csrf"); err == nil {
			csrf = c.Value
		}
	}
	body.WriteString(`<ol class="history">`)
	for i, c := range commits {
		fmt.Fprintf(&body, `<li><a class="sha" href="?diff=%s">%s</a>`+
			`<span class="subject">%s</span>`+
			`<span class="meta">%s · %s%s</span></li>`,
			template.HTMLEscapeString(c.SHA),
			template.HTMLEscapeString(c.Short),
			template.HTMLEscapeString(c.Subject),
			template.HTMLEscapeString(c.Author),
			template.HTMLEscapeString(formatWhen(c.When)),
			restoreButtonHTML(canRestore, csrf, rel, c.SHA, i == 0),
		)
	}
	body.WriteString(`</ol>`)
	s.renderShell(w, r, urlPath, title, template.HTML(body.String()), nil, false)
}

// restoreButtonHTML renders an inline form for "(restore)" alongside
// a history row. Empty string when the visitor isn't signed in, or
// for the most-recent commit (no point restoring to current state).
func restoreButtonHTML(canRestore bool, csrf, path, sha string, isCurrent bool) string {
	if !canRestore || isCurrent {
		return ""
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, ` · <form method="post" action="/_api/restore" `+
		`style="display:inline" `+
		`onsubmit="return confirm('Restore /%s to %s?')">`+
		`<input type="hidden" name="path" value="%s">`+
		`<input type="hidden" name="sha" value="%s">`+
		`<input type="hidden" name="csrf" value="%s">`+
		`<button type="submit" `+
		`style="background:none;border:0;color:var(--accent);cursor:pointer;font:inherit;padding:0">restore</button>`+
		`</form>`,
		template.HTMLEscapeString(path),
		template.HTMLEscapeString(sha[:min(10, len(sha))]),
		template.HTMLEscapeString(path),
		template.HTMLEscapeString(sha),
		template.HTMLEscapeString(csrf),
	)
	return b.String()
}

// min is the smaller of two ints; Go 1.21+ has it as a builtin but
// we keep a local definition to stay compatible with older toolchains.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderDiff serves ?diff=<sha> or ?diff=<from>..<to>.
func (s *Server) renderDiff(w http.ResponseWriter, r *http.Request, urlPath, diffSpec string) {
	rel := strings.Trim(strings.TrimPrefix(urlPath, "/"), "/")
	from, to := parseDiffSpec(diffSpec)
	title := "Diff — " + pathLabel(urlPath)

	var body bytes.Buffer
	fmt.Fprintf(&body, `<h1>%s</h1>`, template.HTMLEscapeString(title))
	fmt.Fprintf(&body, `<p class="ab-muted">Showing <code>%s..%s</code> for <code>%s</code>. `+
		`<a href="?history=1">Back to history</a> · <a href="%s">view file</a>.</p>`,
		template.HTMLEscapeString(shortRev(from, "parent")),
		template.HTMLEscapeString(shortRev(to, "")),
		template.HTMLEscapeString("/"+rel),
		template.HTMLEscapeString("/"+rel))

	if s.DiffFn == nil {
		body.WriteString(`<p class="ab-muted">Diff is not available on this instance.</p>`)
		s.renderShell(w, r, urlPath, title, template.HTML(body.String()), nil, false)
		return
	}
	raw, err := s.DiffFn(rel, from, to)
	if err != nil {
		fmt.Fprintf(&body, `<p class="ab-muted">Could not load diff: %s</p>`, template.HTMLEscapeString(err.Error()))
		s.renderShell(w, r, urlPath, title, template.HTML(body.String()), nil, false)
		return
	}
	if strings.TrimSpace(raw) == "" {
		body.WriteString(`<p class="ab-muted">No textual diff (same content or empty change).</p>`)
		s.renderShell(w, r, urlPath, title, template.HTML(body.String()), nil, false)
		return
	}
	body.WriteString(`<style>
  .diff { font-family: var(--ab-mono, monospace); font-size: .8rem;
    background: var(--bg-secondary); border: 1px solid var(--border);
    border-radius: var(--ab-radius); padding: .75rem 1rem; overflow-x: auto;
    line-height: 1.4; }
  .diff .line { display: block; padding: 0 .25rem; white-space: pre; }
  .diff .add { background: rgba(34,197,94,.12); color: var(--success); }
  .diff .del { background: rgba(220,38,38,.12); color: var(--error); }
  .diff .hunk { color: var(--accent); font-weight: 500; }
  .diff .meta { color: var(--text-secondary); }
</style>`)
	body.WriteString(`<div class="diff">`)
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		cls := ""
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index "):
			cls = "meta"
		case strings.HasPrefix(line, "@@"):
			cls = "hunk"
		case strings.HasPrefix(line, "+"):
			cls = "add"
		case strings.HasPrefix(line, "-"):
			cls = "del"
		}
		fmt.Fprintf(&body, `<span class="line %s">%s</span>`, cls,
			template.HTMLEscapeString(line))
	}
	body.WriteString(`</div>`)
	s.renderShell(w, r, urlPath, title, template.HTML(body.String()), nil, false)
}

// renderEdit serves /<path>?edit=1 as an HTML editor form. The form
// POSTs to /_api/edit (a real, CSRF-gated endpoint in the server
// package). Anonymous visitors get bounced to /login?next=...
func (s *Server) renderEdit(w http.ResponseWriter, r *http.Request, urlPath string) {
	rel := strings.Trim(strings.TrimPrefix(urlPath, "/"), "/")
	if rel == "" {
		http.Error(w, "edit requires a file path", http.StatusBadRequest)
		return
	}
	// Sign-in gate. The HistoryFn-style soft auth attaches the user
	// to the request context; if there's no user, bounce to /login.
	if !s.userIsSignedIn(r) {
		http.Redirect(w, r,
			"/login?next="+url.QueryEscape(urlPath+"?edit=1"),
			http.StatusFound)
		return
	}
	// Pull the CSRF cookie value so we can echo it into the form.
	csrf := ""
	if c, err := r.Cookie("agentboard_csrf"); err == nil {
		csrf = c.Value
	}

	abs, ok := s.resolvePath(rel)
	if !ok {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	body := ""
	if data, err := os.ReadFile(abs); err == nil {
		body = string(data)
	}

	title := "Edit — " + pathLabel(urlPath)
	var buf bytes.Buffer
	buf.WriteString(`<style>
  form.editor { display: flex; flex-direction: column; gap: .85rem; }
  form.editor label { font-size: .85rem; color: var(--text-secondary); font-weight: 500; }
  form.editor textarea { width: 100%; min-height: 60vh; padding: .85rem;
    font-family: var(--ab-mono, monospace); font-size: .85rem; line-height: 1.5;
    border: 1px solid var(--border); border-radius: var(--ab-radius);
    background: var(--bg-secondary); color: var(--text); resize: vertical; }
  form.editor textarea:focus { outline: none; border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-light); }
  form.editor input[type=text] { padding: .55rem .75rem;
    border: 1px solid var(--border); border-radius: 6px;
    background: var(--bg); color: var(--text); font-size: .9rem; }
  form.editor .actions { display: flex; gap: .5rem; justify-content: flex-end; }
  form.editor button { padding: .55rem 1.25rem; background: var(--accent);
    color: #fff; border: 0; border-radius: 6px; font-weight: 500;
    cursor: pointer; }
  form.editor a.cancel { padding: .55rem 1.25rem; color: var(--text-secondary);
    text-decoration: none; align-self: center; }
</style>`)
	fmt.Fprintf(&buf, `<h1>Edit /%s</h1>
<p class="ab-muted">Changes commit as <strong>@%s</strong>. The dashboard
re-renders on save.</p>
<form class="editor" method="post" action="/_api/edit">
  <input type="hidden" name="path" value="%s">
  <input type="hidden" name="csrf" value="%s">
  <label for="ab-edit-body">File body</label>
  <textarea id="ab-edit-body" name="body" spellcheck="false">%s</textarea>
  <label for="ab-edit-msg">Commit message</label>
  <input type="text" id="ab-edit-msg" name="message" value="Edit %s">
  <div class="actions">
    <a class="cancel" href="%s">Cancel</a>
    <button type="submit">Save</button>
  </div>
</form>`,
		template.HTMLEscapeString(rel),
		template.HTMLEscapeString(s.UserResolver(r)),
		template.HTMLEscapeString(rel),
		template.HTMLEscapeString(csrf),
		template.HTMLEscapeString(body),
		template.HTMLEscapeString(rel),
		template.HTMLEscapeString(urlPath),
	)
	s.renderShell(w, r, urlPath, title, template.HTML(buf.String()), nil, true)
}

// userIsSignedIn reports whether the request has an authenticated
// user attached to context. Wraps UserResolver to keep the edit-form
// gate concise.
func (s *Server) userIsSignedIn(r *http.Request) bool {
	if s.UserResolver == nil {
		return false
	}
	return s.UserResolver(r) != ""
}

// renderSearch serves /?q=needle as server-rendered results.
func (s *Server) renderSearch(w http.ResponseWriter, r *http.Request, q string) {
	title := "Search — " + q

	var body bytes.Buffer
	body.WriteString(`<style>
  .search-form { display: flex; gap: .5rem; margin: 1rem 0 1.5rem; }
  .search-form input { flex: 1; padding: .55rem .75rem;
    border: 1px solid var(--border); border-radius: 6px;
    background: var(--bg); color: var(--text); font-size: 1rem; }
  .search-form button { padding: .55rem 1.25rem; background: var(--accent);
    color: #fff; border: 0; border-radius: 6px; font-weight: 500;
    cursor: pointer; }
  .hits { list-style: none; padding: 0; margin: 0; }
  .hits > li { padding: .85rem 0; border-bottom: 1px solid var(--border); }
  .hits .path { font-family: var(--ab-mono, monospace);
    color: var(--accent); text-decoration: none; font-size: .9rem; }
  .hits .path:hover { text-decoration: underline; }
  .hits .snip { margin: .35rem 0 0; color: var(--text-secondary);
    font-size: .9rem; line-height: 1.5; }
  .hits .snip mark { background: var(--accent-light); color: var(--text);
    padding: 0 .15rem; border-radius: 2px; }
</style>`)
	fmt.Fprintf(&body, `<h1>Search</h1>
<form class="search-form" method="get" action="/">
  <input name="q" value="%s" placeholder="Search the workspace…" autofocus>
  <button type="submit">Search</button>
</form>`, template.HTMLEscapeString(q))

	if s.SearchFn == nil {
		body.WriteString(`<p class="ab-muted">Search is not available on this instance.</p>`)
		s.renderShell(w, r, "/", title, template.HTML(body.String()), nil, false)
		return
	}
	hits, err := s.SearchFn(q, 50)
	if err != nil {
		fmt.Fprintf(&body, `<p class="ab-muted">Search failed: %s</p>`, template.HTMLEscapeString(err.Error()))
		s.renderShell(w, r, "/", title, template.HTML(body.String()), nil, false)
		return
	}
	if len(hits) == 0 {
		fmt.Fprintf(&body, `<p class="ab-muted">No matches for <code>%s</code>.</p>`, template.HTMLEscapeString(q))
		s.renderShell(w, r, "/", title, template.HTML(body.String()), nil, false)
		return
	}
	fmt.Fprintf(&body, `<p class="ab-muted">%d matches.</p><ol class="hits">`, len(hits))
	for _, h := range hits {
		fmt.Fprintf(&body, `<li><a class="path" href="/%s">/%s</a><p class="snip">%s</p></li>`,
			template.HTMLEscapeString(h.Path),
			template.HTMLEscapeString(h.Path),
			h.SnippetHTML)
	}
	body.WriteString(`</ol>`)
	s.renderShell(w, r, "/", title, template.HTML(body.String()), nil, false)
}

// parseDiffSpec splits `<from>..<to>` or `<sha>` (implicit parent) into
// (from, to). Validation lives in the gitserver layer.
func parseDiffSpec(spec string) (from, to string) {
	if i := strings.Index(spec, ".."); i >= 0 {
		return spec[:i], spec[i+2:]
	}
	return "", spec
}

// shortRev shortens a revision identifier for display. Falls back to
// `def` for the empty string (used in ?diff=<sha> implicit-parent case).
func shortRev(rev, def string) string {
	if rev == "" {
		return def
	}
	if len(rev) > 10 {
		return rev[:10]
	}
	return rev
}

// formatWhen makes ISO-8601 timestamps a bit friendlier. Falls back
// to the raw value for inputs we don't understand.
func formatWhen(iso string) string {
	if iso == "" {
		return ""
	}
	// 2026-05-14T01:23:45+02:00 → "2026-05-14 01:23"
	if len(iso) >= 16 {
		return iso[:10] + " " + iso[11:16]
	}
	return iso
}

// previewMeta is the data passed to the preview header partial.
type previewMeta struct {
	Path        string
	Filename    string
	Size        string
	ContentType string
	LastAuthor  string
	LastWhen    string
	DownloadURL string
	RawURL      string
}

// metaFor builds the metadata strip shown above every preview shell.
// Last-edit comes from the workspace's git history (one-row lookup);
// nil-safe if HistoryFn isn't wired.
func (s *Server) metaFor(urlPath string, info os.FileInfo, ext string) previewMeta {
	rel := strings.Trim(strings.TrimPrefix(urlPath, "/"), "/")
	m := previewMeta{
		Path:        urlPath,
		Filename:    filepath.Base(urlPath),
		Size:        humanSize(info.Size()),
		ContentType: contentTypeFor(ext),
		DownloadURL: urlPath + "?download=1",
		RawURL:      urlPath + "?raw=1",
	}
	if s.HistoryFn != nil {
		if commits, err := s.HistoryFn(rel, 1); err == nil && len(commits) > 0 {
			m.LastAuthor = commits[0].Author
			m.LastWhen = formatWhen(commits[0].When)
		}
	}
	return m
}

// humanSize formats a byte count as B / KB / MB.
func humanSize(n int64) string {
	const k = 1024
	if n < k {
		return fmt.Sprintf("%d B", n)
	}
	if n < k*k {
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
}

// previewHeader writes the shared metadata strip into buf.
func previewHeader(buf *bytes.Buffer, m previewMeta) {
	fmt.Fprintf(buf, `<style>
  .preview-meta { display: grid; grid-template-columns: 1fr auto; gap: .5rem 1rem;
    align-items: center; margin: 0 0 1.5rem; padding-bottom: .85rem;
    border-bottom: 1px solid var(--border); font-size: .85rem;
    color: var(--text-secondary); }
  .preview-meta h1 { margin: 0; font-size: 1.25rem; font-family: var(--ab-mono,monospace);
    color: var(--text); word-break: break-all; }
  .preview-meta dl { margin: 0; display: flex; flex-wrap: wrap; gap: .25rem 1.25rem;
    font-size: .8rem; }
  .preview-meta dl div { display: flex; gap: .35rem; }
  .preview-meta dt { color: var(--text-secondary); }
  .preview-meta dd { margin: 0; color: var(--text); }
  .preview-actions { display: flex; gap: .5rem; }
  .preview-actions a { padding: .4rem .85rem; background: var(--accent); color: #fff;
    border-radius: 6px; text-decoration: none; font-size: .85rem; font-weight: 500; }
  .preview-actions a.secondary { background: var(--bg-secondary); color: var(--text);
    border: 1px solid var(--border); }
  .preview-actions a:hover { filter: brightness(1.05); }
  @media (max-width: 600px) {
    .preview-meta { grid-template-columns: 1fr; }
    .preview-actions { justify-content: flex-start; }
  }
</style>
<div class="preview-meta">
  <div>
    <h1>%s</h1>
    <dl>
      <div><dt>size</dt><dd>%s</dd></div>
      <div><dt>type</dt><dd>%s</dd></div>`,
		template.HTMLEscapeString(m.Filename),
		template.HTMLEscapeString(m.Size),
		template.HTMLEscapeString(m.ContentType))
	if m.LastAuthor != "" {
		fmt.Fprintf(buf, `<div><dt>last edit</dt><dd>@%s · %s</dd></div>`,
			template.HTMLEscapeString(m.LastAuthor),
			template.HTMLEscapeString(m.LastWhen))
	}
	fmt.Fprintf(buf, `</dl>
  </div>
  <div class="preview-actions">
    <a class="secondary" href="%s">View raw</a>
    <a href="%s">Download</a>
  </div>
</div>`,
		template.HTMLEscapeString(m.RawURL),
		template.HTMLEscapeString(m.DownloadURL))
}

func (s *Server) renderImagePreview(w http.ResponseWriter, r *http.Request, urlPath, abs string, info os.FileInfo, ext string) {
	m := s.metaFor(urlPath, info, ext)
	var buf bytes.Buffer
	previewHeader(&buf, m)
	fmt.Fprintf(&buf, `<style>
  .img-preview { background: var(--bg-secondary); border: 1px solid var(--border);
    border-radius: var(--ab-radius, 8px); padding: 2rem; text-align: center;
    margin: 1rem 0; }
  .img-preview img { max-width: 100%%; max-height: 70vh; height: auto;
    background:
      linear-gradient(45deg, var(--bg) 25%%, transparent 25%%),
      linear-gradient(-45deg, var(--bg) 25%%, transparent 25%%),
      linear-gradient(45deg, transparent 75%%, var(--bg) 75%%),
      linear-gradient(-45deg, transparent 75%%, var(--bg) 75%%);
    background-size: 16px 16px;
    background-position: 0 0, 0 8px, 8px -8px, -8px 0; }
</style>
<div class="img-preview"><img src="%s" alt="%s"></div>`,
		template.HTMLEscapeString(m.RawURL),
		template.HTMLEscapeString(m.Filename))
	s.renderShell(w, r, urlPath, m.Filename, template.HTML(buf.String()), nil, true)
}

func (s *Server) renderTextPreview(w http.ResponseWriter, r *http.Request, urlPath, abs string, info os.FileInfo, body string) {
	ext := strings.ToLower(filepath.Ext(abs))
	m := s.metaFor(urlPath, info, ext)
	var buf bytes.Buffer
	previewHeader(&buf, m)
	// Cap inline body at 256 KB. Anything larger gets a notice + the
	// download button is the way to see it.
	const maxInline = 256 * 1024
	truncated := false
	if len(body) > maxInline {
		body = body[:maxInline]
		truncated = true
	}
	fmt.Fprintf(&buf, `<style>
  .text-preview { background: var(--bg-secondary); border: 1px solid var(--border);
    border-radius: var(--ab-radius, 8px); padding: 1rem; margin: 1rem 0; overflow: auto; }
  .text-preview pre { margin: 0; font-family: var(--ab-mono, monospace);
    font-size: .85rem; line-height: 1.5; white-space: pre; word-break: normal; }
  .text-truncated { margin: .75rem 0 0; padding: .75rem 1rem;
    background: rgba(245,158,11,.12); color: var(--warning);
    border-radius: 6px; font-size: .85rem; }
</style>
<div class="text-preview"><pre>%s</pre></div>`,
		template.HTMLEscapeString(body))
	if truncated {
		fmt.Fprintf(&buf, `<p class="text-truncated">Preview truncated at 256 KB.
       <a href="%s">Download</a> to see the full file.</p>`,
			template.HTMLEscapeString(m.DownloadURL))
	}
	s.renderShell(w, r, urlPath, m.Filename, template.HTML(buf.String()), nil, true)
}

func (s *Server) renderBinaryPreview(w http.ResponseWriter, r *http.Request, urlPath, abs string, info os.FileInfo, ext string) {
	m := s.metaFor(urlPath, info, ext)
	var buf bytes.Buffer
	previewHeader(&buf, m)
	buf.WriteString(`<style>
  .binary-notice { background: var(--bg-secondary); border: 1px solid var(--border);
    border-radius: var(--ab-radius, 8px); padding: 2rem; text-align: center;
    margin: 1rem 0; color: var(--text-secondary); }
</style>
<div class="binary-notice">
  <p>No inline preview available for this file type.</p>
  <p style="margin-top:.5rem;font-size:.85rem">
    Use the Download button above to grab a copy, or <a href="?history=1">view its history</a>.
  </p>
</div>`)
	s.renderShell(w, r, urlPath, m.Filename, template.HTML(buf.String()), nil, true)
}

func (s *Server) renderText(w http.ResponseWriter, r *http.Request, urlPath string, raw []byte, contentType string) {
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(raw)
}

func (s *Server) render404(w http.ResponseWriter, r *http.Request, urlPath string) {
	w.WriteHeader(http.StatusNotFound)
	body := fmt.Sprintf(`<h1>404 — not in this workspace</h1><p>Nothing at <code>%s</code>. The path may exist on another branch, or you may need to <code>git pull</code>.</p>`,
		template.HTMLEscapeString(urlPath))
	s.renderShell(w, r, urlPath, "Not found", template.HTML(body), nil, false)
}

// MetaBar is the small "last edited by …" strip above page content.
type MetaBar struct {
	LastActor    string
	LastAt       string
	Version      string
	ShortVersion string
}

func metaBarFrom(fm map[string]any) *MetaBar {
	if meta, ok := fm["_meta"].(map[string]any); ok {
		mb := &MetaBar{}
		if s, ok := meta["modified_by"].(string); ok {
			mb.LastActor = s
		}
		if s, ok := meta["created_at"].(string); ok {
			mb.LastAt = s
		}
		if s, ok := meta["version"].(string); ok {
			mb.Version = s
			if len(s) >= 10 {
				mb.ShortVersion = s[:10]
			} else {
				mb.ShortVersion = s
			}
		}
		if mb.LastActor != "" || mb.Version != "" {
			return mb
		}
	}
	return nil
}

type breadcrumb struct {
	Label string
	Href  string
}

// renderShell wraps `content` in the dashboard chrome (header,
// sidebar tree, breadcrumbs).
func (s *Server) renderShell(w http.ResponseWriter, r *http.Request, urlPath, title string, content template.HTML, mb *MetaBar, wide bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	user := ""
	if s.UserResolver != nil {
		user = s.UserResolver(r)
	}
	// Show the (history) link on real file pages, not on directory
	// listings, 404 surfaces, or the history view itself.
	showHistory := s.HistoryFn != nil &&
		urlPath != "" && urlPath != "/" &&
		!strings.HasSuffix(urlPath, "/") &&
		!strings.HasPrefix(title, "History — ") &&
		!strings.HasPrefix(title, "Diff — ") &&
		!strings.HasPrefix(title, "Edit — ") &&
		title != "Not found"
	// Show the (edit) link to signed-in users on file pages.
	showEdit := showHistory && user != ""
	// Show the admin link in the header for admin-kind users.
	isAdmin := s.IsAdminFn != nil && s.IsAdminFn(r)
	// Workspace-level theme: load /theme.css at the end of the head
	// if the file exists at the worktree root. Single stat per render.
	hasTheme := s.workspaceHasTheme()
	data := map[string]any{
		"Title":           title,
		"WorkspaceName":   s.Workspace,
		"Branch":          s.Branch,
		"User":            user,
		"Path":            urlPath,
		"Tree":            s.buildTree(urlPath),
		"Breadcrumbs":     breadcrumbsFor(urlPath),
		"Content":         content,
		"MetaBar":         mb,
		"Wide":            wide,
		"ShowHistoryLink": showHistory,
		"ShowEditLink":    showEdit,
		"IsAdmin":         isAdmin,
		"HasThemeCSS":     hasTheme,
	}
	if err := s.tmpl.ExecuteTemplate(w, "shell", data); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// workspaceHasTheme reports whether the workspace ships a theme.css
// at the worktree root. Cheap stat — gives agents a single-file
// override surface for workspace-wide styling without template churn.
// Used by renderShell to conditionally inject `<link href="/theme.css">`
// at the end of the shell's <head>.
func (s *Server) workspaceHasTheme() bool {
	if s.WorktreeRoot == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(s.WorktreeRoot, "theme.css"))
	return err == nil && !info.IsDir()
}

func breadcrumbsFor(urlPath string) []breadcrumb {
	if urlPath == "/" || urlPath == "" {
		return nil
	}
	parts := strings.Split(strings.Trim(urlPath, "/"), "/")
	out := make([]breadcrumb, 0, len(parts)+1)
	out = append(out, breadcrumb{Label: "/", Href: "/"})
	acc := ""
	for _, p := range parts {
		acc += "/" + p
		out = append(out, breadcrumb{Label: p, Href: acc})
	}
	return out
}

// treeNode describes one entry in the left sidebar tree.
// Folders carry their own Children; files have nil Children. Open is
// true when the current request's URL is somewhere under this folder
// — controls the <details> open state in the template.
type treeNode struct {
	Label    string
	Href     string
	IsDir    bool
	Depth    int
	Active   bool
	Open     bool
	Children []treeNode
}

// buildTree walks the worktree recursively up to maxTreeDepth levels.
// Hidden entries (.git, .agentboard) are skipped. Folders containing
// the current page auto-expand; everything else stays collapsed.
const maxTreeDepth = 3

func (s *Server) buildTree(currentPath string) []treeNode {
	return s.walkTreeDir(s.WorktreeRoot, "", currentPath, 0)
}

func (s *Server) walkTreeDir(absDir, urlPrefix, currentPath string, depth int) []treeNode {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil
	}
	var out []treeNode
	for _, e := range entries {
		name := e.Name()
		if isServerInternal(name) {
			continue
		}
		isDir := e.IsDir()
		nodeURL := urlPrefix + "/" + name
		if isDir {
			nodeURL += "/"
		}
		label := name
		if !isDir {
			label = strings.TrimSuffix(name, filepath.Ext(name))
		}
		nodeBase := urlPrefix + "/" + name
		// "Open" — current request path goes through this folder. Used
		// to auto-expand ancestors of the active file.
		open := isDir && strings.HasPrefix(currentPath, nodeBase+"/")
		// "Active" — exact match on the URL of this entry. Trailing
		// slash optional for folder index navigation.
		exact := currentPath == nodeBase
		if isDir {
			exact = exact || currentPath == nodeBase+"/"
		}
		// A directory index landing also counts the folder as active
		// (e.g. currentPath="/pages/" highlights pages/).
		active := exact
		node := treeNode{
			Label:  label,
			Href:   nodeURL,
			IsDir:  isDir,
			Depth:  depth,
			Active: active,
			Open:   open,
		}
		if isDir && depth+1 < maxTreeDepth {
			node.Children = s.walkTreeDir(
				filepath.Join(absDir, name),
				nodeBase,
				currentPath,
				depth+1,
			)
		}
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func titleFromFrontmatter(fm map[string]any) string {
	if t, ok := fm["title"].(string); ok && t != "" {
		return t
	}
	return ""
}

func isWide(fm map[string]any) bool {
	if v, ok := fm["wide"].(bool); ok {
		return v
	}
	return false
}

func pathLabel(urlPath string) string {
	clean := strings.Trim(urlPath, "/")
	if clean == "" {
		return "Home"
	}
	return strings.TrimSuffix(filepath.Base(clean), filepath.Ext(clean))
}

func indentJSON(raw []byte) string {
	var buf bytes.Buffer
	in := false
	depth := 0
	for _, b := range raw {
		switch b {
		case '"':
			in = !in
			buf.WriteByte(b)
		case '{', '[':
			buf.WriteByte(b)
			if !in {
				depth++
				buf.WriteByte('\n')
				for i := 0; i < depth*2; i++ {
					buf.WriteByte(' ')
				}
			}
		case '}', ']':
			if !in {
				depth--
				buf.WriteByte('\n')
				for i := 0; i < depth*2; i++ {
					buf.WriteByte(' ')
				}
			}
			buf.WriteByte(b)
		case ',':
			buf.WriteByte(b)
			if !in {
				buf.WriteByte('\n')
				for i := 0; i < depth*2; i++ {
					buf.WriteByte(' ')
				}
			}
		case ':':
			buf.WriteByte(b)
			if !in {
				buf.WriteByte(' ')
			}
		default:
			buf.WriteByte(b)
		}
	}
	return buf.String()
}
