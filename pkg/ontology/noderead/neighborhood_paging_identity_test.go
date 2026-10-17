package noderead

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNeighborhoodBoundedCanonicalOrderMatchesUnbounded(t *testing.T) {
	ctx := context.Background()
	refs := []ontology.NodeRef{
		{NotePath: "a.md", Kind: ontology.NodeKindEmbedded, Fragment: "^b", NodeID: "a.md#^b"},
		{NotePath: "a.md", Kind: ontology.NodeKindEmbedded, Fragment: "^c", NodeID: "a.md#^c"},
	}
	scope := neighborhoodCatalogScope(t, refs, []semdb.OntologyEdgeRow{
		{SrcPath: "source.md", RelationName: "related", DstPath: "a.md", DstNodeID: refs[0].NodeID, Structural: true},
		{SrcPath: "source.md", RelationName: "related", DstPath: "a.md", DstNodeID: refs[1].NodeID, Structural: true},
		{SrcPath: "source.md", RelationName: "related", DstPath: "a.md.md", Structural: true},
	})
	request := NeighborhoodRequest{Sources: []ontology.NodeRef{{NotePath: "source.md", Kind: ontology.NodeKindNote}}, IncludeStructural: true}
	unbounded, err := scope.Neighborhood(ctx, request)
	require.NoError(t, err)
	require.Len(t, unbounded.Edges, 3)
	require.Equal(t, "a.md.md", unbounded.Edges[0].Target.NotePath)

	request.FirstPerSource = 1
	bounded, err := scope.Neighborhood(ctx, request)
	require.NoError(t, err)
	require.Equal(t, unbounded.Edges[:1], bounded.Edges)
	require.True(t, bounded.Sources[0].Truncated)
}

func TestNeighborhoodDeduplicatesCanonicalSourceAliasesBeforePaging(t *testing.T) {
	ctx := context.Background()
	source := ontology.NodeRef{NotePath: "source.md", Kind: ontology.NodeKindEmbedded, Fragment: "^source", NodeID: "source.md#^source"}
	scope := neighborhoodCatalogScope(t, []ontology.NodeRef{source}, []semdb.OntologyEdgeRow{
		{SrcPath: source.NotePath, SrcNodeID: source.NodeID, RelationName: "related", DstPath: "target.md", Structural: true},
	})
	result, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources: []ontology.NodeRef{
			{NotePath: source.NotePath, Kind: source.Kind, Fragment: source.Fragment},
			{NotePath: source.NotePath, Kind: source.Kind, NodeID: "catalog-0"},
		},
		IncludeStructural: true,
		FirstPerSource:    1,
	})
	require.NoError(t, err)
	require.Len(t, result.Sources, 1)
	require.Len(t, result.Edges, 1)
	require.Equal(t, source, result.Sources[0].Source)
	require.Equal(t, "target.md", result.Edges[0].Target.NotePath)
	require.False(t, result.Sources[0].Truncated, "aliases of one source must not consume separate edge slots")
}

func neighborhoodCatalogScope(t *testing.T, refs []ontology.NodeRef, edges []semdb.OntologyEdgeRow) *Scope {
	t.Helper()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, "index.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), semdb.OntologySnapshot{Edges: edges}))
	rows := make([]readmodel.NodeRow, 0, len(refs))
	paths := make([]string, 0, len(refs))
	for i, ref := range refs {
		encoded, err := json.Marshal(ref)
		require.NoError(t, err)
		rows = append(rows, readmodel.NodeRow{
			NodeID: "catalog-" + strconv.Itoa(i), NotePath: ref.NotePath, NodeKind: string(ref.Kind),
			NodeRefJSON: string(encoded), SourceLocator: ontology.NodeSourceLocator(ref), Fragment: ref.Fragment,
		})
		paths = append(paths, ref.NotePath)
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(context.Background(), readmodel.NodeReadModel{NotePaths: paths, Nodes: rows}))
	return NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})
}

func TestNeighborhoodInboundSelfLoopPagingMatchesUnbounded(t *testing.T) {
	ctx := context.Background()
	scope := neighborhoodCatalogScope(t, nil, []semdb.OntologyEdgeRow{
		{SrcPath: "a.md", DstPath: "a.md", RelationName: "related", Structural: true},
		{SrcPath: "b.md", DstPath: "a.md", RelationName: "related", Structural: true},
		{SrcPath: "c.md", DstPath: "a.md", RelationName: "related", Structural: true},
	})
	request := NeighborhoodRequest{Sources: []ontology.NodeRef{{NotePath: "a.md", Kind: ontology.NodeKindNote}}, Direction: TraversalDirectionInbound, IncludeStructural: true}
	unbounded, err := scope.Neighborhood(ctx, request)
	require.NoError(t, err)
	require.Len(t, unbounded.Edges, 3)
	require.Equal(t, "a.md", unbounded.Edges[0].Target.NotePath)
	request.FirstPerSource = 1
	bounded, err := scope.Neighborhood(ctx, request)
	require.NoError(t, err)
	require.Equal(t, unbounded.Edges[:1], bounded.Edges)
	require.True(t, bounded.Sources[0].Truncated)
}
