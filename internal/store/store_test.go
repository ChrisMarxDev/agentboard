package store

import (
	"encoding/json"
	"errors"
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

func TestIncrement(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Increment("counter", 1, "alice")
	if err != nil {
		t.Fatalf("first increment: %v", err)
	}
	if string(a.Value) != "1" {
		t.Fatalf("first: %s", a.Value)
	}

	b, err := s.Increment("counter", 41, "alice")
	if err != nil {
		t.Fatalf("second increment: %v", err)
	}
	if string(b.Value) != "42" {
		t.Fatalf("after +41: %s", b.Value)
	}
}

func TestIncrementOnNonNumber(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Set("k", json.RawMessage(`"hello"`), "", "alice")
	_, err := s.Increment("k", 1, "alice")
	if !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}
}

func TestCASRoundtrip(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.Set("k", json.RawMessage(`{"col":"todo"}`), "", "alice")

	// Mismatch.
	_, err := s.CAS("k", json.RawMessage(`{"col":"doing"}`), json.RawMessage(`{"col":"done"}`), "bob")
	var casErr *CASError
	if !errors.As(err, &casErr) {
		t.Fatalf("want CASError on mismatch, got %v", err)
	}
	if casErr.Current == nil || !strings.Contains(string(casErr.Current.Value), "todo") {
		t.Fatalf("CASError should embed current, got %+v", casErr.Current)
	}

	// Match.
	env, err := s.CAS("k", json.RawMessage(`{"col":"todo"}`), json.RawMessage(`{"col":"doing"}`), "bob")
	if err != nil {
		t.Fatalf("CAS match: %v", err)
	}
	if !strings.Contains(string(env.Value), "doing") {
		t.Fatalf("post-CAS: %s", env.Value)
	}
}

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
