package store

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// newTestStore returns a Store rooted in t.TempDir(). The store and its
// subscribers are cleaned up automatically when the test ends.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(Config{ProjectRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSingletonRoundtrip(t *testing.T) {
	s := newTestStore(t)

	env, err := s.Set("dev.metric", json.RawMessage(`42`), "", "alice")
	if err != nil {
		t.Fatalf("Set new: %v", err)
	}
	if string(env.Value) != "42" {
		t.Fatalf("value: got %s want 42", env.Value)
	}
	if env.Meta.Version == "" {
		t.Fatal("version: empty")
	}
	if env.Meta.ModifiedBy != "alice" {
		t.Fatalf("modified_by: got %q want alice", env.Meta.ModifiedBy)
	}

	got, err := s.ReadSingleton("dev.metric")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got.Value) != "42" || got.Meta.Version != env.Meta.Version {
		t.Fatalf("read mismatch: %+v vs %+v", got, env)
	}
}

func TestSingletonCASMatch(t *testing.T) {
	s := newTestStore(t)

	v1, _ := s.Set("k", json.RawMessage(`1`), "", "alice")
	v2, err := s.Set("k", json.RawMessage(`2`), v1.Meta.Version, "alice")
	if err != nil {
		t.Fatalf("Set with matching version: %v", err)
	}
	if v2.Meta.Version == v1.Meta.Version {
		t.Fatal("version should advance after CAS write")
	}
}

func TestSingletonCASMismatch(t *testing.T) {
	s := newTestStore(t)

	_, _ = s.Set("k", json.RawMessage(`1`), "", "alice")
	_, _ = s.Set("k", json.RawMessage(`2`), "*", "bob")

	_, err := s.Set("k", json.RawMessage(`3`), "stale-version", "alice")
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("want ConflictError, got %v", err)
	}
	if conflict.Current == nil || string(conflict.Current.Value) != "2" {
		t.Fatalf("conflict should embed current value, got %+v", conflict.Current)
	}
}

func TestVersionRequiredOnExisting(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Set("k", json.RawMessage(`1`), "", "alice")
	_, err := s.Set("k", json.RawMessage(`2`), "", "alice")
	if !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("want ErrVersionRequired, got %v", err)
	}
}

func TestMergeNeverConflicts(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Set("k", json.RawMessage(`{"a":1,"b":2}`), "", "alice")
	env, err := s.Merge("k", json.RawMessage(`{"a":99,"c":3}`), "bob")
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var got map[string]int
	_ = json.Unmarshal(env.Value, &got)
	if got["a"] != 99 || got["b"] != 2 || got["c"] != 3 {
		t.Fatalf("merge wrong: %v", got)
	}
}

// TestIncrement, TestIncrementOnNonNumber, TestCASRoundtrip removed
// in Cut 2 — atomic ops were dropped in favour of file-level CAS via
// _meta.version + agent-side read-modify-write.

func TestWrongShape(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Set("k", json.RawMessage(`1`), "", "alice")
	_, err := s.Append("k", json.RawMessage(`{}`), "alice")
	var ws *WrongShapeError
	if !errors.As(err, &ws) {
		t.Fatalf("want WrongShapeError, got %v", err)
	}
	if ws.Actual != ShapeSingleton || ws.Attempt != ShapeStream {
		t.Fatalf("wrong shape error fields: %+v", ws)
	}
}

func TestDeleteSingletonIdempotent(t *testing.T) {
	s := newTestStore(t)
	if err := s.DeleteSingleton("never-existed", "*", "alice"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	_, _ = s.Set("k", json.RawMessage(`1`), "", "alice")
	if err := s.DeleteSingleton("k", "*", "alice"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteSingleton("k", "*", "alice"); err != nil {
		t.Fatalf("delete twice (idempotent): %v", err)
	}
}

func TestCollectionPerIDIsolation(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UpsertItem("board", "task-1", json.RawMessage(`{"title":"a"}`), "", "alice")
	if err != nil {
		t.Fatalf("upsert task-1: %v", err)
	}
	_, err = s.UpsertItem("board", "task-2", json.RawMessage(`{"title":"b"}`), "", "alice")
	if err != nil {
		t.Fatalf("upsert task-2: %v", err)
	}

	items, err := s.ListCollection("board")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}

	if err := s.DeleteItem("board", "task-1", "*", "alice"); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	items, _ = s.ListCollection("board")
	if len(items) != 1 || items[0].ID != "task-2" {
		t.Fatalf("after delete: %+v", items)
	}
}

func TestStreamAppendAndTail(t *testing.T) {
	s := newTestStore(t)
	for i := range 5 {
		_, err := s.Append("events", json.RawMessage(jsonNum(i)), "alice")
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	lines, err := s.ReadStream("events", ReadStreamOpts{Limit: 3})
	if err != nil {
		t.Fatalf("ReadStream: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("want 3, got %d", len(lines))
	}
	if string(lines[0].Value) != "2" || string(lines[2].Value) != "4" {
		t.Fatalf("tail order wrong: %+v", lines)
	}
}

func TestCatalogTracksWrites(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Set("singleton-key", json.RawMessage(`1`), "", "alice")
	_, _ = s.UpsertItem("collection-key", "id1", json.RawMessage(`{}`), "", "alice")
	_, _ = s.Append("stream-key", json.RawMessage(`"hi"`), "alice")

	cat := s.Catalog()
	shapes := map[string]string{}
	for _, e := range cat {
		shapes[e.Key] = e.Shape
	}
	if shapes["singleton-key"] != ShapeSingleton {
		t.Errorf("singleton-key shape: %q", shapes["singleton-key"])
	}
	if shapes["collection-key"] != ShapeCollection {
		t.Errorf("collection-key shape: %q", shapes["collection-key"])
	}
	if shapes["stream-key"] != ShapeStream {
		t.Errorf("stream-key shape: %q", shapes["stream-key"])
	}
}

func TestVersionMonotonic(t *testing.T) {
	s := newTestStore(t)
	var prev string
	for i := range 100 {
		env, err := s.Set("k", json.RawMessage(jsonNum(i)), "*", "alice")
		if err != nil {
			t.Fatalf("set %d: %v", i, err)
		}
		if env.Meta.Version <= prev {
			t.Fatalf("non-monotonic at %d: %q <= %q", i, env.Meta.Version, prev)
		}
		prev = env.Meta.Version
	}
}

func TestSearchFindsValueText(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Set("doc1", json.RawMessage(`"the quick brown fox"`), "", "alice")
	_, _ = s.Set("doc2", json.RawMessage(`"a slow turtle"`), "", "alice")

	results, err := s.Search(SearchOpts{Query: "fox"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Key != "doc1" {
		t.Fatalf("want one match for doc1, got %+v", results)
	}
}

func TestKeyValidation(t *testing.T) {
	cases := []string{"", "../etc/passwd", ".hidden", "a/b", `bad\back`, "ok..no", "\x00", strings.Repeat("a", 300)}
	for _, c := range cases {
		if err := ValidateKey(c); err == nil {
			t.Errorf("ValidateKey(%q): want error, got nil", c)
		}
	}
	good := []string{"a", "dev.metrics.requests", "task-42", "Welcome"}
	for _, c := range good {
		if err := ValidateKey(c); err != nil {
			t.Errorf("ValidateKey(%q): want ok, got %v", c, err)
		}
	}
}

// jsonNum is a test helper — saves repeating json.Marshal in arithmetic
// tests. Returns the JSON encoding of an integer as raw bytes.
func jsonNum(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// TestActivityRotation forces the active file past the cap, then
// confirms (a) the active file is fresh, (b) a .1 segment exists,
// (c) ReadActivity stitches both files into a contiguous timeline.
//
// We use os.Truncate to simulate a 100MB file rather than actually
// writing 100MB of entries — the rotation logic only checks size
// before deciding to rotate, so a sparse file behaves identically.
func TestActivityRotation(t *testing.T) {
	s := newTestStore(t)

	// Write one entry so the active file exists.
	_, err := s.Set("rot.k", json.RawMessage(`1`), "", "alice")
	if err != nil {
		t.Fatalf("seed write: %v", err)
	}

	// Inflate the active file to just under the cap.
	if err := os.Truncate(s.activityLog, activityRotateBytes-10); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// Next write should trigger rotation.
	_, err = s.Set("rot.k", json.RawMessage(`2`), "*", "alice")
	if err != nil {
		t.Fatalf("post-rotation write: %v", err)
	}

	// Active should now be the new fresh file (small, holding only the
	// most recent entry); .1 should exist (the rotated big file).
	rotated := rotatedActivityPath(s.activityLog, 1)
	if _, err := os.Stat(rotated); err != nil {
		t.Fatalf(".1 segment missing after rotation: %v", err)
	}
	fi, err := os.Stat(s.activityLog)
	if err != nil {
		t.Fatalf("active stat: %v", err)
	}
	if fi.Size() >= activityRotateBytes {
		t.Fatalf("active file should be small after rotation, got %d bytes", fi.Size())
	}
}

