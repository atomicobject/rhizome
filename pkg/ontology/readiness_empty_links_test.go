package ontology

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSyncPublishesEmptyRequiredLinkList(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) { name: String }
type Project @node(paths: ["notes/*.md"]) { name: String! owners: [Person!]! @link }
`)
	writeOntologyNote(t, root, "people/owner.md", "---\nname: Owner\n---\n")
	writeOntologyNote(t, root, "notes/empty.md", "---\nname: Empty\nowners: []\n---\n")
	writeOntologyNote(t, root, "notes/assigned.md", "---\nname: Assigned\nowners: ['[[people/owner]]']\n---\n")
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	indexer := testkit.NoteMetadataIndexer(t)
	vault := obsidian.VaultDefinition{Path: root}
	reader := &obsidian.Note{}
	_, err = indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	queue := &recordingDeltaQueue{}
	result, err := SyncPublishedPaths(ctx, indexer, vault, reader, store, queue, nil, nil)
	require.NoError(t, err)
	require.Empty(t, result.Issues, "an explicitly empty link list is valid authored content")
	require.Len(t, queue.deltas, 1)
	require.NoError(t, store.ApplyOntologyDelta(ctx, queue.deltas[0]), "valid authored content must be publishable")
	assertReady := func(want bool) {
		t.Helper()
		runtime, err := PublishedRuntimeWithStore(ctx, indexer, vault, store)
		require.NoError(t, err)
		require.Equal(t, want, runtime.Ready)
	}
	assertReady(true)
	result, err = syncPublishedPathsForTest(ctx, indexer, vault, reader, store, nil, nil, nil)
	require.NoError(t, err)
	require.False(t, result.Dirty, "accepted emptiness must not trigger repeated repair")

	for _, tc := range []struct {
		name     string
		mutation string
	}{
		{"missing scalar row", `DELETE FROM ontology_node_field_values WHERE note_path='notes/empty.md' AND field_name='name'`},
		{"missing nonempty link list row", `DELETE FROM ontology_node_field_values WHERE note_path='notes/assigned.md' AND field_name='owners'`},
		{"corrupt nonempty link list row", `UPDATE ontology_node_field_values SET value_text='Corrupt',value_norm='corrupt' WHERE note_path='notes/assigned.md' AND field_name='owners'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed, err := store.DB().ExecContext(ctx, tc.mutation)
			require.NoError(t, err)
			count, err := changed.RowsAffected()
			require.NoError(t, err)
			require.Positive(t, count, "the fixture must damage a required field row")
			assertReady(false)
			result, err := syncPublishedPathsForTest(ctx, indexer, vault, reader, store, nil, nil, nil)
			require.NoError(t, err)
			require.True(t, result.Dirty)
			require.Empty(t, result.Issues)
			assertReady(true)
			assertOntologyBuildMatchesStore(t, ctx, vault, reader, store)
		})
	}

	writeOntologyNote(t, root, "notes/empty.md", "---\nname: Missing owners\n---\n")
	result, err = ensureIndexedForTest(ctx, indexer, vault, reader, store)
	require.NoError(t, err)
	require.Len(t, result.Issues, 1)
	require.Equal(t, "missing_required_field", result.Issues[0].Code)
	require.Equal(t, "owners", result.Issues[0].FieldName)
	assertReady(true)
}
