package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is broadcast on every successful write so SSE/in-process
// subscribers can update without re-reading. Mirrors what data.DataEvent
// gave the SQLite-backed store, with version + shape added.
type Event struct {
	Key     string `json:"key"`
	ID      string `json:"id,omitempty"` // for collection items
	Op      string `json:"op"`           // SET | MERGE | UPSERT_BY_ID | ... | DELETE
	Shape   string `json:"shape"`
	Version string `json:"version,omitempty"` // empty on DELETE
}

// Config bundles the inputs to NewStore.
type Config struct {
	// ProjectRoot is the project directory. The store creates and owns
	// <root>/data/ and <root>/.agentboard/ subtrees.
	ProjectRoot string
}

// Store is the top-level handle. It exposes per-shape sub-handles
// (Singleton, Collection, Stream) that share its locks, version
// generator, and notification channel.
//
// Concurrency contract:
//   - Singleton + Collection writes hold the per-path mutex for the
//     duration of read-modify-write.
//   - Stream APPENDs use O_APPEND (lock-free) for writes <= PIPE_BUF.
//   - Reads never block writes (they open the file independently;
//     atomic-rename means they always see a coherent snapshot).
type Store struct {
	dataDir     string
	historyDir  string
	activityLog string

	versions *VersionGen
	locks    *pathLocks
	cat      *catalog

	streamsMu sync.Mutex
	streams   map[string]*streamState

	activityMu sync.Mutex

	subMu       sync.Mutex
	subscribers []chan Event
}

// NewStore opens (or initializes) a store rooted at cfg.ProjectRoot.
// The data and .agentboard directories are created on first use; this
// matches how the SQLite store auto-created its file.
func NewStore(cfg Config) (*Store, error) {
	if cfg.ProjectRoot == "" {
		return nil, fmt.Errorf("store: ProjectRoot required")
	}

	dataDir := filepath.Join(cfg.ProjectRoot, "data")
	abDir := filepath.Join(cfg.ProjectRoot, ".agentboard")
	historyDir := filepath.Join(abDir, "history")

	for _, d := range []string{dataDir, abDir, historyDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("store: mkdir %s: %w", d, err)
		}
	}

	cat, err := LoadCatalog(dataDir)
	if err != nil {
		return nil, fmt.Errorf("store: load catalog: %w", err)
	}

	// Seed the version generator from the highest catalog version, so
	// the next monotonic timestamp is strictly after every existing
	// envelope on disk. Avoids version-collision after a process
	// restart (rare but ugly when it happens).
	seed := time.Time{}
	for _, e := range cat.m {
		if e.Version == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, e.Version); err == nil && t.After(seed) {
			seed = t
		}
	}

	s := &Store{
		dataDir:     dataDir,
		historyDir:  historyDir,
		activityLog: filepath.Join(abDir, "activity.ndjson"),
		versions:    NewVersionGen(seed),
		locks:       newPathLocks(),
		cat:         cat,
	}
	return s, nil
}

// DataDir returns the directory holding singleton/collection/stream
// files. Exposed for tests and CLI tools (`agentboard backup`).
func (s *Store) DataDir() string { return s.dataDir }

// HistoryDir returns the per-key history root. Each key gets a file
// `<historyDir>/<key>.ndjson`.
func (s *Store) HistoryDir() string { return s.historyDir }

// ActivityLog returns the path to the global activity NDJSON.
func (s *Store) ActivityLog() string { return s.activityLog }

// Subscribe returns a channel that receives every Event. Buffered (64)
// so a slow consumer can lag briefly without blocking writes; events
// are dropped silently if the buffer fills (matches the existing
// SQLite store behavior — SSE clients reconcile via /api/index).
func (s *Store) Subscribe() <-chan Event {
	ch := make(chan Event, 64)
	s.subMu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.subMu.Unlock()
	return ch
}

// Close releases subscriber channels. Disk state needs no cleanup —
// every prior write was already fsynced.
func (s *Store) Close() error {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subscribers {
		close(ch)
	}
	s.subscribers = nil
	return nil
}

// notify broadcasts an Event to all subscribers. Non-blocking — drops
// to a slow consumer rather than stall a write.
func (s *Store) notify(evt Event) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}
