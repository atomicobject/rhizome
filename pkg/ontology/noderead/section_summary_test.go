package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestSectionSummaryHydratesFromIndexAndPreview(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/product.md"]) {
 abstract: Section @contains(level: H2, heading: "Summary") @display(role: SUMMARY)
 }`, "# Product\n\n## Summary\n\nFirst paragraph.\n\nSecond paragraph.\n")
	ref := ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}
	// A nil NoteReader makes any source projection fail: summary hydration must
	// use the persisted compact field row.
	scope := NewService(vault, nil, store, schema).NewScope(ctx, ScopeOptions{})
	records, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, []string{"First paragraph. Second paragraph."}, records[0].InlineProps["abstract"])
	overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"specs/product.md": "# Product\n\n## Summary\n\nStaged prose.\n"}}
	scope = NewService(vault, nil, store, schema).NewScope(ctx, ScopeOptions{ReadOverlay: overlay})
	records, err = scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, []string{"Staged prose."}, records[0].InlineProps["abstract"])
}
