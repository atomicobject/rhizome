package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"strings"
)

func dropVecTablesByPrefix(ctx context.Context, db *sql.DB, prefix string) error {
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
	case strings.Contains(msg, "cannot add a column"):
		return true
	case strings.Contains(msg, "cannot alter table"):
		return true
	case strings.Contains(msg, "is a view"):
		return true
	default:
		return false
	}
}

// IsSchemaIncompatibleError reports schema-version mismatches that require
// rebuild/replacement rather than forward migration.
func IsSchemaIncompatibleError(err error) bool {
	if err == nil {
		return false
	}
	var future *migration.ErrFutureSchema
	if errors.As(err, &future) {
		return true
	}
	var missing *migration.ErrMissingStep
	if errors.As(err, &missing) {
		return true
	}
	var drift *migration.ErrSchemaDrift
	if errors.As(err, &drift) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "schema version downgrade") ||
		strings.Contains(msg, "missing migration for version")
}

func dropViewIfExists(ctx context.Context, db *sql.DB, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `DROP VIEW IF EXISTS `+name)
	if err == nil {
		return nil
	}
	// SQLite returns "use DROP TABLE to delete table X" if X is a table; treat that as a no-op.
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "use drop table") {
		return nil
	}
	return err
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func tableExistsQuery(ctx context.Context, q queryRower, table string) (bool, error) {
	var name string
	err := q.QueryRowContext(ctx, `
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

func tableHasColumnQuery(ctx context.Context, q queryRower, table, column string) (bool, error) {
	var found int
	err := q.QueryRowContext(ctx, `
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

func indexExistsQuery(ctx context.Context, q queryRower, name string) (bool, error) {
	var indexName string
	err := q.QueryRowContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type='index' AND name=?
		LIMIT 1
	`, name).Scan(&indexName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return indexName == name, nil
}

func (s *Store) tableExists(ctx context.Context, table string) (bool, error) {
	return tableExistsQuery(ctx, s.db, table)
}

func (s *Store) tableHasColumn(ctx context.Context, table, column string) (bool, error) {
	return tableHasColumnQuery(ctx, s.db, table, column)
}
