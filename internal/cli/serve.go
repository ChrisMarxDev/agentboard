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
	"github.com/christophermarx/agentboard/internal/data"
	embedpkg "github.com/christophermarx/agentboard/internal/embed"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/christophermarx/agentboard/internal/server"
	"github.com/spf13/cobra"
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

	// Check if project exists, if not init it
	var proj *project.Project
	if _, err := os.Stat(projPath); os.IsNotExist(err) {
		log.Printf("Creating new project at %s", projPath)
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

	// Open data store
	store, err := data.NewSQLiteStore(proj.DatabasePath())
	if err != nil {
		return fmt.Errorf("open data store: %w", err)
	}
	defer store.Close()

	// Open auth store on the same SQLite connection pool.
	authStore, err := auth.NewStore(store.DB())
	if err != nil {
		return fmt.Errorf("open auth store: %w", err)
	}

	// Seed sample data if this is a fresh database
	keys, _ := store.ListKeys()
	if len(keys) == 0 {
		if err := project.SeedSampleData(store); err != nil {
			log.Printf("Warning: could not seed sample data: %v", err)
		}
	}

	// Start history pruning background job
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		retention := time.Duration(proj.Config.HistoryRetentionDays) * 24 * time.Hour
		for range ticker.C {
			if err := store.PruneHistory(cmd.Context(), retention); err != nil {
				log.Printf("Warning: history pruning failed: %v", err)
			}
		}
	}()

	// Set up embedded frontend filesystem
	var frontendHTTPFS http.FileSystem
	distFS, err := fs.Sub(agentboard.FrontendDist, "frontend/dist")
	if err == nil {
		frontendHTTPFS = http.FS(distFS)
	}

	// Flag overrides config (off by default in both).
	uploadEnabled := proj.Config.AllowComponentUpload || allowComponentUpload

	// Mint the initial admin token on first run (and fold AGENTBOARD_AUTH_TOKEN
	// into a legacy-agent identity if it's set). Idempotent: no-op if any
	// identity already exists. See AUTH.md §"Bootstrap".
	legacyToken := authToken
	if legacyToken == "" {
		legacyToken = os.Getenv("AGENTBOARD_AUTH_TOKEN")
	}
	if err := authStore.BootstrapOnEmpty(legacyToken, log.Default()); err != nil {
		return fmt.Errorf("auth bootstrap: %w", err)
	}

	// Create server
	srv := server.New(server.ServerConfig{
		Project:              proj,
		Store:                store,
		Auth:                 authStore,
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
		log.Printf("Auth: identity-backed. Sign in at /login; admins manage users + tokens at /admin. Agents use Bearer/Basic/?token=.")
	} else {
		log.Printf("Auth: board is UNCLAIMED. First visitor at /login picks an admin username and gets the token. Alternatively run `agentboard admin mint-admin <username>` on the host.")
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

Or ask any agent to POST to:
  %s/api/data/:key

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
