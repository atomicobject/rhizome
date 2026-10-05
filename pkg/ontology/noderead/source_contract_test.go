package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopeReadOverlayHydratesStagedNoteFieldChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  status: String @field
}
`, `---
status: Planned
---
# Product

Base content.
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).WithNoteFormats(markdownRuntime(t)).NewScope(ctx, ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": `---
status: Done
---
# Product

Staged content.
`,
		}, TouchedPaths: []string{"specs/product.md"}},
	})

	records, err := scope.Hydrate(ctx, []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}}, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "Done", records[0].Frontmatter["status"])
	require.Contains(t, records[0].Content, "Staged content.")
	require.Equal(t, noteformat.FormatID("markdown"), records[0].Format)
	require.Equal(t, ontology.SourceRepresentationUTF8, records[0].SourceRepresentation)
	require.Contains(t, records[0].Capabilities, noteformat.CapabilityStructuralContentMutation)
}

func TestScopeReadOverlayHydratesStagedEmbeddedFieldChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
}
`, `# Product

## Stories

### Story A
^story-a
status:: Planned
`)
	committed := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	instances, err := committed.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 1)

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": `# Product

## Stories

### Story A
^story-a
status:: Done
`,
		}, TouchedPaths: []string{"specs/product.md"}},
	})

	records, err := scope.Hydrate(ctx, []ontology.NodeRef{instances.Items[0].Ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, []string{"Done"}, records[0].InlineProps["status"])
	require.Empty(t, records[0].Format)
	require.Empty(t, records[0].SourceRepresentation)
	require.Empty(t, records[0].EvidenceRepresentation)
	require.Empty(t, records[0].Capabilities)
}

func TestProjectionRootWithNoCapabilitiesStillOwnsSourceContract(t *testing.T) {
	t.Parallel()

	record := NodeRecord{
		Format:                 "stale",
		SourceRepresentation:   "stale",
		EvidenceRepresentation: "stale",
		Capabilities:           []noteformat.Capability{noteformat.CapabilitySourceReading},
	}
	projection := &ontology.NodeProjection{
		Ref: ontology.NodeRef{NotePath: "prototype.html", Kind: ontology.NodeKindNote},
		RootSnapshot: &ontology.RootDocumentSnapshot{
			Format:                 "html",
			SourceRepresentation:   ontology.SourceRepresentationUTF8,
			EvidenceRepresentation: ontology.EvidenceRepresentationProviderProjection,
			Projection:             mustProjectionWithoutCapabilities(t),
		},
	}

	result := (&Scope{}).withProjectionSource(record, projection)

	require.Equal(t, noteformat.FormatID("html"), result.Format)
	require.Equal(t, ontology.SourceRepresentationUTF8, result.SourceRepresentation)
	require.Equal(t, ontology.EvidenceRepresentationProviderProjection, result.EvidenceRepresentation)
	require.Empty(t, result.Capabilities)
}

func mustProjectionWithoutCapabilities(t *testing.T) noteformat.Projection {
	t.Helper()
	projection, err := noteformat.NewProjection(
		"test-provider-v1",
		"test-projection-v1",
		noteformat.ProjectionStatusCurrent,
		nil,
		noteformat.MustCapabilities(),
	)
	require.NoError(t, err)
	return projection
}
