package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopeNeighborhoodEmbeddedEndpointsDoNotListVault(t *testing.T) {
	ctx := context.Background()
	vault, store, _ := buildFixture(t, expansionCatalogSchema, "# Product\n\n## Items\n\n- First item ^first\n  owner:: [[people/alice]]\n  target:: [[destinations/one]]\n")
	writeFixtureNote(t, vault.Path, "people/alice.md", "# Alice\n")
	writeFixtureNote(t, vault.Path, "destinations/one.md", "# One\n")
	runtime, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	schema := runtime.Schema
	items, err := NewService(vault, nil, store, schema).NewScope(ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "TaskItem"})
	require.NoError(t, err)
	require.Len(t, items.Items, 1)
	for _, entry := range []string{"Neighborhood", "RelationCounts", "Execute"} {
		t.Run(entry, func(t *testing.T) {
			reader := &countingNoteReader{Note: &obsidian.Note{}}
			scope := NewService(vault, reader, store, schema).NewScope(ScopeOptions{})
			refs := []ontology.NodeRef{items.Items[0].Ref}
			switch entry {
			case "Neighborhood":
				result, err := scope.Neighborhood(ctx, NeighborhoodRequest{Sources: refs, Direction: TraversalDirectionOutbound})
				require.NoError(t, err)
				require.NotEmpty(t, result.Edges)
			case "RelationCounts":
				result, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: refs, Direction: TraversalDirectionOutbound})
				require.NoError(t, err)
				require.NotZero(t, result.Sources[0].Count)
			case "Execute":
				result, err := scope.Execute(ctx, NodeReadPlan{Hydrate: HydrateOptions{Profile: HydrateSummary}, Roots: []NodeReadRoot{{Ref: refs[0]}}, Relations: []RelationSelection{{Direction: TraversalDirectionOutbound}}})
				require.NoError(t, err)
				require.NotEmpty(t, result.Roots)
			}
			t.Logf("source reads: contents=%d lists=%d", reader.contents, reader.lists)
			require.Zero(t, reader.contents)
			require.Zero(t, reader.lists)
		})
	}
}
