package data

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestIfMatch_Set_StaleRejected(t *testing.T) {
	store := newTestStore(t)

	// Seed.
	if err := store.Set("k", json.RawMessage(`1`), "seed"); err != nil {
		t.Fatal(err)
	}
	m1, _ := store.GetMeta("k")

	// Second write under a concurrent actor bumps updated_at.
	if err := store.Set("k", json.RawMessage(`2`), "other"); err != nil {
		t.Fatal(err)
	}

	// Our write with the original updated_at must fail with ErrStale.
	err := store.SetIfMatch("k", json.RawMessage(`99`), "us", m1.UpdatedAt)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}

	// Value should still be 2.
	val, _ := store.Get("k")
	if string(val) != "2" {
		t.Errorf("want 2, got %s", val)
	}
}

func TestIfMatch_Set_FreshSucceeds(t *testing.T) {
	store := newTestStore(t)

	if err := store.Set("k", json.RawMessage(`1`), "seed"); err != nil {
		t.Fatal(err)
	}
	m, _ := store.GetMeta("k")

	if err := store.SetIfMatch("k", json.RawMessage(`2`), "us", m.UpdatedAt); err != nil {
		t.Fatalf("fresh write should succeed: %v", err)
	}

	val, _ := store.Get("k")
	if string(val) != "2" {
		t.Errorf("want 2, got %s", val)
	}
}

func TestIfMatch_Set_MissingKey(t *testing.T) {
	store := newTestStore(t)

	// If-Match on a key that doesn't exist yet → ErrNotFoundForMatch.
	err := store.SetIfMatch("new", json.RawMessage(`1`), "us", "some-ts")
	if !errors.Is(err, ErrNotFoundForMatch) {
		t.Fatalf("want ErrNotFoundForMatch, got %v", err)
	}
}

func TestIfMatch_Set_EmptyExpectedSkipsCheck(t *testing.T) {
	store := newTestStore(t)

	// Empty expected = no check; SetIfMatch behaves like Set.
	if err := store.SetIfMatch("k", json.RawMessage(`1`), "us", ""); err != nil {
		t.Fatalf("empty expected should pass: %v", err)
	}
	val, _ := store.Get("k")
	if string(val) != "1" {
		t.Errorf("want 1, got %s", val)
	}
}

func TestIfMatch_Merge_Stale(t *testing.T) {
	store := newTestStore(t)
	if err := store.Set("obj", json.RawMessage(`{"a":1}`), "seed"); err != nil {
		t.Fatal(err)
	}
	m1, _ := store.GetMeta("obj")

	// Concurrent write bumps the timestamp.
	if err := store.Set("obj", json.RawMessage(`{"a":1,"b":2}`), "other"); err != nil {
		t.Fatal(err)
	}

	err := store.MergeIfMatch("obj", json.RawMessage(`{"c":3}`), "us", m1.UpdatedAt)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}
}

func TestIfMatch_Append_Stale(t *testing.T) {
	store := newTestStore(t)
	if err := store.Set("list", json.RawMessage(`[]`), "seed"); err != nil {
		t.Fatal(err)
	}
	m1, _ := store.GetMeta("list")

	if err := store.Append("list", json.RawMessage(`"a"`), "other"); err != nil {
		t.Fatal(err)
	}

	err := store.AppendIfMatch("list", json.RawMessage(`"b"`), "us", m1.UpdatedAt)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}
}

func TestIfMatch_Delete_Stale(t *testing.T) {
	store := newTestStore(t)
	if err := store.Set("k", json.RawMessage(`1`), "seed"); err != nil {
		t.Fatal(err)
	}
	m1, _ := store.GetMeta("k")

	if err := store.Set("k", json.RawMessage(`2`), "other"); err != nil {
		t.Fatal(err)
	}

	err := store.DeleteIfMatch("k", "us", m1.UpdatedAt)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}

	// Key should still exist.
	val, _ := store.Get("k")
	if string(val) != "2" {
		t.Errorf("key was deleted despite stale failure")
	}
}

func TestIfMatch_UpsertById_FreshPath(t *testing.T) {
	store := newTestStore(t)
	if err := store.Set("items", json.RawMessage(`[{"id":"a","n":1}]`), "seed"); err != nil {
		t.Fatal(err)
	}
	m, _ := store.GetMeta("items")

	if err := store.UpsertByIdIfMatch("items", "b", json.RawMessage(`{"n":2}`), "us", m.UpdatedAt); err != nil {
		t.Fatalf("fresh upsert-by-id should succeed: %v", err)
	}
	val, _ := store.Get("items")
	if string(val) != `[{"id":"a","n":1},{"id":"b","n":2}]` {
		t.Errorf("unexpected: %s", val)
	}
}
