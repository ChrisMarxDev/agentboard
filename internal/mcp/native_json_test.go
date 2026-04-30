package mcp

// Regression coverage for ISSUES.md #1 and #2: the legacy
// agentboard_write / agentboard_merge double-stringified payloads —
// `frontmatter.value: 23` came back as `"23"` (string), and
// `merge({label: "Open"})` clobbered an object singleton down to
// `'{"label":"Open"}'`. Cut 6's batch tools accept native JSON; these
// tests prove a write round-trips the original type.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/christophermarx/agentboard/internal/project"
	"github.com/christophermarx/agentboard/internal/store"
)

func newMCPServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	proj, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := proj.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.NewStore(store.Config{ProjectRoot: dir})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		FileStore: st,
		Pages:     store.NewPageManager(proj),
	}, dir
}

func TestMCPWrite_NativeJSONNumber(t *testing.T) {
	s, _ := newMCPServer(t)
	ctx := context.Background()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil).WithContext(ctx)

	args := mustJSON(map[string]any{
		"items": []map[string]any{
			{
				"path":        "metrics/dau",
				"frontmatter": map[string]any{"value": 23, "label": "Daily active users"},
			},
		},
	})
	res, rpcErr := s.handleToolCall(req, callBody("agentboard_write", args))
	if rpcErr != nil {
		t.Fatalf("write rpc error: %+v", rpcErr)
	}
	results := decodeResults(t, res)
	if len(results) != 1 || !resultSuccess(results[0]) {
		t.Fatalf("write failed: %+v", results)
	}

	// Read back via the same tool.
	readArgs := mustJSON(map[string]any{"paths": []string{"metrics/dau"}})
	readRes, rpcErr := s.handleToolCall(req, callBody("agentboard_read", readArgs))
	if rpcErr != nil {
		t.Fatalf("read rpc error: %+v", rpcErr)
	}
	rr := decodeReadResults(t, readRes)
	if len(rr) != 1 || !resultSuccess(rr[0]) {
		t.Fatalf("read failed: %+v", rr)
	}
	got, ok := rr[0]["frontmatter"].(map[string]any)
	if !ok {
		t.Fatalf("frontmatter not an object: %+v", rr[0])
	}
	v, ok := got["value"]
	if !ok {
		t.Fatalf("value missing from round-trip frontmatter: %+v", got)
	}
	switch v := v.(type) {
	case float64:
		if v != 23 {
			t.Errorf("value = %v, want 23", v)
		}
	case string:
		t.Errorf("Issue 1 regression: value came back as a string %q (should be number 23)", v)
	default:
		t.Errorf("unexpected value type %T (%v); want number 23", v, v)
	}
}

func TestMCPPatch_PreservesObjectShape(t *testing.T) {
	s, _ := newMCPServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)

	// Seed a multi-field object singleton.
	seed := mustJSON(map[string]any{
		"items": []map[string]any{
			{
				"path": "vet/clinic.status",
				"frontmatter": map[string]any{
					"label": "Open",
					"hours": "9-5",
					"team":  []string{"alice", "bob"},
				},
			},
		},
	})
	if _, rpcErr := s.handleToolCall(req, callBody("agentboard_write", seed)); rpcErr != nil {
		t.Fatalf("seed: %+v", rpcErr)
	}

	// Patch one field; the rest must survive.
	patch := mustJSON(map[string]any{
		"items": []map[string]any{
			{
				"path":              "vet/clinic.status",
				"frontmatter_patch": map[string]any{"label": "Closed"},
			},
		},
	})
	if _, rpcErr := s.handleToolCall(req, callBody("agentboard_patch", patch)); rpcErr != nil {
		t.Fatalf("patch: %+v", rpcErr)
	}

	read := mustJSON(map[string]any{"paths": []string{"vet/clinic.status"}})
	res, rpcErr := s.handleToolCall(req, callBody("agentboard_read", read))
	if rpcErr != nil {
		t.Fatalf("read: %+v", rpcErr)
	}
	rr := decodeReadResults(t, res)
	if len(rr) != 1 || !resultSuccess(rr[0]) {
		t.Fatalf("read result: %+v", rr)
	}
	fm, _ := rr[0]["frontmatter"].(map[string]any)
	if got := fm["label"]; got != "Closed" {
		t.Errorf("label = %v, want Closed", got)
	}
	if got := fm["hours"]; got != "9-5" {
		t.Errorf("Issue 2 regression: untouched field 'hours' lost (got %v, want 9-5). Patch must deep-merge, not replace.", got)
	}
	if _, ok := fm["team"].([]any); !ok {
		t.Errorf("Issue 2 regression: 'team' array lost or coerced (got %T %v)", fm["team"], fm["team"])
	}
}

func TestMCPWrite_ShapeWarning(t *testing.T) {
	s, _ := newMCPServer(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)

	args := mustJSON(map[string]any{
		"items": []map[string]any{
			{
				"path":        "tasks/missing-fields",
				"frontmatter": map[string]any{"priority": 2}, // no title, no status
			},
		},
	})
	res, rpcErr := s.handleToolCall(req, callBody("agentboard_write", args))
	if rpcErr != nil {
		t.Fatalf("write: %+v", rpcErr)
	}
	results := decodeResults(t, res)
	if len(results) != 1 || !resultSuccess(results[0]) {
		t.Fatalf("write should still succeed: %+v", results)
	}
	wraw, ok := results[0].rawWarnings()
	if !ok || len(wraw) == 0 {
		t.Fatalf("expected at least one shape_hint warning, got %+v", results[0])
	}
	// Decode and check shape == task and missing fields.
	type warn struct {
		Code                   string   `json:"code"`
		Shape                  string   `json:"shape"`
		MissingSuggestedFields []string `json:"missing_suggested_fields"`
	}
	var ws []warn
	for _, raw := range wraw {
		var w warn
		_ = json.Unmarshal(raw, &w)
		ws = append(ws, w)
	}
	if ws[0].Code != "shape_hint" || ws[0].Shape != "task" {
		t.Errorf("warning shape: got %+v, want shape_hint/task", ws[0])
	}
	if !containsAll(ws[0].MissingSuggestedFields, []string{"title", "status"}) {
		t.Errorf("missing fields = %v, want title+status", ws[0].MissingSuggestedFields)
	}
}

// ----- helpers -----

type batchRow map[string]any

func (r batchRow) rawWarnings() ([]json.RawMessage, bool) {
	w, ok := r["warnings"]
	if !ok {
		return nil, false
	}
	// w is []any from the JSON round-trip; re-marshal each item.
	arr, ok := w.([]any)
	if !ok {
		return nil, false
	}
	out := make([]json.RawMessage, 0, len(arr))
	for _, e := range arr {
		raw, _ := json.Marshal(e)
		out = append(out, raw)
	}
	return out, true
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func callBody(name string, args json.RawMessage) json.RawMessage {
	body, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	return body
}

// decodeResults pulls the per-item results array out of an MCP
// content payload. Returns batchRow values for inspection.
func decodeResults(t *testing.T, payload any) []batchRow {
	t.Helper()
	text := mcpText(t, payload)
	var wrapper struct {
		Results []batchRow `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &wrapper); err != nil {
		t.Fatalf("decode results: %v\n%s", err, text)
	}
	return wrapper.Results
}

func decodeReadResults(t *testing.T, payload any) []batchRow {
	return decodeResults(t, payload)
}

func mcpText(t *testing.T, payload any) string {
	t.Helper()
	m, ok := payload.(map[string]interface{})
	if !ok {
		t.Fatalf("payload not a map: %T %v", payload, payload)
	}
	contents, ok := m["content"].([]map[string]string)
	if !ok {
		t.Fatalf("content not [{type,text}]: %T %v", m["content"], m["content"])
	}
	if len(contents) == 0 {
		t.Fatalf("empty content")
	}
	return contents[0]["text"]
}

func resultSuccess(r batchRow) bool {
	v, ok := r["success"].(bool)
	return ok && v
}

func containsAll(haystack, needles []string) bool {
	for _, n := range needles {
		found := false
		for _, h := range haystack {
			if h == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
