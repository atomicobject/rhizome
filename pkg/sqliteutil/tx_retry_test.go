package sqliteutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestExecTxWithRetryDoesNotRetryPermanentBeginError(t *testing.T) {
	want := errors.New("database cannot begin")
	conn := &retryTestConn{beginErr: func(int) error { return want }}
	db := sql.OpenDB(retryTestConnector{conn})
	t.Cleanup(func() { _ = db.Close() })
	err := ExecTxWithRetry(context.Background(), db, func(*sql.Tx) error {
		t.Fatal("callback ran after failed begin")
		return nil
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, conn.begins)
}

func TestExecTxWithRetryTransactionFailures(t *testing.T) {
	for _, stage := range []string{"begin", "callback", "commit"} {
		for _, tc := range []struct {
			name      string
			err       error
			failures  int
			attempts  int
			wantError bool
		}{
			{"busy recovers", sqlite3.Error{Code: sqlite3.ErrBusy}, 2, 3, false},
			{"wrapped locked recovers", fmt.Errorf("write: %w", sqlite3.Error{Code: sqlite3.ErrLocked}), 2, 3, false},
			{"busy exhausted", sqlite3.Error{Code: sqlite3.ErrBusy}, 5, 5, true},
			{"permanent failure", sqlite3.Error{Code: sqlite3.ErrConstraint}, 5, 1, true},
			{"misleading busy text", errors.New("busy transaction within a transaction"), 5, 1, true},
		} {
			t.Run(stage+"/"+tc.name, func(t *testing.T) {
				conn := &retryTestConn{}
				failure := func(attempt int) error {
					if attempt <= tc.failures {
						return tc.err
					}
					return nil
				}
				if stage == "begin" {
					conn.beginErr = failure
				}
				if stage == "commit" {
					conn.commitErr = failure
				}
				db := sql.OpenDB(retryTestConnector{conn})
				t.Cleanup(func() { _ = db.Close() })
				var waits []time.Duration
				err := execTxWithRetry(context.Background(), db, func(*sql.Tx) error {
					if stage == "callback" {
						return failure(conn.begins)
					}
					return nil
				}, func(_ context.Context, delay time.Duration) error {
					waits = append(waits, delay)
					return nil
				})
				if tc.wantError {
					require.ErrorIs(t, err, tc.err)
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, tc.attempts, conn.begins)
				require.Len(t, waits, tc.attempts-1)
				if stage == "callback" {
					require.Equal(t, min(tc.failures, tc.attempts), conn.rollbacks)
				}
			})
		}
	}
}

func TestExecTxWithRetryStopsWhenBackoffIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &retryTestConn{beginErr: func(int) error { return sqlite3.Error{Code: sqlite3.ErrBusy} }}
	db := sql.OpenDB(retryTestConnector{conn})
	t.Cleanup(func() { _ = db.Close() })
	err := execTxWithRetry(ctx, db, func(*sql.Tx) error { return nil }, func(ctx context.Context, delay time.Duration) error {
		cancel()
		return waitForTxRetry(ctx, delay)
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, conn.begins)
}

func TestExecTxWithRetryReturnsCancellationFromEveryStage(t *testing.T) {
	for _, stage := range []string{"begin", "callback", "commit"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := func(int) error {
				cancel()
				return sqlite3.Error{Code: sqlite3.ErrBusy}
			}
			conn := &retryTestConn{}
			if stage == "begin" {
				conn.beginErr = failure
			}
			if stage == "commit" {
				conn.commitErr = failure
			}
			db := sql.OpenDB(retryTestConnector{conn})
			t.Cleanup(func() { _ = db.Close() })
			err := ExecTxWithRetry(ctx, db, func(*sql.Tx) error {
				if stage == "callback" {
					return failure(1)
				}
				return nil
			})
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, 1, conn.begins)
		})
	}
}

func TestExecTxWithRetryRollsBackFailedAttempt(t *testing.T) {
	db, err := OpenDSN(DSN(filepath.Join(t.TempDir(), "retry.sqlite")), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE retry_items(id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)
	attempts := 0
	err = execTxWithRetry(context.Background(), db, func(tx *sql.Tx) error {
		attempts++
		if _, err := tx.Exec(`INSERT INTO retry_items(id) VALUES (1)`); err != nil {
			return err
		}
		if attempts == 1 {
			return sqlite3.Error{Code: sqlite3.ErrBusy}
		}
		return nil
	}, func(context.Context, time.Duration) error { return nil })
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM retry_items`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestExecTxWithRetryRecoversAfterSQLiteWriterReleasesLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dsn := DSNWithOptions(filepath.Join(t.TempDir(), "locked.sqlite"), DSNOptions{BusyTimeoutMs: 1})
	blocker, err := OpenDSN(dsn, Options{MaxOpenConns: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Close() })
	writer, err := OpenDSN(dsn, Options{MaxOpenConns: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	_, err = blocker.ExecContext(ctx, "CREATE TABLE retry_items(id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	lock, err := blocker.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = lock.Rollback() }()
	_, err = lock.ExecContext(ctx, "INSERT INTO retry_items VALUES (1)")
	require.NoError(t, err)

	blocked := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- ExecTxWithRetry(ctx, writer, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO retry_items VALUES (2)")
			if err != nil {
				select {
				case blocked <- err:
				default:
				}
			}
			return err
		})
	}()
	select {
	case err := <-blocked:
		var sqliteErr sqlite3.Error
		require.ErrorAs(t, err, &sqliteErr)
		require.Equal(t, sqlite3.ErrBusy, sqliteErr.Code)
	case <-ctx.Done():
		t.Fatal("writer did not encounter the held SQLite lock")
	}
	require.NoError(t, lock.Commit())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("writer did not recover after SQLite lock release")
	}
	var count int
	require.NoError(t, writer.QueryRowContext(ctx, "SELECT COUNT(*) FROM retry_items").Scan(&count))
	require.Equal(t, 2, count)
}

func TestWaitForTxRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- waitForTxRetry(ctx, time.Hour) }()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("canceled backoff did not return")
	}
}

type retryTestConnector struct{ conn *retryTestConn }

func (c retryTestConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c retryTestConnector) Driver() driver.Driver                        { return retryTestDriver{} }

type retryTestDriver struct{}

func (retryTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type retryTestConn struct {
	beginErr  func(int) error
	commitErr func(int) error
	begins    int
	commits   int
	rollbacks int
}

func (*retryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*retryTestConn) Close() error { return nil }
func (c *retryTestConn) Begin() (driver.Tx, error) {
	c.begins++
	if c.beginErr != nil {
		if err := c.beginErr(c.begins); err != nil {
			return nil, err
		}
	}
	return c, nil
}
func (c *retryTestConn) Commit() error {
	c.commits++
	if c.commitErr != nil {
		return c.commitErr(c.commits)
	}
	return nil
}
func (c *retryTestConn) Rollback() error { c.rollbacks++; return nil }
