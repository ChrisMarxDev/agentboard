package store

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchCallback is called when pages change.
type WatchCallback func(pagePath string)

// WatchOptions wires the unified file watcher to the right downstream
// hooks. Cut 5 collapsed the previous two watchers (one for pages, one
// for data) into a single fsnotify watcher rooted at the project tree
// — `OnPage` fires for `.md` writes under the page tree (project root +
// `content/**`), `OnData` fires for changes under the data tree
// (`data/**`). A nil callback skips the hook for that tree.
//
// Direct-disk writes propagate through this watcher just like API
// writes: the post-change hooks bound by `OnPage` (RefStore /
// SearchStore re-indexing in cli/serve.go) run regardless of who put
// the bytes on disk. This is the fix to the gotcha called out in
// docs/archive/REWRITE-cuts-1-4.md ("don't write content/* directly")
// — agents and humans dropping files into the tree are now safe.
type WatchOptions struct {
	OnPage WatchCallback
	OnData WatchCallback
}

// StartWatcher watches the project for content + data file changes.
// One fsnotify watcher covers both subtrees so a direct-disk drop
// anywhere in the project tree fans out to the correct subscriber.
//
// Pages live under content/ in arbitrary nesting (e.g.
// content/skills/<slug>/SKILL.md), so every subdirectory below
// `content/` is added to the watcher — fsnotify is non-recursive by
// default. Data lives under data/ with the same nesting rule. New
// subdirectories created after startup are picked up on their Create
// event.
func (pm *PageManager) StartWatcher(onChange WatchCallback) error {
	return pm.StartWatcherOpts(WatchOptions{OnPage: onChange})
}

// StartWatcherOpts is StartWatcher with a richer subscriber API.
// `OnPage` fires for any `.md` change under the page tree (project
// root + `content/**`). `OnData` fires for `.md` / `.ndjson` changes
// under the data tree (`data/**`). Nil callbacks are skipped.
func (pm *PageManager) StartWatcherOpts(opts WatchOptions) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	projectDir := pm.project.Path
	contentDir := pm.project.ContentDir()
	dataDir := filepath.Join(projectDir, "data")

	// Watch project root (for index.md).
	if err := watcher.Add(projectDir); err != nil {
		log.Printf("Warning: could not watch %s: %v", projectDir, err)
	}
	// Watch content/ and every subdirectory below it.
	addContentTree(watcher, contentDir)
	// Watch data/ if the store's tree exists. Best-effort — a project
	// that's only used as a docs site won't have data/ yet.
	if _, err := os.Stat(dataDir); err == nil {
		addContentTree(watcher, dataDir)
	}

	go func() {
		defer watcher.Close()

		// Per-path debounce: a single editor save can emit multiple
		// fsnotify events (rename + create + write). Coalescing per
		// path means we only fire the callback once per writeable
		// transition, but parallel writes to different files don't
		// stomp on each other's debounce timers.
		debounces := map[string]*time.Timer{}

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// New directories must be added to the watch set so their
				// contents generate events too.
				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = watcher.Add(event.Name)
					}
				}

				// Decide which tree this path belongs to BEFORE
				// filtering — `.ndjson` is data-tree-only, `.md` is in
				// either.
				inData := dataDir != "" && strings.HasPrefix(event.Name, dataDir+string(os.PathSeparator))
				inPage := !inData
				isMD := strings.HasSuffix(event.Name, ".md")
				isStream := strings.HasSuffix(event.Name, ".ndjson")
				if !isMD && !isStream {
					continue
				}
				if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Remove) {
					continue
				}

				path := event.Name
				if t := debounces[path]; t != nil {
					t.Stop()
				}
				debounces[path] = time.AfterFunc(500*time.Millisecond, func() {
					if inPage {
						pm.ScanPages()
						if opts.OnPage != nil {
							rel, _ := filepath.Rel(pm.project.Path, path)
							opts.OnPage(rel)
						}
						return
					}
					if inData && opts.OnData != nil {
						// Pass the relative key (without dataDir prefix
						// + extension) so the callback can look up the
						// catalog entry directly.
						rel, _ := filepath.Rel(dataDir, path)
						rel = strings.TrimSuffix(rel, ".md")
						rel = strings.TrimSuffix(rel, ".ndjson")
						opts.OnData(rel)
					}
				})

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Page watcher error: %v", err)
			}
		}
	}()

	return nil
}

// addContentTree registers root and every subdirectory below it with the
// watcher. Walk errors are logged and skipped; a partial watch set is better
// than no watcher at all.
func addContentTree(watcher *fsnotify.Watcher, root string) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Warning: could not walk %s: %v", path, err)
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if err := watcher.Add(path); err != nil {
			log.Printf("Warning: could not watch %s: %v", path, err)
		}
		return nil
	})
}
