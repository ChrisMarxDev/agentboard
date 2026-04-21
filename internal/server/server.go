package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

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
	Broadcaster          *Broadcaster
	Pages                *mdx.PageManager
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
	SkillFile            string
	FrontendFS           http.FileSystem // embedded frontend
	DevMode              bool
	DevProxy             string // Vite dev server URL for dev mode
	AllowComponentUpload bool
	MaxFileSizeMB        int
	AuthToken            string // when non-empty, every route (except /api/health) requires a matching token
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
	compManager := components.NewManager(cfg.Project)
	fileManager := files.NewManager(cfg.Project, cfg.MaxFileSizeMB)
	errorBuffer := interrors.NewBuffer()
	grabber := &grab.Materializer{Pages: pageManager, Store: cfg.Store}

	mcpServer := &mcp.Server{
		Store:                cfg.Store,
		Pages:                pageManager,
		Components:           compManager,
		Files:                fileManager,
		Errors:               errorBuffer,
		Grab:                 grabber,
		AllowComponentUpload: cfg.AllowComponentUpload,
	}

	s := &Server{
		Project:              cfg.Project,
		Store:                cfg.Store,
		Broadcaster:          broadcaster,
		Pages:                pageManager,
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
	r.Use(authMiddleware(cfg.AuthToken))

	// API routes
	r.Route("/api", func(r chi.Router) {
		// Data endpoints
		r.Get("/data", s.handleGetAllData)
		r.Get("/data/schema", s.handleGetSchema)

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
		r.Get("/files/*", s.handleGetFile)
		r.Put("/files/*", s.handleWriteFile)
		r.Delete("/files/*", s.handleDeleteFile)

		// Skills — a read view on top of files/skills/<slug>/SKILL.md
		r.Get("/skills", s.handleListSkills)
		r.Get("/skills/{slug}", s.handleGetSkill)

		// Render-error beacons from frontend components (Mermaid, Markdown, Image, …)
		r.Get("/errors", s.handleListErrors)
		r.Post("/errors", s.handleRecordError)
		r.Delete("/errors", s.handleClearErrors)

		// Grab — materialize a list of picks into agent-ready text
		r.Post("/grab", s.handleGrab)

		// SSE
		r.Get("/events", s.Broadcaster.ServeHTTP)

		// Meta
		r.Get("/health", s.handleHealth)
		r.Get("/config", s.handleConfig)
	})

	// Skill file
	r.Get("/skill", s.handleSkill)

	// MCP endpoint
	r.Post("/mcp", s.MCP.ServeHTTP)
	r.Get("/mcp", s.MCP.ServeHTTP)

	// Frontend — serve embedded SPA or proxy to dev server
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
