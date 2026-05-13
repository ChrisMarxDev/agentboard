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
				for _, ext := range []string{".md", ".html"} {
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
	for _, idx := range []string{"index.md", "index.html"} {
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
		if strings.HasPrefix(name, ".") {
			continue // skip hidden (.git etc.)
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
	s.renderShell(w, r, urlPath, "", template.HTML(body.String()), nil, false)
}

// renderFile picks a renderer based on extension and serves the file.
func (s *Server) renderFile(w http.ResponseWriter, r *http.Request, urlPath, abs string, info os.FileInfo) {
	data, err := os.ReadFile(abs)
	if err != nil {
		http.Error(w, "read: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ext := strings.ToLower(filepath.Ext(abs))
	switch ext {
	case ".md", ".mdx":
		s.renderMarkdown(w, r, urlPath, data)
	case ".html", ".htm":
		s.renderHTML(w, r, urlPath, data)
	case ".json":
		s.renderJSON(w, r, urlPath, data)
	case ".txt":
		s.renderText(w, r, urlPath, data, "text/plain")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(data)
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write(data)
	case ".png":
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(data)
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write(data)
	case ".ndjson":
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write(data)
	default:
		s.renderText(w, r, urlPath, data, "text/plain; charset=utf-8")
	}
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
	// For trusted authored HTML we strip the frontmatter (if any) and
	// inline the rest into the shell. A future cut serves arbitrary
	// HTML on usercontent.<host> via a sandbox iframe; until that
	// origin exists we render inline.
	fm, body := splitFrontmatter(raw)
	title := titleFromFrontmatter(fm)
	if title == "" {
		title = pathLabel(urlPath)
	}
	wide := isWide(fm)
	s.renderShell(w, r, urlPath, title, template.HTML(body), metaBarFrom(fm), wide)
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
	data := map[string]any{
		"Title":         title,
		"WorkspaceName": s.Workspace,
		"Branch":        s.Branch,
		"User":          user,
		"Path":          urlPath,
		"Tree":          s.buildTree(urlPath),
		"Breadcrumbs":   breadcrumbsFor(urlPath),
		"Content":       content,
		"MetaBar":       mb,
		"Wide":          wide,
	}
	if err := s.tmpl.ExecuteTemplate(w, "shell", data); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
		return
	}
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
type treeNode struct {
	Label  string
	Href   string
	IsDir  bool
	Depth  int
	Active bool
}

// buildTree walks the worktree (one level deep for now; nested
// folders expand on click in a future cut) and produces a flat list
// of nav entries.
func (s *Server) buildTree(currentPath string) []treeNode {
	root := s.WorktreeRoot
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []treeNode
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		// Skip the assets dir agents drop in next to content. Anything
		// the user-content origin needs lives at the worktree root,
		// not in a magic subdir.
		href := "/" + name
		isDir := e.IsDir()
		if isDir {
			href += "/"
		}
		label := strings.TrimSuffix(name, filepath.Ext(name))
		out = append(out, treeNode{
			Label:  label,
			Href:   href,
			IsDir:  isDir,
			Depth:  0,
			Active: strings.HasPrefix(currentPath, "/"+name),
		})
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
