package sqliteutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

// IntegrityCheckFailed indicates the integrity check ran successfully but returned
// a non-ok result, meaning actual database corruption was detected.
// This is distinct from transient errors (busy, locked, timeout) that prevent
// the check from running.
type IntegrityCheckFailed struct {
	Result string
}

func (e IntegrityCheckFailed) Error() string {
	return "sqlite integrity check: " + e.Result
}

// QuickIntegrityCheck checks database pages and FTS5 indexes during maintenance.
// Only typed SQLite contention is retried, and cancellation bounds both queries
// and waits.
func QuickIntegrityCheck(ctx context.Context, db *sql.DB) error {
	return quickIntegrityCheck(ctx, db, waitForTxRetry)
}

func quickIntegrityCheck(ctx context.Context, db *sql.DB, wait func(context.Context, time.Duration) error) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for attempt := 0; ; attempt++ {
		err := quickIntegrityCheckOnce(ctx, db)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil || attempt == 2 || !IsBusyOrLocked(err) {
			return err
		}
		if err := wait(ctx, 50*time.Millisecond*time.Duration(attempt+1)); err != nil {
			return err
		}
	}
}

func quickIntegrityCheckOnce(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Refreshing FTS5 caches and checking must share one read snapshot, or a
	// concurrent commit could leave the cache stale again before the check.
	if _, err := conn.ExecContext(ctx, "BEGIN DEFERRED"); err != nil {
		return err
	}
	defer func() {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); err != nil {
			// Never return a connection holding a read snapshot to the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if err := refreshFTS5Structures(ctx, conn); err != nil {
		return err
	}
	var result string
	if err := conn.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&result); err != nil {
		// Query failed to run - this is NOT proof of corruption.
		// Could be busy, locked, timeout, permissions, etc.
		// Return the raw error without wrapping it as "integrity check failed"
		return err
	}
	if result != "ok" {
		// Check ran and returned a corruption signal
		return IntegrityCheckFailed{Result: result}
	}
	return nil
}

// refreshFTS5Structures opens a cursor on each FTS5 table. FTS5's integrity
// method trusts the segment structure cached by earlier statements on this
// connection, so after another connection merges segments it reports blobs that
// no longer exist as corruption. Opening a cursor makes FTS5 compare
// data_version and reload a stale cache.
func refreshFTS5Structures(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `SELECT name FROM sqlite_schema
		WHERE type = 'table' AND lower(sql) LIKE 'create virtual table%using fts5%'`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		var ignored int
		query := `SELECT 1 FROM "` + strings.ReplaceAll(name, `"`, `""`) + `" LIMIT 1`
		if err := conn.QueryRowContext(ctx, query).Scan(&ignored); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

// IsCorruptError returns true if the error indicates SQLite database corruption.
// This includes:
//   - IntegrityCheckFailed (from QuickIntegrityCheck when check ran and returned non-ok)
//   - SQLite error codes SQLITE_CORRUPT and SQLITE_NOTADB
//   - Error messages indicating malformed database or schema
//
// IMPORTANT: This does NOT treat transient errors (busy, locked, timeout) as corruption.
// Those errors mean we couldn't determine the database state, not that it's corrupt.
func IsCorruptError(err error) bool {
	if err == nil {
		return false
	}

	// Check for typed integrity failure (the check ran and found corruption)
	var checkFailed IntegrityCheckFailed
	if errors.As(err, &checkFailed) {
		return true
	}

	// Check for SQLite corruption error codes
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		// SQLITE_CORRUPT (11) and SQLITE_NOTADB (26) indicate corruption
		if sqliteErr.Code == sqlite3.ErrCorrupt || sqliteErr.Code == sqlite3.ErrNotADB {
			return true
		}
	}

	// Fall back to string matching for errors that don't use typed errors
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database disk image is malformed") ||
		strings.Contains(msg, "malformed database schema") ||
		strings.Contains(msg, "file is not a database")
	// NOTE: We intentionally do NOT match "database integrity check failed" here.
	// That string was previously used to wrap ALL quickIntegrityCheck errors,
	// including transient ones like SQLITE_BUSY. Now we use IntegrityCheckFailed
	// for actual corruption detection.
}
