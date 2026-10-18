package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
)

// schemaDB keeps schema recovery on the same database or transaction connection.
type schemaDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func verifyPreservedRows(ctx context.Context, db schemaDB, source, destination string) error {
	var before, after int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM `+source+`), (SELECT COUNT(*) FROM `+destination+`)`).Scan(&before, &after); err != nil {
		return err
	}
	if before != after {
		return fmt.Errorf("embedding recovery would lose rows from %s: %d backed up, %d restored", source, before, after)
	}
	return nil
}

type embeddingCacheRow struct {
	hash string
	vec  embeddings.Embedding
}

func dropVecTablesByPrefix(ctx context.Context, db schemaDB, prefix string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name LIKE ?
	`, prefix+"%")
	if err != nil {
		return err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range tables {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
			return err
		}
	}
	return nil
}

func tableExists(ctx context.Context, db schemaDB, table string) (bool, error) {
	var name string
	err := db.QueryRowContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type='table' AND name=?
		LIMIT 1
	`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return name == table, nil
}

func addColumnIfMissingTx(ctx context.Context, tx *sql.Tx, table, column, decl string) error {
	var found int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM pragma_table_info(?)
		WHERE name = ?
		LIMIT 1
	`, table, column).Scan(&found)
	if err == nil && found == 1 {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+decl)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return nil
	}
	return err
}

func isSchemaError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no such column"):
		return true
	case strings.Contains(msg, "no such table"):
		return true
	case strings.Contains(msg, "malformed database schema"):
		return true
	case strings.Contains(msg, "views may not be indexed"):
		return true
	case strings.Contains(msg, "is a view"):
		return true
	default:
		return false
	}
}

func dropViewIfExists(ctx context.Context, db schemaDB, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `DROP VIEW IF EXISTS `+name)
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "use drop table") {
		return nil
	}
	return err
}

func execWithRetry(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	const maxAttempts = 5
	const baseBackoff = 50 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := db.ExecContext(ctx, query, args...)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isBusyError(err) || attempt == maxAttempts {
			return nil, err
		}
		time.Sleep(baseBackoff * time.Duration(attempt))
	}
	return nil, lastErr
}

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "locked") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "transaction within a transaction")
}

func tableHasColumn(ctx context.Context, db tableColumnQueryer, table, column string) (bool, error) {
	var found int
	err := db.QueryRowContext(ctx, `
		SELECT 1
		FROM pragma_table_info(?)
		WHERE name = ?
		LIMIT 1
	`, table, column).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found == 1, nil
}
