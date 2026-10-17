package noderead

import (
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestScopeRelationCacheReusesBatchedEndpointPartitions(t *testing.T) {
	root := ontology.NodeRef{NotePath: "note.md", Kind: ontology.NodeKindNote}
	child := ontology.NodeRef{NotePath: "note.md", NodeID: "note.md#^child", Fragment: "^child", Kind: ontology.NodeKindEmbedded, TypeName: "Item"}
	peer := ontology.NodeRef{NotePath: "peer.md", Kind: ontology.NodeKindNote}
	empty := ontology.NodeRef{NotePath: "empty.md", Kind: ontology.NodeKindNote}
	for _, direction := range []TraversalDirection{TraversalDirectionOutbound, TraversalDirectionInbound, TraversalDirectionBoth} {
		for _, limit := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/limit%d", direction, limit), func(t *testing.T) {
				scope := neighborhoodCatalogScope(t, []ontology.NodeRef{child}, []semdb.OntologyEdgeRow{
					{SrcPath: "note.md", DstPath: "peer.md", RelationName: "related", Provenance: "body_link"},
					{SrcPath: "note.md", SrcNodeID: "note.md#^child", DstPath: "peer.md", RelationName: "related", Provenance: "body_link"},
					{SrcPath: "peer.md", DstPath: "note.md", RelationName: "related", Provenance: "backlink"},
					{SrcPath: "peer.md", DstPath: "note.md", DstNodeID: "note.md#^child", RelationName: "related", Provenance: "body_link"},
					{SrcPath: "note.md", DstPath: "note.md", RelationName: "related", Provenance: "body_link"},
				})
				store := &queryRelationBatchStore{ReadStore: scope.service.Store}
				scope.service.Store = store
				request := NeighborhoodRequest{
					Sources: []ontology.NodeRef{root, child, peer, empty}, Direction: direction,
					RelationNames: []string{"related", "other", "related"}, IncludeAmbient: true, FirstPerSource: limit,
				}
				batch, err := scope.Edges(t.Context(), request)
				require.NoError(t, err)
				require.Len(t, batch.Sources, 4)
				require.Empty(t, batch.Sources[3].Edges)
				if limit == 0 {
					rootCount, childCount := 2, 1
					if direction == TraversalDirectionBoth {
						rootCount, childCount = 4, 2
					}
					require.Len(t, batch.Sources[0].Edges, rootCount)
					require.Len(t, batch.Sources[1].Edges, childCount)
				}
				request.RelationNames = []string{"other", "related"}
				for i, source := range []ontology.NodeRef{root, child, peer, empty} {
					request.Sources = []ontology.NodeRef{source}
					result, err := scope.Edges(t.Context(), request)
					require.NoError(t, err)
					require.Equal(t, []NeighborhoodSourceResult{batch.Sources[i]}, result.Sources)
				}
				request.Sources = []ontology.NodeRef{peer, root, child, empty}
				reordered, err := scope.Edges(t.Context(), request)
				require.NoError(t, err)
				require.Equal(t, batch.Edges, reordered.Edges, "shared rows and self loops must not duplicate when source partitions rejoin")
				require.Equal(t, 1, store.calls)
			})
		}
	}
}

func TestScopeRelationCacheLoadsOnlyMissingSourcesAndRetriesFailures(t *testing.T) {
	scope := neighborhoodCatalogScope(t, nil, []semdb.OntologyEdgeRow{
		{SrcPath: "a.md", DstPath: "owner-a.md", RelationName: "owner", Structural: true},
		{SrcPath: "b.md", DstPath: "owner-b.md", RelationName: "owner", Structural: true},
	})
	store := &queryRelationBatchStore{ReadStore: scope.service.Store}
	scope.service.Store = store
	request := NeighborhoodRequest{Sources: queryRelationRefs([]string{"a.md", "empty.md"}), RelationNames: []string{"owner"}, IncludeStructural: true}
	_, err := scope.Edges(t.Context(), request)
	require.NoError(t, err)
	request.Sources = queryRelationRefs([]string{"empty.md", "b.md", "a.md"})
	store.cancelNext = true
	_, err = scope.Edges(t.Context(), request)
	require.ErrorIs(t, err, context.Canceled)
	for range 2 {
		result, err := scope.Edges(t.Context(), request)
		require.NoError(t, err)
		require.Empty(t, result.Sources[0].Edges)
		require.Equal(t, "owner-b.md", result.Sources[1].Edges[0].Target.NotePath)
		require.Equal(t, "owner-a.md", result.Sources[2].Edges[0].Target.NotePath)
	}
	require.Equal(t, 3, store.calls)
	require.Equal(t, [][]readmodel.EdgeEndpoint{
		{{Path: "a.md"}, {Path: "empty.md"}},
		{{Path: "b.md"}},
		{{Path: "b.md"}},
	}, store.sources)
	scope.invalidateAfterApply()
	result, err := scope.Edges(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, result.Edges, 2)
	require.Equal(t, 4, store.calls)
	require.Equal(t, []readmodel.EdgeEndpoint{{Path: "empty.md"}, {Path: "b.md"}, {Path: "a.md"}}, store.sources[3])
}
