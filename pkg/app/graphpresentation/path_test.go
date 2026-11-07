package graphpresentation

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/graph/algorithms"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/stretchr/testify/require"
)

func TestPathInputsPreserveAliasesEdgeOrderAndConfidence(t *testing.T) {
	edges, paths, canonical := PathInputsFromFacts(noderead.GraphFactsResult{
		Nodes: []noderead.GraphEndpoint{
			{ID: "note:docs/a.md", Kind: noderead.GraphEndpointNote, NotePath: "docs/a.md", SourceLocator: "shared"},
			{ID: "embedded:story", Kind: noderead.GraphEndpointEmbedded, NotePath: "docs/b.md", SourceLocator: "docs/b.md#^story"},
			{ID: "code:src/main.go", Kind: noderead.GraphEndpointCode, Path: "src/main.go", SourceLocator: "shared"},
		},
		Edges: []noderead.GraphFactEdge{
			{Source: "note:docs/a.md", Target: "embedded:story", Kind: "ontology", RelationName: "selectedStories", Confidence: 1},
			{Source: "", Target: "embedded:story"},
			{Source: "embedded:story", Target: "embedded:story"},
			{Source: "embedded:story", Target: "code:src/main.go", Kind: "coderef", RelationName: "ignored", Confidence: 0.75},
		},
	})

	require.Equal(t, []algorithms.GraphDocEdgeMinimal{
		{SrcPath: "note:docs/a.md", DstPath: "embedded:story", Kind: "selectedStories", Confidence: "extracted", ConfidenceScore: 1},
		{SrcPath: "embedded:story", DstPath: "code:src/main.go", Kind: "coderef", Confidence: "inferred", ConfidenceScore: 0.75},
	}, edges)
	require.ElementsMatch(t, []string{"docs/a.md", "docs/b.md", "docs/b.md#^story", "embedded:story", "src/main.go", "code:src/main.go", "shared"}, paths)
	require.Equal(t, "note:docs/a.md", CanonicalPath(canonical, " docs/a.md "))
	require.Equal(t, "embedded:story", CanonicalPath(canonical, "docs/b.md#^story"))
	require.Equal(t, "code:src/main.go", CanonicalPath(canonical, "shared"), "later aliases retain their current precedence")
	require.Equal(t, " unknown ", CanonicalPath(canonical, " unknown "))
	require.Equal(t, "extracted", FactConfidenceLabel(noderead.GraphFactEdge{}))
}
