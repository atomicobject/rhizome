package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func sessionSQLitePoolOptions() sqliteutil.Options {
	return sqliteutil.Options{MaxOpenConns: 1, MaxIdleConns: 1}
}

// initSessionStoreOnly opens the narrow existing-only session writer without
// making note-only requests construct or await the CodeIndex reader.
func (rt *LiveRuntime) initSessionStoreOnly() {
	defer close(rt.sessionReady)
	if rt.requirements.Includes(RuntimeCapabilityCodeIndex) {
		// Initial WAL connections can contend even before a query executes.
		// Open the required reader before its optional session writer.
		select {
		case <-rt.ctx.Done():
			return
		case <-rt.codeReady:
		}
	}
	codeCfg := rt.Configuration().Code
	store, err := semdb.OpenSessionStoreExisting(codeCfg.IndexPath, rt.ctx, sessionSQLitePoolOptions())
	if err != nil || store == nil || rt.isClosed() {
		if store != nil {
			_ = store.Close()
		}
		return
	}
	rt.sessionStore.Store(&sessionStoreHolder{store: store})
	rt.addCloser(func() { _ = store.Close() })
}

func (rt *LiveRuntime) startSessionCleanup(store *semdb.Store) {
	if store == nil {
		return
	}
	doCleanup := func() {
		removed, ran, err := runSessionCleanupIfDue(context.Background(), store, time.Now())
		if err != nil {
			if diagnostics.FromContext(rt.ctx) != nil {
				rt.recordDiagnostic(slog.LevelWarn, "sessions.cleanup_failed", err)
			}
		} else if ran && removed > 0 {
			rt.recordDiagnostic(slog.LevelInfo, "sessions.cleaned", nil, slog.Int64("removed", removed))
		}
	}
	rt.startWorker(doCleanup)
	ticker := time.NewTicker(12 * time.Hour)
	rt.startWorker(func() {
		defer ticker.Stop()
		for {
			select {
			case <-rt.ctx.Done():
				return
			case <-ticker.C:
				doCleanup()
			}
		}
	})
}

func (rt *LiveRuntime) ownsSessionCleanup() bool {
	return !rt.readOnlyCodeIndex && !rt.disableSessionStore
}

func runSessionCleanupIfDue(ctx context.Context, store *semdb.Store, now time.Time) (removed int64, ran bool, err error) {
	return runSessionCleanupIfDueWith(ctx, store, now, store.CleanupSessions)
}

func runSessionCleanupIfDueWith(
	ctx context.Context,
	store *semdb.Store,
	now time.Time,
	cleanup func(context.Context, time.Time) (int64, error),
) (removed int64, ran bool, err error) {
	due, err := store.SessionCleanupDue(ctx, now)
	if err != nil || !due {
		return 0, false, err
	}
	claimedUntil := now.Add(12 * time.Hour)
	claimed, err := store.ClaimSessionCleanup(ctx, now, claimedUntil)
	if err != nil || !claimed {
		return 0, false, err
	}
	removed, err = cleanup(ctx, now.Add(-semdb.DefaultSessionRetention))
	if err != nil {
		// Cleanup can fail because ctx was canceled. Use a bounded context that
		// preserves values but not cancellation so the failed claimant can
		// relinquish without suppressing retries for the full cadence.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, releaseErr := store.RelinquishSessionCleanup(releaseCtx, claimedUntil, now)
		return removed, true, errors.Join(err, releaseErr)
	}
	return removed, true, err
}
