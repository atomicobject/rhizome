package noderead

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopeExactNoteHydrationRetainsPropertiesAndTagsWhileMetadataPending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
}
`, `---
summary: Product summary
tags: [startup, exact]
---
# Product
`)
	_, err := store.ApplyOwnershipTransitions(ctx, []semdb.OwnershipTransition{{
		Path:   "specs/unrelated.md",
		Target: semdb.OwnershipTargetUnowned,
	}})
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.False(t, state.Ready)

	service := NewService(vaultDef, &obsidian.Note{}, store, schema)
	service.ExactNoteMetadataRows = store.DurableNoteMetadataRowsByPaths
	service.ExactNotePropertyValues = store.DurableNotePropertyValuesByPaths
	service.ExactNoteTags = store.DurableNoteTagsByPaths
	scope := service.NewScope(ScopeOptions{})
	records, err := scope.Hydrate(ctx, []ontology.NodeRef{
		{NotePath: "specs/product.md", Kind: ontology.NodeKindNote},
		{NotePath: "specs/product.md", Kind: ontology.NodeKindNote},
	}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, "Product summary", records[0].Frontmatter["summary"])
	require.ElementsMatch(t, []string{"exact", "startup"}, records[0].Tags)
	require.Equal(t, records[0], records[1])
}

func TestScopeHydrateEmbeddedIdentityUsesCatalogWithoutProjection(t *testing.T) {
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
  summary: String! @field
}
`, `# Product

## Stories

### Story A
summary:: Ship it
^story-a
`)
	scope := NewService(vaultDef, nil, store, schema).NewScope(ScopeOptions{})
	instances, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 1)

	records, err := scope.Hydrate(ctx, []ontology.NodeRef{instances.Items[0].Ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "Story A", records[0].Title)
	require.Equal(t, "UserStory", records[0].TypeName)
	require.Equal(t, "specs/product.md#^story-a", records[0].NodeLocator.SourceLocator)
	require.Equal(t, []string{"Ship it"}, records[0].InlineProps["summary"])
	require.Len(t, records[0].FieldValues["summary"], 1)
	require.Equal(t, "ship it", records[0].FieldValues["summary"][0].ValueNorm)
	before := scope.Diagnostics()
	again, err := scope.Hydrate(ctx, []ontology.NodeRef{instances.Items[0].Ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, ontology.NodeKindEmbedded, again[0].Ref.Kind)
	require.Equal(t, "Story A", again[0].Title)
	require.Equal(t, before.RecordLoads, scope.Diagnostics().RecordLoads)
	require.Greater(t, scope.Diagnostics().RecordHits, before.RecordHits)
	require.Empty(t, records[0].Content)
	require.Equal(t, 2, scope.Diagnostics().CatalogHits)
	require.Zero(t, scope.Diagnostics().ProjectionFallbacks)
}

func TestScopeCachesStoreReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
}
`, `---
summary: Product summary
---
# Product
`)
	counting := &countingStore{Store: store}
	scope := NewService(vaultDef, &obsidian.Note{}, counting, schema).NewScope(ScopeOptions{})

	_, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "ProductSpec"})
	require.NoError(t, err)
	_, err = scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "ProductSpec"})
	require.NoError(t, err)
	require.Zero(t, counting.currentNoteMetadataRows)
	require.Equal(t, 1, counting.currentNoteMetadataRowsByPath)
	require.Equal(t, 1, counting.ontologyAssessmentFlagsByPath)
	require.Zero(t, counting.ontologyAssessmentsByPaths)
	require.Equal(t, 1, counting.ontologyPathsByType)

	_, err = scope.TypesByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	_, err = scope.TypesByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	require.Equal(t, 1, counting.ontologyTypesByPaths)

	_, err = scope.AssessmentsByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	_, err = scope.AssessmentsByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	require.Equal(t, 1, counting.ontologyAssessmentsByPaths)
}

func TestScopeHydrateContentOnlyReadsFilesWhenRequested(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
}
`, `---
summary: Product summary
---
# Product

Full body.
`)
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(ScopeOptions{})
	ref := ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"}

	summary, err := scope.Hydrate(ctx, []ontology.NodeRef{ref, ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, summary, 2)
	require.Equal(t, "Product summary", summary[0].Frontmatter["summary"])
	require.Equal(t, summary[0].Path, summary[1].Path)
	require.Empty(t, summary[0].Content)
	repeated, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, repeated, 1)
	require.Equal(t, 1, scope.Diagnostics().RecordLoads)
	require.Equal(t, 1, scope.Diagnostics().RecordHits)
	require.Zero(t, reader.contents)
	require.Zero(t, scope.Diagnostics().ContentLoads)

	content, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Len(t, content, 1)
	require.Contains(t, content[0].Content, "Full body.")
	require.Equal(t, 1, reader.contents)
	require.Equal(t, 1, scope.Diagnostics().ContentLoads)

	again, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, 1, reader.contents)
	require.Equal(t, 1, scope.Diagnostics().ContentHits)
}

func TestScopeHydrateContentReturnsFileReadErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
}
`, `---
summary: Product summary
---
# Product

Full body.
`)
	require.NoError(t, os.Remove(filepath.Join(vaultDef.Path, "specs", "product.md")))
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
	ref := ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"}

	records, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 1)

	_, err = scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
	require.Error(t, err)
}

func TestScopeAllowsConcurrentUse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/*.md"]) {
  summary: String!
  related: ProductSpec @link
}
`, "---\nsummary: Product summary\n---\n# Product\n\nrelated:: [[specs/other]]\n")
	writeFixtureNote(t, vaultDef.Path, "specs/other.md", "---\nsummary: Other summary\n---\n# Other\n")
	_, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
	ref := ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}
	operations := []func(){
		func() {
			records, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
			assert.NoError(t, err)
			assert.Len(t, records, 1)
		},
		func() {
			result, err := scope.Neighborhood(ctx, NeighborhoodRequest{Sources: []ontology.NodeRef{ref}, IncludeStructural: true, IncludeAmbient: true,
				Hydrate: HydrateOptions{Profile: HydrateContent}})
			assert.NoError(t, err)
			assert.Len(t, result.Edges, 1)
			assert.Len(t, result.Nodes, 1)
		},
		func() {
			result, err := scope.Graph(ctx, GraphRequest{Sources: []ontology.NodeRef{ref}})
			assert.NoError(t, err)
			assert.Len(t, result.Nodes, 2)
		},
		func() {
			result, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "ProductSpec"})
			assert.NoError(t, err)
			assert.Len(t, result.Items, 2)
		},
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 4 {
		for _, operation := range operations {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				operation()
			}()
		}
	}
	close(start)
	wg.Wait()
}
