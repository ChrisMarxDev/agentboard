// Package errors stores render-time errors reported by frontend components.
// Agents poll this buffer to know if a page they just wrote has broken bindings
// — "did my Mermaid source parse?", "does the Image URL resolve?", etc.
//
// Design rationale: the Go binary can't natively parse Mermaid / MDX / Prism
// languages, so validation happens lazily in the browser. Components catch
// their own errors and POST them here. Agents then have a single place to
// check: "is anything on this page broken?"
package errors

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxEntries caps the buffer. Oldest distinct errors are evicted once exceeded.
const MaxEntries = 100

// DupWindow is the minimum interval between accepted re-fires of the SAME
// error. Beacons that arrive faster than this bump LastSeen but don't increment
// Count. Prevents a single bad render loop from flooding the buffer.
const DupWindow = 1 * time.Second

// Entry is one recorded error. Two beacons that agree on
// (Component, Source, Page, first line of Error) are merged into one Entry.
type Entry struct {
	Key       string    `json:"key"`              // dedupe fingerprint, stable across fires
	Component string    `json:"component"`        // e.g. "Mermaid", "Markdown", "Image"
	Source    string    `json:"source,omitempty"` // data key OR direct URL OR "inline"
	Page      string    `json:"page,omitempty"`   // page path the component was rendered on
	Error     string    `json:"error"`            // full error text
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int       `json:"count"`
}

// Input is the shape accepted by Buffer.Record (and the POST /api/errors body).
// All fields are optional except Error; the buffer tolerates missing metadata
// so a failing component never becomes a second failure.
type Input struct {
	Component string `json:"component"`
	Source    string `json:"source"`
	Page      string `json:"page"`
	Error     string `json:"error"`
}

// Buffer holds the recent-errors ring. Safe for concurrent Record/List/Clear.
type Buffer struct {
	mu      sync.Mutex
	entries map[string]*Entry // key -> entry
	now     func() time.Time  // injectable for tests
}

// NewBuffer returns an empty buffer using time.Now.
func NewBuffer() *Buffer {
	return &Buffer{
		entries: make(map[string]*Entry),
		now:     time.Now,
	}
}

// fingerprint derives a dedupe key from an Input. We hash the first line of
// Error only — subsequent lines (stack traces, source snippets) can vary run-
// to-run but the first line is usually the stable parser message.
func fingerprint(in Input) string {
	errLine := strings.SplitN(in.Error, "\n", 2)[0]
	errLine = strings.TrimSpace(errLine)
	h := sha1.New()
	fmt.Fprintf(h, "%s|%s|%s|%s", in.Component, in.Source, in.Page, errLine)
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// Record adds or updates an entry. Returns the entry after mutation.
// Empty Error is treated as a no-op (returns nil).
func (b *Buffer) Record(in Input) *Entry {
	if strings.TrimSpace(in.Error) == "" {
		return nil
	}
	now := b.now()
	key := fingerprint(in)

	b.mu.Lock()
	defer b.mu.Unlock()

	if existing, ok := b.entries[key]; ok {
		// Within the dup window: refresh LastSeen but don't bump count.
		if now.Sub(existing.LastSeen) < DupWindow {
			existing.LastSeen = now
			return existing
		}
		existing.LastSeen = now
		existing.Count++
		// Keep the freshest error text (in case it shifts slightly between fires).
		existing.Error = in.Error
		return existing
	}

	entry := &Entry{
		Key:       key,
		Component: in.Component,
		Source:    in.Source,
		Page:      in.Page,
		Error:     in.Error,
		FirstSeen: now,
		LastSeen:  now,
		Count:     1,
	}
	b.entries[key] = entry
	b.evictIfFull()
	return entry
}

// evictIfFull trims the oldest entries (by LastSeen) until len <= MaxEntries.
// Caller holds the lock.
func (b *Buffer) evictIfFull() {
	if len(b.entries) <= MaxEntries {
		return
	}
	type kt struct {
		key string
		t   time.Time
	}
	all := make([]kt, 0, len(b.entries))
	for k, e := range b.entries {
		all = append(all, kt{k, e.LastSeen})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
	evictCount := len(all) - MaxEntries
	for i := 0; i < evictCount; i++ {
		delete(b.entries, all[i].key)
	}
}

// List returns all entries sorted by LastSeen descending (newest first).
func (b *Buffer) List() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]Entry, 0, len(b.entries))
	for _, e := range b.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// Clear drops all entries. Returns the count removed.
func (b *Buffer) Clear() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(b.entries)
	b.entries = make(map[string]*Entry)
	return n
}

// ClearByKey removes a single entry. Useful when an agent fixes the underlying
// source and wants to mark it resolved without waiting for the dup window to
// age out naturally. Returns true if the entry existed.
func (b *Buffer) ClearByKey(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.entries[key]; ok {
		delete(b.entries, key)
		return true
	}
	return false
}

// Len returns the current number of distinct errors held.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}
