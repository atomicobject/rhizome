package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkOpenExistingIndex(b *testing.B) {
	path := filepath.Join(b.TempDir(), "index.sqlite")
	store, err := Open(path)
	require.NoError(b, err)
	_, err = store.db.Exec(`CREATE TABLE integrity_workload(id INTEGER PRIMARY KEY, body BLOB);
		WITH RECURSIVE rows(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM rows WHERE n<16384)
		INSERT INTO integrity_workload SELECT n, zeroblob(4096) FROM rows;`)
	require.NoError(b, err)
	require.NoError(b, store.Close())
	b.ResetTimer()
	for range b.N {
		store, err := Open(path)
		require.NoError(b, err)
		require.NoError(b, store.Close())
	}
}
