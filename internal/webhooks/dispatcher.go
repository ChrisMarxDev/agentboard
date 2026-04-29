package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Event is what the dispatcher fans out. `Name` is the dotted event
// identifier ("data.set.welcome.users", "content.handbook.updated");
// `Data` is whatever structured payload the emitter attached.
//
// We keep the shape permissive — one schema per event kind would be
// nicer but rigidity here costs more than it earns while the surface
// is still moving.
type Event struct {
	Name string         `json:"name"`
	At   time.Time      `json:"at"`
	Data map[string]any `json:"data"`
}

// Dispatcher connects the event source to the subscription store. It
// owns the delivery goroutine pool; callers just call Start(ctx) +
// Emit(Event).
type Dispatcher struct {
	store      *Store
	httpClient *http.Client
	// secretResolver maps id → plaintext secret. In production the
	// plaintext is only shown once at Create, so we need a live cache;
	// for now we assume the dispatcher caller provides the secrets
	// it knows about via a callback. Leaving this injectable keeps
	// tests simple.
	secretResolver func(id string) (plaintext string, ok bool)
	// retrySchedule defines the backoff between delivery attempts.
	// Three entries = three attempts total (initial + 2 retries).
	retrySchedule []time.Duration
	// events is the in-process buffer between Emit callers and the
	// delivery worker. Buffered so bursty emitters don't block.
	events chan Event
}

// DispatcherOptions tunes behaviour. Zero values are sensible.
type DispatcherOptions struct {
	SecretResolver func(id string) (string, bool)
	RetrySchedule  []time.Duration
	BufferSize     int
	HTTPClient     *http.Client
}

// NewDispatcher builds a ready-to-Start dispatcher. Nothing ticks
// until Start(ctx) is called; Emit can be invoked before Start but
// events only drain once the worker is running.
func NewDispatcher(store *Store, opts DispatcherOptions) *Dispatcher {
	if opts.BufferSize <= 0 {
		opts.BufferSize = 256
	}
	if len(opts.RetrySchedule) == 0 {
		opts.RetrySchedule = []time.Duration{
			0,                // first attempt, immediate
			5 * time.Second,  // second attempt
			30 * time.Second, // third + final attempt
		}
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if opts.SecretResolver == nil {
		opts.SecretResolver = func(string) (string, bool) { return "", false }
	}
	return &Dispatcher{
		store:          store,
		httpClient:     opts.HTTPClient,
		secretResolver: opts.SecretResolver,
		retrySchedule:  opts.RetrySchedule,
		events:         make(chan Event, opts.BufferSize),
	}
}

// Emit queues an event for matching + delivery. Non-blocking — drops
// with a log line if the buffer is full rather than slowing down the
// caller. Webhook fan-out is best-effort by design.
func (d *Dispatcher) Emit(evt Event) {
	if evt.At.IsZero() {
		evt.At = time.Now().UTC()
	}
	select {
	case d.events <- evt:
	default:
		log.Printf("webhooks: event buffer full, dropping %q", evt.Name)
	}
}

// Start runs the worker loop until ctx is cancelled. Safe to call
// once. Returns when ctx is done.
func (d *Dispatcher) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-d.events:
			d.dispatch(ctx, evt)
		}
	}
}

// dispatch looks up matching subscriptions and fires a delivery
// goroutine for each. A slow destination can't starve another —
// goroutines are independent and bounded by `retrySchedule`.
func (d *Dispatcher) dispatch(ctx context.Context, evt Event) {
	subs, err := d.store.ListActive()
	if err != nil {
		log.Printf("webhooks: list subscriptions: %v", err)
		return
	}
	for _, sub := range subs {
		if !MatchEvent(sub.EventPattern, evt.Name) {
			continue
		}
		go d.deliverWithRetry(ctx, sub.ID, sub.DestinationURL, evt)
	}
}

// deliverWithRetry POSTs the signed event to the destination URL,
// retrying per the configured schedule. Exponential backoff applied
// between attempts. On final failure the subscription is marked
// dead_lettered — operators can see it in /admin/webhooks and revoke
// or fix the URL.
func (d *Dispatcher) deliverWithRetry(ctx context.Context, subID, url string, evt Event) {
	secret, _ := d.secretResolver(subID)
	body, err := json.Marshal(evt)
	if err != nil {
		_ = d.store.RecordAttempt(subID, StatusDeadLettered, 0, "marshal: "+err.Error(), false)
		return
	}

	var lastCode int
	var lastErr string
	for attempt, wait := range d.retrySchedule {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		code, reqErr := d.postOnce(ctx, url, secret, body)
		lastCode = code
		lastErr = ""
		if reqErr != nil {
			lastErr = reqErr.Error()
		}
		success := reqErr == nil && code >= 200 && code < 300
		if success {
			_ = d.store.RecordAttempt(subID, StatusOK, code, "", true)
			return
		}
		// More attempts remain?
		if attempt < len(d.retrySchedule)-1 {
			_ = d.store.RecordAttempt(subID, StatusRetrying, code, lastErr, false)
			continue
		}
	}
	_ = d.store.RecordAttempt(subID, StatusDeadLettered, lastCode, lastErr, false)
}

// postOnce is a single HTTP attempt. Returns (status code, network
// error). A non-2xx status is not an error at this layer — the caller
// inspects the code.
func (d *Dispatcher) postOnce(ctx context.Context, url, secret string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "agentboard-webhook/0")
	if secret != "" {
		req.Header.Set("X-AgentBoard-Signature", "sha256="+SignHMAC(body, secret))
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// SignHMAC returns the hex-encoded HMAC-SHA256 over `body` keyed by
// `secret`. Receivers verify by recomputing and comparing in constant
// time.
func SignHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// DeliverOne performs one synchronous delivery attempt — used by the
// test-delivery endpoint so callers get a real response. Bypasses the
// queue + retry schedule; the caller inspects the returned code/err
// directly.
func (d *Dispatcher) DeliverOne(ctx context.Context, subID, url, secret string, evt Event) (int, error) {
	body, err := json.Marshal(evt)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}
	code, reqErr := d.postOnce(ctx, url, secret, body)
	success := reqErr == nil && code >= 200 && code < 300
	status := StatusRetrying
	errMsg := ""
	if success {
		status = StatusOK
	} else if reqErr != nil {
		errMsg = reqErr.Error()
	}
	_ = d.store.RecordAttempt(subID, status, code, errMsg, success)
	return code, reqErr
}
