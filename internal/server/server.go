package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/components"
	interrors "github.com/christophermarx/agentboard/internal/errors"
	"github.com/christophermarx/agentboard/internal/files"
	"github.com/christophermarx/agentboard/internal/grab"
	"github.com/christophermarx/agentboard/internal/inbox"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/christophermarx/agentboard/internal/locks"
	"github.com/christophermarx/agentboard/internal/mcp"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/christophermarx/agentboard/internal/publicroutes"
	"github.com/christophermarx/agentboard/internal/share"
	"github.com/christophermarx/agentboard/internal/store"
	"github.com/christophermarx/agentboard/internal/teams"
	"github.com/christophermarx/agentboard/internal/view"
	"github.com/christophermarx/agentboard/internal/webhooks"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server is the main AgentBoard HTTP server.
type Server struct {
	Project              *project.Project
	Conn                 *sql.DB      // shared SQLite connection for auth + co-stores
	FileStore            *store.Store // files-first content store; sole data source
	Auth                 *auth.Store
	Broadcaster          *Broadcaster
	Pages                *store.PageManager
	PageMeta             *store.MetaStore
	PageApproval         *store.ApprovalStore
	PageRefs             *store.RefStore
	Search               *store.SearchStore
	Components           *components.Manager
	Files                *files.Manager
	Errors               *interrors.Buffer
	Grab                 *grab.Materializer
	MCP                  *mcp.Server
	Share                *share.Store
	ViewSessions         *view.SessionStore
	ViewScope            *view.ScopeBuilder
	ViewPublic           *publicroutes.Matcher
	Webhooks             *webhooks.Store
	WebhookDispatcher    *webhooks.Dispatcher
	webhookSecrets       sync.Map // id → plaintext secret, set at Create
	Inbox                *inbox.Store
	Teams                *teams.Store
	Invitations          *invitations.Store
	Locks                *locks.Store
	UploadTokens         *uploadTokens   // one-shot presigned upload tokens (spec §12)
	Limits               *storeRateStore // per-actor token bucket for /api/data writes
	Router               chi.Router
	SkillFile            string
	AllowComponentUpload bool
}

// ServerConfig holds configuration for creating a new server.
type ServerConfig struct {
	Project              *project.Project
	Conn                 *sql.DB
	FileStore            *store.Store
	Auth                 *auth.Store // identity-backed auth; required
	Invitations          *invitations.Store
	Locks                *locks.Store
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

	// Wire FileStore events to the SSE broadcaster — the only data
	// source now that the legacy KV is gone (Cut 1). The Type stays
	// "data" for one more cut so the frontend's listener keeps
	// working; Cut 3 renames it to "data".
	if cfg.FileStore != nil {
		go func() {
			ch := cfg.FileStore.Subscribe()
			for evt := range ch {
				eventData, _ := json.Marshal(evt)
				broadcaster.Broadcast(SSEEvent{
					Type: "data",
					Data: eventData,
				})
			}
		}()
	}

	pageManager := store.NewPageManager(cfg.Project)
	// Best-effort: if the store exposes a *sql.DB (SQLiteStore does), build
	// a MetaStore over it so we can surface "last edited by" on pages. When
	// the assert fails, PageMeta stays nil and handlers fall back to "no
	// meta available" — never fatal.
	var metaStore *store.MetaStore
	if cfg.Conn != nil {
		if ms, err := store.NewMetaStore(cfg.Conn); err == nil {
			metaStore = ms
		}
	}

	// Approval store — same best-effort pattern as MetaStore. Nil-safe
	// at every read/write site so a DB without SQL backing just
	// degrades to "no approvals", not a boot error.
	var approvalStore *store.ApprovalStore
	if cfg.Conn != nil {
		if a, err := store.NewApprovalStore(cfg.Conn); err == nil {
			approvalStore = a
		}
	}

	// Ref store — the page-dependency graph the view broker uses to
	// decide what a page touches. Best-effort like the others.
	var refStore *store.RefStore
	if cfg.Conn != nil {
		if rs, err := store.NewRefStore(cfg.Conn); err == nil {
			refStore = rs
			// Backfill: walk every page on boot and record its refs.
			// PageInfo.Path has a leading slash; the ref store uses the
			// map key (no slash). Normalise before recording.
			for _, p := range pageManager.ListPages() {
				key := strings.TrimPrefix(p.Path, "/")
				if key == "" {
					key = "index"
				}
				_ = refStore.Record(key, store.ExtractRefs(p.Source, key))
			}
		} else {
			log.Printf("agentboard: page_refs unavailable (continuing without): %v", err)
		}
	}

	// Full-text search index (SQLite FTS5). Bootstrapped from the page
	// manager's initial scan; kept in sync by the page handlers. Same
	// best-effort posture as MetaStore — if the DB can't be addressed or
	// FTS isn't available, search silently becomes a no-op rather than
	// failing the boot.
	var searchStore *store.SearchStore
	if cfg.Conn != nil {
		if ss, err := store.NewSearchStore(cfg.Conn); err == nil {
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

	// Share-token store — needed for per-page public share links. Same
	// best-effort posture as the other *sql.DB-backed stores: if the
	// underlying data store doesn't expose a DB, share stays nil and
	// the middleware becomes a no-op.
	var shareStore *share.Store
	if cfg.Conn != nil {
		if ss, err := share.NewStore(cfg.Conn); err == nil {
			shareStore = ss
		} else {
			log.Printf("agentboard: share tokens unavailable (continuing without): %v", err)
		}
	}

	// Inbox store — per-user reminders.
	var inboxStore *inbox.Store
	if cfg.Conn != nil {
		if is, err := inbox.NewStore(cfg.Conn); err == nil {
			inboxStore = is
		} else {
			log.Printf("agentboard: inbox unavailable (continuing without): %v", err)
		}
	}

	// Teams store — role groups that expand mentions to members.
	var teamStore *teams.Store
	if cfg.Conn != nil {
		if ts, err := teams.NewStore(cfg.Conn); err == nil {
			teamStore = ts
		} else {
			log.Printf("agentboard: teams unavailable (continuing without): %v", err)
		}
	}

	// Invitations store — one-time codes that let new users claim
	// their account and first token without admin-CLI hand-delivery.
	// Caller (cli/serve.go) pre-opens this so BootstrapFirstAdmin can
	// run before server.New(); fall back to creating one locally for
	// any caller that didn't.
	invStore := cfg.Invitations
	if invStore == nil {
		if cfg.Conn != nil {
			if is, err := invitations.NewStore(cfg.Conn); err == nil {
				invStore = is
			} else {
				log.Printf("agentboard: invitations unavailable (continuing without): %v", err)
			}
		}
	}

	// Page-locks store — admin-gated freeze on individual pages.
	lockStore := cfg.Locks
	if lockStore == nil {
		if cfg.Conn != nil {
			if ls, err := locks.NewStore(cfg.Conn); err == nil {
				lockStore = ls
			} else {
				log.Printf("agentboard: page locks unavailable (continuing without): %v", err)
			}
		}
	}

	// Webhook subscription store. Same best-effort posture.
	var webhookStore *webhooks.Store
	if cfg.Conn != nil {
		if ws, err := webhooks.NewStore(cfg.Conn); err == nil {
			webhookStore = ws
		} else {
			log.Printf("agentboard: webhooks unavailable (continuing without): %v", err)
		}
	}

	// View session store — cookie-backed redeemed share sessions.
	var viewSessions *view.SessionStore
	if cfg.Conn != nil {
		if vs, err := view.NewSessionStore(cfg.Conn); err == nil {
			viewSessions = vs
		} else {
			log.Printf("agentboard: view sessions unavailable (continuing without): %v", err)
		}
	}

	// Public-routes matcher reused by the view broker for anonymous
	// reads. Empty config means anonymous gets nothing.
	var publicMatcher *publicroutes.Matcher
	if cfg.Project != nil && cfg.Project.Config != nil {
		publicMatcher = publicroutes.New(cfg.Project.Config.Public.Paths)
	}

	// ScopeBuilder is the per-request scope factory.
	var viewScope *view.ScopeBuilder
	if refStore != nil {
		viewScope = &view.ScopeBuilder{Refs: refStore, PublicMatcher: publicMatcher}
	}

	compManager := components.NewManager(cfg.Project)
	fileManager := files.NewManager(cfg.Project, cfg.MaxFileSizeMB)
	errorBuffer := interrors.NewBuffer()
	grabber := &grab.Materializer{Pages: pageManager, FileStore: cfg.FileStore}

	mcpServer := &mcp.Server{
		FileStore: cfg.FileStore,
		Pages:     pageManager,
		Files:     fileManager,
		Grab:      grabber,
		Auth:      cfg.Auth,
		// WebhookDispatcher is set below after the dispatcher is built.
	}
	// Wire the upload-token mint into MCP. The base URL needs a host;
	// for now we use localhost + port, which works for every Claude
	// Code or Codex agent connecting to the same machine. Hosted-mode
	// callers will see this URL through the X-Forwarded-Host path on
	// the REST mint and never reach this MCP fallback. Wider-trust
	// rework lives behind the same flag as `--allow-component-upload`
	// when it lands.

	s := &Server{
		Project:              cfg.Project,
		Conn:                 cfg.Conn,
		FileStore:            cfg.FileStore,
		Auth:                 cfg.Auth,
		Broadcaster:          broadcaster,
		Pages:                pageManager,
		PageMeta:             metaStore,
		PageApproval:         approvalStore,
		PageRefs:             refStore,
		Search:               searchStore,
		Components:           compManager,
		Files:                fileManager,
		Errors:               errorBuffer,
		Inbox:                inboxStore,
		Teams:                teamStore,
		Invitations:          invStore,
		Locks:                lockStore,
		UploadTokens:         newUploadTokens(),
		Limits:               newRateStore(),
		Grab:                 grabber,
		MCP:                  mcpServer,
		Share:                shareStore,
		Webhooks:             webhookStore,
		ViewSessions:         viewSessions,
		ViewScope:            viewScope,
		ViewPublic:           publicMatcher,
		SkillFile:            cfg.SkillFile,
		AllowComponentUpload: cfg.AllowComponentUpload,
	}

	// MCP fallback for the upload-token flow when the agent doesn't
	// have an HTTP-side base URL handy. The REST endpoint is the
	// preferred path (it reflects X-Forwarded-Host); this mint defaults
	// to localhost:<port> which is the right answer for the local-dev
	// majority of MCP usage.
	mcpServer.MintUploadToken = func(name, actor string, sizeBytes int64) (string, string, int64, bool) {
		if s.UploadTokens == nil || s.Files == nil {
			return "", "", 0, false
		}
		clean, err := files.ValidateName(name)
		if err != nil {
			return "", "", 0, false
		}
		maxBytes := s.Files.MaxSizeBytes()
		if sizeBytes > maxBytes {
			return "", "", 0, false
		}
		tok := s.UploadTokens.Mint(clean, actor, maxBytes)
		// Best-effort URL: localhost is correct on a dev machine; for
		// hosted instances the REST mint is the supported path.
		url := fmt.Sprintf("http://localhost:%d/api/upload/%s", cfg.Project.Config.Port, tok.Token)
		return url, tok.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"), tok.MaxSizeBytes, true
	}

	// Start the webhook dispatcher once the server struct is fully
	// populated. Runs in the background — we use context.Background()
	// here because the lifetime of the dispatcher is the lifetime of
	// the process; graceful-stop support would swap this for a Server
	// context if we ever add one.
	if webhookStore != nil {
		s.WebhookDispatcher = webhooks.NewDispatcher(webhookStore, webhooks.DispatcherOptions{
			SecretResolver: s.webhookSecretFor,
		})
		// Back-reference so the MCP tools can emit events too.
		mcpServer.WebhookDispatcher = s.WebhookDispatcher
		go s.WebhookDispatcher.Start(context.Background())
	}

	// Wire the broadcaster's events into the webhook dispatcher. We
	// subscribe as just another SSE listener and re-shape events into
	// the webhook vocabulary ("data.set.<key>", "content.<path>.updated",
	// etc.) before emitting.
	if s.WebhookDispatcher != nil {
		go s.bridgeBroadcastToWebhooks()
	}

	s.Router = s.buildRouter(cfg)
	return s
}

// bridgeBroadcastToWebhooks subscribes to the broadcaster and
// translates internal SSE events into the outbound webhook event
// vocabulary. Non-data events pass through with a short-name mapping;
// `heartbeat` is dropped (nothing observable happened).
func (s *Server) bridgeBroadcastToWebhooks() {
	id, ch := s.Broadcaster.Subscribe()
	defer s.Broadcaster.Unsubscribe(id)
	for evt := range ch {
		name, payload, keep := webhookEventName(evt)
		if !keep {
			continue
		}
		s.WebhookDispatcher.Emit(webhooks.Event{
			Name: name,
			Data: payload,
		})
	}
}

// webhookEventName maps an internal SSEEvent to the outbound webhook
// event name + structured payload. Returns keep=false for events that
// shouldn't fan out (e.g. heartbeat).
func webhookEventName(evt SSEEvent) (string, map[string]any, bool) {
	switch evt.Type {
	case "heartbeat":
		return "", nil, false
	case "data":
		var raw struct {
			Key    string `json:"key"`
			Action string `json:"action"`
			Value  any    `json:"value"`
			ID     string `json:"id,omitempty"`
			Source string `json:"source,omitempty"`
		}
		_ = json.Unmarshal(evt.Data, &raw)
		action := raw.Action
		if action == "" {
			action = "updated"
		}
		name := "data." + action
		if raw.Key != "" {
			name = name + "." + raw.Key
		}
		payload := map[string]any{"key": raw.Key, "action": raw.Action, "value": raw.Value}
		if raw.ID != "" {
			payload["id"] = raw.ID
		}
		if raw.Source != "" {
			payload["source"] = raw.Source
		}
		return name, payload, true
	case "page-updated":
		var raw struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(evt.Data, &raw)
		name := "content.updated"
		if raw.Path != "" {
			name = name + "." + strings.TrimPrefix(raw.Path, "/")
		}
		return name, map[string]any{"path": raw.Path}, true
	case "file-updated":
		var raw struct {
			Name    string `json:"name"`
			Deleted bool   `json:"deleted"`
		}
		_ = json.Unmarshal(evt.Data, &raw)
		suffix := "updated"
		if raw.Deleted {
			suffix = "deleted"
		}
		name := "file." + suffix
		if raw.Name != "" {
			name = name + "." + raw.Name
		}
		return name, map[string]any{"name": raw.Name, "deleted": raw.Deleted}, true
	case "page-approval":
		var raw struct {
			Path     string `json:"path"`
			Approved bool   `json:"approved"`
		}
		_ = json.Unmarshal(evt.Data, &raw)
		suffix := "approved"
		if !raw.Approved {
			suffix = "unapproved"
		}
		name := "approval." + suffix
		if raw.Path != "" {
			name = name + "." + strings.TrimPrefix(raw.Path, "/")
		}
		return name, map[string]any{"path": raw.Path, "approved": raw.Approved}, true
	}
	// Everything else (components-updated, error-*, page-approval
	// fallbacks) forwards with the raw type as event name. Coarse but
	// predictable.
	return evt.Type, map[string]any{"raw": string(evt.Data)}, true
}

func (s *Server) buildRouter(cfg ServerConfig) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)
	r.Use(rejectAPITraversal)

	// Token-auth is scoped to the API / MCP / skill endpoints only. The
	// SPA assets (`/`, `/login`, static JS/CSS) must render without a
	// token so the browser can reach /login and sign the user in. The
	// SPA has its own client-side gate (SessionGate + apiFetch's 401
	// redirect).
	//
	// Public matcher wraps auth — if the request path matches a configured
	// read-rule, auth is skipped and the request proceeds with
	// IsPublicRequest(ctx)=true. Writes and /api/admin/* always route
	// through auth, no matter what the config says. The matcher was
	// built in New(); we reuse it here via s.ViewPublic to keep a single
	// source of truth.
	tokenMw := auth.TokenMiddleware(cfg.Auth, auth.MiddlewareConfig{
		// /api/config is always anonymous: the SPA reads it before it
		// knows whether the current URL is supposed to render as public
		// or gated. The response contains no user data, only the
		// operator-authored config — including public.paths — which the
		// SPA uses to decide whether to skip the login redirect.
		//
		// /api/introduction is always anonymous too: it's the
		// paste-one-URL-to-teach-an-agent entry point. Under /api/ so a
		// user page at "/introduction" can't collide.
		OpenPaths: []string{
			"/api/setup/status",
			"/api/config",
			"/api/introduction",
			"/api/share/redeem",
			// /api/auth/login mints credentials, /api/auth/logout
			// must succeed even when the cookie is invalid (so
			// we can clear it). Both run anonymous; the handlers
			// resolve the session themselves when relevant.
			//
			// /api/auth/me also runs anonymous so the SPA can
			// probe sign-in status — the handler reads the
			// session cookie directly and 401s when none is
			// present, but no /login redirect is triggered.
			"/api/auth/login",
			"/api/auth/logout",
			"/api/auth/me",
		},
	})
	gatedAuth := publicroutes.Gate(s.ViewPublic, tokenMw, publicroutes.GateOptions{})
	// Old share.Middleware was deleted — shares now redeem into a
	// cookie (handleRedeemShare) and carry that cookie on every
	// /api/view/* request. The view broker does its own authority
	// resolution; this gated chain only covers bearer/public auth.
	//
	// CSRFMiddleware is mounted AFTER the auth chain so it can read
	// SessionFromContext. It only fires on cookie-authenticated
	// state-changing requests; bearer / OAuth-token requests pass
	// through unchanged.
	gated := func(r chi.Router) {
		r.Use(gatedAuth)
		r.Use(auth.AuthorizeMiddleware())
		r.Use(auth.CSRFMiddleware())
	}

	// /api/view/* is OUTSIDE the gated group: the broker does its own
	// authority resolution (bearer | share cookie | public). Mounting
	// it here means the auth middleware never gets a chance to 401 a
	// cookie-only share visitor before the broker can inspect the
	// cookie.
	r.Post("/api/view/open", s.handleViewOpen)
	r.Get("/api/view/events", s.handleViewEvents)
	r.Get("/api/view/files/*", s.handleViewFile)

	// /api/invitations/{id}[/redeem] are public — anyone with the URL
	// can hit them to see the invite metadata and redeem it. Mounted
	// OUTSIDE the gated group so no 401 slips in; the invite ID is
	// the credential.
	r.Get("/api/invitations/{id}", s.handleGetInvitationPublic)
	r.Post("/api/invitations/{id}/redeem", s.handleRedeemInvitation)

	// /api/upload/{token} is public — the unguessable token is the
	// credential. Mounted here so the bearer-required middleware
	// doesn't reject the upload before the handler can validate the
	// token. The MINTING endpoint stays inside the auth group below.
	r.Put("/api/upload/{token}", s.handleUploadWithToken)

	// MCP / OAuth 2.1 discovery endpoints. MUST be reachable without
	// authentication: a client doesn't have a token yet when it asks
	// "where is your authorization server?". Published per
	// RFC 9728 (Protected Resource Metadata) + RFC 8414 (Authorization
	// Server Metadata), referenced by the WWW-Authenticate Bearer
	// challenge that auth.unauthorized() emits on every 401.
	r.Get("/.well-known/oauth-protected-resource", auth.HandleProtectedResourceMetadata)
	r.Get("/.well-known/oauth-authorization-server", auth.HandleAuthorizationServerMetadata)

	// OAuth 2.1 authorization-server endpoints. Reachable anonymously
	// for the same reason: the user is acquiring a credential via
	// /oauth/authorize, then the client exchanges code → token at
	// /oauth/token. Both paths do their own validation. /oauth/register
	// is RFC 7591 Dynamic Client Registration.
	r.Post("/oauth/register", s.handleOAuthRegister)
	r.Get("/oauth/authorize", s.handleOAuthAuthorize)
	r.Post("/oauth/authorize/decide", s.handleOAuthAuthorizeDecide)
	r.Post("/oauth/token", s.handleOAuthToken)

	// API routes
	r.Group(func(r chi.Router) {
		gated(r)
		r.Route("/api", func(api chi.Router) {
			// Mount the registered handlers, then override chi's default
			// plaintext "404 page not found" with a JSON envelope so
			// agents that mistype an /api/* path get a parseable error
			// instead of a wall of HTML or "404 page not found" text.
			apiRoutes(s)(api)
			api.NotFound(apiNotFound)
			api.MethodNotAllowed(apiMethodNotAllowed)
		})
		r.Get("/skill", s.handleSkill)
		r.Post("/mcp", s.MCP.ServeHTTP)
		r.Get("/mcp", s.MCP.ServeHTTP)
	})

	// Frontend — serve embedded SPA or proxy to dev server. Unknown
	// `/api/*` requests are caught by the api router's NotFound
	// handler above (apiNotFound). This fallback fires for non-API
	// URLs.
	//
	// Guard: only GET/HEAD/OPTIONS fall through to the SPA. A write
	// verb (PUT/POST/PATCH/DELETE) hitting an unmatched non-API path
	// — e.g. a client that strips `..` from `/api/content/../foo`
	// before sending and lands on `/foo` — must NOT receive 200 +
	// dashboard HTML. Return JSON 404 so the caller sees an error.
	spaFallback := func() http.HandlerFunc {
		if cfg.DevMode && cfg.DevProxy != "" {
			return devProxyHandler(cfg.DevProxy)
		}
		if cfg.FrontendFS != nil {
			fileServer := http.FileServer(cfg.FrontendFS)
			return func(w http.ResponseWriter, r *http.Request) {
				path := r.URL.Path
				f, err := cfg.FrontendFS.Open(path[1:])
				if err != nil {
					r.URL.Path = "/"
					fileServer.ServeHTTP(w, r)
					return
				}
				f.Close()
				fileServer.ServeHTTP(w, r)
			}
		}
		return s.handleFrontend
	}()
	r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			spaFallback(w, r)
		default:
			respondError(w, http.StatusNotFound, "ROUTE_NOT_FOUND",
				"no route matches "+r.Method+" "+r.URL.Path)
		}
	})

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

		// Per-user token management — self-or-admin scope.
		s.registerUserTokenRoutes(r)

		// Per-user password + session management — self-or-admin
		// scope, mirroring the token surface.
		s.registerUserPasswordRoutes(r)
		s.registerUserSessionRoutes(r)

		// Browser-session login surface (POST /login, /logout,
		// GET /me). Open-listed in the middleware config above
		// so they pass without a token.
		s.registerAuthRoutes(r)

		// Teams — readable by any authenticated token. Writes sit
		// under /api/admin/teams (registerAdminTeamRoutes).
		s.registerTeamRoutes(r)

		// /api/me — the shell's "who am I signed in as" hook.
		r.Get("/me", s.handleAdminMe)

		// Setup status — open. POST /api/setup was removed in Auth v1;
		// board claim now happens via /invite/<id>.
		r.Get("/setup/status", s.handleSetupStatus)

		// Content side-channel endpoints. The legacy /api/content/<path>
		// CRUD verbs got retired in Cut 8 — pages now live at /api/<path>
		// alongside data leaves through the unified namespace
		// (handlers_unified.go). The cross-cutting verbs below are not
		// content reads, so they keep dedicated routes.
		r.Get("/content", s.handleListPages)
		r.Post("/content/move", s.handleMovePage)
		r.Post("/content/bulk-delete", s.handleBulkDeleteContent)

		// Share tokens — per-page public links. Lives under /api/share
		// (not nested under /content/) so chi's content wildcard doesn't
		// swallow the sub-route.
		r.Post("/share", s.handleCreateShare)
		r.Get("/share", s.handleListShares)
		r.Delete("/share/{id}", s.handleRevokeShare)
		// Fragment → cookie handshake. Anonymous callers hit this with
		// the plaintext token extracted from the URL fragment; the
		// server mints a scoped cookie. No auth required.
		r.Post("/share/redeem", s.handleRedeemShare)

		// Per-page approval — "a human read this version and said it's
		// correct". Lives under /api/approval for the same reason as
		// /api/share (avoid chi's /content/* wildcard).
		r.Get("/approval", s.handleGetApproval)
		r.Post("/approval", s.handleCreateApproval)
		r.Delete("/approval", s.handleRevokeApproval)

		// Page locks — admin-only "freeze" on individual pages.
		s.registerLockRoutes(r)

		// Webhook subscriptions — outbound event delivery. Owners see
		// their own; admins see all (handler branches on auth).
		r.Post("/webhooks", s.handleCreateWebhook)
		r.Get("/webhooks", s.handleListWebhooks)
		r.Get("/webhooks/{id}", s.handleGetWebhook)
		r.Patch("/webhooks/{id}", s.handleUpdateWebhook)
		r.Delete("/webhooks/{id}", s.handleRevokeWebhook)
		r.Post("/webhooks/{id}/test", s.handleTestWebhook)
		// Ad-hoc fire — used by the <Button fires="..."> component and
		// by any agent that wants to produce a user-triggered event.
		r.Post("/webhooks/fire", s.handleFireWebhook)

		// Inbox — per-user reminder queue. Every endpoint reads the
		// recipient from the request context (bearer's user), never
		// from a query param, so cross-user reads are impossible even
		// for admins. Strong privacy boundary.
		r.Get("/inbox", s.handleListInbox)
		r.Get("/inbox/count", s.handleInboxCount)
		r.Post("/inbox/read-all", s.handleInboxMarkAllRead)
		r.Post("/inbox/{id}/read", s.handleInboxItem)
		r.Post("/inbox/{id}/archive", s.handleInboxItem)
		r.Delete("/inbox/{id}", s.handleInboxItem)

		// /api/view/* is registered OUTSIDE this gated group — the
		// broker does its own authority resolution (bearer | cookie |
		// anonymous-public). Registering here would kick share-cookie
		// visitors out with 401 before the handler can inspect the
		// cookie.

		// /api/data/* + /api/index + /api/search + /api/activity —
		// the files-first store namespace. Singletons live at
		// /api/data/<key>; collection items at /api/data/<key>/<id>;
		// streams (.ndjson) live next to docs and POST :append.
		// Mounted under the gated chain so reads + writes share the
		// same auth posture as content/files.
		s.registerStoreRoutes(r)

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

		// Skills — a read view on top of content/skills/<slug>/SKILL.md
		r.Get("/skills", s.handleListSkills)
		r.Get("/skills/{slug}", s.handleGetSkill)

		// Render-error beacons from frontend components (Mermaid, Markdown, Image, …)
		r.Get("/errors", s.handleListErrors)
		r.Post("/errors", s.handleRecordError)
		r.Delete("/errors", s.handleClearErrors)

		// Lightweight combined tree — pages + files, no source bodies
		r.Get("/tree", s.handleTree)

		// /api/search is the unified store search (registered via
		// registerStoreRoutes above). The page-only FTS5 search lives
		// at /api/search/pages until Cut 4 merges them.
		r.Get("/search/pages", s.handleSearch)

		// Grab — materialize a list of picks into agent-ready text
		r.Post("/grab", s.handleGrab)

		// SSE
		r.Get("/events", s.Broadcaster.ServeHTTP)

		// Meta
		r.Get("/health", s.handleHealth)
		r.Get("/config", s.handleConfig)

		// Public agent primer. Registered inside /api but the auth
		// middleware short-circuits on its path (see OpenPaths above).
		// Namespaced under /api so a user-authored page at /introduction
		// doesn't collide with the discovery endpoint.
		r.Get("/introduction", s.handleIntroduction)

		// ---------- Cut 7: unified /api/<path> namespace (spec §5) ----------
		//
		// One namespace, one CRUD per leaf. Reserved /api/* prefixes
		// above (admin, auth, content, data, files, components, etc.)
		// win the chi dispatcher; this catch-all picks up anything
		// else as a content-tier path. Mirrors handlers_unified.go's
		// dispatcher logic — page tree first, data catalog second.
		//
		// MUST stay last in this function so the more-specific routes
		// register first.
		r.Get("/*", s.handleUnifiedRead)
		r.Put("/*", s.handleUnifiedWrite)
		r.Patch("/*", s.handleUnifiedPatch)
		r.Delete("/*", s.handleUnifiedDelete)
		r.Post("/*", s.handleUnifiedAppend) // requires `:append` suffix on the path
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

// apiNotFound is the catch-all for `/api/*` URLs that don't match any
// registered route. Without this, unknown API paths fell through to
// the SPA's `*` handler and returned the dashboard HTML with status
// 200 — agents reading the response could not distinguish "wrote ok"
// from "no route here" until they parsed the body.
func apiNotFound(w http.ResponseWriter, r *http.Request) {
	respondError(w, http.StatusNotFound, "ROUTE_NOT_FOUND",
		"no API route matches "+r.Method+" "+r.URL.Path)
}

// apiMethodNotAllowed mirrors apiNotFound for the case where the path
// matches a registered route but the verb doesn't. chi defaults to a
// plaintext 405; we want the same JSON envelope as every other error.
func apiMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	respondError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
		r.Method+" not allowed on "+r.URL.Path)
}

// rejectAPITraversal catches `/api/...` URLs whose raw RequestURI
// contains `..` segments BEFORE chi's path normalisation rewrites them
// out of the API namespace. Without this, `PUT /api/content/../foo`
// silently became `PUT /foo`, which fell through to the SPA and
// returned 200 + dashboard HTML. The path-cleaning is harmless from a
// disk-access standpoint (chi never lets the `..` reach a file
// system call), but the response shape was misleading to agents.
//
// Inspecting the *RequestURI* (raw bytes from the wire) rather than
// `r.URL.Path` (already cleaned) is the only reliable way to spot
// the traversal attempt.
func rejectAPITraversal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ru := r.RequestURI
		// Strip query string for the traversal check.
		if i := strings.IndexByte(ru, '?'); i >= 0 {
			ru = ru[:i]
		}
		if strings.HasPrefix(ru, "/api/") && strings.Contains(ru, "/..") {
			respondError(w, http.StatusBadRequest, "INVALID_PATH",
				"path traversal segments are not allowed in /api/ URLs")
			return
		}
		next.ServeHTTP(w, r)
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
