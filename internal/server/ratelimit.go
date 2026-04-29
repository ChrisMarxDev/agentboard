package server

// Per-token rate limiter for /api/data mutation endpoints (spec §18).
//
// Defaults: 200 writes/min sustained, 50/sec burst. Reads are
// unthrottled here — the storeread paths read from the in-memory
// catalog and are cheap enough that a buggy poll loop is annoying,
// not actually expensive.
//
// Implementation: golang.org/x/time/rate token bucket per token, kept
// in a sync.Map. Idle entries (no traffic for 1 hour) get evicted by
// a periodic janitor so a long-running server doesn't accumulate
// limiters for one-shot tokens.
//
// Friendly-error contract per CORE_GUIDELINES §12: every 429 carries a
// human-readable explanation, the configured limit, the current rate,
// and the seconds until the bucket refills enough for the next call.

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"golang.org/x/time/rate"
)

// store rate-limit defaults. Tuned generously — agents have to be doing
// something genuinely runaway to trip them. The bucket size + refill
// rate together mean ~3 writes/sec sustained, 50 in a burst.
const (
	writeBurst       = 50
	writesPerMinute  = 200
	limiterIdleEvict = 1 * time.Hour
)

// storeRateStore holds one token bucket per actor name. Anonymous /
// unauthenticated callers should never reach a store handler (auth gates
// them earlier), but as a defensive default the empty actor maps to a
// shared "anonymous" bucket so no single client can saturate.
type storeRateStore struct {
	mu sync.Mutex
	m  map[string]*storeRateEntry
}

type storeRateEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newRateStore() *storeRateStore {
	s := &storeRateStore{m: map[string]*storeRateEntry{}}
	go s.janitor()
	return s
}

// limiterFor returns (or creates) the per-actor token bucket. Holds
// s.mu briefly; the limiter itself uses internal locking, so the hot
// Allow() path is uncontended.
func (s *storeRateStore) limiterFor(actor string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[actor]
	if !ok {
		e = &storeRateEntry{
			limiter: rate.NewLimiter(rate.Limit(float64(writesPerMinute)/60.0), writeBurst),
		}
		s.m[actor] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

func (s *storeRateStore) janitor() {
	tick := time.NewTicker(15 * time.Minute)
	defer tick.Stop()
	for range tick.C {
		cutoff := time.Now().Add(-limiterIdleEvict)
		s.mu.Lock()
		for k, e := range s.m {
			if e.lastSeen.Before(cutoff) {
				delete(s.m, k)
			}
		}
		s.mu.Unlock()
	}
}

// storeRateLimit is the chi middleware applied to /api/data mutation
// routes. Reads (GET, HEAD) bypass it — the bucket only matters for
// state-changing calls.
func (s *Server) storeRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		actor := "anonymous"
		if u := auth.UserFromContext(r.Context()); u != nil {
			actor = u.Username
		}
		lim := s.Limits.limiterFor(actor)
		res := lim.Reserve()
		if !res.OK() {
			// Reserve always reports OK for non-zero buckets — only
			// reaches false in pathological config. Treat as 503.
			writeError(w, http.StatusServiceUnavailable, "limiter_misconfigured", "v2 limiter misconfigured")
			return
		}
		delay := res.Delay()
		if delay > 0 {
			res.Cancel() // don't actually consume — we're rejecting.
			retrySec := max(int(delay.Round(time.Second).Seconds()), 1)
			w.Header().Set("Retry-After", strconv.Itoa(retrySec))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":               "rate_limited",
				"message":             "You're writing too fast — try again in " + strconv.Itoa(retrySec) + " seconds.",
				"retry_after_seconds": retrySec,
				"limit":               "200 writes/min, 50/sec burst",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
