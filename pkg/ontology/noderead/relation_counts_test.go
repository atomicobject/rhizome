package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestScopeRelationCountsUsesBatchedEdgesWithoutHydration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "decisions", DstPath: "notes/c.md", DstType: "Decision", Structural: true},
			{SrcPath: "notes/c.md", RelationName: "back", DstPath: "notes/a.md", DstType: "Project", Structural: false},
			{SrcPath: "notes/b.md", SrcNodeID: "story", RelationName: "owner", DstPath: "notes/a.md", DstType: "Project", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})

	result, err := scope.RelationCounts(ctx, RelationCountsRequest{
		Sources:           []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}, {NotePath: "notes/b.md", Kind: ontology.NodeKindNote}},
		Direction:         TraversalDirectionBoth,
		IncludeStructural: true,
		IncludeAmbient:    true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, scope.Diagnostics().EdgeLoads)
	require.Zero(t, scope.Diagnostics().RecordLoads)
	listed, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources:   []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}, {NotePath: "notes/b.md", Kind: ontology.NodeKindNote}},
		Direction: TraversalDirectionBoth, IncludeStructural: true, IncludeAmbient: true,
	})
	require.NoError(t, err)
	for _, source := range []string{"notes/a.md", "notes/b.md"} {
		targets := map[string]bool{}
		for _, edge := range listed.Edges {
			if edge.Source.NotePath == source {
				targets[RefIdentityKey(edge.Target)] = true
			}
		}
		require.Equal(t, len(targets), result.BySource[source].Count)
	}
}

func TestScopeRelationCountsMatchesUnboundedNeighborSelections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "efforts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "main.md"), []byte("# Main\n\n### Story ^story-a\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "efforts", "one.md"), []byte("# One\n\n[[main#^story-a]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "efforts", "two.md"), []byte("# Two\n"), 0o644))
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{}, Interfaces: map[string]*ontoschema.InterfaceType{}}
	decisions := &ontoschema.Field{Name: "decisions", Kind: ontoschema.FieldKindNeighbor, TypeName: "Decision", List: true, Direction: ontoschema.NeighborDirectionOutbound, Scope: ontoschema.NeighborScopeNote}
	efforts := &ontoschema.Field{Name: "efforts", Kind: ontoschema.FieldKindNeighbor, TypeName: "Effort", List: true, Direction: ontoschema.NeighborDirectionInbound, Scope: ontoschema.NeighborScopeSubtree}
	schema.Types["Project"] = &ontoschema.NoteType{Name: "Project", Role: ontoschema.TypeRoleNote, Fields: []*ontoschema.Field{decisions}, ByName: map[string]*ontoschema.Field{"decisions": decisions}}
	schema.Types["Decision"] = &ontoschema.NoteType{Name: "Decision", Role: ontoschema.TypeRoleNote, ByName: map[string]*ontoschema.Field{}}
	schema.Types["Person"] = &ontoschema.NoteType{Name: "Person", Role: ontoschema.TypeRoleNote, ByName: map[string]*ontoschema.Field{}}
	schema.Types["UserStory"] = &ontoschema.NoteType{Name: "UserStory", Role: ontoschema.TypeRoleEmbeddedNode, Fields: []*ontoschema.Field{efforts}, ByName: map[string]*ontoschema.Field{"efforts": efforts}}
	schema.Types["Effort"] = &ontoschema.NoteType{Name: "Effort", Role: ontoschema.TypeRoleNote, ByName: map[string]*ontoschema.Field{}}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{
			{NotePath: "projects/a.md", TypeName: "Project"},
			{NotePath: "projects/b.md", TypeName: "Project"},
			{NotePath: "decisions/one.md", TypeName: "Decision"},
			{NotePath: "people/alice.md", TypeName: "Person"},
			{NotePath: "efforts/one.md", TypeName: "Effort"},
			{NotePath: "efforts/two.md", TypeName: "Effort"},
		},
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "projects/a.md", RelationName: "decisions", DstPath: "decisions/one.md", DstType: "Decision", Provenance: "body_link"},
			{SrcPath: "projects/a.md", RelationName: "decisions", DstPath: "people/alice.md", DstType: "Person", Provenance: "body_link"},
			{SrcPath: "projects/b.md", RelationName: "decisions", DstPath: "decisions/one.md", DstType: "Decision", Provenance: "body_link"},
			{SrcPath: "efforts/two.md", RelationName: "stories", DstPath: "specs/main.md", DstNodeID: "specs/main.md#^story-a", DstType: "UserStory", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
	projects := []ontology.NodeRef{
		{NotePath: "projects/a.md", Kind: ontology.NodeKindNote, TypeName: "Project"},
		{NotePath: "projects/b.md", Kind: ontology.NodeKindNote, TypeName: "Project"},
	}
	projectSelection, ok := RelationSelectionForField(schema, decisions)
	require.True(t, ok)
	projectSelection.Sources = projects
	list, err := scope.executeSingleRelationSelection(ctx, projectSelection, TraverseLimits{})
	require.NoError(t, err)
	counts, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: projects, Selections: []RelationSelection{projectSelection}})
	require.NoError(t, err)
	require.Equal(t, len(list.Sources[0].Edges), counts.BySource[RefIdentityKey(projects[0])].Count)
	require.Equal(t, 1, counts.BySource[RefIdentityKey(projects[0])].Count)
	require.Equal(t, 1, counts.BySource[RefIdentityKey(projects[1])].Count)

	story := ontology.NodeRef{NotePath: "specs/main.md", Fragment: "^story-a", NodeID: "specs/main.md#^story-a", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory"}
	storySelection, ok := RelationSelectionForField(schema, efforts)
	require.True(t, ok)
	storySelection.Sources = []ontology.NodeRef{story}
	storyList, err := scope.executeSingleRelationSelection(ctx, storySelection, TraverseLimits{})
	require.NoError(t, err)
	storyCounts, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: []ontology.NodeRef{story}, Selections: []RelationSelection{storySelection}})
	require.NoError(t, err)
	require.Len(t, storyList.Edges, 2)
	require.Equal(t, len(storyList.Edges), storyCounts.BySource[RefIdentityKey(story)].Count)
}
