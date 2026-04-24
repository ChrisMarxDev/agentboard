package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/christophermarx/agentboard/internal/mdx"
	"github.com/christophermarx/agentboard/internal/view"
)

// handleViewOpen is the SPA's single read entry point for a page.
// Returns a bundle containing page source + resolved data keys + file
// refs + subpages — everything the client needs to render. No caching.
//
//	POST /api/view/open
//	body: { "path": "handbook" }
func (s *Server) handleViewOpen(w http.ResponseWriter, r *http.Request) {
	if s.PageRefs == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "view broker unavailable")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_VALUE", "body must be JSON {path}")
		return
	}
	authority, user, session := view.ResolveAuthority(r, s.Auth, s.ViewSessions)

	// Share visitors are anchored to their share's path. If they ask
	// for something else, re-anchor rather than leaking a second view.
	requestedPath := strings.TrimPrefix(strings.TrimSuffix(body.Path, ".md"), "/")
	if requestedPath == "" {
		requestedPath = "index"
	}
	scopeRoot := requestedPath
	if authority == view.AuthorityShare && session != nil {
		scopeRoot = strings.TrimPrefix(strings.TrimSuffix(session.Path, ".md"), "/")
		if scopeRoot == "" {
			scopeRoot = "index"
		}
	}

	// Anonymous access requires the requested path to actually be in
	// public.paths (as a page URL). Avoids anonymous probing.
	if authority == view.AuthorityAnonymous {
		if s.ViewPublic == nil || !s.ViewPublic.IsPubliclyReadable(http.MethodGet, "/"+requestedPath) {
			respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "this page is not public")
			return
		}
	}

	// Page must exist. Resolve against the scope root, not the
	// request path — so a share visitor to /handbook is actually
	// rendering /handbook regardless of what they typed.
	page := s.Pages.GetPage(scopeRoot)
	if page == nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "page not found: "+scopeRoot)
		return
	}
	scope, err := s.ViewScope.Build(scopeRoot, authority, user)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Resolve data keys in scope. Anything out-of-scope returns
	// undefined to the client; not an error. The client was probably
	// going to render an empty component and that's fine.
	dataOut := map[string]any{}
	for key := range scope.DataKeys {
		if !scope.CanReadData(key) {
			continue
		}
		raw, err := s.Store.Get(key)
		if err != nil || len(raw) == 0 {
			continue
		}
		var v any
		if jerr := json.Unmarshal(raw, &v); jerr != nil {
			continue
		}
		dataOut[key] = v
	}

	// Filter files.
	filesOut := make([]string, 0, len(scope.Files))
	for f := range scope.Files {
		if scope.CanReadFile(f) {
			filesOut = append(filesOut, f)
		}
	}
	sort.Strings(filesOut)

	// Subpages: title-lookup via page manager so the sidebar can render.
	type subpageOut struct {
		Path  string `json:"path"`
		Title string `json:"title"`
	}
	subs := make([]subpageOut, 0, len(scope.Subpages))
	for p := range scope.Subpages {
		if !scope.CanReadSubpage(p) {
			continue
		}
		if pg := s.Pages.GetPage(p); pg != nil {
			subs = append(subs, subpageOut{Path: p, Title: pg.Title})
		}
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].Path < subs[j].Path })

	// Attribution + approval echo so the meta bar has one fewer round-trip.
	var lastActor, lastAt string
	if s.PageMeta != nil {
		if m, _ := s.PageMeta.Get(scopeRoot); m != nil {
			lastActor, lastAt = m.LastActor, m.LastAt
		}
	}
	var approval map[string]any
	if s.PageApproval != nil {
		if a, _ := s.PageApproval.Get(scopeRoot); a != nil {
			approval = map[string]any{
				"approved_by":   a.ApprovedBy,
				"approved_at":   a.ApprovedAt,
				"approved_etag": a.ApprovedEtag,
				"stale":         a.ApprovedEtag != page.Etag,
			}
		}
	}

	payload := map[string]any{
		"path":       scopeRoot,
		"title":      page.Title,
		"source":     page.Source,
		"etag":       page.Etag,
		"data":       dataOut,
		"files":      filesOut,
		"subpages":   subs,
		"authority":  authorityName(authority),
		"last_actor": lastActor,
		"last_at":    lastAt,
		"approval":   approval,
	}
	// Embed-friendly headers when the response is intended for
	// anonymous consumption (share or public). frame-ancestors=* lets
	// the SPA iframe the view anywhere; authed callers still get the
	// default same-origin policy.
	applyEmbedHeaders(w, authority)
	respondJSON(w, http.StatusOK, payload)
}

// applyEmbedHeaders opts a response in to cross-origin embedding
// when the authority is share or anonymous-public. Authed callers
// keep the default SAMEORIGIN policy.
func applyEmbedHeaders(w http.ResponseWriter, authority view.AuthorityKind) {
	if authority != view.AuthorityShare && authority != view.AuthorityAnonymous {
		return
	}
	w.Header().Set("Content-Security-Policy", "frame-ancestors *")
	w.Header().Set("X-Frame-Options", "ALLOWALL")
}

// handleViewEvents is the SSE stream scoped to one view. Only events
// for keys/paths within the session's scope are forwarded; everything
// else is dropped at the broker.
//
//	GET /api/view/events?path=handbook
func (s *Server) handleViewEvents(w http.ResponseWriter, r *http.Request) {
	if s.PageRefs == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "view broker unavailable")
		return
	}
	authority, user, session := view.ResolveAuthority(r, s.Auth, s.ViewSessions)

	requestedPath := strings.TrimPrefix(strings.TrimSuffix(r.URL.Query().Get("path"), ".md"), "/")
	if requestedPath == "" {
		requestedPath = "index"
	}
	if authority == view.AuthorityShare && session != nil {
		requestedPath = strings.TrimPrefix(strings.TrimSuffix(session.Path, ".md"), "/")
		if requestedPath == "" {
			requestedPath = "index"
		}
	}
	if authority == view.AuthorityAnonymous {
		if s.ViewPublic == nil || !s.ViewPublic.IsPubliclyReadable(http.MethodGet, "/"+requestedPath) {
			respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "this page is not public")
			return
		}
	}

	// SSE setup.
	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	applyEmbedHeaders(w, authority)

	scope, err := s.ViewScope.Build(requestedPath, authority, user)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Subscribe to the upstream broadcaster. We fan out from that
	// single channel to every view-SSE subscriber, filtered by scope.
	subID, upstream := s.Broadcaster.Subscribe()
	defer s.Broadcaster.Unsubscribe(subID)

	// Initial hello — client knows the stream is live.
	fmt.Fprintf(w, "event: ready\ndata: {\"path\":%q}\n\n", requestedPath)
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			// Comment keeps the connection alive through proxies.
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case evt, ok := <-upstream:
			if !ok {
				return
			}
			forward, kind, payload := shouldForward(evt, scope)
			if !forward {
				continue
			}
			// Rebuild scope after page edits so new refs propagate
			// (scope-changed notice triggers a client re-open).
			if kind == "page-updated" {
				newScope, err := s.ViewScope.Build(requestedPath, authority, user)
				if err == nil && !scopeEquivalent(scope, newScope) {
					scope = newScope
					fmt.Fprintf(w, "event: scope-changed\ndata: {}\n\n")
					flusher.Flush()
					continue
				}
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, payload)
			flusher.Flush()
		}
	}
}

// shouldForward decides whether an upstream broadcaster event should
// reach this view session, and returns the translated kind + payload.
func shouldForward(evt SSEEvent, scope *view.Scope) (bool, string, []byte) {
	switch evt.Type {
	case "data":
		// evt.Data is the DataEvent JSON. Peek the key to filter.
		var de struct {
			Key string `json:"key"`
		}
		_ = json.Unmarshal(evt.Data, &de)
		if de.Key == "" || !scope.CanReadData(de.Key) {
			return false, "", nil
		}
		return true, "data", evt.Data
	case "page-updated":
		var pu struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(evt.Data, &pu)
		if pu.Path == "" {
			return false, "", nil
		}
		norm := strings.TrimPrefix(strings.TrimSuffix(pu.Path, ".md"), "/")
		if norm != scope.Path && !strings.HasPrefix(norm, scope.Path+"/") {
			return false, "", nil
		}
		return true, "page-updated", evt.Data
	}
	return false, "", nil
}

func scopeEquivalent(a, b *view.Scope) bool {
	if len(a.DataKeys) != len(b.DataKeys) || len(a.Files) != len(b.Files) || len(a.Subpages) != len(b.Subpages) {
		return false
	}
	for k := range a.DataKeys {
		if !b.DataKeys[k] {
			return false
		}
	}
	for k := range a.Files {
		if !b.Files[k] {
			return false
		}
	}
	for k := range a.Subpages {
		if !b.Subpages[k] {
			return false
		}
	}
	return true
}

// handleViewFile serves a single file referenced by a page the caller
// has access to. Shares resolve to the session's path; other flows
// resolve against an explicit ?path= param.
//
//	GET /api/view/files/<name>?path=handbook
func (s *Server) handleViewFile(w http.ResponseWriter, r *http.Request) {
	if s.PageRefs == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "view broker unavailable")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/view/files/")
	if name == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "file name required")
		return
	}
	authority, user, session := view.ResolveAuthority(r, s.Auth, s.ViewSessions)

	requestedPath := strings.TrimPrefix(strings.TrimSuffix(r.URL.Query().Get("path"), ".md"), "/")
	if requestedPath == "" {
		requestedPath = "index"
	}
	if authority == view.AuthorityShare && session != nil {
		requestedPath = strings.TrimPrefix(strings.TrimSuffix(session.Path, ".md"), "/")
		if requestedPath == "" {
			requestedPath = "index"
		}
	}
	scope, err := s.ViewScope.Build(requestedPath, authority, user)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if !scope.CanReadFile("/api/files/" + name) {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "file not in current view's scope")
		return
	}

	// Serve from disk via the file manager's content dir. We go
	// through os.Stat+open directly to avoid path traversal — the
	// check above already proved name is in a trusted ref set, but
	// belt and braces.
	if s.Files == nil {
		respondError(w, http.StatusNotImplemented, "NOT_SUPPORTED", "files unavailable")
		return
	}
	root := s.Files.FilesDir()
	abs := filepath.Join(root, filepath.Clean("/"+name))
	if !strings.HasPrefix(abs, filepath.Clean(root)+string(os.PathSeparator)) && abs != filepath.Clean(root) {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "invalid path")
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "file missing")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if ct := contentTypeForName(name); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	applyEmbedHeaders(w, authority)
	_, _ = io.Copy(w, f)
}

// authorityName surfaces the scope's authority so the client can
// adjust UI (e.g. hide admin controls in share mode).
func authorityName(a view.AuthorityKind) string {
	switch a {
	case view.AuthorityAdmin:
		return "admin"
	case view.AuthorityAgent:
		return "agent"
	case view.AuthorityShare:
		return "share"
	case view.AuthorityAnonymous:
		return "anonymous"
	}
	return "unknown"
}

// contentTypeForName is a minimal lookup — only the types we actually
// serve from the AgentBoard file store. Unknown extensions fall back
// to the default (application/octet-stream via empty string).
func contentTypeForName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".json":
		return "application/json"
	case ".txt", ".md":
		return "text/plain; charset=utf-8"
	}
	return ""
}

// Unused alias to keep mdx import honest — referenced via PageRefs.
var _ = mdx.RefSet{}
