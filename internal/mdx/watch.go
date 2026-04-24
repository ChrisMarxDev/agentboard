package mdx

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

// StartWatcher watches the project for page file changes. Pages live under
// content/ in arbitrary nesting (e.g. content/skills/<slug>/SKILL.md), so
// every subdirectory is added to the watcher — fsnotify is non-recursive by
// default. New subdirectories created after startup are picked up on their
// Create event.
func (pm *PageManager) StartWatcher(onChange WatchCallback) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Watch project root (for index.md).
	projectDir := pm.project.Path
	if err := watcher.Add(projectDir); err != nil {
		log.Printf("Warning: could not watch %s: %v", projectDir, err)
	}

	// Watch content/ and every subdirectory below it.
	contentDir := pm.project.ContentDir()
	addContentTree(watcher, contentDir)

	go func() {
		defer watcher.Close()

		// Debounce timer
		var debounceTimer *time.Timer

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

				// Only care about .md files
				if !strings.HasSuffix(event.Name, ".md") {
					continue
				}

				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
					// Debounce: wait 500ms before processing
					if debounceTimer != nil {
						debounceTimer.Stop()
					}

					pagePath := event.Name
					debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
						pm.ScanPages()
						if onChange != nil {
							rel, _ := filepath.Rel(pm.project.Path, pagePath)
							onChange(rel)
						}
					})
				}

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
