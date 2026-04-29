// Package backup ships the project-folder tarball implementation
// behind the `agentboard backup` and `agentboard restore` CLI commands
// (docs/archive/spec-file-storage.md §20). The ICP doesn't install rclone or
// aws-cli; we ship the tool.
//
// Phase 1: local tarball (.tar.gz). Phase 2 (deferred): S3 destination
// via the AWS SDK; the CLI flag --to s3://… is reserved.
//
// Consistency model: writes via the API are all atomic-rename, so any
// individual file is internally coherent. A backup that runs while the
// server is live can catch a project mid-multi-key-update; the
// resulting tarball is still restorable (every file is some valid
// version of itself), but observers may see a momentary "this v2 key
// updated, this related v2 key did not" inconsistency. The CLI prints
// a warning when it detects an active server. For absolute consistency,
// run with the server stopped — same caveat as `sqlite3 .backup`.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// excludedPaths are project-relative paths we never include in a
// tarball. SQLite WAL/SHM files are mid-flight state — restoring them
// without their main DB at the right moment would corrupt the next
// boot. The data.sqlite itself IS included; SQLite's checkpoint-on-
// close behavior means a clean shutdown leaves the WAL empty, and a
// hot-backup of just the file is *usually* fine, with the same risk
// `sqlite3 .backup` is designed to avoid. We document and accept it.
var excludedSuffixes = []string{
	"-wal",
	"-shm",
	".tmp", // partial atomic-rename targets
}

// excludedNames are exact filename matches stripped from every backup.
// Add to this list when a new transient file pattern shows up.
var excludedNames = map[string]bool{
	".DS_Store":    true,
	"Thumbs.db":    true,
	".tmp-temp":    true,
	"first-admin-invite.url": true, // contains a one-shot bootstrap secret
}

// Backup writes a gzip'd tar of projectRoot to outPath. Returns the
// number of files included and the total uncompressed bytes.
func Backup(projectRoot, outPath string) (int, int64, error) {
	if projectRoot == "" {
		return 0, 0, errors.New("backup: projectRoot required")
	}
	if outPath == "" {
		return 0, 0, errors.New("backup: --to required")
	}
	info, err := os.Stat(projectRoot)
	if err != nil {
		return 0, 0, fmt.Errorf("backup: stat project root: %w", err)
	}
	if !info.IsDir() {
		return 0, 0, fmt.Errorf("backup: project root is not a directory: %s", projectRoot)
	}

	out, err := os.Create(outPath)
	if err != nil {
		return 0, 0, fmt.Errorf("backup: create %s: %w", outPath, err)
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	var fileCount int
	var totalBytes int64

	walkErr := filepath.Walk(projectRoot, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldExclude(rel, fi) {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		// Use forward slashes inside the archive — POSIX-portable and
		// matches every other tooling expectation.
		hdr.Name = filepath.ToSlash(rel)

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if !fi.Mode().IsRegular() {
			// Symlinks, sockets, devices etc. — header was written
			// (with its mode bits); no body to copy.
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		n, err := io.Copy(tw, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		fileCount++
		totalBytes += n
		return nil
	})

	if walkErr != nil {
		return fileCount, totalBytes, fmt.Errorf("backup: walk: %w", walkErr)
	}
	return fileCount, totalBytes, nil
}

// Restore unpacks a tarball into targetDir. Refuses to write into a
// non-empty directory unless force=true — accidentally untarring over
// a live project is exactly the kind of foot-gun a CLI should not
// help with.
func Restore(archivePath, targetDir string, force bool) (int, error) {
	if archivePath == "" || targetDir == "" {
		return 0, errors.New("restore: --from and target dir required")
	}

	if !force {
		if entries, err := os.ReadDir(targetDir); err == nil && len(entries) > 0 {
			return 0, fmt.Errorf("restore: %s is not empty (pass --force to override)", targetDir)
		}
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return 0, fmt.Errorf("restore: mkdir target: %w", err)
	}

	in, err := os.Open(archivePath)
	if err != nil {
		return 0, fmt.Errorf("restore: open %s: %w", archivePath, err)
	}
	defer in.Close()

	gz, err := gzip.NewReader(in)
	if err != nil {
		return 0, fmt.Errorf("restore: gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var count int
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, fmt.Errorf("restore: read header: %w", err)
		}
		// Path-traversal guard: the spec forbids `..` and absolute paths
		// in keys; the tar format makes no such promise. Reject names
		// that would escape the target directory.
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || strings.Contains(clean, "../") || strings.HasPrefix(clean, "/") {
			return count, fmt.Errorf("restore: refusing path traversal entry %q", hdr.Name)
		}
		dst := filepath.Join(targetDir, clean)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, os.FileMode(hdr.Mode)); err != nil {
				return count, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return count, err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return count, err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return count, err
			}
			if err := f.Close(); err != nil {
				return count, err
			}
			count++
		default:
			// Symlinks etc. are skipped — the project shouldn't contain
			// any. If a backup includes one, document and skip rather
			// than silently restore a security surprise.
		}
	}
	return count, nil
}

func shouldExclude(rel string, _ os.FileInfo) bool {
	base := filepath.Base(rel)
	if excludedNames[base] {
		return true
	}
	for _, suf := range excludedSuffixes {
		if strings.HasSuffix(rel, suf) {
			return true
		}
	}
	return false
}
