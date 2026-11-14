package ontology

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
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
	assertReady := func(t *testing.T, want bool) {
		t.Helper()
		runtime, err := PublishedRuntimeWithStore(ctx, indexer, vault, store)
		require.NoError(t, err)
		require.Equal(t, want, runtime.Ready)
	}
	assertReady(t, true)
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
			assertReady(t, false)
			result, err := syncPublishedPathsForTest(ctx, indexer, vault, reader, store, nil, nil, nil)
			require.NoError(t, err)
			require.True(t, result.Dirty)
			require.Empty(t, result.Issues)
			assertReady(t, true)
			assertOntologyBuildMatchesStore(t, ctx, vault, reader, store)
		})
	}

	writeOntologyNote(t, root, "notes/empty.md", "---\nname: Missing owners\n---\n")
	result, err = ensureIndexedForTest(ctx, indexer, vault, reader, store)
	require.NoError(t, err)
	require.Len(t, result.Issues, 1)
	require.Equal(t, "missing_required_field", result.Issues[0].Code)
	require.Equal(t, "owners", result.Issues[0].FieldName)
	assertReady(t, true)
}

func TestRequiredFieldCompletenessNeedsMatchingEmptyLinkListEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		kind     ontoschema.FieldKind
		list     bool
		nodeKind NodeKind
		rootType string
		present  bool
		values   []string
		want     bool
	}{
		{"empty root link list", ontoschema.FieldKindLink, true, NodeKindNote, "Project", true, nil, true},
		{"absent root link list", ontoschema.FieldKindLink, true, NodeKindNote, "Project", false, nil, false},
		{"nonempty root link list", ontoschema.FieldKindLink, true, NodeKindNote, "Project", true, []string{"[[people/owner]]"}, false},
		{"empty scalar list", ontoschema.FieldKindScalar, true, NodeKindNote, "Project", true, nil, false},
		{"empty singular link", ontoschema.FieldKindLink, false, NodeKindNote, "Project", true, nil, false},
		{"different root type", ontoschema.FieldKindLink, true, NodeKindNote, "Other", true, nil, false},
		{"embedded link list", ontoschema.FieldKindLink, true, NodeKindEmbedded, "Project", true, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field := &ontoschema.Field{Name: "owners", Kind: tc.kind, List: tc.list, Required: true}
			schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{
				"Project": {Fields: []*ontoschema.Field{field}},
			}}
			node := readmodel.NodeRow{TypeName: "Project", NodeKind: string(tc.nodeKind)}
			assessment := &NoteAssessment{
				ResolvedType: tc.rootType,
				Fields:       []FieldAssessment{{Name: "owners", Present: tc.present, Values: tc.values}},
			}
			require.Equal(t, tc.want, ontologyNodeRequiredFieldRowsMaterialized(schema, node, nil, assessment))
		})
	}
}
