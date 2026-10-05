package indexlock

// Heartbeats keep a held lock fresh and let a background holder yield to an
// interactive writer. Both are owner-checked: a lock this process no longer
// owns must never be kept alive by it (SPEC-0104 US6).

import (
	"context"
	"os"
	"sync"
	"time"
)

// StartHeartbeat periodically touches the lock file to indicate active work.
// Returns an idempotent stop function that joins the heartbeat before returning.
func StartHeartbeat(ctx context.Context, lockPath string, interval time.Duration) func() {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	owner, ok := ReadLockData(lockPath)
	if !ok || owner.PID != os.Getpid() {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if !touchIfOwner(lockPath, owner) {
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(stop) })
		<-done
	}
}

// StartYieldingHeartbeat is like StartHeartbeat but also checks for priority requests
// and calls the yield callback when another process requests priority.
// The yield callback should release the lock and return; the heartbeat will stop.
func StartYieldingHeartbeat(ctx context.Context, lockPath string, interval time.Duration, onYield func()) func() {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	owner, ok := ReadLockData(lockPath)
	if !ok || owner.PID != os.Getpid() {
		return func() {}
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				// Check for priority before touching the heartbeat so interactive
				// callers do not have to wait for activeStaleAfter to reclaim work.
				if CheckPriority(lockPath) {
					if onYield != nil {
						onYield()
					}
					return
				}
				if !touchIfOwner(lockPath, owner) {
					return
				}
			}
		}
	}()
	return func() {
		select {
		case <-stop:
			// Already closed
		default:
			close(stop)
		}
	}
}
