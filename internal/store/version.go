package store

import (
	"sync"
	"time"
)

// VersionGen produces strictly-monotonic version strings for the data
// store. The format is RFC 3339Nano (e.g. "2026-04-27T14:32:11.123456789Z");
// human-readable, sortable lexically, and exposed verbatim as the HTTP
// ETag header.
//
// "Strictly monotonic" matters because two writes in the same nanosecond
// must still produce distinct versions — otherwise CAS comparisons can
// incorrectly succeed. The generator clamps each call to
// max(time.Now(), last + 1ns) so the sequence never repeats and never
// goes backward, even under NTP corrections.
type VersionGen struct {
	mu   sync.Mutex
	last time.Time
}

// NewVersionGen returns a fresh generator. Pass an initial seed (typically
// the highest version observed across all files at startup) to ensure new
// versions sort after any historical ones; pass time.Time{} for a clean
// boot.
func NewVersionGen(seed time.Time) *VersionGen {
	return &VersionGen{last: seed}
}

// Next returns the next monotonic version as an RFC3339Nano string.
func (g *VersionGen) Next() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now().UTC()
	if !now.After(g.last) {
		now = g.last.Add(time.Nanosecond)
	}
	g.last = now
	return now.UTC().Format(time.RFC3339Nano)
}

// Observe pushes the high-water mark forward when an external version
// (e.g. one parsed off disk on startup) is later than what the generator
// has seen. Idempotent and safe to call repeatedly.
func (g *VersionGen) Observe(v string) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if t.After(g.last) {
		g.last = t
	}
}
