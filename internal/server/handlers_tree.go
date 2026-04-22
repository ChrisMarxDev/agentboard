package server

import (
	"net/http"
	"strings"
)

// TreeEntry is one node in the combined content manifest returned by
// GET /api/tree. No source bodies — just enough to render a sidebar or
// orient an agent.
type TreeEntry struct {
	Kind string `json:"kind"`          // "page" | "file"
	Path string `json:"path"`          // URL-style path (e.g. "/features/grab" or "skills/x/banner.svg")
	Name string `json:"name,omitempty"` // filename for files
	// Pages only:
	Title string `json:"title,omitempty"`
	Etag  string `json:"etag,omitempty"`
	Order int    `json:"order,omitempty"`
	// Files only:
	Size        int64  `json:"size,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

// handleTree returns a lightweight manifest of every page + file in the
// project in a single call. Agents and the frontend sidebar use this to
// orient themselves without dragging down the full source of every page
// (which /api/content would do — see DOGFOOD_NOTES §1).
//
// Supports ?prefix= to scope to a subtree.
func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	normPagePrefix := ""
	if prefix != "" {
		normPagePrefix = "/" + strings.TrimPrefix(prefix, "/")
	}
	normFilePrefix := strings.TrimPrefix(prefix, "/")

	out := make([]TreeEntry, 0, 64)

	// Pages
	for _, p := range s.Pages.ListPages() {
		if normPagePrefix != "" && !strings.HasPrefix(p.Path, normPagePrefix) {
			continue
		}
		out = append(out, TreeEntry{
			Kind:  "page",
			Path:  p.Path,
			Title: p.Title,
			Etag:  p.Etag,
			Order: p.Order,
		})
	}

	// Files
	if s.Files != nil {
		list, err := s.Files.List()
		if err == nil {
			for _, f := range list {
				if normFilePrefix != "" && !strings.HasPrefix(f.Name, normFilePrefix) {
					continue
				}
				// Derive a display name (last path segment).
				name := f.Name
				if i := strings.LastIndex(name, "/"); i >= 0 {
					name = name[i+1:]
				}
				out = append(out, TreeEntry{
					Kind:        "file",
					Path:        f.Name,
					Name:        name,
					Size:        f.Size,
					ContentType: f.ContentType,
				})
			}
		}
	}

	respondJSON(w, http.StatusOK, out)
}
