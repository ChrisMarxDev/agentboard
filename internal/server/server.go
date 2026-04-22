package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/components"
	"github.com/christophermarx/agentboard/internal/data"
	interrors "github.com/christophermarx/agentboard/internal/errors"
	"github.com/christophermarx/agentboard/internal/files"
	"github.com/christophermarx/agentboard/internal/grab"
	"github.com/christophermarx/agentboard/internal/mcp"
	"github.com/christophermarx/agentboard/internal/mdx"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server is the main AgentBoard HTTP server.
type Server struct {
	Project              *project.Project
	Store                data.DataStore
	Auth                 *auth.Store
	Broadcaster          *Broadcaster
	Pages                *mdx.PageManager
	PageMeta             *mdx.MetaStore
	Search               *mdx.SearchStore
	Components           *components.Manager
	Files                *files.Manager
	Errors               *interrors.Buffer
	Grab                 *grab.Materializer
	MCP                  *mcp.Server
	Router               chi.Router
	SkillFile            string
	AllowComponentUpload bool
}

// ServerConfig holds configuration for creating a new server.
type ServerConfig struct {
	Project              *project.Project
	Store                data.DataStore
	Auth                 *auth.Store // identity-backed auth (agents + admins); required
	SkillFile            string
	FrontendFS           http.FileSystem // embedded frontend
	DevMode              bool
	DevProxy             string // Vite dev server URL for dev mode
	AllowComponentUpload bool
	MaxFileSizeMB        int
}

// New creates a new AgentBoard server.
func New(cfg ServerConfig) *Server {
	broadcaster := NewBroadcaster()
	broadcaster.StartHeartbeat()

	// Wire data store events to SSE broadcaster
	go func() {
		ch := cfg.Store.Subscribe()
		for evt := range ch {
			eventData, _ := json.Marshal(evt)
			broadcaster.Broadcast(SSEEvent{
				Type: "data",
				Data: eventData,
			})
		}
	}()

	pageManager := mdx.NewPageManager(cfg.Project)
	// Best-effort: if the store exposes a *sql.DB (SQLiteStore does), build
	// a MetaStore over it so we can surface "last edited by" on pages. When
	// the assert fails, PageMeta stays nil and handlers fall back to "no
	// meta available" — never fatal.
	var metaStore *mdx.MetaStore
	if dber, ok := cfg.Store.(interface{ DB() *sql.DB }); ok {
		if ms, err := mdx.NewMetaStore(dber.DB()); err == nil {
			metaStore = ms
		}
	}

	// Full-text search index (SQLite FTS5). Bootstrapped from the page
	// manager's initial scan; kept in sync by the page handlers. Same
	// best-effort posture as MetaStore — if the DB can't be addressed or
	// FTS isn't available, search silently becomes a no-op rather than
	// failing the boot.
	var searchStore *mdx.SearchStore
	if dber, ok := cfg.Store.(interface{ DB() *sql.DB }); ok {
		if ss, err := mdx.NewSearchStore(dber.DB()); err == nil {
			searchStore = ss
			// Prime the index from whatever's on disk. Zero-cost on a
			// fresh project; O(N) on an existing one.
			if err := searchStore.Rebuild(pageManager.ListPages()); err != nil {
				log.Printf("agentboard: search index rebuild failed (continuing without search): %v", err)
			}
		} else {
			log.Printf("agentboard: FTS5 search unavailable (continuing without search): %v", err)
		}
	}

	compManager := components.NewManager(cfg.Project)
	fileManager := files.NewManager(cfg.Project, cfg.MaxFileSizeMB)
	errorBuffer := interrors.NewBuffer()
	grabber := &grab.Materializer{Pages: pageManager, Store: cfg.Store}

	mcpServer := &mcp.Server{
		Store:                cfg.Store,
		Pages:                pageManager,
		Search:               searchStore,
		Components:           compManager,
		Files:                fileManager,
		Errors:               errorBuffer,
		Grab:                 grabber,
		AllowComponentUpload: cfg.AllowComponentUpload,
	}

	s := &Server{
		Project:              cfg.Project,
		Store:                cfg.Store,
		Auth:                 cfg.Auth,
		Broadcaster:          broadcaster,
		Pages:                pageManager,
		PageMeta:             metaStore,
		Search:               searchStore,
		Components:           compManager,
		Files:                fileManager,
		Errors:               errorBuffer,
		Grab:                 grabber,
		MCP:                  mcpServer,
		SkillFile:            cfg.SkillFile,
		AllowComponentUpload: cfg.AllowComponentUpload,
	}

	s.Router = s.buildRouter(cfg)
	return s
}

func (s *Server) buildRouter(cfg ServerConfig) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Token-auth is scoped to the API / MCP / skill endpoints only. The
	// SPA assets (`/`, `/login`, static JS/CSS) must render without a
	// token so the browser can reach /login and sign the user in. The
	// SPA has its own client-side gate (SessionGate + apiFetch's 401
	// redirect).
	gated := func(r chi.Router) {
		r.Use(auth.TokenMiddleware(cfg.Auth, auth.MiddlewareConfig{
			OpenPaths: []string{"/api/setup", "/api/setup/status"},
		}))
		r.Use(auth.AuthorizeMiddleware())
	}

	// API routes
	r.Group(func(r chi.Router) {
		gated(r)
		r.Route("/api", apiRoutes(s))
		r.Get("/skill", s.handleSkill)
		r.Post("/mcp", s.MCP.ServeHTTP)
		r.Get("/mcp", s.MCP.ServeHTTP)
	})

	// Frontend — serve embedded SPA or proxy to dev server.
	if cfg.DevMode && cfg.DevProxy != "" {
		r.HandleFunc("/*", devProxyHandler(cfg.DevProxy))
	} else if cfg.FrontendFS != nil {
		fileServer := http.FileServer(cfg.FrontendFS)
		r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
			// Try to serve the file; if not found, serve index.html (SPA fallback)
			path := r.URL.Path
			f, err := cfg.FrontendFS.Open(path[1:]) // strip leading /
			if err != nil {
				// SPA fallback
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
				return
			}
			f.Close()
			fileServer.ServeHTTP(w, r)
		})
	} else {
		r.HandleFunc("/*", s.handleFrontend)
	}

	return r
}

// apiRoutes builds the /api subtree. Extracted so the middleware scoping
// in buildRouter stays readable; the contents are the same set of handlers.
func apiRoutes(s *Server) func(r chi.Router) {
	return func(r chi.Router) {
		// Admin routes — AdminRequired is scoped inside registerAdminRoutes
		// so it only guards /api/admin/*. AuthorizeMiddleware is a no-op for
		// admin users (they ignore per-user rules).
		s.registerAdminRoutes(r)

		// Users directory — readable by any authenticated token. Powers
		// @mention autocomplete and assignee-field resolution.
		r.Get("/users", s.handleListUsersPublic)
		r.Post("/users/resolve", s.handleResolveUsernames)

		// Setup endpoints — open until the board is claimed, then 409 forever.
		r.Get("/setup/status", s.handleSetupStatus)
		r.Post("/setup", s.handleSetup)

		// Data endpoints
		r.Get("/data", s.handleGetAllData)
		r.Get("/data/schema", s.handleGetSchema)
		r.Post("/data/bulk-delete", s.handleBulkDeleteData)

		// Data key endpoints — use a wildcard to support dotted keys
		r.Route("/data/{key}", func(r chi.Router) {
			r.Get("/", s.handleGetData)
			r.Put("/", s.handleSetData)
			r.Patch("/", s.handleMergeData)
			r.Post("/", s.handleAppendData)
			r.Delete("/", s.handleDeleteData)

			// ID-based operations
			r.Get("/{id}", s.handleGetDataById)
			r.Put("/{id}", s.handleUpsertById)
			r.Patch("/{id}", s.handleMergeById)
			r.Delete("/{id}", s.handleDeleteById)
		})

		// Content endpoints (MDX dashboards + knowledge docs)
		r.Get("/content", s.handleListPages)
		r.Post("/content/move", s.handleMovePage)
		r.Post("/content/bulk-delete", s.handleBulkDeleteContent)
		r.Get("/content/*", s.handleGetPage)
		r.Put("/content/*", s.handleWritePage)
		r.Delete("/content/*", s.handleDeletePage)

		// Component endpoints
		r.Get("/components", s.handleListComponents)
		r.Get("/components.js", s.handleComponentsBundle)
		r.Get("/components/{name}", s.handleGetComponent)
		r.Put("/components/{name}", s.handleWriteComponent)
		r.Delete("/components/{name}", s.handleDeleteComponent)

		// File endpoints (/api/files/*  supports nested paths like exports/q1.csv)
		r.Get("/files", s.handleListFiles)
		r.Post("/files/bulk-delete", s.handleBulkDeleteFiles)
		r.Get("/files/*", s.handleGetFile)
		r.Head("/files/*", s.handleGetFile)
		r.Put("/files/*", s.handleWriteFile)
		r.Delete("/files/*", s.handleDeleteFile)

		// Skills — a read view on top of files/skills/<slug>/SKILL.md
		r.Get("/skills", s.handleListSkills)
		r.Get("/skills/{slug}", s.handleGetSkill)

		// Render-error beacons from frontend components (Mermaid, Markdown, Image, …)
		r.Get("/errors", s.handleListErrors)
		r.Post("/errors", s.handleRecordError)
		r.Delete("/errors", s.handleClearErrors)

		// Lightweight combined tree — pages + files, no source bodies
		r.Get("/tree", s.handleTree)

		// Full-text search over page content (FTS5)
		r.Get("/search", s.handleSearch)

		// Grab — materialize a list of picks into agent-ready text
		r.Post("/grab", s.handleGrab)

		// SSE
		r.Get("/events", s.Broadcaster.ServeHTTP)

		// Meta
		r.Get("/health", s.handleHealth)
		r.Get("/config", s.handleConfig)
	}
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	log.Printf("Listening on %s", addr)
	return http.ListenAndServe(addr, s.Router)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Agent-Source, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func respondJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func respondError(w http.ResponseWriter, status int, code string, message string) {
	respondJSON(w, status, map[string]string{
		"error": message,
		"code":  code,
	})
}

func devProxyHandler(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Simple proxy to Vite dev server
		http.Redirect(w, r, target+r.URL.Path, http.StatusTemporaryRedirect)
	}
}

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	// Serve embedded frontend — implemented in Step 10
	// For now, serve a simple HTML page
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>AgentBoard</title></head>
<body>
<div id="root"></div>
<script>
document.getElementById('root').innerHTML = '<h1>AgentBoard</h1><p>Frontend loading...</p>';
</script>
</body>
</html>`)
}
