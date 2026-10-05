package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

func TestOpenAppliesPragmasToAllConnections(t *testing.T) {
	ctx := context.Background()

	store, err := Open(t.TempDir() + "/code.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	store.db.SetMaxOpenConns(2)

	conn1, err := store.db.Conn(ctx)
	require.NoError(t, err)
	defer conn1.Close()

	var mode1 string
	require.NoError(t, conn1.QueryRowContext(ctx, `PRAGMA journal_mode;`).Scan(&mode1))
	require.Equal(t, "wal", strings.ToLower(mode1))
	for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 30000} {
		var got int
		require.NoError(t, conn1.QueryRowContext(ctx, `PRAGMA `+pragma).Scan(&got))
		require.Equal(t, want, got, pragma)
	}

	conn2, err := store.db.Conn(ctx)
	require.NoError(t, err)
	defer conn2.Close()

	var mode2 string
	require.NoError(t, conn2.QueryRowContext(ctx, `PRAGMA journal_mode;`).Scan(&mode2))
	require.Equal(t, "wal", strings.ToLower(mode2))
	for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 30000} {
		var got int
		require.NoError(t, conn2.QueryRowContext(ctx, `PRAGMA `+pragma).Scan(&got))
		require.Equal(t, want, got, pragma)
	}
}

func TestOpenWithDBDoesNotCloseSharedDB(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	db, err := sqliteutil.OpenDSN(sqliteutil.DSN(dbPath), sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store, err := OpenWithDB(db)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	require.NoError(t, db.PingContext(ctx))
}
