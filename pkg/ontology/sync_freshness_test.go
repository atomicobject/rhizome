package ontology

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureIndexed_UsesNoteMetadataVersionFromStore(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/project.md", `---
type: Project
name: Roadmap
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)
	require.True(t, state.Ready)

	err = store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash: "old",
			NotesHash:  "old",
			LoadedAt:   state.LoadedAt - 1,
			Ready:      true,
		},
	})
	require.NoError(t, err)

	result, err := ensureIndexedForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	updated, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.Equal(t, state.LoadedAt, updated.LoadedAt)

	typ, ok, err := store.GetOntologyTypeByPath(context.Background(), "notes/project.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Project", typ.TypeName)
}

func TestEnsureIndexed_ReusesSnapshotWhenOnlyLoadedAtChanges(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/project.md", `---
type: Project
name: Roadmap
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	noteState, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	_, err = ensureIndexedForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	err = store.UpsertOntologySchemaState(context.Background(), codeanchorsqlite.OntologySchemaState{
		SchemaHash:             schema.Hash,
		NotesHash:              noteState.NotesHash,
		MaterializationVersion: OntologyMaterializationVersion,
		LoadedAt:               noteState.LoadedAt - 1,
		Ready:                  true,
	})
	require.NoError(t, err)

	result, err := ensureIndexedForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.False(t, result.Dirty)
}

func TestEnsureIndexedRefreshesStaleReadyNoteMetadataBeforeOntologyReuse(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/existing.md", "# Existing\n\nNo actions yet.\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)

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
	states, err := store.OntologyNoteStatesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	require.NotEmpty(t, states["notes/pizza-party-2026.md"].InputFingerprint)
}

func TestSyncPaths_RebuildsOntologyWhenOnlySchemaChanges(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/item.md", `---
type: Project
name: Item
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologySchema(t, root, `
type Initiative @node(paths: ["notes/*.md"]) {
  name: String!
}
`)

	result, err := syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)
	require.True(t, result.Dirty)

	typ, ok, err := store.GetOntologyTypeByPath(context.Background(), "notes/item.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Initiative", typ.TypeName)
}

func TestSyncPathsSchemaRebuildRefreshesStaleReadyNoteMetadata(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/existing.md", "# Existing\n\nNo actions yet.\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
}
`)

	result, err := syncPathsForTest(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)

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

func TestSyncPaths_FullRebuildWithZeroNotesStillUpdatesSchemaState(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String
}
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: []codeanchorsqlite.OntologyNoteTypeRow{
			{NotePath: "notes/stale.md", TypeName: "Project", SchemaHash: "old", UpdatedAt: 1},
		},
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash: "old",
			NotesHash:  "old-notes",
			LoadedAt:   1,
			Ready:      true,
		},
	}))

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	result, err := syncPathsForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)
	require.True(t, result.Dirty)

	state, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, schema.Hash, state.SchemaHash)
	require.True(t, state.Ready)

	_, ok, err := store.GetOntologyTypeByPath(context.Background(), "notes/stale.md")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestEnsureIndexed_PureFrontmatterSchemaProducesNoteRootFieldRows(t *testing.T) {
	// Regression: pure-frontmatter schemas (no section fields, no global
	// sources) used to skip content hydration. Projection rebuilds
	// DocumentSnapshot from doc.Content, so an empty content yielded an empty
	// snapshot.Frontmatter and zero ontology_node_field_values rows for
	// note-root types — even though doc.Frontmatter held the values.
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum SpecStatus {
  proposed
  active
  retired
}

type Spec @node(paths: ["docs/specs/**/*.md"]) {
  status: SpecStatus @field
  lastUpdated: Date @field(source: "last-updated")
}
`)
	writeOntologyNote(t, root, "docs/specs/alpha.md", `---
type: Spec
status: active
last-updated: 2026-04-01
---
# Alpha
`)
	writeOntologyNote(t, root, "docs/specs/beta.md", `---
type: Spec
status: proposed
last-updated: 2026-05-01
---
# Beta
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ensureIndexedForTest(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	rows, err := store.OntologyNodesByType(context.Background(), "Spec")
	require.NoError(t, err)
	require.Len(t, rows, 2, "expected one catalog row per Spec note")

	nodeIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		require.Equal(t, string(NodeKindNote), row.NodeKind)
		nodeIDs = append(nodeIDs, row.NodeID)
	}

	fields, err := store.OntologyNodeFieldValuesByNodeIDs(context.Background(), nodeIDs, nil)
	require.NoError(t, err)
	require.Len(t, fields, 4, "expected status + lastUpdated rows for each Spec; got %d", len(fields))

	byNodeField := make(map[string]map[string]string)
	for _, row := range fields {
		if byNodeField[row.NodeID] == nil {
			byNodeField[row.NodeID] = map[string]string{}
		}
		byNodeField[row.NodeID][row.FieldName] = row.ValueNorm
	}
	require.Len(t, byNodeField, 2)
	for _, byField := range byNodeField {
		require.Contains(t, byField, "status")
		require.Contains(t, byField, "lastupdated")
	}
}
