package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HistoryEntry is one snapshot of a value before it was overwritten,
// plus the actor and timestamp of the write that displaced it.
type HistoryEntry struct {
	TS        string          `json:"ts"`
	Actor     string          `json:"actor"`
	Op        string          `json:"op"`
	Version   string          `json:"version"`
	PrevValue json.RawMessage `json:"prev_value,omitempty"`
	Key       string          `json:"key"`
	ID        string          `json:"id,omitempty"`
}

// historyRetention bounds the per-key file. 100 entries keeps the file
// small enough to scan linearly (the typical "show history" UI shows
// the most recent 10–20) while still giving "revert to N versions ago"
// real headroom.
const historyRetention = 100

// recordHistory appends a HistoryEntry to <historyDir>/<historyFile>.
// Failures are logged-and-tolerated (see callers): durability of the
// primary value is more important than durability of the audit trail.
//
// historyFile is the filename — for singletons it's "<key>.ndjson", for
// collection items it's "<key>__<id>.ndjson" (double underscore avoids
// collision with keys that contain underscores; "/" is forbidden in
// keys + IDs so we can't use it).
func (s *Store) recordHistory(key, id, op, actor string, prev *Envelope, newVersion string) {
	if s.historyDir == "" {
		return
	}
	entry := HistoryEntry{
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Actor:   actor,
		Op:      op,
		Version: newVersion,
		Key:     key,
		ID:      id,
	}
	if prev != nil {
		entry.PrevValue = prev.Value
	}
	bytes, err := json.Marshal(entry)
	if err != nil {
		return
	}
	bytes = append(bytes, '\n')

	path := filepath.Join(s.historyDir, historyFilename(key, id))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(bytes)

	// Trim the file in-place to the retention window. Done by reading,
	// keeping the last N, rewriting via atomic rename. Cheap because
	// the file is bounded.
	go s.trimHistoryAsync(path)
}

func (s *Store) trimHistoryAsync(path string) {
	entries, err := readHistoryFile(path)
	if err != nil || len(entries) <= historyRetention {
		return
	}
	keep := entries[len(entries)-historyRetention:]
	var sb strings.Builder
	for _, e := range keep {
		b, err := json.Marshal(e)
		if err != nil {
			continue
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	_ = writeFileAtomic(path, []byte(sb.String()))
}

// ReadHistory returns the on-disk history for a key (or one collection
// item), oldest first. Limit caps to the most recent N entries; 0 returns
// all retained.
func (s *Store) ReadHistory(key, id string, limit int) ([]HistoryEntry, error) {
	path := filepath.Join(s.historyDir, historyFilename(key, id))
	entries, err := readHistoryFile(path)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries, nil
}

func readHistoryFile(path string) ([]HistoryEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []HistoryEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var e HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func historyFilename(key, id string) string {
	if id == "" {
		return key + ".ndjson"
	}
	return fmt.Sprintf("%s__%s.ndjson", key, id)
}
