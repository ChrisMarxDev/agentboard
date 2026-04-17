package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSetAndGet(t *testing.T) {
	store := newTestStore(t)

	// Set a number
	if err := store.Set("count", json.RawMessage(`42`), "test"); err != nil {
		t.Fatal(err)
	}

	val, err := store.Get("count")
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "42" {
		t.Errorf("got %s, want 42", string(val))
	}

	// Set an object
	obj := `{"name":"alice","age":30}`
	if err := store.Set("user", json.RawMessage(obj), "test"); err != nil {
		t.Fatal(err)
	}
	val, err = store.Get("user")
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != obj {
		t.Errorf("got %s, want %s", string(val), obj)
	}

	// Overwrite
	if err := store.Set("count", json.RawMessage(`99`), "test"); err != nil {
		t.Fatal(err)
	}
	val, _ = store.Get("count")
	if string(val) != "99" {
		t.Errorf("got %s, want 99", string(val))
	}
}

func TestGetNonexistent(t *testing.T) {
	store := newTestStore(t)
	val, err := store.Get("nope")
	if err != nil {
		t.Fatal(err)
	}
	if val != nil {
		t.Errorf("expected nil, got %s", string(val))
	}
}

func TestGetMeta(t *testing.T) {
	store := newTestStore(t)
	store.Set("x", json.RawMessage(`"hello"`), "agent-1")

	meta, err := store.GetMeta("x")
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil {
		t.Fatal("expected meta, got nil")
	}
	if meta.Key != "x" {
		t.Errorf("key = %s, want x", meta.Key)
	}
	if string(meta.Value) != `"hello"` {
		t.Errorf("value = %s, want \"hello\"", string(meta.Value))
	}
	if meta.UpdatedBy != "agent-1" {
		t.Errorf("updated_by = %s, want agent-1", meta.UpdatedBy)
	}
	if meta.UpdatedAt == "" {
		t.Error("updated_at is empty")
	}
}

func TestDelete(t *testing.T) {
	store := newTestStore(t)
	store.Set("x", json.RawMessage(`1`), "test")

	if err := store.Delete("x", "test"); err != nil {
		t.Fatal(err)
	}

	val, _ := store.Get("x")
	if val != nil {
		t.Errorf("expected nil after delete, got %s", string(val))
	}
}

func TestGetAll(t *testing.T) {
	store := newTestStore(t)
	store.Set("a.one", json.RawMessage(`1`), "test")
	store.Set("a.two", json.RawMessage(`2`), "test")
	store.Set("b.one", json.RawMessage(`3`), "test")

	// All
	all, err := store.GetAll("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 keys, got %d", len(all))
	}

	// Prefix filter
	filtered, err := store.GetAll("a.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 {
		t.Errorf("expected 2 keys with prefix a., got %d", len(filtered))
	}

	// Specific keys
	specific, err := store.GetAll("", []string{"a.one", "b.one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(specific) != 2 {
		t.Errorf("expected 2 specific keys, got %d", len(specific))
	}
}

func TestListKeys(t *testing.T) {
	store := newTestStore(t)
	store.Set("z", json.RawMessage(`1`), "test")
	store.Set("a", json.RawMessage(`2`), "test")
	store.Set("m", json.RawMessage(`3`), "test")

	keys, err := store.ListKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
	// Should be sorted
	if keys[0] != "a" || keys[1] != "m" || keys[2] != "z" {
		t.Errorf("keys not sorted: %v", keys)
	}
}

func TestMerge(t *testing.T) {
	store := newTestStore(t)

	// Merge into nonexistent key = create
	store.Merge("cfg", json.RawMessage(`{"a":1,"b":2}`), "test")
	val, _ := store.Get("cfg")

	var obj map[string]interface{}
	json.Unmarshal(val, &obj)
	if obj["a"] != float64(1) || obj["b"] != float64(2) {
		t.Errorf("initial merge failed: %s", string(val))
	}

	// Merge updates + adds
	store.Merge("cfg", json.RawMessage(`{"b":3,"c":4}`), "test")
	val, _ = store.Get("cfg")
	json.Unmarshal(val, &obj)
	if obj["a"] != float64(1) || obj["b"] != float64(3) || obj["c"] != float64(4) {
		t.Errorf("merge update failed: %s", string(val))
	}

	// Merge with null removes key
	store.Merge("cfg", json.RawMessage(`{"a":null}`), "test")
	val, _ = store.Get("cfg")
	var obj2 map[string]interface{}
	json.Unmarshal(val, &obj2)
	if _, exists := obj2["a"]; exists {
		t.Errorf("null merge should remove key: %s", string(val))
	}
}

func TestAppend(t *testing.T) {
	store := newTestStore(t)

	// Append to nonexistent creates array
	store.Append("log", json.RawMessage(`{"msg":"one"}`), "test")
	val, _ := store.Get("log")

	var arr []interface{}
	json.Unmarshal(val, &arr)
	if len(arr) != 1 {
		t.Fatalf("expected 1 item, got %d", len(arr))
	}

	// Append more
	store.Append("log", json.RawMessage(`{"msg":"two"}`), "test")
	val, _ = store.Get("log")
	json.Unmarshal(val, &arr)
	if len(arr) != 2 {
		t.Errorf("expected 2 items, got %d", len(arr))
	}
}

func TestUpsertById(t *testing.T) {
	store := newTestStore(t)

	// Upsert into empty = creates array with item
	store.UpsertById("items", "1", json.RawMessage(`{"id":"1","name":"first"}`), "test")
	val, _ := store.Get("items")

	var arr []map[string]interface{}
	json.Unmarshal(val, &arr)
	if len(arr) != 1 {
		t.Fatalf("expected 1 item, got %d", len(arr))
	}
	if arr[0]["name"] != "first" {
		t.Errorf("expected name=first, got %v", arr[0]["name"])
	}

	// Upsert second item
	store.UpsertById("items", "2", json.RawMessage(`{"id":"2","name":"second"}`), "test")
	val, _ = store.Get("items")
	json.Unmarshal(val, &arr)
	if len(arr) != 2 {
		t.Errorf("expected 2 items, got %d", len(arr))
	}

	// Upsert existing = replace
	store.UpsertById("items", "1", json.RawMessage(`{"id":"1","name":"updated"}`), "test")
	val, _ = store.Get("items")
	json.Unmarshal(val, &arr)
	if len(arr) != 2 {
		t.Errorf("expected 2 items after upsert, got %d", len(arr))
	}
	if arr[0]["name"] != "updated" {
		t.Errorf("expected name=updated, got %v", arr[0]["name"])
	}
}

func TestMergeById(t *testing.T) {
	store := newTestStore(t)

	store.Set("items", json.RawMessage(`[{"id":"1","name":"alice","score":10}]`), "test")

	// Merge into item
	store.MergeById("items", "1", json.RawMessage(`{"score":20}`), "test")
	val, _ := store.Get("items")

	var arr []map[string]interface{}
	json.Unmarshal(val, &arr)
	if arr[0]["name"] != "alice" {
		t.Errorf("name should be preserved: %v", arr[0])
	}
	if arr[0]["score"] != float64(20) {
		t.Errorf("score should be 20: %v", arr[0])
	}

	// Merge into nonexistent ID = error
	err := store.MergeById("items", "999", json.RawMessage(`{}`), "test")
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestDeleteById(t *testing.T) {
	store := newTestStore(t)
	store.Set("items", json.RawMessage(`[{"id":"1","name":"a"},{"id":"2","name":"b"},{"id":"3","name":"c"}]`), "test")

	store.DeleteById("items", "2", "test")
	val, _ := store.Get("items")

	var arr []map[string]interface{}
	json.Unmarshal(val, &arr)
	if len(arr) != 2 {
		t.Errorf("expected 2 items after delete, got %d", len(arr))
	}
	for _, item := range arr {
		if item["id"] == "2" {
			t.Error("item 2 should be deleted")
		}
	}
}

func TestGetById(t *testing.T) {
	store := newTestStore(t)
	store.Set("items", json.RawMessage(`[{"id":"a","val":1},{"id":"b","val":2}]`), "test")

	item, err := store.GetById("items", "b")
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]interface{}
	json.Unmarshal(item, &obj)
	if obj["val"] != float64(2) {
		t.Errorf("expected val=2, got %v", obj["val"])
	}

	// Nonexistent
	item, err = store.GetById("items", "zzz")
	if err != nil {
		t.Fatal(err)
	}
	if item != nil {
		t.Errorf("expected nil for nonexistent id, got %s", string(item))
	}
}

func TestHistory(t *testing.T) {
	store := newTestStore(t)

	// Set value twice — first write should be archived
	store.Set("x", json.RawMessage(`"v1"`), "test")
	store.Set("x", json.RawMessage(`"v2"`), "test")

	// Check history has the previous value
	var count int
	err := store.db.QueryRow(`SELECT COUNT(*) FROM data_history WHERE key = 'x'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 history record, got %d", count)
	}

	var histValue string
	store.db.QueryRow(`SELECT value FROM data_history WHERE key = 'x'`).Scan(&histValue)
	if histValue != `"v1"` {
		t.Errorf("history value = %s, want \"v1\"", histValue)
	}
}

func TestPruneHistory(t *testing.T) {
	store := newTestStore(t)

	// Insert old history manually
	past := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	store.db.Exec(
		`INSERT INTO data_history (key, value, written_at, written_by) VALUES (?, ?, ?, ?)`,
		"old", `"old_value"`, past, "test",
	)

	// Insert recent history
	store.Set("recent", json.RawMessage(`"v1"`), "test")
	store.Set("recent", json.RawMessage(`"v2"`), "test")

	// Prune with 24h retention
	err := store.PruneHistory(t.Context(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	store.db.QueryRow(`SELECT COUNT(*) FROM data_history`).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 history record after prune, got %d", count)
	}
}

func TestSubscribe(t *testing.T) {
	store := newTestStore(t)
	ch := store.Subscribe()

	store.Set("x", json.RawMessage(`1`), "test")

	select {
	case evt := <-ch:
		if evt.Key != "x" {
			t.Errorf("event key = %s, want x", evt.Key)
		}
		if string(evt.Value) != "1" {
			t.Errorf("event value = %s, want 1", string(evt.Value))
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}
}

func TestInferSchema(t *testing.T) {
	store := newTestStore(t)
	store.Set("num", json.RawMessage(`42`), "test")
	store.Set("str", json.RawMessage(`"hello"`), "test")
	store.Set("arr", json.RawMessage(`[{"id":"1"}]`), "test")
	store.Set("obj", json.RawMessage(`{"a":1}`), "test")

	schema, err := store.InferSchema()
	if err != nil {
		t.Fatal(err)
	}

	if schema["num"].Type != "number" {
		t.Errorf("num type = %s, want number", schema["num"].Type)
	}
	if schema["str"].Type != "string" {
		t.Errorf("str type = %s, want string", schema["str"].Type)
	}
	if schema["arr"].Type != "array" {
		t.Errorf("arr type = %s, want array", schema["arr"].Type)
	}
	if schema["obj"].Type != "object" {
		t.Errorf("obj type = %s, want object", schema["obj"].Type)
	}
}

func TestJsonMergePatch(t *testing.T) {
	tests := []struct {
		name     string
		original string
		patch    string
		want     string
	}{
		{"add field", `{"a":1}`, `{"b":2}`, `{"a":1,"b":2}`},
		{"update field", `{"a":1}`, `{"a":2}`, `{"a":2}`},
		{"remove field", `{"a":1,"b":2}`, `{"a":null}`, `{"b":2}`},
		{"nested merge", `{"a":{"x":1}}`, `{"a":{"y":2}}`, `{"a":{"x":1,"y":2}}`},
		{"replace non-object", `"hello"`, `{"a":1}`, `{"a":1}`},
		{"replace with non-object", `{"a":1}`, `42`, `42`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonMergePatch([]byte(tt.original), []byte(tt.patch))
			// Normalize by round-tripping through JSON
			var gotVal, wantVal interface{}
			json.Unmarshal(got, &gotVal)
			json.Unmarshal([]byte(tt.want), &wantVal)

			gotJSON, _ := json.Marshal(gotVal)
			wantJSON, _ := json.Marshal(wantVal)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("got %s, want %s", string(gotJSON), string(wantJSON))
			}
		})
	}
}

// Ensure temp dirs are cleaned
func TestNewSQLiteStoreCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "data.sqlite")

	// Should fail if parent dir doesn't exist
	_, err := NewSQLiteStore(dbPath)
	if err != nil {
		// Create parent and retry
		os.MkdirAll(filepath.Dir(dbPath), 0755)
		store, err := NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		store.Close()
	}
}
