package server

// In-memory store for one-shot, time-bounded upload tokens. Spec §12:
// the agent calls a metadata MCP tool to get a presigned URL, then
// shells out to `curl --data-binary @file <url>` — bytes never go
// through MCP's JSON envelope.
//
// Tokens are 32 random bytes, base64url-encoded, prefixed `ut_`. They
// carry the expected file name + size cap + actor name; the upload
// handler validates against those and rejects mismatches with a
// poka-yoke error. Single-use: once redeemed, removed.
//
// Persistence: none. The store lives in process memory because tokens
// are short-lived (5 minutes) — a server restart is far longer than
// any agent's redemption window, and forcing a re-mint on the next
// request is the cheapest UX. No on-disk state means no GC story
// either; the periodic janitor below evicts expired entries.

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// UploadToken records one minted credential. Fields beyond the bare
// secret are validated at redemption — defense against a shared token
// being repurposed for a different upload.
type UploadToken struct {
	Token        string
	Name         string
	MaxSizeBytes int64
	Actor        string
	ExpiresAt    time.Time
}

// uploadTokenTTL is the validity window. 5 minutes matches the AWS
// presigned-URL default and is long enough for any reasonable
// shell-and-upload sequence; short enough that a leaked URL becomes
// unusable quickly.
const uploadTokenTTL = 5 * time.Minute

// uploadTokens is the in-memory store. Methods are safe for concurrent
// use; the lock is held briefly so contention is invisible at our
// write rate.
type uploadTokens struct {
	mu sync.Mutex
	m  map[string]*UploadToken
}

func newUploadTokens() *uploadTokens {
	t := &uploadTokens{m: map[string]*UploadToken{}}
	// Janitor: walk the map every minute and drop expired entries so a
	// long-running server doesn't accumulate dead tokens. Synchronous
	// with mu — fine because the map is bounded by mint rate × TTL.
	go t.janitor()
	return t
}

// Mint creates a new token for an upload. The caller is the
// authenticated agent (`actor`). Returns the public token string the
// agent receives.
func (t *uploadTokens) Mint(name, actor string, maxSizeBytes int64) *UploadToken {
	tok := &UploadToken{
		Token:        randomToken(),
		Name:         name,
		MaxSizeBytes: maxSizeBytes,
		Actor:        actor,
		ExpiresAt:    time.Now().Add(uploadTokenTTL),
	}
	t.mu.Lock()
	t.m[tok.Token] = tok
	t.mu.Unlock()
	return tok
}

// Redeem returns the token and removes it from the store. Returns nil
// when no live token matches. Caller validates name + size against
// the redeemed token's fields.
func (t *uploadTokens) Redeem(token string) *UploadToken {
	t.mu.Lock()
	defer t.mu.Unlock()
	ut, ok := t.m[token]
	if !ok {
		return nil
	}
	delete(t.m, token)
	if time.Now().After(ut.ExpiresAt) {
		return nil
	}
	return ut
}

func (t *uploadTokens) janitor() {
	tick := time.NewTicker(1 * time.Minute)
	defer tick.Stop()
	for range tick.C {
		now := time.Now()
		t.mu.Lock()
		for k, ut := range t.m {
			if now.After(ut.ExpiresAt) {
				delete(t.m, k)
			}
		}
		t.mu.Unlock()
	}
}

// randomToken returns the `ut_<43 base64url chars>` shape the rest of
// the system expects. 32 bytes of entropy → 256 bits, well above any
// practical guess threshold.
func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "ut_" + base64.RawURLEncoding.EncodeToString(b)
}
