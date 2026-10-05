package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestManagedHandleBusyTimeoutPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intel.db")
	writer, err := Open(path)
	require.NoError(t, err)
	defer writer.Close()
	var timeout int
	require.NoError(t, writer.db.QueryRow("PRAGMA busy_timeout").Scan(&timeout))
	require.Equal(t, 30000, timeout, "ordinary writer keeps its default")
	for _, tc := range []struct {
		name     string
		readOnly bool
		want     int
	}{
		{"reader", true, 0}, {"session", false, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := openValidatedExisting(path, t.Context(), sqliteutil.Options{}, tc.readOnly)
			require.NoError(t, err)
			defer store.Close()
			require.NoError(t, store.db.QueryRow("PRAGMA busy_timeout").Scan(&timeout))
			require.Equal(t, tc.want, timeout)
		})
	}
}

func TestManagedReaderOpenDoesNotWaitForExclusiveLock(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "active context"
		if canceled {
			name = "canceled context"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "intel.db")
			store, err := Open(path)
			require.NoError(t, err)
			require.NoError(t, store.Close())
			locker, err := sqliteutil.OpenDSN(sqliteutil.DSN(path), sqliteutil.Options{MaxOpenConns: 1})
			require.NoError(t, err)
			defer locker.Close()
			_, err = locker.Exec("PRAGMA locking_mode=EXCLUSIVE")
			require.NoError(t, err)
			_, err = locker.Exec("BEGIN EXCLUSIVE")
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if canceled {
				cancel()
			}
			type result struct {
				store *Store
				err   error
			}
			done := make(chan result, 1)
			go func() {
				reader, err := OpenReadOnlyExisting(path, ctx, sqliteutil.Options{})
				done <- result{reader, err}
			}()
			var got result
			returned := false
			select {
			case got = <-done:
				returned = true
			case <-time.After(time.Second):
				t.Error("managed reader waited for the held lock, even though its busy timeout should be zero")
			}
			// Release the real lock even on failure, so the regression never pays the
			// old thirty-second native busy timeout or leaks an opening goroutine.
			require.NoError(t, locker.Close())
			if !returned {
				got = <-done
			}
			if got.store != nil {
				require.NoError(t, got.store.Close())
			}
			if canceled {
				require.ErrorIs(t, got.err, context.Canceled)
			} else {
				var busy sqlite3.Error
				require.ErrorAs(t, got.err, &busy)
				require.Equal(t, sqlite3.ErrBusy, busy.Code)
			}
			require.False(t, IsSchemaIncompatibleError(got.err), "contention must never authorize a rebuild")
			recovered, err := OpenReadOnlyExisting(path, t.Context(), sqliteutil.Options{})
			require.NoError(t, err, "releasing the lock restores the existing reader without repair")
			require.NoError(t, recovered.Close())
		})
	}
}
