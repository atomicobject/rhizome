package bootstrap

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/stretchr/testify/require"
)

func newCodeOpeningRuntime(t *testing.T, root string) *LiveRuntime {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	rt := &LiveRuntime{
		ctx: ctx, cancelCtx: cancel, VaultPath: root,
		disableLeaderWork: true, readOnlyCodeIndex: true, disableSessionStore: true,
		requirements: RequireRuntimeCapabilities(RuntimeCapabilityCodeIndex),
		codeReady:    make(chan struct{}), leaderCh: make(chan struct{}),
	}
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	return rt
}

func lockedCodeIndex(t *testing.T) (string, func()) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".rhizome", "db.sqlite")
	store, err := semdb.Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA locking_mode=EXCLUSIVE")
	require.NoError(t, err)
	_, err = db.Exec("BEGIN EXCLUSIVE")
	require.NoError(t, err)
	// Commit retains an exclusive connection lock until that connection closes.
	_, err = db.Exec("COMMIT")
	require.NoError(t, err)
	return root, func() { require.NoError(t, db.Close()) }
}

func TestWritableCodeRuntimeRecoversOrClosesAfterContention(t *testing.T) {
	for _, closeRuntime := range []bool{false, true} {
		name := "release"
		if closeRuntime {
			name = "close"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".rhizome", "db.sqlite")
			writer, err := semdb.Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, writer.Close()) })
			db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(path, sqliteutil.DSNOptions{BusyTimeoutMs: 1}), sqliteutil.Options{MaxOpenConns: 1})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			noteStore, err := embsqlite.OpenWithDB(db, 4)
			require.NoError(t, err)
			// Force Phase 3 to validate and republish the proof behind a real writer.
			_, err = writer.DB().Exec("DELETE FROM rzm_migration_validation")
			require.NoError(t, err)
			locker, err := writer.DB().BeginTx(t.Context(), nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = locker.Rollback() })
			_, err = locker.Exec("UPDATE schema_version SET version=version")
			require.NoError(t, err)
			rt := newCodeOpeningRuntime(t, root)
			rt.readOnlyCodeIndex = false
			rt.noteIndex.Store(noteStore)
			rt.embCfg.Store(&embeddings.Config{IndexPath: path})
			rt.addCloser(func() { _ = db.Close() })
			collector := indexingperf.New()
			rt.ctx = indexingperf.WithCollector(rt.ctx, collector)
			rt.startWorker(rt.initPhase3Code)
			// Existing diagnostics identify a completed open, without timing or hooks.
			require.Eventually(t, func() bool {
				for _, phase := range collector.AgentStartDiagnostics().Phases {
					if phase.Label == indexingperf.AgentStartPhaseStoreWarmOpen && phase.Count > 0 {
						return true
					}
				}
				return false
			}, 3*time.Second, 10*time.Millisecond)
			select {
			case <-rt.codeReady:
				t.Fatalf("writable startup became terminal under contention: %v", rt.WaitForCodeIndex(t.Context()))
			default:
			}
			require.Nil(t, rt.IntelStore())
			if closeRuntime {
				require.NoError(t, rt.Close())
				require.ErrorIs(t, rt.WaitForCodeIndex(t.Context()), context.Canceled)
				require.Nil(t, rt.IntelStore())
				require.ErrorContains(t, db.Ping(), "database is closed")
			} else {
				waitCtx, cancelWait := context.WithTimeout(t.Context(), 500*time.Millisecond)
				defer cancelWait()
				require.ErrorIs(t, rt.WaitForCodeIndex(waitCtx), context.DeadlineExceeded)
				// A caller's deadline does not cancel runtime-owned recovery.
				require.NoError(t, locker.Rollback())
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				require.NoError(t, rt.WaitForCodeIndex(ctx))
				require.NotNil(t, rt.IntelStore())
			}
		})
	}
}

func TestCodeStartupPreservesPermanentOpeningFailures(t *testing.T) {
	for _, incompatible := range []bool{false, true} {
		name := "missing"
		if incompatible {
			name = "incompatible"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if incompatible {
				store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
				require.NoError(t, err)
				_, err = store.DB().Exec("DROP TABLE files")
				require.NoError(t, err)
				require.NoError(t, store.Close())
			}
			rt := newCodeOpeningRuntime(t, root)
			rt.startWorker(rt.initPhase3Code)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			err := rt.WaitForCodeIndex(ctx)
			require.Error(t, err)
			require.NotErrorIs(t, err, context.DeadlineExceeded)
			if incompatible {
				var drift *migration.ErrSchemaDrift
				require.ErrorAs(t, err, &drift)
			} else {
				require.ErrorIs(t, err, os.ErrNotExist)
			}
			require.Nil(t, rt.IntelStore())
		})
	}
}

func TestCodeStartupLockRecoveryIsBounded(t *testing.T) {
	root, _ := lockedCodeIndex(t)
	path := filepath.Join(root, ".rhizome", "db.sqlite")
	attempts := 0
	var lastErr error
	store, cleanup, err := openIntelStoreWithRetry(t.Context(), 250*time.Millisecond, func() (*semdb.Store, func(), error) {
		attempts++
		opened, openErr := semdb.OpenReadOnlyExisting(path, t.Context(), sqliteutil.Options{})
		lastErr = openErr
		return opened, nil, openErr
	})
	require.True(t, sqliteutil.IsBusyOrLocked(err), "%v", err)
	require.ErrorIs(t, err, lastErr)
	require.Nil(t, store)
	require.Nil(t, cleanup)
	require.GreaterOrEqual(t, attempts, 1)
	require.LessOrEqual(t, attempts, 2, "deadline must prevent another attempt after the capped wait")
}

func TestIndexedReadOnlyRuntimeReturnsContentionPromptly(t *testing.T) {
	root, release := lockedCodeIndex(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	rt, err := NewLiveRuntime(t.Context(), IndexedReadOnlyRuntimeOptions(LiveOptions{
		VaultName: root, DisableSessionStore: true,
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err = rt.WaitForCodeIndex(ctx)
	require.True(t, sqliteutil.IsBusyOrLocked(err), "expected immediate SQLite contention, got %v", err)
	require.Nil(t, rt.IntelStore())
	release()
	// Fail-soft one-shot state is final; a new invocation can open the index.
	next, err := NewLiveRuntime(t.Context(), IndexedReadOnlyRuntimeOptions(LiveOptions{
		VaultName: root, DisableSessionStore: true,
	}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, next.Close()) })
	require.NoError(t, next.WaitForCodeIndex(t.Context()))
}
