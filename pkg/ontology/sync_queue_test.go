package ontology

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

// recordingDeltaQueue captures submissions through the single durable
// writer lane so tests can inspect one atomic assessment and catalog delta.
type recordingDeltaQueue struct {
	store  *codeanchorsqlite.Store
	deltas []codeanchorsqlite.OntologyDelta
}

func TestSyncPaths_NoSchemaRoutesResetThroughQueue(t *testing.T) {
	root := t.TempDir()
	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	queue := &recordingDeltaQueue{}

	result, err := syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, queue, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, queue.deltas, 1)
	require.True(t, queue.deltas[0].FullRebuild)
}

func TestSyncPublishedPathsRequiresMetadataBeforeSchemaOrQueueMutation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
	}{
		{name: "missing schema"},
		{name: "invalid schema", schema: "type Broken {"},
		{name: "valid schema", schema: `type Project @node(paths: ["projects/*.md"]) {name: String}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			if tt.schema != "" {
				writeOntologySchema(t, root, tt.schema)
			}
			store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			ctx := context.Background()
			require.NoError(t, store.ReplaceOntologySnapshot(ctx, codeanchorsqlite.OntologySnapshot{
				SchemaState: codeanchorsqlite.OntologySchemaState{
					SchemaHash:             "sentinel-schema",
					NotesHash:              "sentinel-notes",
					MaterializationVersion: OntologyMaterializationVersion,
					LoadedAt:               1,
					Ready:                  true,
				},
			}))
			beforeMetadata, err := store.GetNoteMetadataState(ctx)
			require.NoError(t, err)
			beforeOntology, err := store.GetOntologySchemaState(ctx)
			require.NoError(t, err)
			queue := &recordingDeltaQueue{store: store}

			_, err = SyncPublishedPaths(ctx, notemeta.Indexer{}, obsidian.VaultDefinition{Path: root}, failingNoteReader{}, store, queue, nil, nil)
			require.ErrorContains(t, err, "note format runtime is required")
			require.Empty(t, queue.deltas)

			afterMetadata, err := store.GetNoteMetadataState(ctx)
			require.NoError(t, err)
			afterOntology, err := store.GetOntologySchemaState(ctx)
			require.NoError(t, err)
			require.Equal(t, beforeMetadata, afterMetadata)
			require.Equal(t, beforeOntology, afterOntology)
		})
	}
}

func (q *recordingDeltaQueue) SubmitOntologyDelta(ctx context.Context, delta codeanchorsqlite.OntologyDelta) error {
	q.deltas = append(q.deltas, delta)
	if q.store == nil {
		return nil
	}
	return q.store.ApplyOntologyDelta(ctx, delta)
}

func TestSyncPaths_RoutesCatalogWriteThroughQueue(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
}

type Project @node(paths: ["projects/*.md"]) {
  name: String!
  owner: Person @link
}
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
---
`)
	writeOntologyNote(t, root, "projects/one.md", `---
type: Project
name: One
owner: people/Alice.md
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	queue := &recordingDeltaQueue{store: store}
	result, err := syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, queue, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt, "first sync should be a full rebuild")
	require.NotEmpty(t, queue.deltas, "expected ontology delta routed via queue")
	require.NotEmpty(t, queue.deltas[0].ReadModels, "catalog must be published atomically with the queued delta")

	// Catalog rows should land in the durable store via the queue's apply hook.
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"people/Alice.md", "projects/one.md"})
	require.NoError(t, err)
	require.NotEmpty(t, nodes, "queue submission must reach the catalog")
}

func TestSyncPublishedPathsRequiresDurableQueueBeforeMutation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := codeanchorsqlite.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	_, err = SyncPublishedPaths(ctx, testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, failingNoteReader{}, store, nil, nil, nil)
	require.ErrorContains(t, err, "requires a durable write queue")
	state, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Empty(t, state)
}
