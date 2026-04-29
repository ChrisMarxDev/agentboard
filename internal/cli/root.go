package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	projectName          string
	projectPath          string
	port                 int
	noOpen               bool
	devMode              bool
	allowComponentUpload bool
	authToken            string
)

var rootCmd = &cobra.Command{
	Use:   "agentboard",
	Short: "AgentBoard — dashboard server for agent-driven workflows",
	Long: `AgentBoard is a single-binary dashboard server where agents write
content via MCP / REST and humans read a live dashboard in the browser.`,
	RunE: serveCmd.RunE, // Default command is serve
}

func init() {
	rootCmd.PersistentFlags().StringVar(&projectName, "project", "", "Project name (under ~/.agentboard/)")
	rootCmd.PersistentFlags().StringVar(&projectPath, "path", "", "Explicit project path")
	rootCmd.PersistentFlags().IntVar(&port, "port", 0, "Server port (default from config or 3000)")
	rootCmd.PersistentFlags().BoolVar(&devMode, "dev", false, "Run in development mode")
	rootCmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser on startup")
	rootCmd.Flags().BoolVar(&allowComponentUpload, "allow-component-upload", false, "Enable PUT/DELETE /api/components/:name and MCP write/delete component tools. UNSAFE: components run as arbitrary JS in every visitor's browser.")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(projectsCmd)
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resolvePort() int {
	if port != 0 {
		return port
	}
	if env := os.Getenv("AGENTBOARD_PORT"); env != "" {
		var p int
		fmt.Sscanf(env, "%d", &p)
		if p > 0 {
			return p
		}
	}
	return 3000
}
