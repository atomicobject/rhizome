package ontology

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// recordingDeltaQueue captures submissions through the single durable
// writer lane so tests can prove SyncPaths routes both the assessment
// delta and the catalog read model through the queue.
type recordingDeltaQueue struct {
	store  *codeanchorsqlite.Store
	deltas []codeanchorsqlite.OntologyDelta
	models []codeanchor.IntelOntologyNodeReadModel
}

func TestSyncPaths_NoSchemaRoutesResetThroughQueue(t *testing.T) {
	root := t.TempDir()
	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	queue := &recordingDeltaQueue{}

	result, err := SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, queue, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, queue.deltas, 1)
	require.True(t, queue.deltas[0].FullRebuild)
}

func TestSyncPathsRequireExplicitNoteMetadataIndexerBeforeSchemaOrQueueMutation(t *testing.T) {
	for _, tt := range []struct {
		name      string
		schema    string
		published bool
	}{
		{name: "sync missing schema"},
		{name: "sync invalid schema", schema: "type Broken {"},
		{name: "published missing schema", published: true},
		{name: "published invalid schema", schema: "type Broken {", published: true},
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

			if tt.published {
				_, err = SyncPublishedPaths(ctx, notemeta.Indexer{}, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, queue, nil, nil)
			} else {
				_, err = SyncPaths(ctx, notemeta.Indexer{}, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, queue, nil, nil)
			}
			require.ErrorContains(t, err, "note format runtime is required")
			require.Empty(t, queue.deltas)
			require.Empty(t, queue.models)

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

func (q *recordingDeltaQueue) SubmitOntologyNodeReadModel(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel) error {
	q.models = append(q.models, model)
	if q.store == nil {
		return nil
	}
	return q.store.ReplaceOntologyNodeReadModel(ctx, model)
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
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	queue := &recordingDeltaQueue{store: store}
	result, err := SyncPaths(ctx, testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, queue, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt, "first sync should be a full rebuild")
	require.NotEmpty(t, queue.deltas, "expected ontology delta routed via queue")
	require.NotEmpty(t, queue.deltas[0].ReadModels, "catalog must be published atomically with the queued delta")

	// Catalog rows should land in the durable store via the queue's apply hook.
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"people/Alice.md", "projects/one.md"})
	require.NoError(t, err)
	require.NotEmpty(t, nodes, "queue submission must reach the catalog")
}

// SyncPaths must NOT increment ontology.field_rows_planned. The queue's
// SubmitOntologyNodeReadModel is the single durable recording site so
// every queued submission counts exactly once. A fake queue here records
// nothing, so the absence of `field_rows_planned` in the perf summary
// proves sync.go did not double-count.
func TestSyncPaths_QueueIsSoleFieldRowsPlannedRecorder(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	queue := &recordingDeltaQueue{store: store}
	_, err = SyncPaths(ctx, testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, queue, nil, nil)
	require.NoError(t, err)

	summary := collector.RenderSummary()
	require.NotContains(t, summary, "field_rows_planned")
}
