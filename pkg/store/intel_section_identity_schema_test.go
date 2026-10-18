package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntelSectionIdentityUpgradePreservesSectionsAndCanonicalTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	old, err := openWithOptionsAtSchemaVersion(path, OpenOptions{}, 76)
	require.NoError(t, err)
	// These are persisted pre-upgrade rows, including duplicate headings,
	// an authored block target, and a headingless note root.
	fixtures := []struct {
		id, path, title, body, want string
		start                       int64
	}{
		{"duplicate-2", "notes/a.md", "Decision", "## Decision\nsecond", "notes/a.md#decision-80", 80},
		{"duplicate-1", "notes/a.md", "Decision", "## Decision\nfirst", "notes/a.md#decision-20", 20},
		{"authored", "notes/b.md", "Decision", "## Decision\n^decision\nbody", "notes/b.md#^decision", 50},
		{"root", "notes/c.md", "c.md", "headingless body", "", 0},
	}
	for _, f := range fixtures {
		_, err = old.db.ExecContext(t.Context(), `INSERT INTO intel_doc_sections(section_id,path,title,level,start_byte,end_byte,content,fingerprint,updated_at) VALUES(?,?,?,2,?,?,?,'fp',123)`, f.id, f.path, f.title, f.start, f.start+int64(len(f.body)), f.body)
		require.NoError(t, err)
	}
	require.NoError(t, old.Close())
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	for _, f := range fixtures {
		var nodeID, body string
		var updatedAt int64
		require.NoError(t, store.db.QueryRowContext(t.Context(), `SELECT source_node_id,content,updated_at FROM intel_doc_sections WHERE section_id = ?`, f.id).Scan(&nodeID, &body, &updatedAt))
		require.Equal(t, f.want, nodeID)
		require.Equal(t, f.body, body)
		require.EqualValues(t, 123, updatedAt)
	}
	var first, second int
	require.NoError(t, store.db.QueryRowContext(t.Context(), `SELECT ord FROM intel_doc_sections WHERE section_id = 'duplicate-1'`).Scan(&first))
	require.NoError(t, store.db.QueryRowContext(t.Context(), `SELECT ord FROM intel_doc_sections WHERE section_id = 'duplicate-2'`).Scan(&second))
	require.Equal(t, 0, first)
	require.Equal(t, 1, second)
}
