package noderead

import (
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type graphPrecedenceStore struct {
	Store
	typed []readmodel.GraphTypedEdgeRow
	docs  []readmodel.GraphDocEdgeRow
}

func (*graphPrecedenceStore) GraphOntologyNodes(context.Context, readmodel.GraphNodeQuery) ([]readmodel.GraphNodeRow, error) {
	return nil, nil
}
func (s *graphPrecedenceStore) GraphOntologyEdges(context.Context, readmodel.GraphEdgeQuery) ([]readmodel.GraphTypedEdgeRow, error) {
	return s.typed, nil
}
func (s *graphPrecedenceStore) GraphDocEdges(context.Context, readmodel.GraphDocEdgeQuery) ([]readmodel.GraphDocEdgeRow, error) {
	return s.docs, nil
}
func (*graphPrecedenceStore) GraphIndexedPaths(context.Context, bool) ([]readmodel.GraphPathRow, error) {
	return nil, nil
}

func TestGraphTypedPrecedencePreservesRelationsAndDiagnostics(t *testing.T) {
	ctx := context.Background()
	store := &graphPrecedenceStore{typed: []readmodel.GraphTypedEdgeRow{
		{SrcPath: "a.md", DstPath: "b.md", RelationName: "owner", Provenance: "field"},
		{SrcPath: "a.md", DstPath: "b.md", RelationName: "owner", Provenance: "field"},
		{SrcPath: "a.md", DstPath: "b.md", RelationName: "related", Provenance: "field"},
		{SrcPath: "b.md", DstPath: "a.md", RelationName: "inverse", Provenance: "field"},
	}}
	for _, kind := range []string{"wikilink", "links_to", "section-link"} {
		store.docs = append(store.docs,
			readmodel.GraphDocEdgeRow{SrcPath: "a.md", DstPath: "b.md", SourceKind: "note", TargetKind: "note", Kind: kind},
			readmodel.GraphDocEdgeRow{SrcPath: "b.md", DstPath: "a.md", SourceKind: "note", TargetKind: "note", Kind: kind})
	}
	store.docs = append(store.docs, readmodel.GraphDocEdgeRow{SrcPath: "a.md", DstPath: "c.md", SourceKind: "note", TargetKind: "note", Kind: "wikilink", Weight: 3, ConfidenceScore: 0.75})
	scope := NewService(obsidian.VaultDefinition{}, nil, store, nil).NewScope(ctx, ScopeOptions{})
	req := GraphRequest{Profile: GraphProfileNotesOnly, Diagnostics: true}
	graph, err := scope.Graph(ctx, req)
	require.NoError(t, err)
	require.Len(t, graph.Nodes, 3)
	require.ElementsMatch(t, []GraphReadEdge{
		{Source: "note:a.md", Target: "note:b.md", Kind: "ontology", RelationName: "owner", RelationLabel: "Owner", Provenance: "field", Weight: 2},
		{Source: "note:a.md", Target: "note:b.md", Kind: "ontology", RelationName: "related", RelationLabel: "Related", Provenance: "field", Weight: 1},
		{Source: "note:b.md", Target: "note:a.md", Kind: "ontology", RelationName: "inverse", RelationLabel: "Inverse", Provenance: "field", Weight: 1},
		{Source: "note:a.md", Target: "note:c.md", Kind: "wikilink", Weight: 3, Confidence: 0.75},
	}, graph.Edges)
	require.Len(t, graph.Diagnostics.Dedupe, 6)
	for i, diagnostic := range graph.Diagnostics.Dedupe {
		row := store.docs[i]
		require.Equal(t, GraphDedupeDiagnostic{Source: "note:" + row.SrcPath, Target: "note:" + row.DstPath, SuppressedKind: row.Kind, PreferredKind: "ontology", Reason: "ontology edge supersedes doc link"}, diagnostic)
	}
	cached, err := scope.Graph(ctx, req)
	require.NoError(t, err)
	require.Equal(t, graph, cached)
	facts, err := scope.GraphFacts(ctx, GraphFactsRequest{NodeLimit: 2, EdgeLimit: 1, Diagnostics: true})
	require.NoError(t, err)
	require.Len(t, facts.Nodes, 2)
	require.Len(t, facts.Edges, 1)
	require.Equal(t, "owner", facts.Edges[0].RelationName)
	require.Equal(t, float64(2), facts.Edges[0].Weight)
	require.Equal(t, graph.Diagnostics, facts.Diagnostics)
}

func BenchmarkGraphFactsPrecedence(b *testing.B) {
	for _, mixed := range []bool{false, true} {
		for _, n := range []int{1000, 5000, 10000} {
			b.Run(fmt.Sprintf("mixed=%v/edges=%d", mixed, n), func(b *testing.B) {
				store := &graphPrecedenceStore{}
				for i := 0; i < n; i++ {
					src, dst := fmt.Sprintf("notes/%06d.md", i), fmt.Sprintf("notes/%06d.md", i+1)
					store.typed = append(store.typed, readmodel.GraphTypedEdgeRow{SrcPath: src, DstPath: dst, RelationName: "related"})
					if mixed {
						store.docs = append(store.docs, readmodel.GraphDocEdgeRow{SrcPath: src, DstPath: "extra.md", SourceKind: "note", TargetKind: "note", Kind: "wikilink", Weight: 1})
					}
				}
				service := NewService(obsidian.VaultDefinition{}, nil, store, nil)
				ctx := context.Background()
				expectedEdges := 9
				if mixed {
					expectedEdges = 10
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					result, err := service.NewScope(ctx, ScopeOptions{}).GraphFacts(ctx, GraphFactsRequest{NodeLimit: 10, EdgeLimit: 10})
					if err != nil || len(result.Nodes) != 10 || len(result.Edges) != expectedEdges {
						b.Fatalf("nodes=%d edges=%d err=%v", len(result.Nodes), len(result.Edges), err)
					}
				}
			})
		}
	}
}
