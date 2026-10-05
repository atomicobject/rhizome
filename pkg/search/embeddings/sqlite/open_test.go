package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

func TestOpenAppliesPragmasToBothConnections(t *testing.T) {
	ctx := context.Background()
	store, err := Open(t.TempDir()+"/emb.db", 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	store.db.SetMaxOpenConns(2)

	conn1, err := store.db.Conn(ctx)
	require.NoError(t, err)
	defer conn1.Close()

	var mode1 string
	require.NoError(t, conn1.QueryRowContext(ctx, `PRAGMA journal_mode;`).Scan(&mode1))
	require.Equal(t, "wal", strings.ToLower(mode1))

	var busy1 int
	require.NoError(t, conn1.QueryRowContext(ctx, `PRAGMA busy_timeout;`).Scan(&busy1))
	require.GreaterOrEqual(t, busy1, 5000)

	var fk1 int
	require.NoError(t, conn1.QueryRowContext(ctx, `PRAGMA foreign_keys;`).Scan(&fk1))
	require.Equal(t, 1, fk1)

	conn2, err := store.db.Conn(ctx)
	require.NoError(t, err)
	defer conn2.Close()

	var mode2 string
	require.NoError(t, conn2.QueryRowContext(ctx, `PRAGMA journal_mode;`).Scan(&mode2))
	require.Equal(t, "wal", strings.ToLower(mode2))
	var busy2 int
	require.NoError(t, conn2.QueryRowContext(ctx, `PRAGMA busy_timeout;`).Scan(&busy2))
	require.GreaterOrEqual(t, busy2, 5000)
	var fk2 int
	require.NoError(t, conn2.QueryRowContext(ctx, `PRAGMA foreign_keys;`).Scan(&fk2))
	require.Equal(t, 1, fk2)
}

func TestOpen_ResetsDomainOnSchemaError(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "broken-emb.db")

	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	// Shadow a table name with a view so EnsureSchema hits an index-on-view error.
	_, err = db.ExecContext(ctx, `CREATE VIEW emb_chunk_embeddings AS SELECT 1 AS id;`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(dbPath, 4)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	var typ string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT type FROM sqlite_master WHERE name = 'emb_chunk_embeddings'`).Scan(&typ))
	require.Equal(t, "table", typ)
}

func TestOpenWithDB_DoesNotCloseSharedDB(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	db, err := sqliteutil.OpenDSN(sqliteutil.DSN(dbPath), sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store, err := OpenWithDB(db, 4)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	require.NoError(t, db.PingContext(ctx))
}
