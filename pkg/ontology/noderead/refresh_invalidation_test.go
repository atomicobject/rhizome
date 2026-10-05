package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestResolveApplyInvalidatesCollectionAndGraphCaches(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
type StoriesSection implements Section { stories: [UserStory!] @contains(level: H3) }
type UserStory implements Section @node(locator: EMBEDDED) { summary: String @field }
`, "# Product\n\n## Stories\n\n### Story A\nsummary:: Needs a block ID\n")
	scope := enableTestLinkApply(t, NewService(vault, &obsidian.Note{}, store, schema)).NewScope(ctx, ScopeOptions{})
	collectionRequest := TypeInstancesRequest{TypeName: "UserStory"}
	graphRequest := GraphRequest{Paths: []string{"specs/product.md"}, Profile: GraphProfileOntologyNative}
	factsRequest := GraphFactsRequest{Paths: []string{"specs/product.md"}, IncludeOntology: true, IncludeEmbedded: true}
	storyIDs := func(nodes []GraphEndpoint) []string {
		ids := []string{}
		for _, node := range nodes {
			if node.TypeName == "UserStory" {
				ids = append(ids, node.NodeID)
			}
		}
		return ids
	}

	before, err := scope.TypeInstances(ctx, collectionRequest)
	require.NoError(t, err)
	require.Len(t, before.Items, 1)
	beforeGraph, err := scope.Graph(ctx, graphRequest)
	require.NoError(t, err)
	beforeFacts, err := scope.GraphFacts(ctx, factsRequest)
	require.NoError(t, err)
	beforeGraphIDs := storyIDs(beforeGraph.Nodes)
	beforeFactIDs := storyIDs(beforeFacts.Nodes)
	require.Len(t, beforeGraphIDs, 1)
	require.Len(t, beforeFactIDs, 1)

	result, err := scope.Resolve(ctx, ResolveRequest{
		Targets:          []NodeTarget{{Input: "specs/product.md#Story A"}},
		EnsureLinkTarget: ontology.EnsureLinkTargetApply,
		Hydrate:          HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	require.Equal(t, ontology.NodeLocatorLinkable, result.Resolved[0].Locator.Status)
	require.Contains(t, result.Resolved[0].Locator.LinkTarget.Wikilink, "#^")

	after, err := scope.TypeInstances(ctx, collectionRequest)
	require.NoError(t, err)
	require.Len(t, after.Items, 1)
	require.NotEqual(t, before.Items[0].Ref, after.Items[0].Ref)
	require.Equal(t, result.Resolved[0].Ref, after.Items[0].Ref)
	afterGraph, err := scope.Graph(ctx, graphRequest)
	require.NoError(t, err)
	afterFacts, err := scope.GraphFacts(ctx, factsRequest)
	require.NoError(t, err)
	afterGraphIDs := storyIDs(afterGraph.Nodes)
	afterFactIDs := storyIDs(afterFacts.Nodes)
	require.Len(t, afterGraphIDs, 1)
	require.Len(t, afterFactIDs, 1)
	require.NotEqual(t, beforeGraphIDs, afterGraphIDs)
	require.NotEqual(t, beforeFactIDs, afterFactIDs)
}
