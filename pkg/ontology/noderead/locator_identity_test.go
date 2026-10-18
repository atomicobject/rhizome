package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopeLocatorsRetainsBareEmbeddedIDStatusAcrossCacheHits(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Story implements Section @node(locator: EMBEDDED) { summary: String @field }
`, "# Product\n\n## Stories\n\n### Story A\nsummary:: Needs a durable link\n")
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vault, reader, store, schema).NewScope(ScopeOptions{})
	ref := ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindEmbedded, NodeID: "Story A"}
	key := "specs/product.md#Story A"

	first, err := scope.Locators(ctx, []ontology.NodeRef{ref})
	require.NoError(t, err)
	locator, ok := first[key]
	require.True(t, ok, "a bare embedded ID must retain the link service's locator")
	require.Equal(t, ontology.NodeLocatorRequiresFix, locator.Status)
	require.Equal(t, "specs/product.md#story-a-23", locator.SourceLocator)
	require.NotEmpty(t, locator.FixActions)
	require.NotEmpty(t, locator.Diagnostics)
	require.Equal(t, "missing_embedded_block_id", locator.Diagnostics[0].Code)
	require.Equal(t, 1, reader.contents)

	second, err := scope.Locators(ctx, []ontology.NodeRef{ref})
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, reader.contents)
}
