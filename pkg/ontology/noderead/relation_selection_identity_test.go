package noderead

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestExecuteAndCountsSelectionsKeepEmbeddedAndNoteSourcesExact(t *testing.T) {
	ctx := context.Background()
	for _, structural := range []bool{true, false} {
		name := "ambient"
		if structural {
			name = "structural"
		}
		t.Run(name, func(t *testing.T) {
			embedded := ontology.NodeRef{NotePath: "source.md", Kind: ontology.NodeKindEmbedded, Fragment: "^story", NodeID: "source.md#^story"}
			scope := neighborhoodCatalogScope(t, []ontology.NodeRef{embedded}, []semdb.OntologyEdgeRow{
				{SrcPath: "source.md", SrcNodeID: embedded.NodeID, RelationName: "related", DstPath: "a-target.md", Structural: structural},
				{SrcPath: "source.md", SrcNodeID: "source.md#^other", RelationName: "related", DstPath: "b-target.md", Structural: structural},
				{SrcPath: "source.md", SrcNodeID: "source.md#^third", RelationName: "related", DstPath: "c-target.md", Structural: structural},
				{SrcPath: "source.md", RelationName: "related", DstPath: "z-target.md", Structural: structural},
			})
			for _, test := range []struct {
				name   string
				source ontology.NodeRef
				target string
			}{
				{"embedded", embedded, "a-target.md"},
				{"note root", ontology.NodeRef{NotePath: "source.md", Kind: ontology.NodeKindNote, NodeID: "root-catalog"}, "z-target.md"},
			} {
				t.Run(test.name, func(t *testing.T) {
					selection := RelationSelection{
						Name: "related", Sources: []ontology.NodeRef{test.source}, Direction: TraversalDirectionOutbound,
						RelationNames: []string{"related"}, IncludeStructural: structural, IncludeAmbient: !structural, FirstPerSource: 1,
					}
					result, err := scope.Execute(ctx, NodeReadPlan{Relations: []RelationSelection{selection}})
					require.NoError(t, err)
					require.Len(t, result.Edges, 1)
					require.Equal(t, test.target, result.Edges[0].Target.NotePath)
					require.Len(t, result.Groups, 1)
					require.Equal(t, result.Edges, result.Groups[0].Edges)
					require.False(t, result.Groups[0].Truncated)

					counts, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: []ontology.NodeRef{test.source}, Selections: []RelationSelection{selection}})
					require.NoError(t, err)
					require.Equal(t, 1, counts.BySource[RefIdentityKey(test.source)].Count)
				})
			}
		})
	}
}

func TestRelationCountsDoesNotRestoreSourcesExcludedByKind(t *testing.T) {
	source := ontology.NodeRef{NotePath: "source.md", Kind: ontology.NodeKindNote}
	scope := neighborhoodCatalogScope(t, nil, []semdb.OntologyEdgeRow{{SrcPath: source.NotePath, DstPath: "target.md", Structural: true}})
	for _, explicit := range []bool{false, true} {
		request := RelationCountsRequest{Sources: []ontology.NodeRef{source}, SourceKinds: []ontology.NodeKind{ontology.NodeKindEmbedded}}
		if explicit {
			request.Selections = []RelationSelection{{Sources: []ontology.NodeRef{source}, IncludeStructural: true}}
		}
		result, err := scope.RelationCounts(context.Background(), request)
		require.NoError(t, err)
		require.Empty(t, result.Sources)
		require.Empty(t, result.BySource)
	}
}

func TestRelationCountsSelectionFiltersConcreteSourceTypes(t *testing.T) {
	ctx := context.Background()
	sources := []ontology.NodeRef{
		{NotePath: "included.md", Kind: ontology.NodeKindNote, TypeName: "Included"},
		{NotePath: "excluded.md", Kind: ontology.NodeKindNote, TypeName: "Excluded"},
	}
	scope := neighborhoodCatalogScope(t, nil, []semdb.OntologyEdgeRow{
		{SrcPath: sources[0].NotePath, RelationName: "related", DstPath: "target.md", Structural: true},
		{SrcPath: sources[1].NotePath, RelationName: "related", DstPath: "other.md", Structural: true},
	})
	for _, explicit := range []bool{false, true} {
		selection := RelationSelection{SourceTypes: []string{"Included"}, RelationNames: []string{"related"}, IncludeStructural: true, Direction: TraversalDirectionOutbound}
		if explicit {
			selection.Sources = sources
		}
		result, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: sources, Selections: []RelationSelection{selection}})
		require.NoError(t, err)
		require.Equal(t, 1, result.BySource[RefIdentityKey(sources[0])].Count)
		require.Zero(t, result.BySource[RefIdentityKey(sources[1])].Count)
	}
}

func TestExecuteSubtreeGroupsEdgesForEmbeddedSourceAliases(t *testing.T) {
	ctx := context.Background()
	source := ontology.NodeRef{NotePath: "source.md", Kind: ontology.NodeKindEmbedded, Fragment: "^story", NodeID: "source.md#^story"}
	scope := neighborhoodCatalogScope(t, []ontology.NodeRef{source}, []semdb.OntologyEdgeRow{
		{SrcPath: source.NotePath, SrcNodeID: source.NodeID, RelationName: "related", DstPath: "target.md", Provenance: "section_neighbor", Structural: true},
	})
	alias := ontology.NodeRef{NotePath: source.NotePath, Kind: source.Kind, Fragment: source.Fragment}
	selection := RelationSelection{Name: "related", Sources: []ontology.NodeRef{alias}, RelationNames: []string{"related"}, Direction: TraversalDirectionOutbound, NeighborScope: ontoschema.NeighborScopeSubtree}
	result, err := scope.Execute(ctx, NodeReadPlan{Relations: []RelationSelection{selection}})
	require.NoError(t, err)
	require.Len(t, result.Edges, 1)
	require.Len(t, result.Groups, 1)
	require.Equal(t, source, result.Groups[0].Source)
	require.Equal(t, result.Edges, result.Groups[0].Edges)

	counts, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: []ontology.NodeRef{alias}, Selections: []RelationSelection{selection}})
	require.NoError(t, err)
	require.Equal(t, 1, counts.BySource[RefIdentityKey(alias)].Count)
}
