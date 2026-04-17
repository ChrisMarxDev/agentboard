package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Broadcaster manages SSE subscriptions and broadcasting.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[string]chan SSEEvent
}

// NewBroadcaster creates a new SSE broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[string]chan SSEEvent),
	}
}

// Broadcast sends an event to all connected subscribers.
func (b *Broadcaster) Broadcast(event SSEEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// drop if subscriber is slow
		}
	}
}

// Subscribe registers a new subscriber and returns its ID and channel.
func (b *Broadcaster) Subscribe() (string, <-chan SSEEvent) {
	id := uuid.New().String()
	ch := make(chan SSEEvent, 64)
	b.mu.Lock()
	b.subscribers[id] = ch
	b.mu.Unlock()
	return id, ch
}

// Unsubscribe removes a subscriber.
func (b *Broadcaster) Unsubscribe(id string) {
	b.mu.Lock()
	if ch, ok := b.subscribers[id]; ok {
		close(ch)
		delete(b.subscribers, id)
	}
	b.mu.Unlock()
}

// StartHeartbeat sends periodic heartbeat events.
func (b *Broadcaster) StartHeartbeat() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			b.Broadcast(SSEEvent{
				Type: "heartbeat",
				Data: json.RawMessage(`{}`),
			})
		}
	}()
}

// ServeHTTP handles SSE connections at GET /api/events.
func (b *Broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	id, ch := b.Subscribe()
	defer b.Unsubscribe(id)

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"id\":%q}\n\n", id)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, string(evt.Data))
			flusher.Flush()
		}
	}
}
