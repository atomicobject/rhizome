package web

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// indexGateTimeout caps how long index-dependent handlers wait on a closed
// gate before returning 503 + Retry-After. Sized so cold-boot
// `runBackgroundIndex` runs typically open the gate before the client times
// out, while still releasing the request so a stuck indexer never wedges
// the connection.
const indexGateTimeout = 60 * time.Second

var errIndexInitializing = errors.New("index is initializing")

// EnableIndexGate marks this runtime as one that expects to gate
// index-dependent reads. Without this call, the gate is a no-op and
// WaitIndexReady returns immediately — that keeps the test-friendly default
// permissive while letting `rzm serve` opt in to first-launch gating.
func (r *Runtime) EnableIndexGate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.indexGated = true
	if r.indexReady == nil {
		r.indexReady = make(chan struct{})
	}
	if r.noteReadReady == nil {
		r.noteReadReady = make(chan struct{})
	}
}

// MarkIndexReady opens the gate exactly once. Subsequent calls are no-ops, so
// the boot-time peek (when `ontology_nodes` is already populated) and the
// BackgroundIndexer callback can both fire without coordination. The caller
// must only invoke this after proving that a usable read model exists or that
// the initial index completed successfully; a failed empty build stays gated.
func (r *Runtime) MarkIndexReady() {
	if r == nil {
		return
	}
	// Open the narrower prerequisite first so any observer that sees complete
	// index readiness also sees note-read readiness in the same instant.
	r.MarkNoteReadReady()
	r.mu.Lock()
	if r.indexReady == nil {
		r.indexReady = make(chan struct{})
	}
	ch := r.indexReady
	r.mu.Unlock()
	r.indexReadyOnce.Do(func() {
		close(ch)
	})
}

// MarkNoteReadReady opens the narrower gate used by exact note navigation.
// A warm metadata snapshot can support these reads before the complete
// ontology/graph model is ready.
func (r *Runtime) MarkNoteReadReady() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.noteReadReady == nil {
		r.noteReadReady = make(chan struct{})
	}
	ch := r.noteReadReady
	r.mu.Unlock()
	r.noteReadReadyOnce.Do(func() {
		close(ch)
	})
}

// IndexReady reports whether the gate is open. Returns true when the runtime
// is not gated (default) so status payloads stay truthful in test fixtures
// and headless web embeddings.
func (r *Runtime) IndexReady() bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	gated := r.indexGated
	ch := r.indexReady
	r.mu.RUnlock()
	if !gated {
		return true
	}
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// NoteReadReady reports whether note navigation can use a published metadata
// snapshot. Full index readiness implies note-read readiness.
func (r *Runtime) NoteReadReady() bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	gated := r.indexGated
	ch := r.noteReadReady
	r.mu.RUnlock()
	return gateReady(gated, ch)
}

// WaitIndexReady blocks until the gate is open, the request context is
// canceled, or `timeout` elapses. Ungated runtimes return nil immediately.
// Returns ctx.Err() / context.DeadlineExceeded so the middleware can map the
// failure to 503 + Retry-After.
func (r *Runtime) WaitIndexReady(ctx context.Context, timeout time.Duration) error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	gated := r.indexGated
	ch := r.indexReady
	r.mu.RUnlock()
	return waitGate(ctx, timeout, gated, ch)
}

// WaitNoteReadReady waits only for the metadata snapshot needed by exact note
// navigation. It retains the same cancellation and timeout behavior as the
// complete index gate.
func (r *Runtime) WaitNoteReadReady(ctx context.Context, timeout time.Duration) error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	gated := r.indexGated
	ch := r.noteReadReady
	r.mu.RUnlock()
	return waitGate(ctx, timeout, gated, ch)
}

func gateReady(gated bool, ch <-chan struct{}) bool {
	if !gated {
		return true
	}
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func waitGate(ctx context.Context, timeout time.Duration, gated bool, ch <-chan struct{}) error {
	if !gated {
		return nil
	}
	if ch == nil {
		return context.DeadlineExceeded
	}
	select {
	case <-ch:
		return nil
	default:
	}
	if timeout <= 0 {
		select {
		case <-ch:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case <-ch:
		return nil
	case <-timeoutCtx.Done():
		return timeoutCtx.Err()
	}
}

// requireIndexReady blocks the wrapped handler until the runtime gate opens.
// On timeout it returns 503 with `Retry-After: 5` and the structured
// `INDEX_INITIALIZING` error code. WHY: two writes are not atomic in
// pkg/ontology/index.go (`ontology_schema_state.Ready=true` and
// `ontology_node_field_values` row inserts run in separate transactions), so
// gating on `Ready` alone would let requests through against an empty
// read-model; the gate keys off a pre-rebuild populated model or a successful
// initial build instead.
func (s *Server) requireIndexReady(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.runtime == nil {
			next(w, r)
			return
		}
		if err := s.runtime.WaitIndexReady(r.Context(), indexGateTimeout); err != nil {
			w.Header().Set("Retry-After", "5")
			writePublicError(w, http.StatusServiceUnavailable, PublicErrorIndexInitializing, errIndexInitializing)
			return
		}
		next(w, r)
	}
}

// indexReadyState reports the cheap "initializing" | "ready" tag for the
// status payload so handlers and capabilities helpers stay in sync.
func indexReadyState(ready bool) string {
	if ready {
		return "ready"
	}
	return "initializing"
}
