package watchhub

import "context"

// Start starts the hub once. Calls after Close have no effect.
func (h *Hub) Start(ctx context.Context) {
	h.lifecycleMu.Lock()
	if h.closed || h.ctx != nil {
		h.lifecycleMu.Unlock()
		return
	}
	h.ctx, h.cancel = context.WithCancel(ctx)
	ctx = h.ctx
	h.work.Add(1)
	h.lifecycleMu.Unlock()
	defer h.work.Done()
	if backend := h.backendSnapshot(); backend != nil {
		if err := backend.Start(ctx); err != nil {
			if !h.maybeFallbackBackend(err) {
				h.logger.Printf("watchhub: backend start failed: %v", err)
			}
		}
		h.work.Add(2)
		go func() { defer h.work.Done(); h.fsLoop(ctx) }()
		go func() { defer h.work.Done(); h.pendingAddLoop(ctx) }()
	}
}

// Close discards pending events and waits for admitted work, including subscriber
// callbacks, before closing the backend. It is safe to call repeatedly.
// A callback must return before Close can finish; it must not call Close directly.
func (h *Hub) Close() error {
	h.closeOnce.Do(func() {
		h.lifecycleMu.Lock()
		h.closed = true
		if h.cancel != nil {
			h.cancel()
		}
		close(h.pendingStop)
		h.lifecycleMu.Unlock()

		h.work.Wait()
		h.flushTimerMu.Lock()
		if h.flushTimer != nil {
			h.flushTimer.Stop()
		}
		h.flushTimerMu.Unlock()
		h.pendingMu.Lock()
		clear(h.pending)
		h.pendingMu.Unlock()
		if backend := h.backendSnapshot(); backend != nil {
			h.closeErr = backend.Close()
		}
	})
	return h.closeErr
}

// Admission and closing share a lock so Wait cannot race a new first Add.
func (h *Hub) beginWork() bool {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.closed {
		return false
	}
	h.work.Add(1)
	return true
}

func (h *Hub) context() context.Context {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	return h.ctx
}
