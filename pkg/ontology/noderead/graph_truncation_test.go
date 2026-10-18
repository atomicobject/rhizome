package noderead

import (
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// The store honors the requested SQL limit so late, stronger candidates can
// expose truncation before relevance selection.
type graphTruncationStore struct {
	graphPrecedenceStore
	nodes []readmodel.GraphNodeRow
}

func (s *graphTruncationStore) GraphOntologyNodes(context.Context, readmodel.GraphNodeQuery) ([]readmodel.GraphNodeRow, error) {
	return s.nodes, nil
}

func (s *graphTruncationStore) GraphOntologyEdges(_ context.Context, query readmodel.GraphEdgeQuery) ([]readmodel.GraphTypedEdgeRow, error) {
	rows := s.typed
	if query.Limit > 0 && len(rows) > query.Limit {
		rows = rows[:query.Limit]
	}
	return rows, nil
}

func TestScopeGraphLimitsKeepRelevantEdgesAndTheirEndpoints(t *testing.T) {
	for _, facts := range []bool{false, true} {
		for _, test := range []struct {
			name      string
			nodeLimit int
			edgeLimit int
			paths     []string
			wantNodes []string
			wantEdges int
		}{
			{name: "strongest typed edge", nodeLimit: 2, edgeLimit: 1, wantNodes: []string{"note:y.md", "note:z.md"}, wantEdges: 1},
			{name: "node budget closes edges", nodeLimit: 2, edgeLimit: 20, wantNodes: []string{"note:y.md", "note:z.md"}, wantEdges: 1},
			{name: "single requested source", nodeLimit: 1, edgeLimit: 20, wantNodes: []string{"note:z.md"}},
			{name: "requested paths survive", nodeLimit: 3, edgeLimit: 1, paths: []string{"zz.md"}, wantNodes: []string{"note:y.md", "note:z.md", "note:zz.md"}, wantEdges: 1},
		} {
			t.Run(fmt.Sprintf("facts=%t/%s", facts, test.name), func(t *testing.T) {
				ctx := context.Background()
				store := &graphTruncationStore{}
				for _, target := range []string{"a.md", "b.md", "c.md", "d.md", "e.md", "zz.md"} {
					store.typed = append(store.typed, readmodel.GraphTypedEdgeRow{SrcPath: "z.md", DstPath: target, RelationName: "related"})
				}
				for range 3 {
					store.typed = append(store.typed, readmodel.GraphTypedEdgeRow{SrcPath: "z.md", DstPath: "y.md", RelationName: "related"})
				}
				store.docs = []readmodel.GraphDocEdgeRow{{SrcPath: "z.md", DstPath: "a.go", SourceKind: "note", TargetKind: "code", Kind: "code_ref", Weight: 100}}
				scope := NewService(obsidian.VaultDefinition{}, nil, store, nil).NewScope(ScopeOptions{})
				sources := []ontology.NodeRef{{NotePath: "z.md", Kind: ontology.NodeKindNote}}
				if facts {
					result, err := scope.GraphFacts(ctx, GraphFactsRequest{Sources: sources, Paths: test.paths, IncludeCode: true, NodeLimit: test.nodeLimit, EdgeLimit: test.edgeLimit})
					require.NoError(t, err)
					require.Equal(t, test.wantNodes, graphEndpointIDs(result.Nodes))
					require.Len(t, result.Edges, test.wantEdges)
					for _, edge := range result.Edges {
						require.Equal(t, "note:z.md", edge.Source)
						require.Equal(t, "note:y.md", edge.Target)
						require.Equal(t, "ontology", edge.Kind)
						require.Equal(t, float64(3), edge.Weight)
					}
					return
				}
				result, err := scope.Graph(ctx, GraphRequest{Sources: sources, Paths: test.paths, Profile: GraphProfileCodeAware, NodeLimit: test.nodeLimit, EdgeLimit: test.edgeLimit})
				require.NoError(t, err)
				require.Equal(t, test.wantNodes, graphEndpointIDs(result.Nodes))
				require.Len(t, result.Edges, test.wantEdges)
				for _, edge := range result.Edges {
					require.Equal(t, "note:z.md", edge.Source)
					require.Equal(t, "note:y.md", edge.Target)
					require.Equal(t, "ontology", edge.Kind)
					require.Equal(t, 3, edge.Weight)
				}
			})
		}
	}
}

func TestScopeGraphLimitsPreserveCanonicalEmbeddedSource(t *testing.T) {
	ctx := context.Background()
	store := &graphTruncationStore{nodes: []readmodel.GraphNodeRow{{
		NodeID: "catalog-story", NodeKind: string(ontology.NodeKindEmbedded), NotePath: "spec.md", BlockID: "story",
		NodeRefJSON: `{"notePath":"spec.md","kind":"EMBEDDED","nodeId":"story","fragment":"^story"}`,
	}}}
	store.typed = []readmodel.GraphTypedEdgeRow{{SrcPath: "spec.md", SrcNodeID: "catalog-story", DstPath: "a.md", RelationName: "related"}}
	for _, facts := range []bool{false, true} {
		t.Run(fmt.Sprintf("facts=%t", facts), func(t *testing.T) {
			scope := NewService(obsidian.VaultDefinition{}, nil, store, nil).NewScope(ScopeOptions{})
			sources := []ontology.NodeRef{{NotePath: "spec.md", Kind: ontology.NodeKindEmbedded, NodeID: "story"}}
			if facts {
				result, err := scope.GraphFacts(ctx, GraphFactsRequest{Sources: sources, IncludeEmbedded: true, NodeLimit: 1})
				require.NoError(t, err)
				require.Equal(t, []string{"embedded:catalog-story"}, graphEndpointIDs(result.Nodes))
				require.Empty(t, result.Edges)
				return
			}
			result, err := scope.Graph(ctx, GraphRequest{Sources: sources, NodeLimit: 1})
			require.NoError(t, err)
			require.Equal(t, []string{"embedded:catalog-story"}, graphEndpointIDs(result.Nodes))
			require.Empty(t, result.Edges)
		})
	}
}

func TestScopeGraphLimitsPreserveHeadingSourceWithBlockID(t *testing.T) {
	ctx := context.Background()
	store := &graphTruncationStore{nodes: []readmodel.GraphNodeRow{
		{NodeID: "z-story", NodeKind: string(ontology.NodeKindEmbedded), NotePath: "spec.md", Fragment: "Story Heading", BlockID: "story",
			NodeRefJSON: `{"notePath":"spec.md","kind":"EMBEDDED","nodeId":"story","fragment":"^story"}`},
		{NodeID: "a-target", NodeKind: string(ontology.NodeKindEmbedded), NotePath: "spec.md", BlockID: "target"},
	}}
	store.typed = []readmodel.GraphTypedEdgeRow{{SrcPath: "spec.md", SrcNodeID: "z-story", DstPath: "spec.md", DstNodeID: "a-target", RelationName: "related"}}
	for _, facts := range []bool{false, true} {
		t.Run(fmt.Sprintf("facts=%t", facts), func(t *testing.T) {
			scope := NewService(obsidian.VaultDefinition{}, nil, store, nil).NewScope(ScopeOptions{})
			sources := []ontology.NodeRef{{NotePath: "spec.md", Kind: ontology.NodeKindEmbedded, Fragment: "Story Heading"}}
			if facts {
				result, err := scope.GraphFacts(ctx, GraphFactsRequest{Sources: sources, IncludeEmbedded: true, NodeLimit: 1})
				require.NoError(t, err)
				require.Equal(t, []string{"embedded:z-story"}, graphEndpointIDs(result.Nodes))
				require.Empty(t, result.Edges)
				return
			}
			result, err := scope.Graph(ctx, GraphRequest{Sources: sources, NodeLimit: 1})
			require.NoError(t, err)
			require.Equal(t, []string{"embedded:z-story"}, graphEndpointIDs(result.Nodes))
			require.Empty(t, result.Edges)
		})
	}
}
