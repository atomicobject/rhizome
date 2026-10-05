package bootstrap

import (
	"context"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

const codeIndexOpenRecoveryWindow = 30 * time.Second

// Retry only an unpublished open within the recovery window. An in-flight
// native SQLite call retains its own timeout and may outlast this window.
func openIntelStoreWithRetry(ctx context.Context, recoveryWindow time.Duration, open func() (*semdb.Store, func(), error)) (*semdb.Store, func(), error) {
	deadline := time.Now().Add(recoveryWindow)
	delay := 100 * time.Millisecond
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if lastErr != nil && !time.Now().Before(deadline) {
			return nil, nil, lastErr
		}
		store, cleanup, err := open()
		if err == nil {
			return store, cleanup, nil
		}
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if !sqliteutil.IsBusyOrLocked(err) {
			return nil, nil, err
		}
		lastErr = err
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil, lastErr
		}
		timer := time.NewTimer(min(delay, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
		delay = min(2*delay, time.Second)
	}
}
