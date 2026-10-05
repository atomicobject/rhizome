package sqliteutil

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mattn/go-sqlite3"
)

// IsBusyOrLocked reports SQLite lock contention, including wrapped driver errors.
// Error messages alone do not establish that an operation is safe to retry.
func IsBusyOrLocked(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked)
}

// ExecTxWithRetry retries a write transaction up to five times on SQLite busy or
// locked errors. The callback may run more than once and must keep its effects
// inside the transaction. Backoff waits (50, 100, 150, 200 ms) honor cancellation.
// Callers remain responsible for holding their shared write mutex.
func ExecTxWithRetry(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	return execTxWithRetry(ctx, db, fn, waitForTxRetry)
}

func execTxWithRetry(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error, wait func(context.Context, time.Duration) error) error {
	const maxAttempts = 5
	for attempt := 1; ; attempt++ {
		tx, err := db.BeginTx(ctx, nil)
		if err == nil {
			if err = fn(tx); err != nil {
				_ = tx.Rollback()
			} else {
				err = tx.Commit()
			}
		}
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt == maxAttempts || !IsBusyOrLocked(err) {
			return err
		}
		if err := wait(ctx, 50*time.Millisecond*time.Duration(attempt)); err != nil {
			return err
		}
	}
}

func waitForTxRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
