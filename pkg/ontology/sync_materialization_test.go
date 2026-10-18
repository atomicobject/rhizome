package ontology

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestOntologyRejectsFutureMaterializationWithoutMutation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := codeanchorsqlite.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	want := codeanchorsqlite.OntologySchemaState{
		SchemaHash: "future-schema", NotesHash: "future-notes",
		MaterializationVersion: OntologyMaterializationVersion + 1,
		LoadedAt:               17, Ready: true, ErrorJSON: `[{"code":"future"}]`,
	}
	require.NoError(t, store.UpsertOntologySchemaState(ctx, want))
	_, err = SyncPublishedPaths(ctx, testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, failingNoteReader{}, store, nil, nil, nil)
	require.ErrorIs(t, err, ErrFutureOntologyMaterialization)
	require.ErrorContains(t, err, "upgrade Rhizome")
	got, stateErr := store.GetOntologySchemaState(ctx)
	require.NoError(t, stateErr)
	require.Equal(t, want, got)
}

func TestPublishedRuntimeWithStoreDoesNotTrustUncertifiedReadyState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	store, err := codeanchorsqlite.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertOntologySchemaState(ctx, codeanchorsqlite.OntologySchemaState{
		SchemaHash:             schema.Hash,
		NotesHash:              "notes",
		MaterializationVersion: OntologyMaterializationVersion,
		LoadedAt:               1,
		Ready:                  true,
	}))

	runtime, err := PublishedRuntimeWithStore(ctx, testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, store)
	require.NoError(t, err)
	require.False(t, runtime.Ready)
	require.Nil(t, runtime.Schema)
}

func TestOntologyRebuildsIncompleteMaterialization(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	noteState, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, codeanchorsqlite.OntologySnapshot{
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash:             schema.Hash,
			NotesHash:              noteState.NotesHash,
			MaterializationVersion: OntologyMaterializationVersion,
			LoadedAt:               noteState.LoadedAt,
			Ready:                  true,
		},
	}))

	result, err := syncPublishedPathsForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	require.Equal(t, 1, result.Assessed)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	var embeddedTypes []string
	for _, node := range nodes {
		if node.NodeKind == "EMBEDDED" {
			embeddedTypes = append(embeddedTypes, node.TypeName)
		}
	}
	require.Equal(t, []string{"ActionItem"}, embeddedTypes)
	states, err := store.OntologyNoteStatesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	require.NotEmpty(t, states["notes/pizza-party-2026.md"].InputFingerprint)
}

func TestEnsureIndexedRebuildsWhenOntologyNodesAreMissingButStateIsCurrent(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	first, err := ensureIndexedForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, first.Dirty)

	states, err := store.OntologyNoteStatesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	require.NotEmpty(t, states["notes/pizza-party-2026.md"].InputFingerprint)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, readmodel.NodeReadModel{
		NotePaths: []string{"notes/pizza-party-2026.md"},
	}))

	result, err := ensureIndexedForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	var embeddedTypes []string
	for _, node := range nodes {
		if node.NodeKind == "EMBEDDED" {
			embeddedTypes = append(embeddedTypes, node.TypeName)
		}
	}
	require.Equal(t, []string{"ActionItem"}, embeddedTypes)
}

func TestEnsureIndexedRebuildsWhenOntologyNodeFieldRowsAreMissingButStateIsCurrent(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	first, err := ensureIndexedForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, first.Dirty)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.NodeID)
	}
	fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	require.NotEmpty(t, fields)

	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, readmodel.NodeReadModel{
		NotePaths: []string{"notes/pizza-party-2026.md"},
		Nodes:     nodes,
	}))
	fields, err = store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	require.Empty(t, fields)

	result, err := ensureIndexedForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	fields, err = store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	require.NotEmpty(t, fields)
}
