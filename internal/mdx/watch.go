package mdx

import (
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchCallback is called when pages change.
type WatchCallback func(pagePath string)

// StartWatcher watches the project for page file changes.
func (pm *PageManager) StartWatcher(onChange WatchCallback) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Watch index.md directory
	projectDir := pm.project.Path
	if err := watcher.Add(projectDir); err != nil {
		log.Printf("Warning: could not watch %s: %v", projectDir, err)
	}

	// Watch pages/ directory
	pagesDir := pm.project.PagesDir()
	if err := watcher.Add(pagesDir); err != nil {
		log.Printf("Warning: could not watch %s: %v", pagesDir, err)
	}

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
