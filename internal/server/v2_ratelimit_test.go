package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestV2RateLimit_BurstThenThrottle proves the limiter lets a burst of
// v2WriteBurst writes through immediately and rejects the next one
// with a friendly 429 + Retry-After. Reads bypass the limiter entirely.
//
// Uses the limiter middleware directly with a no-op handler so the
// test doesn't have to spin up the whole Server harness.
func TestV2RateLimit_BurstThenThrottle(t *testing.T) {
	srv := &Server{V2Limits: newV2RateStore()}
	mw := srv.v2RateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	pass := 0
	for i := range v2WriteBurst + 5 {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/data/k", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			pass++
			continue
		}
		// Once we hit 429, every subsequent call should also 429
		// because the bucket hasn't refilled meaningfully in test time.
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("write %d: got %d, want 200 or 429", i, w.Code)
		}
		if w.Header().Get("Retry-After") == "" {
			t.Fatalf("429 without Retry-After header")
		}
		body := w.Body.String()
		if !contains(body, "rate_limited") || !contains(body, "Retry-After") && !contains(body, "retry_after_seconds") {
			t.Fatalf("429 body missing poka-yoke fields: %s", body)
		}
	}
	if pass != v2WriteBurst {
		t.Fatalf("burst: passed %d, want exactly %d", pass, v2WriteBurst)
	}
}

func TestV2RateLimit_ReadsBypass(t *testing.T) {
	srv := &Server{V2Limits: newV2RateStore()}
	mw := srv.v2RateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Many GETs — should never hit the bucket.
	for i := range v2WriteBurst * 5 {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/index", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %d should bypass limiter, got %d", i, w.Code)
		}
	}
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
