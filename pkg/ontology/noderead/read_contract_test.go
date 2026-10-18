package noderead

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopeTypeInstancesEmptyEmbeddedTypeDoesNotReadSource(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `type Product @node(paths:["specs/*.md"]) { title:String }
 type ActionItem implements Section @node(locator:EMBEDDED) @source(shape:CHECKBOX_ITEM,marker:"#action-item",paths:["**/*.md"]) { summary:String @field(sourceKind:ITEM_SUMMARY) }`, "# No action items\n")
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	counting := &countingStore{Store: store}
	for range 2 {
		result, err := NewService(vault, reader, counting, schema).NewScope(ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "ActionItem", Limit: 5})
		require.NoError(t, err)
		require.Empty(t, result.Items)
	}
	require.Zero(t, reader.contents)
	require.Zero(t, reader.lists)
	require.Zero(t, counting.currentNoteMetadataRows)
}

func TestHydrateProfileFieldContract(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, ensureApplySchema, "# Product\n\n## Stories\n\n### Story A\nrelated:: [[specs/product]]\n^story-a\n")
	rows, err := store.AllOntologyNodes(ctx)
	require.NoError(t, err)
	seen := map[ontology.NodeKind]bool{}
	for _, row := range rows {
		ref := nodeRefFromCatalogRow(row)
		if seen[ref.Kind] {
			continue
		}
		seen[ref.Kind] = true
		t.Run(string(ref.Kind), func(t *testing.T) {
			reader := &countingNoteReader{Note: &obsidian.Note{}}
			scope := NewService(vault, reader, store, schema).NewScope(ScopeOptions{})
			summary, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateSummary})
			require.NoError(t, err)
			require.Len(t, summary, 1)
			require.NotEmpty(t, summary[0].Title)
			require.NotEmpty(t, summary[0].TypeName)
			require.Empty(t, summary[0].Content)
			require.Zero(t, reader.contents)
			require.Zero(t, reader.lists)
			content, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
			require.NoError(t, err)
			require.Len(t, content, 1)
			require.NotEmpty(t, content[0].Content)
			reads := reader.contents
			again, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateSummary})
			require.NoError(t, err)
			require.Len(t, again, 1)
			require.Empty(t, again[0].Content)
			require.Equal(t, reads, reader.contents)
		})
	}
	require.True(t, seen[ontology.NodeKindNote])
	require.True(t, seen[ontology.NodeKindEmbedded])
}

func TestScopeSummaryHydrationStoreCallsIndependentOfRefCount(t *testing.T) {
	ctx := context.Background()
	vault, store, _ := buildFixture(t, `type Document @node(paths:["notes/*.md"]) { title:String }`, "# Host\n")
	refs := make([]ontology.NodeRef, 25)
	for i := range refs {
		path := fmt.Sprintf("notes/%02d.md", i)
		writeFixtureNote(t, vault.Path, path, "# A note\n")
		refs[i] = ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote}
	}
	runtime, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	schema := runtime.Schema
	var before []int
	for _, count := range []int{1, 25} {
		counting := &countingStore{Store: store}
		reader := &countingNoteReader{Note: &obsidian.Note{}}
		records, err := NewService(vault, reader, counting, schema).NewScope(ScopeOptions{}).Hydrate(ctx, refs[:count], HydrateOptions{Profile: HydrateSummary})
		require.NoError(t, err)
		require.Len(t, records, count)
		require.Zero(t, reader.contents)
		require.Zero(t, counting.currentNoteMetadataRows)
		calls := []int{counting.currentNoteMetadataRowsByPath, counting.currentNotePropertyValues, counting.currentNoteTags, counting.ontologyTypesByPaths, counting.ontologyAssessmentsByPaths, counting.ontologyAssessmentFlagsByPath}
		if before == nil {
			before = calls
		} else {
			require.Equal(t, before, calls)
		}
	}
}

func TestSummaryOverlayTagsMatchCommittedProjection(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `type Product @node(paths:["specs/*.md"]) { title:String }`, "# Product #committed\n")
	staged := "---\ntags: ['#Shared', 'shared', 'frontmatter']\n---\n# Product #staged #Shared\n"
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"specs/product.md": staged}}
	records, err := NewService(vault, reader, store, schema).NewScope(ScopeOptions{ReadOverlay: overlay}).Hydrate(ctx, []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, []string{"frontmatter", "shared", "staged"}, records[0].Tags)
	require.Zero(t, reader.contents)
	writeFixtureNote(t, vault.Path, "specs/product.md", staged)
	runtime, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	committed, err := NewService(vault, reader, store, runtime.Schema).NewScope(ScopeOptions{}).Hydrate(ctx, []ontology.NodeRef{records[0].Ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, committed, 1)
	require.Equal(t, committed[0].Tags, records[0].Tags)
}

func TestHydrateProviderRootProfileContract(t *testing.T) {
	ctx := context.Background()
	provider := providerRootProjector{}
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	store := newBoundedHydrationStore(t, &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{"prototype.html": {Path: "prototype.html", Title: "Prototype", FormatID: "html", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}}}})
	reader := &boundedHydrationReader{}
	scope := NewService(obsidian.VaultDefinition{}, reader, store, &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{}}).WithNoteFormats(formats).NewScope(ScopeOptions{})
	refs := []ontology.NodeRef{{NotePath: "prototype.html", Kind: ontology.NodeKindNote}}
	for _, profile := range []HydrateProfile{HydrateSummary, HydrateContent, HydrateSummary} {
		records, err := scope.Hydrate(ctx, refs, HydrateOptions{Profile: profile})
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, noteformat.FormatID("html"), records[0].Format)
		if profile == HydrateContent {
			require.NotEmpty(t, records[0].Content)
		} else {
			require.Empty(t, records[0].Content)
		}
	}
	require.Equal(t, 1, reader.contents)
}
