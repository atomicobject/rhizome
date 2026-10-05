package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/stretchr/testify/require"
)

func TestVacuumStatsReadsSQLiteFreelistMetrics(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	stats, err := store.VacuumStats(ctx)
	require.NoError(t, err)
	require.Positive(t, stats.PageCount)
	require.Positive(t, stats.PageSize)
	require.GreaterOrEqual(t, stats.FreelistCount, int64(0))
	require.Equal(t, stats.FreelistCount*stats.PageSize, stats.ReclaimableBytes)
	require.GreaterOrEqual(t, stats.FreeRatio(), 0.0)
	require.LessOrEqual(t, stats.FreeRatio(), 1.0)
}

func TestMaintenancePreservesManagedReadAccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*Store, context.Context) error
	}{
		{"analyze", (*Store).Analyze},
		{"optimize", (*Store).Optimize},
		{"vacuum", (*Store).Vacuum},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "intel.db")
			store, err := Open(path)
			require.NoError(t, err)
			defer store.Close()
			err = tc.run(store, t.Context())
			require.NoError(t, err)
			reader, err := OpenReadOnlyExisting(path, t.Context(), sqliteutil.Options{})
			require.NoError(t, err)
			defer reader.Close()
		})
	}
}

func TestMaintenanceDoesNotApproveSchemaDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intel.db")
	store, err := Open(path)
	require.NoError(t, err)
	defer store.Close()
	_, err = store.db.ExecContext(t.Context(), `DROP INDEX idx_files_call_edges_stale`)
	require.NoError(t, err)
	var drift *migration.ErrSchemaDrift
	require.ErrorAs(t, store.Analyze(t.Context()), &drift)
	reader, err := OpenReadOnlyExisting(path, t.Context(), sqliteutil.Options{})
	if reader != nil {
		defer reader.Close()
	}
	require.ErrorAs(t, err, &drift, "maintenance must not publish proof for an invalid schema")
}
