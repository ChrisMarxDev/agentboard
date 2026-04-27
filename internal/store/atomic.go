package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to path via tmp + fsync + rename.
// On POSIX this guarantees the destination is either fully old or fully
// new — never half-written — which is the durability contract the rest
// of the store relies on.
//
// The temp file lives in the same directory as the destination so the
// rename stays on one filesystem (cross-mount renames are not atomic).
// Permissions are 0644 for files, 0755 for directories — readable by
// the operator, writable only by the server process.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("store: mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("store: create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// On any failure path, remove the temp so we don't leak.
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("store: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("store: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("store: close temp: %w", err)
	}

	if err := os.Chmod(tmpName, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("store: chmod temp: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("store: rename temp -> %s: %w", path, err)
	}
	return nil
}

// removePath deletes a file or empty directory; returns nil if it was
// already gone (idempotent — DELETE semantics demand this).
func removePath(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removeDirRecursive deletes a directory and everything under it.
// Idempotent.
func removeDirRecursive(path string) error {
	err := os.RemoveAll(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
