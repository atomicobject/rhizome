package web

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

// GlobalEvent is a vault-wide event sent over the /api/v1/events SSE channel.
type GlobalEvent struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Data carries event-specific payload (optional).
	Data any `json:"data,omitempty"`
}

// NodeChangedEventData identifies the committed paths and read domains that
// are ready for reads. Semantic indexing completes separately via index.changed.
type NodeChangedEventData struct {
	Paths   []string `json:"paths"`
	Domains []string `json:"domains"`
}

// GlobalEventReason extracts the `reason` string from an event payload.
func GlobalEventReason(data any) string {
	fields, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	reason, _ := fields["reason"].(string)
	return reason
}

// globalEventBroker fans out vault-wide events to SSE subscribers.
type globalEventBroker struct {
	mu     sync.RWMutex
	subs   map[uint64]chan GlobalEvent
	seq    atomic.Uint64
	nextID atomic.Uint64
	closed bool
}

func newGlobalEventBroker() *globalEventBroker {
	return &globalEventBroker{
		subs: make(map[uint64]chan GlobalEvent),
	}
}

func (b *globalEventBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for id, ch := range b.subs {
		delete(b.subs, id)
		close(ch)
	}
}

func (b *globalEventBroker) Subscribe() (<-chan GlobalEvent, func()) {
	ch := make(chan GlobalEvent, 8)
	id := b.seq.Add(1)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	b.subs[id] = ch
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if _, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(ch)
		}
		b.mu.Unlock()
	}
}

// Publish sends an event to all subscribers.
func (b *globalEventBroker) Publish(kind string, data any) {
	if b == nil {
		return
	}
	event := GlobalEvent{
		ID:   fmt.Sprintf("g-%d", b.nextID.Add(1)),
		Kind: kind,
		Data: data,
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
			// Drop if subscriber is slow.
		}
	}
}

// NotifyGlobalEvent publishes a global event to all SSE subscribers.
func (s *Server) NotifyGlobalEvent(kind string, data any) {
	if s != nil {
		s.invalidateFinderOnEvent(kind)
	}
	if s != nil && s.globalEvents != nil {
		s.globalEvents.Publish(kind, data)
	}
}

func (s *Server) handleGlobalEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writePublicError(w, http.StatusInternalServerError, PublicErrorStreamingUnsupported, fmt.Errorf("streaming unsupported"))
		return
	}

	events, unsubscribe := s.globalEvents.Subscribe()
	defer unsubscribe()
	defer s.watchViewFolders()()

	defer s.holdRuntime()()
	streamJSONEvents(w, r, flusher, events, func(event GlobalEvent) (string, string) {
		return event.ID, event.Kind
	})
}
