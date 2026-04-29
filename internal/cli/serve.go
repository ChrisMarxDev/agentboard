package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	agentboard "github.com/christophermarx/agentboard"
	"github.com/christophermarx/agentboard/internal/auth"
	dbpkg "github.com/christophermarx/agentboard/internal/db"
	embedpkg "github.com/christophermarx/agentboard/internal/embed"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/christophermarx/agentboard/internal/locks"
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
	serveCmd.Flags().BoolVar(&allowComponentUpload, "allow-component-upload", false, "Enable PUT/DELETE /api/components/:name and MCP write/delete component tools. UNSAFE: components run as arbitrary JS in every visitor's browser.")
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
	})

	if uploadEnabled {
		log.Printf("WARNING: component upload is enabled. Any caller of this server can inject JS that runs in every dashboard visitor's browser.")
	}
	hasUser, _ := authStore.HasAnyUser()
	if hasUser {
		log.Printf("Auth: identity-backed. Sign in at /login; admins manage users + invitations + tokens at /admin.")
	}

	// Start page watcher
	if err := srv.Pages.StartWatcher(func(pagePath string) {
		log.Printf("Page updated: %s", pagePath)
		eventData, _ := json.Marshal(map[string]string{"path": pagePath})
		srv.Broadcaster.Broadcast(server.SSEEvent{
			Type: "page-updated",
			Data: eventData,
		})
	}); err != nil {
		log.Printf("Warning: could not start page watcher: %v", err)
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
  %s/api/content/:path        # MDX pages
  %s/api/data/:key             # frontmatter values + collections

`, server.Version(), proj.Config.Title, projPath, url, url, url, url, url)

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
