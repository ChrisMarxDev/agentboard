package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christophermarx/agentboard/internal/data"
	"github.com/christophermarx/agentboard/internal/project"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()

	dir := t.TempDir()
	projPath := filepath.Join(dir, "project")
	os.MkdirAll(projPath, 0755)
	os.WriteFile(filepath.Join(projPath, "index.md"), []byte("# Test\n\nHello world"), 0644)
	os.MkdirAll(filepath.Join(projPath, "content"), 0755)
	os.MkdirAll(filepath.Join(projPath, "components"), 0755)
	os.MkdirAll(filepath.Join(projPath, ".agentboard"), 0755)

	proj, err := project.Load(projPath)
	if err != nil {
		t.Fatal(err)
	}

	store, err := data.NewSQLiteStore(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	srv := New(ServerConfig{
		Project:   proj,
		Store:     store,
		SkillFile: "# Test skill",
	})

	ts := httptest.NewServer(srv.Router)
	t.Cleanup(ts.Close)

	return srv, ts
}

func TestHealthEndpoint(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["ok"] != true {
		t.Errorf("ok = %v, want true", body["ok"])
	}
}

func TestDataSetAndGet(t *testing.T) {
	_, ts := newTestServer(t)

	// SET
	req, _ := http.NewRequest("PUT", ts.URL+"/api/data/test.key", strings.NewReader(`{"value":42}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Source", "test-agent")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("SET status = %d, want 200", resp.StatusCode)
	}

	// GET
	resp, err = http.Get(ts.URL + "/api/data/test.key")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var meta map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&meta)
	if meta["key"] != "test.key" {
		t.Errorf("key = %v, want test.key", meta["key"])
	}
	if meta["updated_by"] != "test-agent" {
		t.Errorf("updated_by = %v, want test-agent", meta["updated_by"])
	}
}

func TestDataGetNotFound(t *testing.T) {
	_, ts := newTestServer(t)

	resp, _ := http.Get(ts.URL + "/api/data/nonexistent")
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDataGetAll(t *testing.T) {
	_, ts := newTestServer(t)

	// Set two values
	put(t, ts, "/api/data/a.one", `1`)
	put(t, ts, "/api/data/a.two", `2`)

	resp, _ := http.Get(ts.URL + "/api/data")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var all map[string]json.RawMessage
	json.Unmarshal(body, &all)
	if len(all) != 2 {
		t.Errorf("expected 2 keys, got %d", len(all))
	}
}

func TestDataMerge(t *testing.T) {
	_, ts := newTestServer(t)

	put(t, ts, "/api/data/obj", `{"a":1,"b":2}`)
	patch(t, ts, "/api/data/obj", `{"b":3,"c":4}`)

	resp, _ := http.Get(ts.URL + "/api/data/obj")
	var meta map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()

	val := meta["value"].(map[string]interface{})
	if val["a"] != float64(1) || val["b"] != float64(3) || val["c"] != float64(4) {
		t.Errorf("merge result wrong: %v", val)
	}
}

func TestDataAppend(t *testing.T) {
	_, ts := newTestServer(t)

	post(t, ts, "/api/data/events", `{"msg":"one"}`)
	post(t, ts, "/api/data/events", `{"msg":"two"}`)

	resp, _ := http.Get(ts.URL + "/api/data/events")
	var meta map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()

	arr := meta["value"].([]interface{})
	if len(arr) != 2 {
		t.Errorf("expected 2 items, got %d", len(arr))
	}
}

func TestDataDelete(t *testing.T) {
	_, ts := newTestServer(t)

	put(t, ts, "/api/data/del.me", `"gone"`)

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/data/del.me", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("DELETE status = %d, want 200", resp.StatusCode)
	}

	resp, _ = http.Get(ts.URL + "/api/data/del.me")
	if resp.StatusCode != 404 {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestDataUpsertById(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("PUT", ts.URL+"/api/data/items/abc", strings.NewReader(`{"name":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	resp, _ = http.Get(ts.URL + "/api/data/items/abc")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var obj map[string]interface{}
	json.Unmarshal(body, &obj)
	if obj["name"] != "test" {
		t.Errorf("expected name=test, got %v", obj)
	}
}

func TestDataSchema(t *testing.T) {
	_, ts := newTestServer(t)

	put(t, ts, "/api/data/num", `42`)
	put(t, ts, "/api/data/str", `"hello"`)

	resp, _ := http.Get(ts.URL + "/api/data/schema")
	var schema map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&schema)
	resp.Body.Close()

	numSchema := schema["num"].(map[string]interface{})
	if numSchema["type"] != "number" {
		t.Errorf("num type = %v, want number", numSchema["type"])
	}
}

func TestInvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("PUT", ts.URL+"/api/data/bad", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400 for invalid JSON", resp.StatusCode)
	}
}

func TestPagesEndpoints(t *testing.T) {
	_, ts := newTestServer(t)

	// List pages
	resp, _ := http.Get(ts.URL + "/api/content")
	var pages []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&pages)
	resp.Body.Close()

	if len(pages) < 1 {
		t.Fatal("expected at least 1 page (index)")
	}

	// Get page source
	resp, _ = http.Get(ts.URL + "/api/content/index")
	var page map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&page)
	resp.Body.Close()

	if page["source"] == nil || page["source"] == "" {
		t.Error("expected non-empty page source")
	}

	// Write a new page
	req, _ := http.NewRequest("PUT", ts.URL+"/api/content/test-page",
		strings.NewReader("# Test Page\n\nHello"))
	req.Header.Set("Content-Type", "text/markdown")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("write page status = %d, want 200", resp.StatusCode)
	}

	// Cannot delete index
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/content/index", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("delete index status = %d, want 400", resp.StatusCode)
	}
}

func TestPageMove(t *testing.T) {
	_, ts := newTestServer(t)

	seed := func(path, body string) {
		req, _ := http.NewRequest("PUT", ts.URL+"/api/content/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "text/markdown")
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("seed %s: status %d", path, resp.StatusCode)
		}
	}

	move := func(from, to string) (*http.Response, map[string]any) {
		payload, _ := json.Marshal(map[string]string{"from": from, "to": to})
		req, _ := http.NewRequest("POST", ts.URL+"/api/content/move", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := http.DefaultClient.Do(req)
		var body map[string]any
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		return resp, body
	}

	pathExists := func(pagePath string) bool {
		resp, err := http.Get(ts.URL + "/api/content/" + pagePath)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode == 200
	}

	// Happy path — rename a flat page.
	seed("draft", "# Draft\n\nbody")
	resp, body := move("draft", "final")
	if resp.StatusCode != 200 {
		t.Fatalf("rename: status %d, body %v", resp.StatusCode, body)
	}
	if body["to"] != "final" || body["from"] != "draft" {
		t.Errorf("response body = %v, want from=draft to=final", body)
	}
	if pathExists("draft") {
		t.Error("old path still reachable after move")
	}
	if !pathExists("final") {
		t.Error("new path not reachable after move")
	}

	// Move into a nested folder that doesn't exist yet — MkdirAll should create it.
	seed("spec", "# Spec\n\nwip")
	resp, _ = move("spec", "archive/specs/2026-q1")
	if resp.StatusCode != 200 {
		t.Fatalf("move into new folder: status %d", resp.StatusCode)
	}
	if !pathExists("archive/specs/2026-q1") {
		t.Error("nested destination not reachable")
	}

	// Source doesn't exist → 404.
	resp, _ = move("does-not-exist", "somewhere")
	if resp.StatusCode != 404 {
		t.Errorf("missing source: status %d, want 404", resp.StatusCode)
	}

	// Destination already exists → 409.
	seed("a", "# a")
	seed("b", "# b")
	resp, _ = move("a", "b")
	if resp.StatusCode != 409 {
		t.Errorf("destination exists: status %d, want 409", resp.StatusCode)
	}

	// Moving from or to "index" is forbidden.
	resp, _ = move("index", "home")
	if resp.StatusCode != 400 {
		t.Errorf("index as from: status %d, want 400", resp.StatusCode)
	}
	seed("src", "# src")
	resp, _ = move("src", "index")
	if resp.StatusCode != 400 {
		t.Errorf("index as to: status %d, want 400", resp.StatusCode)
	}

	// Path traversal is rejected.
	resp, _ = move("a", "../../etc/passwd")
	if resp.StatusCode != 400 {
		t.Errorf("traversal: status %d, want 400", resp.StatusCode)
	}

	// Empty paths → 400.
	resp, _ = move("", "somewhere")
	if resp.StatusCode != 400 {
		t.Errorf("empty from: status %d, want 400", resp.StatusCode)
	}

	// Same source and destination → 400.
	seed("same", "# same")
	resp, _ = move("same", "same")
	if resp.StatusCode != 400 {
		t.Errorf("from==to: status %d, want 400", resp.StatusCode)
	}

	// Malformed JSON body → 400.
	req, _ := http.NewRequest("POST", ts.URL+"/api/content/move", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("malformed body: status %d, want 400", resp.StatusCode)
	}

	// Accepts "/features/foo" and "foo.md" forms (client-side friendliness).
	seed("original", "# original")
	resp, _ = move("/original", "renamed.md")
	if resp.StatusCode != 200 {
		t.Errorf("normalized paths: status %d, want 200", resp.StatusCode)
	}
	if !pathExists("renamed") {
		t.Error("normalized destination not reachable")
	}
}

func TestComponentsEndpoint(t *testing.T) {
	_, ts := newTestServer(t)

	resp, _ := http.Get(ts.URL + "/api/components")
	var comps []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&comps)
	resp.Body.Close()

	if len(comps) < 9 {
		t.Errorf("expected at least 9 built-in components, got %d", len(comps))
	}

	// Check a specific component exists
	found := false
	for _, c := range comps {
		if c["name"] == "Metric" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Metric component not found")
	}
}

func TestSkillEndpoint(t *testing.T) {
	_, ts := newTestServer(t)

	resp, _ := http.Get(ts.URL + "/skill")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/markdown" {
		t.Errorf("content-type = %s, want text/markdown", resp.Header.Get("Content-Type"))
	}
	if string(body) != "# Test skill" {
		t.Errorf("body = %s, want # Test skill", string(body))
	}
}

func TestMCPInitialize(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	resp, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var rpc map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&rpc)

	result := rpc["result"].(map[string]interface{})
	serverInfo := result["serverInfo"].(map[string]interface{})
	if serverInfo["name"] != "agentboard" {
		t.Errorf("server name = %v, want agentboard", serverInfo["name"])
	}
}

func TestMCPToolsList(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	resp, _ := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(body))
	defer resp.Body.Close()

	var rpc map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&rpc)

	result := rpc["result"].(map[string]interface{})
	tools := result["tools"].([]interface{})
	// 13 data/page/component core + 3 file tools + 2 error tools + 1 grab tool = 19.
	// (Component-upload write/delete are gated on --allow-component-upload and
	//  aren't advertised in the default test config.)
	if len(tools) != 21 {
		t.Errorf("expected 21 MCP tools, got %d", len(tools))
	}
}

func TestMCPToolCall(t *testing.T) {
	_, ts := newTestServer(t)

	// Set data first
	put(t, ts, "/api/data/test.mcp", `"hello"`)

	// Call agentboard_get via MCP
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"agentboard_get","arguments":{"key":"test.mcp"}}}`
	resp, _ := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(body))
	defer resp.Body.Close()

	var rpc map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&rpc)

	if rpc["error"] != nil {
		t.Errorf("unexpected error: %v", rpc["error"])
	}
}

func TestCORS(t *testing.T) {
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/data", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("missing CORS header")
	}
}

// helpers
func put(t *testing.T, ts *httptest.Server, path, body string) {
	t.Helper()
	req, _ := http.NewRequest("PUT", ts.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func patch(t *testing.T, ts *httptest.Server, path, body string) {
	t.Helper()
	req, _ := http.NewRequest("PATCH", ts.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func post(t *testing.T, ts *httptest.Server, path, body string) {
	t.Helper()
	resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func newAuthedTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()

	dir := t.TempDir()
	projPath := filepath.Join(dir, "project")
	os.MkdirAll(projPath, 0755)
	os.MkdirAll(filepath.Join(projPath, "content"), 0755)
	os.MkdirAll(filepath.Join(projPath, "components"), 0755)
	os.MkdirAll(filepath.Join(projPath, ".agentboard"), 0755)

	proj, err := project.Load(projPath)
	if err != nil {
		t.Fatal(err)
	}

	store, err := data.NewSQLiteStore(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	srv := New(ServerConfig{
		Project:   proj,
		Store:     store,
		SkillFile: "# Test skill",
		AuthToken: token,
	})

	ts := httptest.NewServer(srv.Router)
	t.Cleanup(ts.Close)
	return ts
}

func TestAuthMiddleware(t *testing.T) {
	const token = "s3cret-token"
	ts := newAuthedTestServer(t, token)

	do := func(t *testing.T, req *http.Request) *http.Response {
		t.Helper()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	t.Run("missing credentials return 401 with WWW-Authenticate", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/data", nil)
		resp := do(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
		if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
			t.Errorf("WWW-Authenticate = %q, want to contain Basic", got)
		}
	})

	t.Run("valid Bearer token allowed", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/data", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := do(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("wrong Bearer token returns 401", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/data", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		resp := do(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("valid Basic Auth with password=token allowed", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/data", nil)
		req.SetBasicAuth("anyuser", token)
		resp := do(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("query param token allowed", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/data?token="+token, nil)
		resp := do(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("/api/health exempted from auth", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/health")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("OPTIONS preflight passes through without auth", func(t *testing.T) {
		req, _ := http.NewRequest("OPTIONS", ts.URL+"/api/data", nil)
		resp := do(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("write endpoint gated", func(t *testing.T) {
		req, _ := http.NewRequest("PUT", ts.URL+"/api/data/foo", strings.NewReader(`"bar"`))
		req.Header.Set("Content-Type", "application/json")
		resp := do(t, req)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})
}

func TestAuthDisabledWhenTokenEmpty(t *testing.T) {
	ts := newAuthedTestServer(t, "")

	resp, err := http.Get(ts.URL + "/api/data")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (auth should be disabled when token empty)", resp.StatusCode)
	}
}
