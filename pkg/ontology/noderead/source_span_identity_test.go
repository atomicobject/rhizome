package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const sourceSpanIdentitySchema = `
type ProductSpec @node(paths: ["specs/product.md"]) {
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H2, heading: "Acceptance Criteria")
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(shape: LIST_ITEM)
}

type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  summary: String @field(sourceKind: ITEM_SUMMARY)
}
`

func TestScopeHydrateRelocatesUnanchoredItemByContentIdentity(t *testing.T) {
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, sourceSpanIdentitySchema, "# Product\n\n## Acceptance Criteria\n\n- First item\n- Target item\n")
	initial := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	items, err := initial.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, err)
	require.Len(t, items.Items, 2)
	targetRef := items.Items[1].Ref
	require.Contains(t, targetRef.NodeID, "#item-")
	require.NotEmpty(t, targetRef.Structural)

	writeFixtureNote(t, vaultDef.Path, "specs/product.md", "# Product\n\n## Acceptance Criteria\n\n- First item\n- Other! item\n- Target item\n")
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	for _, profile := range []HydrateProfile{HydrateContent, HydrateIdentity, HydrateSummary} {
		t.Run(string(profile), func(t *testing.T) {
			scope := NewService(vaultDef, &obsidian.Note{}, store, runtime.Schema).NewScope(ctx, ScopeOptions{})
			records, err := scope.Hydrate(ctx, []ontology.NodeRef{targetRef}, HydrateOptions{Profile: profile})
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Equal(t, "Target item", records[0].Title)
			require.NotEqual(t, targetRef.NodeID, records[0].Ref.NodeID)
		})
	}
}

func TestScopeHydrateUsesCatalogForCurrentPositionalIdentity(t *testing.T) {
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, sourceSpanIdentitySchema, "# Product\n\n## Acceptance Criteria\n\n- Target item\n")
	listingScope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	items, err := listingScope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, err)
	require.Len(t, items.Items, 1)

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	records, err := scope.Hydrate(ctx, []ontology.NodeRef{items.Items[0].Ref}, HydrateOptions{Profile: HydrateIdentity})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, 1, scope.Diagnostics().CatalogHits)
	require.Zero(t, scope.Diagnostics().ProjectionFallbacks)
}

func TestScopeHydrateRejectsDuplicateUnanchoredContentIdentity(t *testing.T) {
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, sourceSpanIdentitySchema, "# Product\n\n## Acceptance Criteria\n\n- First item\n- Target item\n")
	initial := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	items, err := initial.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, err)
	targetRef := items.Items[1].Ref

	writeFixtureNote(t, vaultDef.Path, "specs/product.md", "# Product\n\n## Acceptance Criteria\n\n- First item\n- Target item\n- Target item\n")
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	scope := NewService(vaultDef, &obsidian.Note{}, store, runtime.Schema).NewScope(ctx, ScopeOptions{})
	listed, listErr := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, listErr)
	require.Len(t, listed.Items, 3)
	_, err = scope.Hydrate(ctx, []ontology.NodeRef{targetRef}, HydrateOptions{Profile: HydrateIdentity})
	require.ErrorIs(t, err, ontology.ErrAmbiguousSourceSpanIdentity)
}

func TestScopeReadOverlayTypeInstancesListsDuplicateUnanchoredContent(t *testing.T) {
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, sourceSpanIdentitySchema, "# Product\n\n## Acceptance Criteria\n\n- Original item\n")
	overlay := &ReadOverlay{
		SourceFormat: "markdown",
		UpdatedContentByPath: map[string]string{
			"specs/product.md": "# Product\n\n## Acceptance Criteria\n\n- Same item\n- Same item\n",
		},
		TouchedPaths: []string{"specs/product.md"},
	}
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
		ReadOverlay: overlay,
	})

	listed, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)
	require.NotEqual(t, listed.Items[0].Ref.NodeID, listed.Items[1].Ref.NodeID)
	require.Equal(t, listed.Items[0].Ref.Structural, listed.Items[1].Ref.Structural)
}

func TestScopeHydratePreservesUnanchoredIdentityAfterBlockIDInsertion(t *testing.T) {
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, sourceSpanIdentitySchema, "# Product\n\n## Acceptance Criteria\n\n- Target item\n")
	initial := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	items, err := initial.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, err)
	require.Len(t, items.Items, 1)
	targetRef := items.Items[0].Ref

	writeFixtureNote(t, vaultDef.Path, "specs/product.md", "# Product\n\n## Acceptance Criteria\n\n- Target item ^target-item\n")
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	for _, profile := range []HydrateProfile{HydrateContent, HydrateIdentity, HydrateSummary} {
		t.Run(string(profile), func(t *testing.T) {
			scope := NewService(vaultDef, &obsidian.Note{}, store, runtime.Schema).NewScope(ctx, ScopeOptions{})
			records, err := scope.Hydrate(ctx, []ontology.NodeRef{targetRef}, HydrateOptions{Profile: profile})
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Equal(t, "Target item", records[0].Title)
			require.Equal(t, "^target-item", records[0].Ref.Fragment)
		})
	}
}
