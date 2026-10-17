package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

type summaryReadStore struct {
	ReadStore
	catalogCalls, typeCalls, inventoryCalls, metadataCalls, flagCalls int
}

func (s *summaryReadStore) AllOntologyNodes(ctx context.Context) ([]readmodel.NodeRow, error) {
	s.catalogCalls++
	return s.ReadStore.AllOntologyNodes(ctx)
}
func (s *summaryReadStore) OntologyNodesByType(ctx context.Context, name string) ([]readmodel.NodeRow, error) {
	s.typeCalls++
	return s.ReadStore.OntologyNodesByType(ctx, name)
}
func (s *summaryReadStore) OntologyNodesByTypePlan(ctx context.Context, plan readmodel.NodeQueryPlan) ([]readmodel.NodeRow, error) {
	s.typeCalls++
	return s.ReadStore.OntologyNodesByTypePlan(ctx, plan)
}
func (s *summaryReadStore) OntologyPathsByType(ctx context.Context, name string, limit int) ([]string, error) {
	s.typeCalls++
	return s.ReadStore.OntologyPathsByType(ctx, name, limit)
}
func (s *summaryReadStore) CurrentNoteMetadataRows(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	s.inventoryCalls++
	return s.ReadStore.CurrentNoteMetadataRows(ctx)
}
func (s *summaryReadStore) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	s.metadataCalls++
	return s.ReadStore.CurrentNoteMetadataRowsByPaths(ctx, paths)
}
func (s *summaryReadStore) OntologyAssessmentFlagsByPaths(ctx context.Context, paths []string) (map[string]semdb.OntologyAssessmentFlags, error) {
	s.flagCalls++
	return s.ReadStore.OntologyAssessmentFlagsByPaths(ctx, paths)
}

func TestScopeTypeSummariesMatchesListingsWithOneCatalogRead(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
interface WorkItem { summary: String! }
type ProductSpec implements WorkItem @node(paths: ["specs/*.md"], keyField: "summary") {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
type TechnicalSpec implements WorkItem @node(paths: ["technical.md"]) { summary: String! }
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Story implements WorkItem & Section @node(locator: EMBEDDED) { summary: String! @field }
type UnusedTask implements Section @node(locator: EMBEDDED) @source(shape: LIST_ITEM, marker: "#unused") { summary: String @field }
`, "---\nsummary: A label\n---\n# Z heading\n\n## Stories\n\n### Story A\n^story-a\n\n### Story B\n^story-b\nsummary:: Second story\n")
	writeFixtureNote(t, vault.Path, "specs/product-b.md", "---\nsummary: Z label\n---\n# A heading\n")
	writeFixtureNote(t, vault.Path, "technical.md", "---\nsummary: Technical\n---\n# Technical\n")
	_, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	typeNames := []string{"ProductSpec", "TechnicalSpec", "Story", "UnusedTask", "WorkItem", ontoschema.FallbackNoteTypeName}
	for _, staged := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "staged"}[staged], func(t *testing.T) {
			options := ScopeOptions{}
			if staged {
				options.ReadOverlay = &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
					"specs/product.md": "---\nsummary: Updated label\n---\n# Z heading\n\n## Stories\n\n### Story A\n^story-a\nsummary:: Fixed staged story\n",
				}}
			}
			counted := &summaryReadStore{ReadStore: store}
			reader := &countingNoteReader{Note: &obsidian.Note{}}
			scope := NewService(vault, reader, counted, schema).NewScope(options)
			results, err := scope.TypeSummaries(ctx, typeNames, 2)
			require.NoError(t, err)
			for _, name := range typeNames {
				want, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(options).TypeInstances(ctx, TypeInstancesRequest{TypeName: name})
				require.NoError(t, err)
				got := results[name]
				require.Equal(t, want.TypeDoc, got.TypeDoc, name)
				require.Equal(t, want.Count, got.Count, name)
				require.Equal(t, want.IssueCount, got.IssueCount, name)
				require.Len(t, got.Items, min(2, len(want.Items)), name)
				for i, item := range got.Items {
					require.Equal(t, want.Items[i], item, name)
				}
			}
			require.Equal(t, 2, results["ProductSpec"].Count)
			require.Equal(t, 1, results["ProductSpec"].IssueCount)
			require.Equal(t, 1, counted.catalogCalls)
			require.Zero(t, counted.typeCalls)
			require.Zero(t, counted.inventoryCalls)
			require.Equal(t, 1, counted.metadataCalls)
			require.Equal(t, 1, counted.flagCalls)
			require.Zero(t, reader.contents)
			require.Zero(t, reader.lists)
		})
	}
}
