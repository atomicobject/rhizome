package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntelUpgradeFromBaselineMatchesFreshSchema(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	upgradedPath := filepath.Join(dir, "upgrade.sqlite")
	baseline, err := openWithOptionsAtSchemaVersion(upgradedPath, OpenOptions{}, intelBaselineVersion)
	require.NoError(t, err)
	require.NoError(t, baseline.Close())
	upgraded, err := Open(upgradedPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, upgraded.Close()) })
	fresh, err := Open(filepath.Join(dir, "fresh.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fresh.Close()) })
	schema := func(store *Store) map[string]string {
		t.Helper()
		rows, err := store.db.QueryContext(ctx, `SELECT type, name, sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type, name`)
		require.NoError(t, err)
		defer rows.Close()
		result := map[string]string{}
		for rows.Next() {
			var kind, name, sql string
			require.NoError(t, rows.Scan(&kind, &name, &sql))
			result[kind+":"+name] = strings.Join(strings.Fields(sql), " ")
		}
		require.NoError(t, rows.Err())
		return result
	}
	require.Equal(t, schema(fresh), schema(upgraded))
	var legacyTables int
	require.NoError(t, upgraded.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = 'schema_version'`).Scan(&legacyTables))
	require.Zero(t, legacyTables)
	var before, after int
	require.NoError(t, upgraded.db.QueryRowContext(ctx, `SELECT count(*) FROM rzm_migration_log`).Scan(&before))
	require.NoError(t, upgraded.ensureSchemaVersion(ctx, currentSchemaVersion))
	require.NoError(t, upgraded.db.QueryRowContext(ctx, `SELECT count(*) FROM rzm_migration_log`).Scan(&after))
	require.Equal(t, before, after)
}
