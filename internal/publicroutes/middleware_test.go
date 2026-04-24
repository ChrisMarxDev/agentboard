package publicroutes

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// authProbe records whether the wrapped auth middleware ran.
type authProbe struct {
	ran bool
}

func (p *authProbe) middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p.ran = true
			next.ServeHTTP(w, r)
		})
	}
}

// buildHandler composes Gate + auth around a tiny terminal handler that
// reports whether IsPublicRequest was set in the request context.
func buildHandler(patterns []string) (http.Handler, *authProbe) {
	m := New(patterns)
	probe := &authProbe{}
	terminal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsPublicRequest(r.Context()) {
			w.Header().Set("X-Public", "1")
		}
		w.WriteHeader(http.StatusOK)
	})
	gated := Gate(m, probe.middleware(), GateOptions{})
	return gated(terminal), probe
}

func TestGate_PublicReadBypassesAuth(t *testing.T) {
	h, probe := buildHandler([]string{"/skills/**"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/skills/deploy-helper", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if rr.Header().Get("X-Public") != "1" {
		t.Errorf("expected X-Public header set")
	}
	if probe.ran {
		t.Errorf("auth middleware ran for a publicly-readable path")
	}
}

func TestGate_NonMatchingPathRunsAuth(t *testing.T) {
	h, probe := buildHandler([]string{"/skills/**"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/handbook/security", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !probe.ran {
		t.Errorf("auth middleware did NOT run for a non-matching path")
	}
}

func TestGate_WritesAlwaysRunAuth(t *testing.T) {
	h, probe := buildHandler([]string{"/skills/**"})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		probe.ran = false
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/skills/deploy-helper", nil)
		h.ServeHTTP(rr, req)
		if !probe.ran {
			t.Errorf("%s should route through auth, but bypassed it", method)
		}
	}
}

func TestGate_AdminPathAlwaysProtected(t *testing.T) {
	// Even if an operator writes a pattern that would nominally allow it,
	// /api/admin/* must never be publicly readable.
	h, probe := buildHandler([]string{"/api/**"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/tokens", nil)
	h.ServeHTTP(rr, req)
	if !probe.ran {
		t.Errorf("auth middleware did NOT run for /api/admin — hard invariant broken")
	}
}

func TestGate_EmptyRulesNoOp(t *testing.T) {
	// When no patterns are configured, Gate should be transparent — every
	// request routes through auth exactly as before.
	m := New(nil)
	probe := &authProbe{}
	terminal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := Gate(m, probe.middleware(), GateOptions{})(terminal)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	h.ServeHTTP(rr, req)
	if !probe.ran {
		t.Errorf("empty matcher should leave auth enforcement untouched")
	}
}
