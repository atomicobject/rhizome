package codeanchor_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

var currentSchemaAnchorTestDB struct {
	sync.Once
	data []byte
	err  error
}

func currentSchemaAnchorTestDBPath(t *testing.T, path string) string {
	t.Helper()
	currentSchemaAnchorTestDB.Do(func() {
		dir := t.TempDir()
		source := filepath.Join(dir, "current-schema-source.db")
		store, err := semdb.Open(source)
		if err != nil {
			currentSchemaAnchorTestDB.err = err
			return
		}
		if err := store.Close(); err != nil {
			currentSchemaAnchorTestDB.err = err
			return
		}

		snapshot := filepath.Join(dir, "current-schema-snapshot.db")
		if err := sqliteutil.SnapshotInto(context.Background(), source, snapshot); err != nil {
			currentSchemaAnchorTestDB.err = err
			return
		}
		currentSchemaAnchorTestDB.data, currentSchemaAnchorTestDB.err = os.ReadFile(snapshot)
	})
	require.NoError(t, currentSchemaAnchorTestDB.err)
	require.NoError(t, os.WriteFile(path, currentSchemaAnchorTestDB.data, 0o600))
	return path
}
