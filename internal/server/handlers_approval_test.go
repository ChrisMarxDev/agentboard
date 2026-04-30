package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestApprovalFlow_CreateReadRevoke walks the happy path:
//  1. Seed a page, which gets an initial etag.
//  2. POST /api/approval with the path → 200.
//  3. GET /api/{path} surfaces the approval record + stale=false.
//  4. Re-write the page → new etag → approval.stale = true.
//  5. DELETE /api/approval?path=... → approval gone.
func TestApprovalFlow_CreateReadRevoke(t *testing.T) {
	_, ts := newTestServer(t)

	// 1. Seed.
	seed, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/approveme", bytes.NewBufferString("# v1"))
	seed.Header.Set("Content-Type", "text/markdown")
	sResp, _ := http.DefaultClient.Do(seed)
	sResp.Body.Close()
	if sResp.StatusCode != http.StatusOK {
		t.Fatalf("seed: status = %d", sResp.StatusCode)
	}

	// 2. Approve.
	body, _ := json.Marshal(map[string]any{"path": "/approveme"})
	aReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/approval", bytes.NewBuffer(body))
	aReq.Header.Set("Content-Type", "application/json")
	aResp, err := http.DefaultClient.Do(aReq)
	if err != nil {
		t.Fatal(err)
	}
	defer aResp.Body.Close()
	if aResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(aResp.Body)
		t.Fatalf("POST /api/approval: status = %d, body = %s", aResp.StatusCode, raw)
	}

	// 3. Read page, confirm approval present and stale = false.
	g1, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/approveme", nil)
	g1.Header.Set("Accept", "application/json")
	g1Resp, err1 := http.DefaultClient.Do(g1)
	if err1 != nil {
		t.Fatal(err1)
	}
	defer g1Resp.Body.Close()
	var payload struct {
		Approval *struct {
			ApprovedBy string `json:"approved_by"`
			Stale      bool   `json:"stale"`
		} `json:"approval"`
	}
	if err := json.NewDecoder(g1Resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Approval == nil {
		t.Fatalf("approval missing from page GET response")
	}
	if payload.Approval.Stale {
		t.Errorf("approval stale=true right after approve")
	}
	if payload.Approval.ApprovedBy == "" {
		t.Errorf("approval.approved_by empty")
	}

	// 4. Re-write → etag changes → approval becomes stale.
	rw, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/approveme", bytes.NewBufferString("# v2\nedited"))
	rw.Header.Set("Content-Type", "text/markdown")
	rwResp, _ := http.DefaultClient.Do(rw)
	rwResp.Body.Close()

	g2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/approveme", nil)
	g2.Header.Set("Accept", "application/json")
	g2Resp, err2 := http.DefaultClient.Do(g2)
	if err2 != nil {
		t.Fatal(err2)
	}
	defer g2Resp.Body.Close()
	var p2 struct {
		Approval *struct {
			Stale bool `json:"stale"`
		} `json:"approval"`
	}
	_ = json.NewDecoder(g2Resp.Body).Decode(&p2)
	if p2.Approval == nil || !p2.Approval.Stale {
		t.Errorf("approval should be stale after re-write: %+v", p2.Approval)
	}

	// 5. Revoke.
	rv, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/approval?path=/approveme", nil)
	rvResp, _ := http.DefaultClient.Do(rv)
	rvResp.Body.Close()
	if rvResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: status = %d", rvResp.StatusCode)
	}
	g3, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/approveme", nil)
	g3.Header.Set("Accept", "application/json")
	g3Resp, err3 := http.DefaultClient.Do(g3)
	if err3 != nil {
		t.Fatal(err3)
	}
	defer g3Resp.Body.Close()
	var p3 struct {
		Approval any `json:"approval"`
	}
	_ = json.NewDecoder(g3Resp.Body).Decode(&p3)
	if p3.Approval != nil {
		t.Errorf("approval still present after revoke: %+v", p3.Approval)
	}
}

// TestApproval_MissingPage404 — approving a non-existent page errors.
func TestApproval_MissingPage404(t *testing.T) {
	_, ts := newTestServer(t)
	body, _ := json.Marshal(map[string]any{"path": "/does-not-exist"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/approval", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /api/approval for missing page: status = %d, want 404", resp.StatusCode)
	}
}

// TestApproval_AnonymousRejected — an anonymous caller can't approve.
func TestApproval_AnonymousRejected(t *testing.T) {
	_, ts := newTestServer(t)

	// Seed with the authed default client.
	seed, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/need-auth", bytes.NewBufferString("# x"))
	seed.Header.Set("Content-Type", "text/markdown")
	sr, _ := http.DefaultClient.Do(seed)
	sr.Body.Close()

	// Approval attempt with bare client → 401.
	bare := &http.Client{}
	body, _ := json.Marshal(map[string]any{"path": "/need-auth"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/approval", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := bare.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon approve: status = %d, want 401", resp.StatusCode)
	}
}

// TestApproval_DeletePageClearsApproval — deleting a page removes its
// approval row so a later re-create starts from zero.
func TestApproval_DeletePageClearsApproval(t *testing.T) {
	_, ts := newTestServer(t)

	// Seed + approve.
	seed, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/ephemeral", bytes.NewBufferString("# x"))
	seed.Header.Set("Content-Type", "text/markdown")
	sr, _ := http.DefaultClient.Do(seed)
	sr.Body.Close()
	body, _ := json.Marshal(map[string]any{"path": "/ephemeral"})
	a, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/approval", bytes.NewBuffer(body))
	a.Header.Set("Content-Type", "application/json")
	ar, _ := http.DefaultClient.Do(a)
	ar.Body.Close()

	// Delete the page.
	d, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/ephemeral", nil)
	dr, _ := http.DefaultClient.Do(d)
	dr.Body.Close()

	// Re-create: approval should not be there any more.
	seed2, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/ephemeral", bytes.NewBufferString("# fresh"))
	seed2.Header.Set("Content-Type", "text/markdown")
	sr2, _ := http.DefaultClient.Do(seed2)
	sr2.Body.Close()

	g, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/ephemeral", nil)
	g.Header.Set("Accept", "application/json")
	gr, err := http.DefaultClient.Do(g)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Body.Close()
	var p struct {
		Approval any `json:"approval"`
	}
	_ = json.NewDecoder(gr.Body).Decode(&p)
	if p.Approval != nil {
		t.Errorf("approval survived page delete+recreate: %+v", p.Approval)
	}
}
