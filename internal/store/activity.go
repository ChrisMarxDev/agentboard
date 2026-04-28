package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Activity-log retention. Logrotate-style: when the active file
// crosses the byte cap we rename it to .1, slide existing segments
// one slot older, drop anything past the segment limit. Same shape as
// stream rotation (§17 of the spec); same numbers tuned for the same
// reason — 500 MB max retained per project, well below any disk
// pressure threshold at our scale.
const (
	activityRotateBytes  = 100 * 1024 * 1024
	activitySegmentLimit = 5
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

	// Rotate before append if the active file would cross the cap.
	// Stat is cheap; doing it on every write keeps rotation precisely
	// aligned to the byte boundary instead of "some time after".
	if fi, err := os.Stat(s.activityLog); err == nil && fi.Size()+int64(len(bytes)) > activityRotateBytes {
		_ = rotateActivityLog(s.activityLog)
	}

	f, err := os.OpenFile(s.activityLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(bytes)
}

// rotateActivityLog renames active → .1, slides others down (1→2,
// 2→3 …), drops anything past activitySegmentLimit. Mirrors the
// stream rotation in stream.go; same atomic-rename guarantees.
func rotateActivityLog(active string) error {
	// Drop the oldest if we're at the cap.
	if err := removePath(rotatedActivityPath(active, activitySegmentLimit)); err != nil {
		return err
	}
	// Slide existing segments down: 4→5, 3→4, …, 1→2.
	for n := activitySegmentLimit - 1; n >= 1; n-- {
		from := rotatedActivityPath(active, n)
		if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Rename(from, rotatedActivityPath(active, n+1)); err != nil {
			return err
		}
	}
	// Active → .1.
	return os.Rename(active, rotatedActivityPath(active, 1))
}

// rotatedActivityPath returns the segment-N path. activity.ndjson →
// activity.1.ndjson, activity.2.ndjson, etc. Pure string fiddling so
// it works regardless of the parent directory layout.
func rotatedActivityPath(active string, n int) string {
	// Strip ".ndjson" suffix if present, append .N.ndjson.
	base := active
	if len(base) > 7 && base[len(base)-7:] == ".ndjson" {
		base = base[:len(base)-7]
	}
	return fmt.Sprintf("%s.%d.ndjson", base, n)
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
// Walks rotated segments oldest-first then the active file, so a query
// spanning a rotation boundary still produces a contiguous timeline.
// At default limits (100) the read is bounded; longer scans pay a
// linear cost that's acceptable for an audit endpoint.
func (s *Store) ReadActivity(opts ReadActivityOpts) ([]ActivityEntry, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}

	var matched []ActivityEntry
	for n := activitySegmentLimit; n >= 1; n-- {
		matched = appendActivityFromFile(matched, rotatedActivityPath(s.activityLog, n), opts)
	}
	matched = appendActivityFromFile(matched, s.activityLog, opts)

	if len(matched) > opts.Limit {
		matched = matched[len(matched)-opts.Limit:]
	}
	return matched, nil
}

// appendActivityFromFile reads one segment and appends matching
// entries to acc. Missing files are tolerated silently (rotation
// produces gaps when fewer than activitySegmentLimit segments exist).
func appendActivityFromFile(acc []ActivityEntry, path string, opts ReadActivityOpts) []ActivityEntry {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return acc
		}
		return acc
	}
	defer f.Close()
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
		acc = append(acc, e)
	}
	return acc
}
