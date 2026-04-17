package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/christophermarx/agentboard/internal/project"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new AgentBoard project",
	RunE: func(cmd *cobra.Command, args []string) error {
		projPath := resolveProjectPath()

		if _, err := os.Stat(projPath); err == nil {
			return fmt.Errorf("project already exists at %s", projPath)
		}

		proj, err := project.InitProject(projPath)
		if err != nil {
			return err
		}

		fmt.Printf("Created project at %s\n", proj.Path)
		fmt.Printf("Run 'agentboard --path %s' to start.\n", proj.Path)
		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print AgentBoard version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("AgentBoard v0.1.0")
	},
}

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List all projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		agentboardDir := filepath.Join(home, ".agentboard")

		entries, err := os.ReadDir(agentboardDir)
		if os.IsNotExist(err) {
			fmt.Println("No projects found.")
			return nil
		}
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if entry.IsDir() {
				fmt.Println(entry.Name())
			}
		}
		return nil
	},
}
