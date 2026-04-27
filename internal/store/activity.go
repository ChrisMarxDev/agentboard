package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"time"
)

// ActivityEntry is one line in the global activity log: one per
// successful write across all keys. Lighter than HistoryEntry — does
// not embed the previous value (history covers that), just enough for
// "who did what when" displays.
type ActivityEntry struct {
	TS       string `json:"ts"`
	Actor    string `json:"actor"`
	Op       string `json:"op"`
	Path     string `json:"path"` // <key> or <key>/<id>
	Version  string `json:"version,omitempty"`
	Shape    string `json:"shape,omitempty"`
}

// recordActivity appends to the global activity log under
// s.activityMu. Audit is auxiliary, never on the hot path of a write's
// correctness, so a coarse lock is fine — fewer moving parts than
// per-file state objects.
func (s *Store) recordActivity(entry ActivityEntry) {
	if s.activityLog == "" {
		return
	}
	if entry.TS == "" {
		entry.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	bytes, err := json.Marshal(entry)
	if err != nil {
		return
	}
	bytes = append(bytes, '\n')

	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	f, err := os.OpenFile(s.activityLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(bytes)
}

// ReadActivityOpts narrows a query against the activity log. All fields
// optional; default returns the most recent 100 entries.
type ReadActivityOpts struct {
	Limit      int    // default 100
	Since      string // RFC3339Nano; only return entries with ts > Since
	Until      string // RFC3339Nano
	Actor      string // exact match
	PathPrefix string // exact prefix match against `path`
}

// ReadActivity returns activity log entries matching opts, newest last.
// Activity log retention isn't enforced here (Phase 1 holds the file
// unbounded; rotation is a deferred concern); we just stream the file.
func (s *Store) ReadActivity(opts ReadActivityOpts) ([]ActivityEntry, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}

	f, err := os.Open(s.activityLog)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var matched []ActivityEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var e ActivityEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if opts.Since != "" && e.TS <= opts.Since {
			continue
		}
		if opts.Until != "" && e.TS > opts.Until {
			continue
		}
		if opts.Actor != "" && e.Actor != opts.Actor {
			continue
		}
		if opts.PathPrefix != "" && len(e.Path) < len(opts.PathPrefix) {
			continue
		}
		if opts.PathPrefix != "" && e.Path[:len(opts.PathPrefix)] != opts.PathPrefix {
			continue
		}
		matched = append(matched, e)
	}
	if len(matched) > opts.Limit {
		matched = matched[len(matched)-opts.Limit:]
	}
	return matched, nil
}
