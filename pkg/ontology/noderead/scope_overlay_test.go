package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopeReadOverlayTypeInstancesMergesTouchedEmbeddedMembership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
interface WorkItem {
  status: String
}

type ProductSpec @node(paths: ["specs/product.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements WorkItem & Section @node(locator: EMBEDDED) {
  status: String @field
}
`, `# Product

## Stories

### Story A
^story-a
status:: Planned
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": `# Product

## Stories

### Story B
^story-b
status:: Done
`,
		}, TouchedPaths: []string{"specs/product.md"}},
	})

	stories, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Equal(t, []string{"specs/product.md#^story-b"}, nodeListItemRefs(stories.Items))

	workItems, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "WorkItem"})
	require.NoError(t, err)
	require.Equal(t, []string{"specs/product.md#^story-b"}, nodeListItemRefs(workItems.Items))
}

func TestScopeReadOverlayTypeInstancesRemovesDeletedTouchedEmbeddedNode(t *testing.T) {
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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": `# Product

## Stories
`,
		}, TouchedPaths: []string{"specs/product.md"}},
	})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Empty(t, result.Items)

	records, err := scope.Hydrate(ctx, []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindEmbedded, Fragment: "^story-a", TypeName: "UserStory"}}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestScopeReadOverlayTypeInstancesRemovesDeletedTouchedNote(t *testing.T) {
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
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", Notes: map[string]ReadOverlayNote{
			"specs/product.md": {Deleted: true},
		}},
	})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "ProductSpec"})
	require.NoError(t, err)
	require.Empty(t, result.Items)

	records, err := scope.Hydrate(ctx, []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestScopeReadOverlayFiltersStagedEmbeddedItemsByInlineFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
enum ItemStatus {
  open
  done
}

type Task implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#task", paths: ["specs/product.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  status: ItemStatus @field
}
`, "# Product\n\n- [ ] First #task\n  status:: open\n")
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": "# Product\n\n- [ ] First #task\n  status:: done\n\n- [ ] Second #task\n",
		}},
	})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName:   "Task",
		Predicates: []readmodel.FieldPredicate{{FieldName: "status", Op: readmodel.FieldOpExists}},
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "First", result.Items[0].Title)
	existsRef := result.Items[0].Ref
	for _, tc := range []struct {
		value string
		count int
	}{{"done", 1}, {"open", 0}} {
		result, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "Task", Predicates: []readmodel.FieldPredicate{{
			FieldName: "status", Op: readmodel.FieldOpEq,
			Values: []readmodel.FieldValueRow{{ValueText: tc.value, ValueNorm: tc.value}},
		}}})
		require.NoError(t, err)
		require.Len(t, result.Items, tc.count)
		if tc.count == 1 {
			require.Equal(t, "First", result.Items[0].Title)
			require.Equal(t, existsRef, result.Items[0].Ref)
		}
	}
}

func TestScopeReadOverlayIssuesUsesStagedNotes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
}
`, "# Committed title\n")
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": "# Staged title\n",
		}},
	})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: ontology.TypeScopeIssues})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "Staged title", result.Items[0].Title)
}

func TestScopeReadOverlayAllNotesUsesStagedNotes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md", "specs/b.md"]) {
  rank: Int @field
}
`, "---\nrank: 1\n---\n# Committed title\n")
	writeFixtureNote(t, vaultDef.Path, "specs/b.md", "---\nrank: 2\n---\n# Kept\n")
	_, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": "---\nrank: 1\n---\n# Staged title\n",
		}},
	})
	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: ontology.TypeScopeAll})
	require.NoError(t, err)
	titles := map[string]string{}
	for _, item := range result.Items {
		titles[item.NotePath] = item.Title
	}
	require.Equal(t, "Staged title", titles["specs/product.md"])
	require.Equal(t, "Kept", titles["specs/b.md"])
}

func TestReadOverlayIndexReplacingPathsReprojectsOnlyChangedPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, _, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product-a.md", "specs/product-b.md"]) {
  status: String @field
}
`, `---
status: old-a
---
# Product A
`)
	base := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
		"specs/product-a.md": `---
status: old-a
---
# Product A
`,
		"specs/product-b.md": `---
status: old-b
---
# Product B
`,
	}}
	previous, err := BuildReadOverlayIndex(ctx, schema, base)
	require.NoError(t, err)
	require.Equal(t, 1, previous.ProjectionCountByPath["specs/product-a.md"])
	require.Equal(t, 1, previous.ProjectionCountByPath["specs/product-b.md"])

	next := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
		"specs/product-a.md": base.UpdatedContentByPath["specs/product-a.md"],
		"specs/product-b.md": `---
status: new-b
---
# Product B
`,
	}}
	updated, err := BuildReadOverlayIndexReplacingPaths(ctx, schema, next, previous, []string{"specs/product-b.md"})
	require.NoError(t, err)
	require.Equal(t, 1, updated.BuildProjectionCount)
	require.Equal(t, []string{"old-a"}, recordFieldValues(overlayIndexRecordForPath(t, updated, "specs/product-a.md"), "status"))
	require.Equal(t, []string{"new-b"}, recordFieldValues(overlayIndexRecordForPath(t, updated, "specs/product-b.md"), "status"))
}

func TestScopeReadOverlayIndexPreservesScopeMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  status: String @field
}
`, `---
status: old
---
# Product
`)
	overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
		"specs/product.md": `---
status: new
---
# Product
`,
	}}
	index, err := BuildReadOverlayIndex(ctx, schema, overlay)
	require.NoError(t, err)
	overlay.Index = index

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{ReadOverlay: overlay})
	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "ProductSpec"})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.NotZero(t, result.Items[0].UpdatedAt)

	records, err := scope.Hydrate(ctx, []ontology.NodeRef{result.Items[0].Ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.NotZero(t, records[0].UpdatedAt)
	require.Equal(t, []string{"new"}, records[0].InlineProps["status"])
}

func TestBuildReadOverlayIndexSkipsFutureProviderContent(t *testing.T) {
	ctx := context.Background()
	_, _, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  status: String @field
}
`, "# Product\n")
	overlay := &ReadOverlay{
		SourceFormat: noteformat.FormatID("future"),
		UpdatedContentByPath: map[string]string{
			"specs/product.md": "---\nstatus: parsed-only-if-markdown\n---\n# Product\n",
		},
	}
	index, err := BuildReadOverlayIndex(ctx, schema, overlay)
	require.NoError(t, err)
	require.Empty(t, index.SnapshotsByPath)
	require.Empty(t, index.RecordsByRef)
}
