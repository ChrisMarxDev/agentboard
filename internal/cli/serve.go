package cli

import (
	"context"

	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	agentboard "github.com/christophermarx/agentboard"
	"github.com/christophermarx/agentboard/internal/auth"
	dbpkg "github.com/christophermarx/agentboard/internal/db"
	embedpkg "github.com/christophermarx/agentboard/internal/embed"
	"github.com/christophermarx/agentboard/internal/gitserver"
	htmlserver "github.com/christophermarx/agentboard/internal/html"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/christophermarx/agentboard/internal/locks"
	"github.com/christophermarx/agentboard/internal/mcp"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/christophermarx/agentboard/internal/server"
	storepkg "github.com/christophermarx/agentboard/internal/store"
	"github.com/spf13/cobra"
	"path/filepath"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the AgentBoard server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser on startup")
	serveCmd.Flags().BoolVar(&allowComponentUpload, "allow-component-upload", false, "Enable PUT/DELETE /api/components/:name (REST). UNSAFE: components run as arbitrary JS in every visitor's browser.")
	serveCmd.Flags().StringVar(&authToken, "auth-token", "", "Shared token required for every request (except /api/health). Accepts Bearer, Basic Auth, or ?token= query. Falls back to AGENTBOARD_AUTH_TOKEN env var.")
}

func resolveProjectPath() string {
	if projectPath != "" {
		return projectPath
	}
	if env := os.Getenv("AGENTBOARD_PATH"); env != "" {
		return env
	}
	if projectName != "" {
		return project.NamedProjectDir(projectName)
	}
	if env := os.Getenv("AGENTBOARD_PROJECT"); env != "" {
		return project.NamedProjectDir(env)
	}
	return project.DefaultProjectDir()
}

func runServe(cmd *cobra.Command, args []string) error {
	projPath := resolveProjectPath()

	// Init when the project dir is missing OR exists but unseeded.
	// The unseeded case matters for hosted deploys: a Docker bind-mount
	// or a manually-created empty volume passes os.Stat but contains no
	// index.md, so Pages.GetPage("index") returns 404 and the SPA can't
	// render anything. InitProject is safe to call on an empty dir
	// (only writes index.md + agentboard.yaml + a seed skill).
	var proj *project.Project
	_, statErr := os.Stat(projPath)
	_, indexErr := os.Stat(filepath.Join(projPath, "index.md"))
	if os.IsNotExist(statErr) || os.IsNotExist(indexErr) {
		log.Printf("Initializing project at %s", projPath)
		var initErr error
		proj, initErr = project.InitProject(projPath)
		if initErr != nil {
			return fmt.Errorf("init project: %w", initErr)
		}
	} else {
		var loadErr error
		proj, loadErr = project.Load(projPath)
		if loadErr != nil {
			return fmt.Errorf("load project: %w", loadErr)
		}
		if err := proj.EnsureDirs(); err != nil {
			return fmt.Errorf("ensure dirs: %w", err)
		}
	}

	// Resolve port
	p := resolvePort()
	if p == 0 {
		p = proj.Config.Port
	}
	if p == 0 {
		p = 3000
	}

	// Open the SQLite connection used by auth + co-stores (teams,
	// invitations, locks, mdx-meta, view-sessions, share, inbox,
	// webhooks). The KV-data layer has moved to files; SQLite now
	// only holds operational metadata.
	dbConn, err := dbpkg.Open(proj.DatabasePath())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer dbConn.Close()

	// Files-first content store. Owns the project tree on disk and
	// is the only data source for the dashboard.
	fileStore, err := storepkg.NewStore(storepkg.Config{ProjectRoot: proj.Path})
	if err != nil {
		return fmt.Errorf("open file store: %w", err)
	}
	defer fileStore.Close()

	// Auth store rides on the shared SQLite connection.
	authStore, err := auth.NewStore(dbConn.Conn())
	if err != nil {
		return fmt.Errorf("open auth store: %w", err)
	}

	// Invitations + locks open ahead of server construction so the
	// first-admin bootstrap below has something to write into.
	invStore, err := invitations.NewStore(dbConn.Conn())
	if err != nil {
		return fmt.Errorf("open invitations store: %w", err)
	}
	lockStore, err := locks.NewStore(dbConn.Conn())
	if err != nil {
		return fmt.Errorf("open locks store: %w", err)
	}

	// Sample content seeding moves to a Cut 5 deliverable — for now
	// fresh projects start empty. The per-doc history NDJSON in the
	// file store is bounded (100 entries per key) + auto-rotated, so
	// no separate pruning loop is needed either.
	_ = fileStore // referenced below; nothing to do here at boot.

	// Set up embedded frontend filesystem
	var frontendHTTPFS http.FileSystem
	distFS, err := fs.Sub(agentboard.FrontendDist, "frontend/dist")
	if err == nil {
		frontendHTTPFS = http.FS(distFS)
	}

	// Flag overrides config (off by default in both).
	uploadEnabled := proj.Config.AllowComponentUpload || allowComponentUpload

	// First-admin bootstrap. If no users exist, mint (or reuse) an
	// admin-role invitation so the operator can claim the first admin
	// via /invite/<id>. Also folds AGENTBOARD_AUTH_TOKEN into a
	// @legacy-agent identity when the env var is set — that path
	// suppresses the invite mint because an identity already exists.
	// See AUTH.md §"Bootstrap".
	legacyToken := authToken
	if legacyToken == "" {
		legacyToken = os.Getenv("AGENTBOARD_AUTH_TOKEN")
	}
	bootstrapInv, err := authStore.BootstrapFirstAdmin(invStore, legacyToken, 0, log.Default())
	if err != nil {
		return fmt.Errorf("auth bootstrap: %w", err)
	}
	if bootstrapInv != nil {
		inviteURL := fmt.Sprintf("http://localhost:%d/invite/%s", p, bootstrapInv.ID)
		inviteFile := filepath.Join(proj.Path, ".agentboard", "first-admin-invite.url")
		_ = os.WriteFile(inviteFile, []byte(inviteURL+"\n"), 0600)
		log.Printf("")
		log.Printf("  ==> Board is unclaimed. Open this URL to create the first admin:")
		log.Printf("      %s", inviteURL)
		log.Printf("      (also written to %s)", inviteFile)
		log.Printf("")
	}

	// Git substrate (spec §§2–5). The Store owns the workspaces table
	// + bare-repo + working-tree dirs under <project>/.agentboard/.
	// Workspace registration happens *before* server.New so the
	// post-receive hook (wired through Hooks.OnPush) can reach the
	// SSE broadcaster the moment the substrate is mounted.
	gitRoot := filepath.Join(proj.Path, ".agentboard")
	gitStore, err := gitserver.NewStore(dbConn.Conn(), gitRoot)
	if err != nil {
		return fmt.Errorf("open git workspace store: %w", err)
	}
	// Seed the dogfood workspace from the existing content/ tree on
	// first boot. Idempotent: if the workspace already exists the
	// call returns the existing row.
	contentSeed := filepath.Join(proj.Path, "content")
	if _, err := gitStore.Create(context.Background(), "dogfood", "system", contentSeed); err != nil {
		log.Printf("Warning: could not initialize dogfood workspace: %v", err)
	}
	// Bootstrap README at workspace root (CORE_GUIDELINES §15, spec §1.5).
	// Idempotent — no-op if README.md already exists in git history.
	if err := gitStore.EnsureFile(
		context.Background(),
		"dogfood",
		"README.md",
		project.BootstrapReadmeMd,
		"system",
		"Add bootstrap README (CORE_GUIDELINES §15)",
	); err != nil {
		log.Printf("Warning: could not seed bootstrap README: %v", err)
	}
	// Materialize the working tree so the SPA has something to read
	// on the first request even if no push has happened yet.
	wtPath, err := gitStore.EnsureWorktree(context.Background(), "dogfood")
	if err != nil {
		log.Printf("Warning: could not materialize dogfood working tree: %v", err)
	} else {
		// Cut 3: SPA reads through the working-tree mirror. The page
		// manager + file watcher + components watcher all key off
		// proj.ContentDir(); routing them through the override turns
		// every push into a live SPA update without touching their
		// internals.
		proj.ContentOverride = wtPath
		log.Printf("Git: dogfood worktree → %s", wtPath)
	}

	// GC any stale proposals (older than 24h, still pending). Cheap
	// recovery for crashes mid-conflict-resolution; on a clean run
	// this is a no-op.
	_ = gitStore.GCStaleProposals(context.Background(), 24*time.Hour)

	// Server-side pushes (Propose, ResolveConflict, EnsureFile,
	// sync-seed) bypass the HTTP push hook, so they wire to this
	// parallel callback to get the same after-push behavior: an
	// event recorded for subscribe + (TODO) SSE for open browsers.
	gitStore.OnInternalPush = func(ctx context.Context, workspace string) {
		_, _ = gitStore.AppendEvent(ctx, gitserver.Event{
			Workspace: workspace,
			Type:      "push",
			At:        time.Now().Unix(),
		})
	}

	// The HTTP push hook fires after every successful receive-pack
	// served over /git/<workspace>.git. It re-checks out the default
	// branch into the working-tree mirror, records an event on the
	// subscribe stream, and broadcasts a page-updated event so open
	// browsers refresh.
	var gitSrvForHooks *gitserver.Server
	gitHooks := gitserver.Hooks{
		OnPush: func(ctx context.Context, workspace string, refs []gitserver.PushedRef) {
			if _, err := gitStore.SyncWorktree(ctx, workspace); err != nil {
				log.Printf("Warning: working-tree sync after push to %s: %v", workspace, err)
				return
			}
			refsJSON, _ := json.Marshal(map[string]any{"refs": refs})
			_, _ = gitStore.AppendEvent(ctx, gitserver.Event{
				Workspace: workspace,
				Type:      "push",
				At:        time.Now().Unix(),
				Payload:   refsJSON,
			})
			log.Printf("Git: pushed %d ref(s) to workspace %s", len(refs), workspace)
			_ = gitSrvForHooks // currently unused; kept for later cuts where the hook reaches into the server.
		},
	}
	gitSrv, err := gitserver.New(gitStore, gitHooks)
	if err != nil {
		return fmt.Errorf("open git server: %w", err)
	}
	gitSrvForHooks = gitSrv

	// HTML renderer: server-rendered dashboard over the worktree mirror.
	// Replaces the React SPA. spec-filesystem-substrate.md §3.
	htmlSrv := &htmlserver.Server{
		WorktreeRoot: wtPath,
		Workspace:    "dogfood",
		Branch:       "main",
		UserResolver: func(r *http.Request) string {
			if u := auth.UserFromContext(r.Context()); u != nil {
				return u.Username
			}
			return ""
		},
	}

	// MCP-side propose adapter. The mcp package carries its own
	// ProposeRequest/Result types so it doesn't have to import
	// gitserver; this shim translates between the two.
	proposeFn := func(ctx context.Context, req mcp.ProposeRequest) (*mcp.ProposeResult, error) {
		gsReq := gitserver.ProposeRequest{
			Workspace: req.Workspace,
			Base:      req.Base,
			Branch:    req.Branch,
			Message:   req.Message,
			Actor:     req.Actor,
		}
		for _, f := range req.Files {
			gsReq.Files = append(gsReq.Files, gitserver.ProposeFile{Path: f.Path, Body: f.Body})
		}
		gsRes, err := gitStore.Propose(ctx, gsReq)
		if err != nil {
			return nil, err
		}
		return &mcp.ProposeResult{
			Success:    gsRes.Success,
			Branch:     gsRes.Branch,
			Commit:     gsRes.Commit,
			ProposalID: gsRes.ProposalID,
			Conflicts:  gsRes.Conflicts,
			Message:    gsRes.Message,
		}, nil
	}

	// Create server
	srv := server.New(server.ServerConfig{
		Project:              proj,
		Conn:                 dbConn.Conn(),
		FileStore:            fileStore,
		Auth:                 authStore,
		Invitations:          invStore,
		Locks:                lockStore,
		SkillFile:            embedpkg.SkillFile(),
		FrontendFS:           frontendHTTPFS,
		DevMode:              devMode,
		DevProxy:             "http://localhost:5173",
		AllowComponentUpload: uploadEnabled,
		MaxFileSizeMB:        proj.Config.MaxFileSizeMB,
		GitServer:            gitSrv,
		HTML:                 htmlSrv,
	})

	// Plumb the git substrate into the MCP server. The agentboard_*
	// tools that talk to git (workspaces, pull, propose,
	// resolve_conflict, subscribe) read these fields on first use;
	// without them they return "git substrate not configured" errors.
	srv.MCP.GitStore = gitStore
	srv.MCP.ProposeFn = proposeFn
	srv.MCP.ResolveConflictFn = func(ctx context.Context, proposalID, file, resolution string) (*mcp.ProposeResult, error) {
		gsRes, err := gitStore.ResolveConflict(ctx, proposalID, file, resolution)
		if err != nil {
			return nil, err
		}
		return &mcp.ProposeResult{
			Success:    gsRes.Success,
			Branch:     gsRes.Branch,
			Commit:     gsRes.Commit,
			ProposalID: gsRes.ProposalID,
			Conflicts:  gsRes.Conflicts,
			Message:    gsRes.Message,
		}, nil
	}
	srv.MCP.SubscribeFn = func(ctx context.Context, workspace string, since int64, types []string, limit int) (*mcp.SubscribeResult, error) {
		events, err := gitStore.ListEvents(ctx, workspace, since, types, limit)
		if err != nil {
			return nil, err
		}
		// If the caller passed since=0 and got nothing back, hand them
		// the current cursor so they can skip historical events and
		// start subscribing from "now."
		cursor := since
		out := make([]any, 0, len(events))
		for _, e := range events {
			out = append(out, e)
			if e.ID > cursor {
				cursor = e.ID
			}
		}
		if cursor == 0 {
			cursor, _ = gitStore.CurrentEventCursor(ctx)
		}
		return &mcp.SubscribeResult{Events: out, Cursor: cursor}, nil
	}

	if uploadEnabled {
		log.Printf("WARNING: component upload is enabled. Any caller of this server can inject JS that runs in every dashboard visitor's browser.")
	}
	hasUser, _ := authStore.HasAnyUser()
	if hasUser {
		log.Printf("Auth: identity-backed. Sign in at /login; admins manage users + invitations + tokens at /admin.")
	}

	// Start the unified content/data watcher. Cut 5 collapsed the
	// previous two watchers (pages + data) into one fsnotify watcher
	// rooted at the project tree. Direct disk writes anywhere under
	// content/, data/, or to index.md propagate the same way an API
	// write would: page changes re-record refs + re-index FTS,
	// data changes refresh the catalog and broadcast a store event.
	// Fixes the gotcha called out in
	// docs/archive/REWRITE-cuts-1-4.md ("don't write content/* directly").
	if err := srv.Pages.StartWatcherOpts(storepkg.WatchOptions{
		OnPage: func(pagePath string) {
			log.Printf("Page updated: %s", pagePath)
			// Re-record refs + re-index FTS so direct-disk writes are
			// indistinguishable from API writes downstream. Best-effort:
			// a refs/search hiccup must not poison the broadcast.
			normalized := storepkg.NormalizePagePath(pagePath)
			normalized = filepath.ToSlash(normalized)
			normalized = strings.TrimPrefix(normalized, "content/")
			if p := srv.Pages.GetPage(normalized); p != nil {
				if srv.PageRefs != nil {
					_ = srv.PageRefs.Record(normalized, storepkg.ExtractRefs(p.Source, normalized))
				}
				if srv.Search != nil {
					_ = srv.Search.IndexPage(p.Path, p.Title, p.Source)
				}
			}
			eventData, _ := json.Marshal(map[string]string{"path": pagePath})
			srv.Broadcaster.Broadcast(server.SSEEvent{
				Type: "page-updated",
				Data: eventData,
			})
		},
		OnData: func(key string) {
			log.Printf("Data updated: %s", key)
			// Refresh the in-memory catalog so /api/index reflects
			// reality. Catalog is best-effort; if the refresh fails,
			// the next /api/index call falls back to walking disk.
			eventData, _ := json.Marshal(map[string]any{"key": key, "op": "EXTERNAL"})
			srv.Broadcaster.Broadcast(server.SSEEvent{
				Type: "data",
				Data: eventData,
			})
		},
	}); err != nil {
		log.Printf("Warning: could not start unified watcher: %v", err)
	}

	// Start component watcher
	if err := srv.Components.StartWatcher(func(names []string) {
		log.Printf("Components updated: %v", names)
		eventData, _ := json.Marshal(map[string][]string{"names": names})
		srv.Broadcaster.Broadcast(server.SSEEvent{
			Type: "components-updated",
			Data: eventData,
		})
	}); err != nil {
		log.Printf("Warning: could not start component watcher: %v", err)
	}

	// Start files watcher
	if err := srv.Files.StartWatcher(func(name string, deleted bool) {
		log.Printf("File %s: %s", map[bool]string{true: "deleted", false: "updated"}[deleted], name)
		eventData, _ := json.Marshal(map[string]any{"name": name, "deleted": deleted})
		srv.Broadcaster.Broadcast(server.SSEEvent{
			Type: "file-updated",
			Data: eventData,
		})
	}); err != nil {
		log.Printf("Warning: could not start files watcher: %v", err)
	}

	// Print startup message
	addr := fmt.Sprintf(":%d", p)
	url := fmt.Sprintf("http://localhost:%d", p)
	fmt.Printf(`
AgentBoard v%s

Project:   %s (%s)
Dashboard: %s
MCP:       %s/mcp

Connect Claude:
  claude mcp add agentboard %s/mcp

Author pages and data via:
  %s/api/<path>                # one namespace, page or data leaf

`, server.Version(), proj.Config.Title, projPath, url, url, url, url)

	// Open browser
	if !noOpen {
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowser(url)
		}()
	}

	fmt.Println("Press Ctrl+C to stop.")
	return srv.ListenAndServe(addr)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}
