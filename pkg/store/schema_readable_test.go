package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/stretchr/testify/require"
)

func TestCheckReadableRejectsAnIndexFromANewerBuild(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "db.sqlite")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	require.NoError(t, CheckReadable(context.Background(), empty), "an index without schema state is created by this build")

	path := filepath.Join(t.TempDir(), "db.sqlite")
	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	require.NoError(t, CheckReadable(context.Background(), path))

	s, err = Open(path)
	require.NoError(t, err)
	_, err = s.db.Exec(`UPDATE rzm_migration_state SET version = ? WHERE domain = ?`, currentSchemaVersion+2, string(migration.DomainIntel))
	require.NoError(t, err)
	require.NoError(t, s.Close())

	var future *migration.ErrFutureSchema
	require.ErrorAs(t, CheckReadable(context.Background(), path), &future)
	require.Equal(t, currentSchemaVersion+2, future.Current)
}
