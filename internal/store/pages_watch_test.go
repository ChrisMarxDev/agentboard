package store

// Watcher reentry test. Cut 5 promises that direct-disk writes anywhere
// under the project tree propagate through the same hooks an API write
// would — the page rescan happens automatically, no rule about "don't
// write to content/* directly". This file exercises the basic shape:
// drop a `.md` on disk, observe the watcher fire OnPage with the
// relative path.

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/christophermarx/agentboard/internal/project"
)

func TestPagesWatch_DirectDiskWriteFiresOnPage(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "content"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, ".agentboard"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "index.md"), []byte("# Test\n"), 0o644)

	proj, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	pm := NewPageManager(proj)
	var (
		mu      sync.Mutex
		fired   []string
		dataHit []string
	)
	if err := pm.StartWatcherOpts(WatchOptions{
		OnPage: func(p string) { mu.Lock(); fired = append(fired, p); mu.Unlock() },
		OnData: func(k string) { mu.Lock(); dataHit = append(dataHit, k); mu.Unlock() },
	}); err != nil {
		t.Fatalf("StartWatcherOpts: %v", err)
	}
	// Give fsnotify a moment to register subscriptions.
	time.Sleep(100 * time.Millisecond)

	// Direct-disk write under content/.
	pagePath := filepath.Join(dir, "content", "direct-disk.md")
	if err := os.WriteFile(pagePath, []byte("# Direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Watcher debounces 500ms; wait that out plus a margin.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(fired)
		mu.Unlock()
		if count > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	got := append([]string(nil), fired...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatalf("watcher did not fire OnPage for direct disk write")
	}
	// Confirm the page manager rescanned and now knows the page.
	if pm.GetPage("direct-disk") == nil {
		t.Errorf("PageManager did not pick up the direct-disk write — rescan never ran")
	}
}
