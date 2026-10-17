package indexstore

import (
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestReadOnlyConfiguredOpenPreservesIndexAndRejectsWrites(t *testing.T) {
	root := t.TempDir()
	cfg := codeanchor.DefaultConfig(root)
	cfg.IndexPath = filepath.Join(root, ".rhizome", "db.sqlite")
	writer, closeWriter, err := Open(root, Options{Access: ReadWrite, Create: true, Config: &cfg})
	require.NoError(t, err)
	require.NoError(t, writer.UpsertFileMeta(t.Context(), codeanchor.FileMeta{Path: "src/a.go", Lang: codeanchor.LangGo, Hash: "paid-source", ParseStatus: codeanchor.ParseOK}))
	closeWriter()
	observer, err := semdb.OpenReadOnlyExisting(cfg.IndexPath, t.Context(), sqliteutil.Options{})
	require.NoError(t, err)
	defer observer.Close()
	conn, err := observer.DB().Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	var before, after int
	require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA data_version").Scan(&before))
	reader, closeReader, err := Open(root, Options{Access: ReadOnly, Config: &cfg})
	require.NoError(t, err)
	require.NotNil(t, reader)
	paths, err := reader.IndexedFilePaths(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"src/a.go"}, paths)
	require.Error(t, reader.UpsertFileMeta(t.Context(), codeanchor.FileMeta{Path: "src/b.go", Lang: codeanchor.LangGo, Hash: "changed", ParseStatus: codeanchor.ParseOK}))
	closeReader()
	require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA data_version").Scan(&after))
	require.Equal(t, before, after)
}

func TestReadOnlyConfiguredOpenRejectsOlderProofWithoutMigration(t *testing.T) {
	root := t.TempDir()
	cfg := codeanchor.DefaultConfig(root)
	cfg.IndexPath = filepath.Join(root, "index.db")
	writer, closeWriter, err := Open(root, Options{Access: ReadWrite, Create: true, Config: &cfg})
	require.NoError(t, err)
	// Model a previously published schema version without running an upgrade.
	_, err = writer.DB().ExecContext(t.Context(), `UPDATE rzm_migration_state SET version=version-1 WHERE domain='intel'`)
	require.NoError(t, err)
	var beforeVersion, beforeLog int
	require.NoError(t, writer.DB().QueryRowContext(t.Context(), `SELECT version FROM rzm_migration_state WHERE domain='intel'`).Scan(&beforeVersion))
	require.NoError(t, writer.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM rzm_migration_log`).Scan(&beforeLog))
	reader, closeReader, err := Open(root, Options{Access: ReadOnly, Config: &cfg})
	require.ErrorContains(t, err, "rzm index")
	require.Nil(t, reader)
	require.Nil(t, closeReader)
	var afterVersion, afterLog int
	require.NoError(t, writer.DB().QueryRowContext(t.Context(), `SELECT version FROM rzm_migration_state WHERE domain='intel'`).Scan(&afterVersion))
	require.NoError(t, writer.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM rzm_migration_log`).Scan(&afterLog))
	require.Equal(t, beforeVersion, afterVersion)
	require.Equal(t, beforeLog, afterLog)
	closeWriter()
}

func TestReadOnlyConfiguredOpenLeavesMissingIndexAbsent(t *testing.T) {
	root := t.TempDir()
	cfg := codeanchor.DefaultConfig(root)
	cfg.IndexPath = filepath.Join(root, "absent", "db.sqlite")
	reader, cleanup, err := Open(root, Options{Access: ReadOnly, Config: &cfg})
	require.NoError(t, err)
	require.Nil(t, reader)
	require.Nil(t, cleanup)
	_, err = os.Stat(filepath.Dir(cfg.IndexPath))
	require.True(t, os.IsNotExist(err))
	for _, opts := range []Options{{Access: ReadOnly, Create: true}, {Access: ReadOnly, Repair: true}, {Access: ReadOnly, TxLockMode: "immediate"}, {}} {
		opts.Config = &cfg
		_, _, err = Open(root, opts)
		require.Error(t, err)
	}
	_, err = os.Stat(filepath.Dir(cfg.IndexPath))
	require.True(t, os.IsNotExist(err))
}
