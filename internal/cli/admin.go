package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/data"
	"github.com/christophermarx/agentboard/internal/project"
	"github.com/spf13/cobra"
)

// adminCmd is the parent "agentboard admin ..." group. Every subcommand
// operates directly on the project's SQLite file — gated by filesystem
// access, which is the correct trust boundary for recovery operations.
var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Administer AgentBoard identities and auth (local file access; not over the network)",
	Long: `agentboard admin exposes local-file operations on the auth store:
mint bootstrap codes, list identities, reset admin access after a lockout.

All commands require filesystem access to the project database. Run them
on the host where AgentBoard runs. They do NOT speak to any network API.`,
}

var adminBootstrapTTL int

var adminBootstrapCodeCmd = &cobra.Command{
	Use:   "bootstrap-code",
	Short: "Generate a one-time bootstrap code for claiming admin via /setup",
	Long: `Prints a fresh bootstrap code and stores its hash in the project
database. Visit /setup in the browser and enter the code along with a
username and password to create (or claim) admin access.

Codes are single-use and expire after --ttl hours (default 24).`,
	RunE: runAdminBootstrapCode,
}

var adminResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset admin access after a lockout (invalidates all admin sessions)",
	Long: `Generates a new bootstrap code AND clears password hashes for every
existing admin identity. Active admin sessions are destroyed. Use this
when the only admin forgot their password; the legacy agent tokens and
non-admin identities are left untouched.`,
	RunE: runAdminReset,
}

var adminListCmd = &cobra.Command{
	Use:   "list",
	Short: "List identities in the project database",
	Long: `Prints one row per identity with name, kind, last-used, and revoked
status. Never prints tokens or password hashes — they're one-way and
not recoverable from the database.`,
	RunE: runAdminList,
}

func init() {
	adminBootstrapCodeCmd.Flags().IntVar(&adminBootstrapTTL, "ttl", 24, "Hours until the code expires")
	adminCmd.AddCommand(adminBootstrapCodeCmd)
	adminCmd.AddCommand(adminResetCmd)
	adminCmd.AddCommand(adminListCmd)
	rootCmd.AddCommand(adminCmd)
}

// openAuthStore opens the project's SQLite DB and returns a Store. The
// caller is responsible for closing the underlying DB via the returned
// closer.
func openAuthStore() (*auth.Store, func(), error) {
	projPath := resolveProjectPath()
	if _, err := os.Stat(projPath); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("project not found at %s — run `agentboard serve` once to create it", projPath)
	}
	proj, err := project.Load(projPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load project: %w", err)
	}
	store, err := data.NewSQLiteStore(proj.DatabasePath())
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	authStore, err := auth.NewStore(store.DB())
	if err != nil {
		store.Close()
		return nil, nil, fmt.Errorf("open auth store: %w", err)
	}
	return authStore, func() { store.Close() }, nil
}

func runAdminBootstrapCode(cmd *cobra.Command, _ []string) error {
	a, closer, err := openAuthStore()
	if err != nil {
		return err
	}
	defer closer()

	ttl := time.Duration(adminBootstrapTTL) * time.Hour
	code, bc, err := a.CreateBootstrapCode(ttl, "cli")
	if err != nil {
		return fmt.Errorf("create bootstrap code: %w", err)
	}
	fmt.Printf(`One-time bootstrap code (expires %s):

  %s

Visit /setup in the browser and enter this code to create admin access.
The code is stored as a one-way hash; save it before closing this terminal.
`, bc.ExpiresAt.Local().Format(time.RFC1123), code)
	return nil
}

func runAdminReset(cmd *cobra.Command, _ []string) error {
	a, closer, err := openAuthStore()
	if err != nil {
		return err
	}
	defer closer()

	// Print a loud banner and require confirmation unless --yes is set.
	if !adminResetYes {
		fmt.Fprintln(os.Stderr, "WARNING: this will clear passwords for ALL admin identities and destroy their sessions.")
		fmt.Fprint(os.Stderr, "Type 'reset' to continue: ")
		var got string
		fmt.Scanln(&got)
		if strings.TrimSpace(got) != "reset" {
			return fmt.Errorf("aborted")
		}
	}

	admins, err := a.ListIdentities()
	if err != nil {
		return err
	}
	cleared := 0
	for _, ident := range admins {
		if ident.Kind != auth.KindAdmin || ident.RevokedAt != nil {
			continue
		}
		// Password-less admins can't log in; a fresh bootstrap code is the
		// only way back in.
		if err := a.SetPassword(ident.ID, ""); err != nil {
			return fmt.Errorf("clear password for %s: %w", ident.Name, err)
		}
		if err := a.DeleteSessionsForIdentity(ident.ID); err != nil {
			return fmt.Errorf("clear sessions for %s: %w", ident.Name, err)
		}
		cleared++
	}

	code, bc, err := a.CreateBootstrapCode(24*time.Hour, "admin-reset")
	if err != nil {
		return fmt.Errorf("create bootstrap code: %w", err)
	}

	fmt.Printf(`Admin reset complete. Cleared %d admin password(s) and killed their sessions.

One-time bootstrap code (expires %s):

  %s

Visit /setup in the browser and enter this code with a new username + password.
`, cleared, bc.ExpiresAt.Local().Format(time.RFC1123), code)
	return nil
}

var adminResetYes bool

func init() {
	adminResetCmd.Flags().BoolVar(&adminResetYes, "yes", false, "Skip the confirmation prompt (unsafe)")
}

func runAdminList(cmd *cobra.Command, _ []string) error {
	a, closer, err := openAuthStore()
	if err != nil {
		return err
	}
	defer closer()

	idents, err := a.ListIdentities()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tMODE\tLAST USED\tREVOKED\tCREATED")
	for _, ident := range idents {
		lastUsed := "-"
		if ident.LastUsedAt != nil {
			lastUsed = humanDuration(time.Since(*ident.LastUsedAt)) + " ago"
		}
		revoked := "-"
		if ident.RevokedAt != nil {
			revoked = ident.RevokedAt.Local().Format("2006-01-02")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			ident.Name, ident.Kind, ident.AccessMode,
			lastUsed, revoked, ident.CreatedAt.Local().Format("2006-01-02"))
	}
	return tw.Flush()
}

// humanDuration returns "5m", "3h", "2d" — the short form people want in
// a terminal listing.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
