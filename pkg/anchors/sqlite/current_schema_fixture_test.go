package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

var currentSchemaTestDB struct {
	sync.Once
	data []byte
	err  error
}

func currentSchemaTestDBPath(t *testing.T, name string) string {
	t.Helper()
	currentSchemaTestDB.Do(func() {
		dir := t.TempDir()
		source := filepath.Join(dir, "current-schema-source.db")
		store, err := Open(source)
		if err != nil {
			currentSchemaTestDB.err = err
			return
		}
		if err := store.Close(); err != nil {
			currentSchemaTestDB.err = err
			return
		}

		snapshot := filepath.Join(dir, "current-schema-snapshot.db")
		if err := sqliteutil.SnapshotInto(context.Background(), source, snapshot); err != nil {
			currentSchemaTestDB.err = err
			return
		}
		currentSchemaTestDB.data, currentSchemaTestDB.err = os.ReadFile(snapshot)
	})
	require.NoError(t, currentSchemaTestDB.err)

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, currentSchemaTestDB.data, 0o600))
	return path
}
