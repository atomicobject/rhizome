package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestNeighborhoodFiltersCanonicalTargetIdentityBeforePaging(t *testing.T) {
	ctx := context.Background()
	section := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindSection, Fragment: "Details", NodeID: "host.md#Details", TypeName: "DetailsSection"}
	embedded := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, Fragment: "^story", NodeID: "host.md#^story", TypeName: "Story"}
	refs := []ontology.NodeRef{section, embedded}
	for _, direction := range []TraversalDirection{TraversalDirectionOutbound, TraversalDirectionInbound} {
		for _, target := range refs {
			t.Run(string(direction)+"/"+string(target.Kind), func(t *testing.T) {
				edges := []semdb.OntologyEdgeRow{}
				for _, ref := range refs {
					row := semdb.OntologyEdgeRow{SrcPath: "source.md", DstPath: ref.NotePath, DstNodeID: ref.NodeID, RelationName: "related", Structural: true}
					if direction == TraversalDirectionInbound {
						row.SrcPath, row.DstPath, row.SrcNodeID, row.DstNodeID = ref.NotePath, "source.md", ref.NodeID, ""
					}
					edges = append(edges, row)
				}
				scope := neighborhoodCatalogScope(t, refs, edges)
				result, err := scope.Neighborhood(ctx, NeighborhoodRequest{
					Sources:   []ontology.NodeRef{{NotePath: "source.md", Kind: ontology.NodeKindNote}},
					Direction: direction, IncludeStructural: true, TargetKinds: []ontology.NodeKind{target.Kind},
					TargetTypes: []string{target.TypeName}, FirstPerSource: 1,
				})
				require.NoError(t, err)
				require.Len(t, result.Edges, 1)
				require.Equal(t, target, result.Edges[0].Target)
				require.Equal(t, target.TypeName, result.Edges[0].TargetType)
				require.False(t, result.Sources[0].Truncated)
			})
		}
	}
}

func TestRelationCountsFiltersCanonicalSectionSources(t *testing.T) {
	ctx := context.Background()
	canonical := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindSection, Fragment: "Details", NodeID: "host.md#Details", TypeName: "DetailsSection"}
	alias := ontology.NodeRef{NotePath: "host.md", Fragment: "Details"}
	scope := neighborhoodCatalogScope(t, []ontology.NodeRef{canonical}, []semdb.OntologyEdgeRow{
		{SrcPath: canonical.NotePath, SrcNodeID: canonical.NodeID, DstPath: "target.md", RelationName: "related", Structural: true},
	})
	for _, kind := range []ontology.NodeKind{ontology.NodeKindSection, ontology.NodeKindEmbedded} {
		for _, explicit := range []bool{false, true} {
			request := RelationCountsRequest{Sources: []ontology.NodeRef{alias}, SourceKinds: []ontology.NodeKind{kind}, Direction: TraversalDirectionOutbound, IncludeStructural: true}
			if explicit {
				request.Selections = []RelationSelection{{Sources: []ontology.NodeRef{alias}, Direction: TraversalDirectionOutbound, IncludeStructural: true}}
			}
			listed, err := scope.Edges(ctx, NeighborhoodRequest{Sources: []ontology.NodeRef{alias}, SourceKinds: []ontology.NodeKind{kind}, Direction: TraversalDirectionOutbound, IncludeStructural: true})
			require.NoError(t, err)
			counts, err := scope.RelationCounts(ctx, request)
			require.NoError(t, err)
			require.Len(t, counts.Sources, len(listed.Sources))
			if kind == ontology.NodeKindSection {
				require.Equal(t, NormalizeRef(alias), counts.Sources[0].Source)
				require.Equal(t, 1, counts.Sources[0].Count)
			} else {
				require.Empty(t, counts.Sources)
			}
		}
	}
}

func TestRelationSelectionsFilterCanonicalSourceTypes(t *testing.T) {
	ctx := context.Background()
	canonical := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, Fragment: "^story", NodeID: "host.md#^story", TypeName: "Story"}
	alias := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, Fragment: "^story"}
	scope := neighborhoodCatalogScope(t, []ontology.NodeRef{canonical}, []semdb.OntologyEdgeRow{
		{SrcPath: canonical.NotePath, SrcNodeID: canonical.NodeID, DstPath: "target.md", RelationName: "related", Structural: true},
	})
	selection := RelationSelection{Sources: []ontology.NodeRef{alias}, SourceTypes: []string{"Story"}, Direction: TraversalDirectionOutbound, IncludeStructural: true}
	listed, err := scope.Execute(ctx, NodeReadPlan{Relations: []RelationSelection{selection}})
	require.NoError(t, err)
	require.Len(t, listed.Edges, 1)
	counts, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: []ontology.NodeRef{alias}, Selections: []RelationSelection{selection}})
	require.NoError(t, err)
	require.Len(t, counts.Sources, 1)
	require.Equal(t, 1, counts.Sources[0].Count)
}
