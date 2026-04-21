// Package files manages user-uploaded binary artifacts (images, PDFs, CSVs,
// etc.) served at /api/files/*. Files live in <project>/files/ on disk — see
// spec-files.md for the full design.
package files

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/christophermarx/agentboard/internal/project"
	"github.com/fsnotify/fsnotify"
)

// DefaultMaxFileSizeMB is the default per-file upload cap. Overridable via
// agentboard.yaml:max_file_size_mb.
const DefaultMaxFileSizeMB = 50

// HardCapMB is the absolute upper bound regardless of configuration. Prevents
// misconfiguration from turning the server into a free file host.
const HardCapMB = 500

// MaxNameLen caps the length of a single name (total path, including separators).
const MaxNameLen = 256

// Errors returned by the Manager. Handlers translate these to HTTP codes.
var (
	ErrInvalidName = errors.New("invalid file name")
	ErrTooLarge    = errors.New("file too large")
	ErrNotFound    = errors.New("file not found")
	ErrIsDirectory = errors.New("path is a directory, not a file")
)

// Info describes a single file on disk.
type Info struct {
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	ModifiedAt  time.Time `json:"modified_at"`
	ETag        string    `json:"etag"`
	URL         string    `json:"url"`
}

// Manager owns the files/ directory and serves as the single choke point for
// all file writes and reads. All validation, MIME detection, ETag computation,
// and SSE broadcasting go through this type.
type Manager struct {
	project   *project.Project
	maxSizeMB int
	mu        sync.RWMutex
}

// NewManager creates a Manager rooted at the project's files/ directory. The
// directory is created lazily on first write; callers don't need to precreate.
func NewManager(proj *project.Project, maxSizeMB int) *Manager {
	if maxSizeMB <= 0 {
		maxSizeMB = DefaultMaxFileSizeMB
	}
	if maxSizeMB > HardCapMB {
		maxSizeMB = HardCapMB
	}
	return &Manager{
		project:   proj,
		maxSizeMB: maxSizeMB,
	}
}

// MaxSizeBytes returns the configured per-file byte cap.
func (m *Manager) MaxSizeBytes() int64 {
	return int64(m.maxSizeMB) * 1024 * 1024
}

// FilesDir returns the filesystem path that backs /api/files/*.
func (m *Manager) FilesDir() string {
	return filepath.Join(m.project.Path, "files")
}

// ValidateName enforces the naming rules documented in spec-files.md §5:
//   - One or more `/`-separated segments, no leading slash
//   - Each segment matches [A-Za-z0-9][A-Za-z0-9._ -]{0,127}
//   - Never contains "..", backslash, or null bytes
//   - Total length <= MaxNameLen
//
// Returns the cleaned name or ErrInvalidName.
func ValidateName(name string) (string, error) {
	if name == "" {
		return "", ErrInvalidName
	}
	if len(name) > MaxNameLen {
		return "", ErrInvalidName
	}
	if strings.ContainsAny(name, "\x00\\") {
		return "", ErrInvalidName
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, ".") {
		return "", ErrInvalidName
	}

	for seg := range strings.SplitSeq(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", ErrInvalidName
		}
		if !validSegment(seg) {
			return "", ErrInvalidName
		}
	}
	return name, nil
}

func validSegment(seg string) bool {
	if len(seg) == 0 || len(seg) > 128 {
		return false
	}
	// First char must be alphanumeric to block dotfiles like `.env`.
	if !isAlphanumeric(seg[0]) {
		return false
	}
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if !isAlphanumeric(c) && c != '.' && c != '_' && c != '-' && c != ' ' {
			return false
		}
	}
	return true
}

func isAlphanumeric(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// Write persists the given bytes at `name`, overwriting if the file already
// exists. Returns Info describing the written file.
func (m *Manager) Write(name string, body io.Reader) (*Info, error) {
	cleaned, err := ValidateName(name)
	if err != nil {
		return nil, err
	}

	// Read up to the size cap + 1 byte so we can detect oversize.
	cap := m.MaxSizeBytes()
	limited := io.LimitReader(body, cap+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > cap {
		return nil, ErrTooLarge
	}

	destPath := filepath.Join(m.FilesDir(), filepath.FromSlash(cleaned))
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return nil, fmt.Errorf("ensure dir: %w", err)
	}

	// Write via a temp file + rename so we never expose a half-written file.
	m.mu.Lock()
	defer m.mu.Unlock()
	tmp, err := os.CreateTemp(filepath.Dir(destPath), ".upload-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("rename: %w", err)
	}

	stat, err := os.Stat(destPath)
	if err != nil {
		return nil, err
	}
	return m.infoFromStat(cleaned, stat, data), nil
}

// Stat returns metadata about a single file (without reading its contents).
func (m *Manager) Stat(name string) (*Info, error) {
	cleaned, err := ValidateName(name)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(m.FilesDir(), filepath.FromSlash(cleaned))
	stat, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if stat.IsDir() {
		return nil, ErrIsDirectory
	}
	return m.infoFromStat(cleaned, stat, nil), nil
}

// Open returns the file contents as a reader plus metadata. Caller must Close.
func (m *Manager) Open(name string) (*os.File, *Info, error) {
	cleaned, err := ValidateName(name)
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(m.FilesDir(), filepath.FromSlash(cleaned))
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if stat.IsDir() {
		f.Close()
		return nil, nil, ErrIsDirectory
	}
	// Sniff the first 512 bytes for MIME.
	var sniff []byte
	if stat.Size() > 0 {
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		sniff = buf[:n]
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return nil, nil, err
		}
	}
	return f, m.infoFromStat(cleaned, stat, sniff), nil
}

// Delete removes a file. Returns ErrNotFound if it didn't exist.
func (m *Manager) Delete(name string) error {
	cleaned, err := ValidateName(name)
	if err != nil {
		return err
	}
	path := filepath.Join(m.FilesDir(), filepath.FromSlash(cleaned))
	m.mu.Lock()
	defer m.mu.Unlock()
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}

// List returns every file under files/ (recursive), sorted by name.
func (m *Manager) List() ([]Info, error) {
	root := m.FilesDir()
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrIsDirectory
	}

	var out []Info
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Skip temp upload files.
		if strings.HasPrefix(d.Name(), ".upload-") && strings.HasSuffix(d.Name(), ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		name := filepath.ToSlash(rel)
		stat, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, *m.infoFromStat(name, stat, nil))
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Stable sort.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	if out == nil {
		out = []Info{}
	}
	return out, nil
}

// infoFromStat builds an Info from a stat + optional sniffed bytes. Caller
// holds the lock if needed; this method only computes.
func (m *Manager) infoFromStat(name string, stat fs.FileInfo, sniff []byte) *Info {
	ct := detectMime(name, sniff)
	etag := computeETag(name, stat)
	return &Info{
		Name:        name,
		Size:        stat.Size(),
		ContentType: ct,
		ModifiedAt:  stat.ModTime().UTC(),
		ETag:        etag,
		URL:         "/api/files/" + name,
	}
}

// detectMime picks the best Content-Type for a file. Sniff wins over extension
// unless sniffing returned a generic octet-stream guess and the extension has a
// better hint.
func detectMime(name string, sniff []byte) string {
	ext := filepath.Ext(name)
	byExt := mime.TypeByExtension(ext)
	var bySniff string
	if len(sniff) > 0 {
		bySniff = http.DetectContentType(sniff)
	}

	// If sniffing returned something specific, prefer it.
	if bySniff != "" && bySniff != "application/octet-stream" && !strings.HasPrefix(bySniff, "text/plain") {
		return bySniff
	}
	// Otherwise fall back to extension.
	if byExt != "" {
		return byExt
	}
	if bySniff != "" {
		return bySniff
	}
	return "application/octet-stream"
}

// computeETag returns a cheap fingerprint that changes when the file's bytes do.
// sha1(name|size|mtime_unix_nano) gives us strong uniqueness without hashing
// the whole body. Wrapped in quotes per RFC 7232.
func computeETag(name string, stat fs.FileInfo) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s|%d|%d", name, stat.Size(), stat.ModTime().UnixNano())
	return `"` + hex.EncodeToString(h.Sum(nil))[:16] + `"`
}

// IsInlineDisposition returns true when the server should send
// Content-Disposition: inline for this MIME type. Ambiguous or binary types
// default to attachment so the browser doesn't try to render them.
func IsInlineDisposition(ct string) bool {
	if ct == "" || ct == "application/octet-stream" {
		return false
	}
	switch {
	case strings.HasPrefix(ct, "image/"):
		return true
	case strings.HasPrefix(ct, "text/plain"):
		return true
	case strings.HasPrefix(ct, "text/markdown"):
		return true
	case ct == "application/pdf":
		return true
	case ct == "application/json":
		return true
	}
	return false
}

// StartWatcher wires fsnotify to the files/ directory. On change/create/delete
// of any file that isn't a temp upload, onChange is invoked with the relative
// name and a "deleted" flag. Safe to call once during server startup.
func (m *Manager) StartWatcher(onChange func(name string, deleted bool)) error {
	root := m.FilesDir()
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Watch the root. Subdirectories would need recursive add; start flat for
	// simplicity and revisit if nested paths see heavy use.
	if err := watcher.Add(root); err != nil {
		log.Printf("files: could not watch %s: %v", root, err)
		return nil
	}

	go func() {
		defer watcher.Close()
		debounce := make(map[string]*time.Timer)
		var mu sync.Mutex

		emit := func(name string, deleted bool) {
			if onChange != nil {
				onChange(name, deleted)
			}
		}

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				base := filepath.Base(event.Name)
				if strings.HasPrefix(base, ".upload-") && strings.HasSuffix(base, ".tmp") {
					continue
				}
				rel, err := filepath.Rel(root, event.Name)
				if err != nil {
					continue
				}
				name := filepath.ToSlash(rel)
				deleted := event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)

				mu.Lock()
				if t, ok := debounce[name]; ok {
					t.Stop()
				}
				debounce[name] = time.AfterFunc(200*time.Millisecond, func() {
					mu.Lock()
					delete(debounce, name)
					mu.Unlock()
					emit(name, deleted)
				})
				mu.Unlock()
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("files watcher error: %v", err)
			}
		}
	}()

	return nil
}
