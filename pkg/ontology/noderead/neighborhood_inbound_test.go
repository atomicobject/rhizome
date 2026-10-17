package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestScopeInboundReverseKeepsEmbeddedSourceTypesAndRefs(t *testing.T) {
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  stories: [ZStory!] @reverse(field: "product")
}
type ZStory implements Section @node(locator: EMBEDDED) @source(shape: LIST_ITEM, marker: "#story") {
  product: ProductSpec @link
}
`, `# Product

- First #story
  product:: [[specs/product]]
- Second #story
  product:: [[specs/product]]

See [[specs/product]] in prose.
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
	source := ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"}
	selection, ok := RelationSelectionForField(schema, schema.Types["ProductSpec"].ByName["stories"])
	require.True(t, ok)
	selection.Sources = []ontology.NodeRef{source}
	listed, err := scope.executeSingleRelationSelection(ctx, selection, TraverseLimits{})
	require.NoError(t, err)
	require.Len(t, listed.Edges, 2)
	require.NotEqual(t, listed.Edges[0].Target.NodeID, listed.Edges[1].Target.NodeID)
	for _, edge := range listed.Edges {
		require.Equal(t, ontology.NodeKindEmbedded, edge.Target.Kind)
		require.Equal(t, "ZStory", edge.Target.TypeName)
		require.True(t, edge.Structural)
		require.Equal(t, "product", edge.RelationName)
	}
	counts, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: []ontology.NodeRef{source}, Selections: []RelationSelection{selection}})
	require.NoError(t, err)
	require.Equal(t, 2, counts.BySource[RefIdentityKey(source)].Count)

}

func TestScopeInboundEmbeddedSourceTypeUsesLocatorNotCatalogID(t *testing.T) {
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  stories: [ZStory!] @reverse(field: "product")
  storySection: StoriesSection @contains(level: H2, heading: "Stories")
}
type StoriesSection implements Section {
  stories: [ZStory!] @contains(level: H3)
}
type ZStory implements Section @node(locator: EMBEDDED) {
  product: ProductSpec @link
}
`, `# Product

## Stories

### Story One
product:: [[specs/product]]

### Story Two
product:: [[specs/product]]
`)
	edges, err := store.OntologyEdgesForPaths(ctx, []string{"specs/product.md"}, true, "product", 0)
	require.NoError(t, err)
	require.Len(t, edges, 2)
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	storyNodes := map[string]readmodel.NodeRow{}
	for _, node := range nodes {
		if node.TypeName == "ZStory" {
			storyNodes[nodeRefFromCatalogRow(node).NodeID] = node
		}
	}
	require.Len(t, storyNodes, 2)
	for _, edge := range edges {
		node, ok := storyNodes[edge.SrcNodeID]
		require.True(t, ok, "production edge ID must match the catalog ref's projection ID")
		// The edge stores the projection ref's locator-form ID. The catalog
		// primary key is a separate hash and is not used for this lookup.
		require.Equal(t, node.SourceLocator, edge.SrcNodeID)
		require.NotEqual(t, node.NodeID, edge.SrcNodeID)
	}

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
	source := ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"}
	unfiltered, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources: []ontology.NodeRef{source}, Direction: TraversalDirectionInbound,
		RelationNames: []string{"product"}, IncludeStructural: true,
	})
	require.NoError(t, err)
	require.Len(t, unfiltered.Edges, 2)
	for _, edge := range unfiltered.Edges {
		require.Equal(t, "ZStory", edge.Target.TypeName)
		require.Equal(t, ontology.NodeKindEmbedded, edge.Target.Kind)
	}
	selection, ok := RelationSelectionForField(schema, schema.Types["ProductSpec"].ByName["stories"])
	require.True(t, ok)
	selection.Sources = []ontology.NodeRef{source}
	filtered, err := scope.executeSingleRelationSelection(ctx, selection, TraverseLimits{})
	require.NoError(t, err)
	require.Len(t, filtered.Edges, 2)
	require.Zero(t, scope.Diagnostics().ProjectionFallbacks)
}
