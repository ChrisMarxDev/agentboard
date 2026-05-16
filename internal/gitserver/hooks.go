package gitserver

import (
	"fmt"
	"os"
	"path/filepath"
)

// InstallPreReceiveHook writes a `hooks/pre-receive` script into the
// bare repo for workspaceID. The script execs the running agentboard
// binary's hidden `internal-pre-receive` subcommand, passing the
// workspace id and the pushing-user-resolved env vars set by
// gitserver.Server's CGI env (AGENTBOARD_USER, AGENTBOARD_PROJECT_PATH).
//
// The hook is the §5.2 permission gate for the smart-HTTP push path —
// other write surfaces (handlers_edit, handlers_restore, MCP propose)
// already gate via permissions.Rules.Allow before they hit gitserver.
// This closes the last enforcement gap.
//
// Idempotent: re-writes the script every call so binary-path changes
// flow through on the next push.
func (s *Store) InstallPreReceiveHook(workspaceID string) error {
	abs, err := os.Executable()
	if err != nil {
		return fmt.Errorf("hooks: resolve agentboard binary: %w", err)
	}
	bare := s.BarePath(workspaceID)
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
		return fmt.Errorf("hooks: bare repo missing for %s: %w", workspaceID, err)
	}
	hooksDir := filepath.Join(bare, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("hooks: mkdir: %w", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
# Installed by agentboard. Gates pushes through internal-pre-receive,
# which loads .agentboard/permissions.yaml + the user's groups and
# evaluates each changed path. Deny → non-zero exit → git rejects the
# push and surfaces the stderr message to the client.
exec %q internal-pre-receive \
    --workspace=%q \
    --bare="$PWD" \
    --user="${AGENTBOARD_USER:-anonymous}" \
    --project="${AGENTBOARD_PROJECT_PATH:-}"
`, abs, workspaceID)
	dst := filepath.Join(hooksDir, "pre-receive")
	if err := os.WriteFile(dst, []byte(script), 0o755); err != nil {
		return fmt.Errorf("hooks: write pre-receive: %w", err)
	}
	return nil
}

// InstallAllHooks retrofits the pre-receive hook onto every existing
// bare repo. Run at server boot to bring legacy workspaces (created
// before §5.2 landed) under the same gate as new ones.
func (s *Store) InstallAllHooks() error {
	reposRoot := filepath.Join(s.root, "repos")
	entries, err := os.ReadDir(reposRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("hooks: scan repos: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Bares are named "<workspace>.git" on disk per BarePath.
		name := e.Name()
		if filepath.Ext(name) != ".git" {
			continue
		}
		ws := name[:len(name)-len(".git")]
		if err := s.InstallPreReceiveHook(ws); err != nil {
			return fmt.Errorf("hooks: install for %s: %w", ws, err)
		}
	}
	return nil
}
