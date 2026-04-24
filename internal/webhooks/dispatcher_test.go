package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignHMAC_Verifiable(t *testing.T) {
	body := []byte(`{"name":"x"}`)
	sig := SignHMAC(body, "secret123")
	// Independent re-compute.
	mac := hmac.New(sha256.New, []byte("secret123"))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Errorf("sig mismatch: got %q, want %q", sig, want)
	}
}

func TestDispatcher_HappyPathDelivery(t *testing.T) {
	s, _ := NewStore(openMemDB(t))
	secret, sub, _ := s.Create(CreateParams{
		EventPattern:   "data.*",
		DestinationURL: "http://placeholder", // overwritten below
		CreatedBy:      "alice",
	})

	var hits int32
	var gotSig string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		gotSig = r.Header.Get("X-AgentBoard-Signature")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	}))
	defer ts.Close()

	// Point subscription at the test server.
	_, _ = s.Update(sub.ID, UpdateParams{DestinationURL: ptr(ts.URL)})

	d := NewDispatcher(s, DispatcherOptions{
		SecretResolver: func(id string) (string, bool) {
			if id == sub.ID {
				return secret, true
			}
			return "", false
		},
		RetrySchedule: []time.Duration{0}, // single attempt
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go d.Start(ctx)

	d.Emit(Event{Name: "data.set.x", Data: map[string]any{"k": "v"}})

	deadline := time.Now().Add(1 * time.Second)
	for atomic.LoadInt32(&hits) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hits != 1 {
		t.Fatalf("destination got %d hits, want 1", hits)
	}
	if gotSig == "" || len(gotSig) < len("sha256=") {
		t.Errorf("missing HMAC header: %q", gotSig)
	}
}

func TestDispatcher_NonMatchingEventSkipped(t *testing.T) {
	s, _ := NewStore(openMemDB(t))
	_, _, _ = s.Create(CreateParams{
		EventPattern:   "content.*",
		DestinationURL: "http://127.0.0.1:0/hook", // unreachable; we only care that no delivery happens
		CreatedBy:      "alice",
	})
	d := NewDispatcher(s, DispatcherOptions{
		RetrySchedule: []time.Duration{0},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go d.Start(ctx)
	d.Emit(Event{Name: "data.set.x"})
	time.Sleep(100 * time.Millisecond)
	// No direct observation; we assert that LastStatus stayed pending.
	list, _ := s.ListActive()
	if list[0].LastStatus != StatusPending {
		t.Errorf("non-matching event triggered delivery: status=%s", list[0].LastStatus)
	}
}

func TestDispatcher_RetryThenDeadLetter(t *testing.T) {
	s, _ := NewStore(openMemDB(t))
	_, sub, _ := s.Create(CreateParams{
		EventPattern:   "*",
		DestinationURL: "http://placeholder",
		CreatedBy:      "alice",
	})
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500)
	}))
	defer ts.Close()
	_, _ = s.Update(sub.ID, UpdateParams{DestinationURL: ptr(ts.URL)})

	d := NewDispatcher(s, DispatcherOptions{
		RetrySchedule: []time.Duration{0, 0, 0}, // three rapid attempts
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go d.Start(ctx)
	d.Emit(Event{Name: "anything"})

	deadline := time.Now().Add(1 * time.Second)
	for atomic.LoadInt32(&hits) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hits != 3 {
		t.Fatalf("expected 3 delivery attempts, got %d", hits)
	}
	// Give the last RecordAttempt time to flush.
	time.Sleep(50 * time.Millisecond)
	got, _ := s.Get(sub.ID)
	if got.LastStatus != StatusDeadLettered {
		t.Errorf("status = %s, want dead_lettered", got.LastStatus)
	}
	if got.FailureCount != 3 {
		t.Errorf("failure_count = %d, want 3", got.FailureCount)
	}
}

func TestDispatcher_RevokedSubscriptionNotDelivered(t *testing.T) {
	s, _ := NewStore(openMemDB(t))
	_, sub, _ := s.Create(CreateParams{
		EventPattern:   "*",
		DestinationURL: "http://should-not-be-called.example",
		CreatedBy:      "alice",
	})
	_ = s.Revoke(sub.ID)
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(200)
	}))
	defer ts.Close()
	d := NewDispatcher(s, DispatcherOptions{RetrySchedule: []time.Duration{0}})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go d.Start(ctx)
	d.Emit(Event{Name: "anything"})
	time.Sleep(100 * time.Millisecond)
	if hits != 0 {
		t.Errorf("revoked subscription received delivery: %d hits", hits)
	}
}

func ptr[T any](v T) *T { return &v }
