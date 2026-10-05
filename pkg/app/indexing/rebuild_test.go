package indexing

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareRebuild_ReplacesIncompatibleSchemaDB(t *testing.T) {
	vault := t.TempDir()
	dbPath := filepath.Join(vault, ".rhizome", "db.sqlite")
	lockPath := filepath.Join(vault, ".rhizome", "index.lock")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE schema_version (version INTEGER NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO schema_version(version) VALUES (999)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	err = PrepareFreshRebuild(vault, lockPath)
	require.NoError(t, err)

	_, err = os.Stat(dbPath)
	require.ErrorIs(t, err, os.ErrNotExist, "incompatible DB must be clobbered for an explicit rebuild")
}

func TestPrepareFreshRebuild_RemovesDatabaseAndSidecars(t *testing.T) {
	vault := t.TempDir()
	dbPath := filepath.Join(vault, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		require.NoError(t, os.WriteFile(path, []byte("stale"), 0o644))
	}

	require.NoError(t, PrepareFreshRebuild(vault, filepath.Join(vault, ".rhizome", "index.lock")))

	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, "explicit rebuild must clobber %s", path)
	}
}
