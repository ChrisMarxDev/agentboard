package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/christophermarx/agentboard/internal/files"
)

// stripFilesPrefix extracts the file name from a /api/files/... URL path.
// Uses URL-unescaped r.URL.Path which chi normalizes. Returns "" if no name.
func stripFilesPrefix(path string) string {
	p := strings.TrimPrefix(path, "/api/files/")
	// Collapse any leading slashes just in case.
	for strings.HasPrefix(p, "/") {
		p = p[1:]
	}
	return p
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "files manager not configured")
		return
	}
	list, err := s.Files.List()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// ?prefix=skills/ — subtree filter against the stored file name.
	if prefix := r.URL.Query().Get("prefix"); prefix != "" {
		norm := strings.TrimPrefix(prefix, "/")
		filtered := list[:0] // reuse backing array; list becomes the filtered slice
		for _, f := range list {
			if strings.HasPrefix(f.Name, norm) {
				filtered = append(filtered, f)
			}
		}
		list = filtered
	}

	respondJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "files manager not configured")
		return
	}
	name := stripFilesPrefix(r.URL.Path)
	if name == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "file name required")
		return
	}
	f, info, err := s.Files.Open(name)
	if errors.Is(err, files.ErrInvalidName) {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "invalid file name")
		return
	}
	if errors.Is(err, files.ErrNotFound) {
		respondError(w, http.StatusNotFound, "NOT_FOUND", "file not found: "+name)
		return
	}
	if errors.Is(err, files.ErrIsDirectory) {
		// /api/files/<path> targets file leaves only. A directory at
		// the same path means "no file here" from the API's vantage —
		// the SPA's fallback chain HEADs files first to disambiguate
		// page vs file vs folder URLs and we mustn't poison it with
		// a 500 just because the path is a folder.
		respondError(w, http.StatusNotFound, "NOT_FOUND", "no file at this path (is a directory)")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer f.Close()

	// ETag / If-None-Match round-trip.
	if match := r.Header.Get("If-None-Match"); match != "" && match == info.ETag {
		w.Header().Set("ETag", info.ETag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	disp := "attachment"
	if files.IsInlineDisposition(info.ContentType) {
		disp = "inline"
	}

	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", info.ETag)
	w.Header().Set("Cache-Control", "no-cache") // forces revalidation, 304 is the fast path
	// Quote the filename to handle spaces. filepath.Base strips any path segments.
	shortName := info.Name
	if i := strings.LastIndex(shortName, "/"); i >= 0 {
		shortName = shortName[i+1:]
	}
	w.Header().Set("Content-Disposition", disp+`; filename="`+shortName+`"`)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		io.Copy(w, f)
	}
}

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "files manager not configured")
		return
	}
	name := stripFilesPrefix(r.URL.Path)
	if name == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "file name required")
		return
	}
	// Cap the body aggressively via MaxBytesReader so clients can't stream 10 GB.
	r.Body = http.MaxBytesReader(w, r.Body, s.Files.MaxSizeBytes()+1)
	info, err := s.Files.Write(name, r.Body)
	switch {
	case errors.Is(err, files.ErrInvalidName):
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "invalid file name")
		return
	case errors.Is(err, files.ErrTooLarge):
		respondError(w, http.StatusRequestEntityTooLarge, "VALUE_TOO_LARGE", "file exceeds size cap")
		return
	case err != nil:
		// http.MaxBytesReader errors surface as a generic io error with "http: request body too large".
		if strings.Contains(err.Error(), "too large") {
			respondError(w, http.StatusRequestEntityTooLarge, "VALUE_TOO_LARGE", "file exceeds size cap")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "file-updated",
		Data: []byte(`{"name":"` + info.Name + `","deleted":false}`),
	})

	// Markdown uploads ARE page writes (FilesDir aliases ContentDir per
	// CORE_GUIDELINES §9). The mdx watcher only scans content/ one level
	// deep, so a nested upload like content/skills/<slug>/SKILL.md would
	// never reach the page index or FTS. Re-scan + re-index inline.
	if strings.HasSuffix(strings.ToLower(info.Name), ".md") {
		s.reindexUploadedPage(info.Name)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"name":         info.Name,
		"size":         info.Size,
		"content_type": info.ContentType,
		"etag":         info.ETag,
		"url":          info.URL,
	})
}

// reindexUploadedPage refreshes the page index and FTS row for a .md file that
// was written through the files API. Mirrors what handleWritePage does after a
// normal /api/content write, minus optimistic-concurrency (the file API has no
// ETag contract for pages).
func (s *Server) reindexUploadedPage(name string) {
	if s.Pages == nil {
		return
	}
	s.Pages.ScanPages()

	// The page index aliases `<folder>/SKILL.md` to `<folder>` (Anthropic
	// skill-format convention — see mdx.ScanPages). Apply the same rule so
	// the FTS lookup finds the aliased page, not the raw file path.
	pageKey := strings.TrimSuffix(name, ".md")
	pageKey = strings.TrimSuffix(pageKey, "/SKILL")
	if s.Search != nil {
		if p := s.Pages.GetPage(pageKey); p != nil {
			_ = s.Search.IndexPage(p.Path, p.Title, p.Source)
		}
	}
	s.Broadcaster.Broadcast(SSEEvent{
		Type: "page-updated",
		Data: []byte(`{"path":"` + name + `"}`),
	})
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "files manager not configured")
		return
	}
	name := stripFilesPrefix(r.URL.Path)
	if name == "" {
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "file name required")
		return
	}
	err := s.Files.Delete(name)
	switch {
	case errors.Is(err, files.ErrInvalidName):
		respondError(w, http.StatusBadRequest, "INVALID_KEY", "invalid file name")
		return
	case errors.Is(err, files.ErrNotFound):
		respondError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	case err != nil:
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	s.Broadcaster.Broadcast(SSEEvent{
		Type: "file-updated",
		Data: []byte(`{"name":"` + name + `","deleted":true}`),
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
