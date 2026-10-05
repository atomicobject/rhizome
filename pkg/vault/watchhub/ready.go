package watchhub

import (
	"context"
	"fmt"
	"time"
)

// WaitForReady blocks until the backend has finished any pending watch-root
// installation, queued directory-watch additions, and fsevents retries.
// It is primarily intended for integration tests that need the watcher to be
// fully active before mutating the vault.
func (h *Hub) WaitForReady(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		backend := h.backendSnapshot()
		if backend == nil {
			return fmt.Errorf("watchhub: backend disabled")
		}
		waitingFSEvents := false
		if backendIsFSEvents(backend) {
			h.fseventsMu.Lock()
			waiting := h.fseventsWait
			tries := h.fseventsTry
			h.fseventsMu.Unlock()
			waitingFSEvents = waiting || tries != 0
		}
		watchInstall, pendingWork, pendingQueued, readyAfter := h.readinessSnapshot()
		if !waitingFSEvents && watchInstall == 0 && pendingWork == 0 && pendingQueued == 0 && (readyAfter.IsZero() || !time.Now().Before(readyAfter)) {
			return nil
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("watchhub: backend not ready after %s", timeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
