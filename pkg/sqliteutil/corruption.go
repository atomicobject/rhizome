package sqliteutil

import (
	"context"
	"database/sql"
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

// QuickIntegrityCheck runs a fast integrity check on the database.
// Returns IntegrityCheckFailed if corruption is detected.
// Returns other errors (busy, locked, timeout) without wrapping them as corruption.
// Retries up to 3 times with exponential backoff for transient errors.
func QuickIntegrityCheck(db *sql.DB) error {
	const maxRetries = 3
	baseDelay := 500 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s, 2s
			delay := baseDelay * time.Duration(1<<(attempt-1))
			time.Sleep(delay)
		}

		err := quickIntegrityCheckOnce(db)
		if err == nil {
			return nil
		}

		// If it's actual corruption, don't retry
		var checkFailed IntegrityCheckFailed
		if errors.As(err, &checkFailed) {
			return err
		}

		lastErr = err
	}
	return lastErr
}

func quickIntegrityCheckOnce(db *sql.DB) error {
	// Use a bounded timeout to avoid blocking forever on locked databases
	// 15 seconds is more reasonable for slow filesystems (e.g., Codespaces)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&result); err != nil {
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
