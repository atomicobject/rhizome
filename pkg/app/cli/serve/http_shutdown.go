package serve

import (
	"net/http"
	"sync"
)

// drainingHandler keeps admitted requests inside the runtime's ownership
// window. Shutdown can time out while a save is still finishing; the election
// lock must outlive that handler even after its connection is closed.
type drainingHandler struct {
	inner   http.Handler
	mu      sync.Mutex
	active  int
	closing bool
	drained chan struct{}
}

func newDrainingHandler(inner http.Handler) *drainingHandler {
	return &drainingHandler{inner: inner, drained: make(chan struct{})}
}

func (h *drainingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		http.Error(w, "runtime is stopping", http.StatusServiceUnavailable)
		return
	}
	h.active++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.active--
		if h.closing && h.active == 0 {
			close(h.drained)
		}
		h.mu.Unlock()
	}()
	h.inner.ServeHTTP(w, r)
}

func (h *drainingHandler) Drain() {
	h.mu.Lock()
	h.closing = true
	if h.active == 0 {
		close(h.drained)
	}
	h.mu.Unlock()
	<-h.drained
}
