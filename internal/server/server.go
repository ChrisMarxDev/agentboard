// Package server is the HTTP front of the AgentBoard binary. Post-pivot
// it does dramatically less than it used to: the v0.13 stores, page
// manager, view broker, SPA fallback, locks, teams, share, inbox,
// components, files, and grab are all gone (spec-filesystem-substrate.md).
// What remains:
//
//   - Auth (sessions + tokens + OAuth) at /_api/auth/*, /oauth/*, /.well-known
//   - Admin user / token / invitation management at /_api/admin/*
//   - Webhook subscriber management at /_api/admin/webhooks
//   - MCP at /mcp
//   - Git smart-HTTPS at /git/*
//   - The HTML dashboard at every other path
//
// Reads against the workspace's working-tree mirror happen through the
// HTML catch-all; writes go through git push or MCP propose.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/christophermarx/agentboard/internal/mcp"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/christophermarx/agentboard/internal/webhooks"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server is the HTTP front.
type Server struct {
	Project           *project.Project
	Conn              *sql.DB
	Auth              *auth.Store
	Broadcaster       *Broadcaster
	MCP               *mcp.Server
	Webhooks          *webhooks.Store
	WebhookDispatcher *webhooks.Dispatcher
	Invitations       *invitations.Store
	Router            chi.Router
	SkillFile         string
	webhookSecrets    sync.Map // subscription id → plaintext secret (set at Create)

	// GitServer mounts at /git/<workspace>.git (smart-HTTPS).
	GitServer http.Handler

	// HTML is the catch-all dashboard. Reads files out of the
	// workspace's working-tree mirror.
	HTML http.Handler
}

// resolveActor pulls the authenticated user off the request context,
// or returns "agent" when nothing is attached. Used by handlers that
// need to attribute writes (webhooks, tokens, etc.).
func resolveActor(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil && u.Username != "" {
		return u.Username
	}
	return "agent"
}

// ServerConfig is the wiring shape cli/serve.go uses.
type ServerConfig struct {
	Project     *project.Project
	Conn        *sql.DB
	Auth        *auth.Store
	Invitations *invitations.Store
	SkillFile   string
	GitServer   http.Handler
	HTML        http.Handler
}

// New constructs a Server. Subsystems are wired internally; the caller
// only supplies the substrate (project, db, auth, invitations) and the
// HTTP-level handlers (git, html).
func New(cfg ServerConfig) *Server {
	broadcaster := NewBroadcaster()
	broadcaster.StartHeartbeat()

	mcpServer := &mcp.Server{
		Auth: cfg.Auth,
	}

	webhookStore, err := webhooks.NewStore(cfg.Conn)
	if err != nil {
		// Webhooks are optional; log and continue. The MCP fire_event
		// tool will just be a no-op when the dispatcher is nil.
		fmt.Fprintln(os.Stderr, "warning: webhooks unavailable:", err)
	}
	var dispatcher *webhooks.Dispatcher
	if webhookStore != nil {
		dispatcher = webhooks.NewDispatcher(webhookStore, webhooks.DispatcherOptions{})
		mcpServer.WebhookDispatcher = dispatcher
	}

	s := &Server{
		Project:           cfg.Project,
		Conn:              cfg.Conn,
		Auth:              cfg.Auth,
		Broadcaster:       broadcaster,
		MCP:               mcpServer,
		Webhooks:          webhookStore,
		WebhookDispatcher: dispatcher,
		Invitations:       cfg.Invitations,
		SkillFile:         cfg.SkillFile,
		GitServer:         cfg.GitServer,
		HTML:              cfg.HTML,
	}
	s.Router = s.buildRouter()
	return s
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	tokenMW := auth.TokenMiddleware(s.Auth, auth.MiddlewareConfig{
		OpenPaths: []string{
			"/_api/setup/status",
			"/_api/config",
			"/_api/introduction",
			"/_api/auth/login",
			"/_api/auth/logout",
			"/_api/auth/me",
			"/_api/health",
			"/_api/invitations",
			"/.well-known/oauth-protected-resource",
			"/.well-known/oauth-authorization-server",
			"/oauth/register",
			"/oauth/authorize",
			"/oauth/authorize/decide",
			"/oauth/token",
			"/_api/events",
		},
	})
	csrfMW := auth.CSRFMiddleware()

	gated := func(r chi.Router) {
		r.Use(tokenMW)
		r.Use(csrfMW)
		r.Use(auth.AuthorizeMiddleware())
	}

	// ----- public endpoints (always anonymous) -----
	r.Get("/_api/health", s.handleHealth)
	r.Get("/_api/setup/status", s.handleSetupStatus)
	r.Get("/_api/config", s.handleConfig)
	r.Get("/_api/introduction", s.handleIntroduction)
	r.Get("/_api/invitations/{id}", s.handleGetInvitationPublic)
	r.Post("/_api/invitations/{id}/redeem", s.handleRedeemInvitation)
	// SSE stream — open to allow the public dashboard to subscribe.
	// Per-event payloads only carry path + workspace, no auth-scoped
	// data, so this is safe to leave anonymous.
	r.Get("/_api/events", s.Broadcaster.ServeHTTP)

	// Auth endpoints. Login / logout / me run anonymously by design;
	// session resolution happens inside the handler.
	r.Group(func(r chi.Router) {
		r.Use(tokenMW)
		r.Post("/_api/auth/login", s.handleAuthLogin)
		r.Post("/_api/auth/logout", s.handleAuthLogout)
		r.Get("/_api/auth/me", s.handleAuthMe)
	})

	// OAuth (RFC 9728 + RFC 7591 dynamic registration).
	r.Get("/.well-known/oauth-protected-resource", auth.HandleProtectedResourceMetadata)
	r.Get("/.well-known/oauth-authorization-server", auth.HandleAuthorizationServerMetadata)
	r.Post("/oauth/register", s.handleOAuthRegister)
	r.Get("/oauth/authorize", s.handleOAuthAuthorize)
	r.Post("/oauth/authorize/decide", s.handleOAuthAuthorizeDecide)
	r.Post("/oauth/token", s.handleOAuthToken)

	// ----- auth-gated endpoints -----
	r.Group(func(r chi.Router) {
		gated(r)

		r.Get("/_skill", s.handleSkill)
		r.Get("/_api/users", s.handleListUsersPublic)
		r.Post("/_api/users/resolve", s.handleResolveUsernames)
		r.Get("/_api/me", s.handleAdminMe)
		// Per-user surfaces under /_api/users/{u}/{tokens,password,sessions}
		// are wired below via the helper registrations — "me" is just an
		// alias the UI resolves client-side using /_api/auth/me.

		// Per-user admin surfaces (tokens / password / sessions) gated
		// by self-or-admin scope inside each helper. Mounted under /_api
		// so they share the auth-gated chain above.
		r.Route("/_api", func(api chi.Router) {
			s.registerUserTokenRoutes(api)
			s.registerUserPasswordRoutes(api)
			s.registerUserSessionRoutes(api)
		})

		// Admin (gated again by AdminRequired inside).
		r.Route("/_api/admin", func(api chi.Router) {
			api.Use(auth.AdminRequired())
			api.Get("/me", s.handleAdminMe)
			api.Get("/users", s.handleListUsers)
			api.Post("/users", s.handleCreateUser)
			api.Patch("/users/{username}", s.handleUpdateUser)
			api.Delete("/users/{username}", s.handleDeactivateUser)
			api.Post("/users/{username}/deactivate", s.handleDeactivateUser)
			api.Get("/invitations", s.handleListInvitations)
			api.Post("/invitations", s.handleCreateInvitation)
			api.Delete("/invitations/{id}", s.handleRevokeInvitation)
			api.Post("/webhooks", s.handleCreateWebhook)
			api.Get("/webhooks", s.handleAdminListWebhooks)
			api.Get("/webhooks/{id}", s.handleGetWebhook)
			api.Patch("/webhooks/{id}", s.handleUpdateWebhook)
			api.Delete("/webhooks/{id}", s.handleRevokeWebhook)
			api.Post("/webhooks/{id}/test", s.handleTestWebhook)
		})

		r.Post("/mcp", s.MCP.ServeHTTP)
		r.Get("/mcp", s.MCP.ServeHTTP)
		if s.GitServer != nil {
			r.Handle("/git/*", s.GitServer)
		}
	})

	// ----- human-facing auth UI -----
	// Anonymous-OK by design (you have to be able to GET /login while
	// logged out). SoftAuthMiddleware attaches user-context when a
	// session cookie is present so /login can short-circuit to redirect
	// when the visitor's already signed in.
	softMW := auth.SoftAuthMiddleware(s.Auth)
	r.Group(func(r chi.Router) {
		r.Use(softMW)
		s.registerAuthUIRoutes(r)
	})

	// ----- HTML catch-all -----
	// Anonymous reads are intentional, but we still want session
	// cookies to resolve so the renderer can show "@username" + a
	// logout link. SoftAuthMiddleware never 401s — perfect for the
	// public dashboard.
	r.Group(func(r chi.Router) {
		r.Use(softMW)
		if s.HTML != nil {
			r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet, http.MethodHead, http.MethodOptions:
					s.HTML.ServeHTTP(w, r)
				default:
					respondError(w, http.StatusNotFound, "ROUTE_NOT_FOUND",
						"no route matches "+r.Method+" "+r.URL.Path)
				}
			})
		} else {
			r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
				respondError(w, http.StatusNotFound, "ROUTE_NOT_FOUND",
					"HTML renderer not configured")
			})
		}
	})

	return r
}

// ListenAndServe boots the HTTP server on `addr`.
func (s *Server) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.Router)
}

// ----- small response helpers (legacy callers use them) -----

func respondJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func respondError(w http.ResponseWriter, status int, code string, message string) {
	respondJSON(w, status, map[string]string{"error": message, "code": code})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	respondJSON(w, status, body)
}

// errorBody is the typed wrapper writeError uses.
type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Error: code, Message: msg})
}

// corsMiddleware mirrors the v0.13 CORS surface — permissive for now;
// the substrate pivot will tighten this per-origin in a future cut.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Agent-Source, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// suppressUnusedTopLevelImports keeps fast-changing imports stable.
var _ = context.Background
var _ = strings.HasPrefix
