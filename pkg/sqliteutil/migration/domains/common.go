package domains

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

func isNoSuchTable(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no such table")
}

func discoverLegacyVersionFromTable(ctx context.Context, tx *sql.Tx, table, column string) (int, error) {
	var version sql.NullInt64
	query := `SELECT ` + column + ` FROM ` + table + ` LIMIT 1`
	if err := tx.QueryRowContext(ctx, query).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) || isNoSuchTable(err) {
			return 0, nil
		}
		return 0, err
	}
	if !version.Valid || version.Int64 < 0 {
		return 0, nil
	}
	return int(version.Int64), nil
}

func sequentialVersionSteps(domain migration.Domain, target int, migrate VersionMigrator) []migration.Step {
	steps := make([]migration.Step, 0, target)
	for to := 1; to <= target; to++ {
		fromVersion := to - 1
		toVersion := to
		steps = append(steps, migration.Step{
			To:   toVersion,
			Name: fmt.Sprintf("%s_v%d_to_v%d", domain, fromVersion, toVersion),
			Run: func(ctx context.Context, tx *sql.Tx) error {
				if migrate == nil {
					return nil
				}
				return migrate(ctx, tx, fromVersion, toVersion)
			},
		})
	}
	return steps
}
