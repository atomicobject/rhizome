package noderead

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func testNoteMetadataIndexer(t testing.TB) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

func TestScopeTypeInstancesListsNoteAndEmbeddedInstances(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
interface SpecLike {
  summary: String!
}

type ProductSpec implements SpecLike @node(paths: ["specs/product.md"]) {
  summary: String!
  metrics: MetricsSection @contains(level: H2, heading: "Metrics")
}

type MetricsSection implements Section {
  metrics: [SpecMetric!] @contains(level: H3)
}

type SpecMetric implements SpecLike & Section @node(locator: EMBEDDED) {
  summary: String! @field
}
`, `---
summary: Product summary
---
# Product

## Metrics

### Throughput metric
summary:: Measures throughput
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{})

	product, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: "ProductSpec"})
	require.NoError(t, err)
	require.Equal(t, 1, product.Count)
	require.Equal(t, ontology.NodeKindNote, product.Items[0].Ref.Kind)

	metric, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: "SpecMetric"})
	require.NoError(t, err)
	require.Equal(t, 1, metric.Count)
	require.Equal(t, "specs/product.md#throughput-metric-56", metric.Items[0].Ref.String())

	iface, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: "SpecLike"})
	require.NoError(t, err)
	require.Equal(t, 2, iface.Count)
	limited, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: "SpecLike", Limit: 1})
	require.NoError(t, err)
	require.Len(t, limited.Items, 1)
	require.Equal(t, 1, limited.Count)
}

func TestScopeTypeInstancesSortsInterfaceByCommonIndexedFieldGlobally(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
interface SpecLike {
  summary: String!
  priority: Int!
}

type ProductSpec implements SpecLike @node(paths: ["specs/product.md", "specs/product-b.md"]) {
  summary: String!
  priority: Int!
}

type TechnicalSpec implements SpecLike @node(paths: ["technical-a.md", "technical-b.md"]) {
  summary: String!
  priority: Int!
}
`, `---
summary: Product summary
priority: 30
---
# Product
`)
	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
summary: Product B
priority: 10
---
# Product B
`)
	writeFixtureNote(t, vaultDef.Path, "technical-a.md", `---
summary: Technical A
priority: 20
---
# Technical A
`)
	writeFixtureNote(t, vaultDef.Path, "technical-b.md", `---
summary: Technical B
priority: 40
---
# Technical B
`)
	_, err := testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		SchemaState: semdb.OntologySchemaState{SchemaHash: schema.Hash, NotesHash: "notes-hash-2", LoadedAt: 2, Ready: true},
	}))
	priorities := map[string]int64{
		"specs/product.md":   30,
		"specs/product-b.md": 10,
		"technical-a.md":     20,
		"technical-b.md":     40,
	}
	typeNames := map[string]string{
		"specs/product.md":   "ProductSpec",
		"specs/product-b.md": "ProductSpec",
		"technical-a.md":     "TechnicalSpec",
		"technical-b.md":     "TechnicalSpec",
	}
	model := codeanchor.IntelOntologyNodeReadModel{FullReplace: true}
	for path, priority := range priorities {
		typeName := typeNames[path]
		nodeID := typeName + ":" + path
		model.NotePaths = append(model.NotePaths, path)
		model.Nodes = append(model.Nodes, readmodel.NodeRow{
			NodeID:        nodeID,
			NotePath:      path,
			NodeRefJSON:   mustNodeRefJSON(t, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote, TypeName: typeName}),
			NodeKind:      string(ontology.NodeKindNote),
			TypeName:      typeName,
			Title:         path,
			SourceLocator: path,
			DisplayLabel:  path,
			SchemaHash:    schema.Hash,
			UpdatedAt:     2,
		})
		value := priority
		model.FieldValues = append(model.FieldValues, readmodel.FieldValueRow{
			NodeID:     nodeID,
			NotePath:   path,
			TypeName:   typeName,
			FieldName:  "priority",
			FieldKind:  "scalar",
			SourceKind: "frontmatter",
			ValueKind:  "int",
			ValueInt:   &value,
			SchemaHash: schema.Hash,
			UpdatedAt:  2,
		})
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, model))

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "SpecLike",
		Limit:    2,
		Sort:     []codeanchor.OntologyFieldSort{{FieldName: "priority", ValueKind: "int"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"specs/product-b.md", "technical-a.md"}, nodeListItemPaths(result.Items))

	result, err = scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "SpecLike",
		Limit:    2,
		Offset:   1,
		Sort:     []codeanchor.OntologyFieldSort{{FieldName: "priority", ValueKind: "int"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"technical-a.md", "specs/product.md"}, nodeListItemPaths(result.Items))
}

func TestScopeTypeInstancesRejectsUnsupportedInterfaceSort(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
interface SpecLike {
  summary: String!
}

type ProductSpec implements SpecLike @node(paths: ["specs/product.md"]) {
  summary: String!
}

type TechnicalSpec implements SpecLike @node(paths: ["specs/technical.md"]) {
  summary: String!
  reviewers: [String!]
}
`, `---
summary: Product summary
---
# Product
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{})

	_, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{
		TypeName: "SpecLike",
		Limit:    10,
		Sort:     []codeanchor.OntologyFieldSort{{FieldName: "reviewers", ValueKind: "string"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-common or non-indexed sort fields")
}

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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
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

func TestScopeReadOverlayTypeInstancesFillsLimitedPageAfterTouchedRowLeavesFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md", "specs/product-b.md", "specs/product-c.md"]) {
  status: String @field
}
`, `---
status: open
---
# Product A
`)
	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
status: open
---
# Product B
`)
	writeFixtureNote(t, vaultDef.Path, "specs/product-c.md", `---
status: open
---
# Product C
`)
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NotNil(t, runtime.Schema)

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown",
			UpdatedContentByPath: map[string]string{"specs/product.md": "---\nstatus: closed\n---\n# Product A\n"},
		},
	})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "ProductSpec",
		Limit:    2,
		Sort:     []codeanchor.OntologyFieldSort{{FieldName: "title"}},
		Predicates: []codeanchor.OntologyFieldPredicate{{FieldName: "status", Op: codeanchor.OntologyFieldOpEq,
			Values: []codeanchor.IntelOntologyNodeFieldValue{{ValueText: "open", ValueNorm: "open"}}}},
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.Equal(t, []string{"specs/product-b.md", "specs/product-c.md"}, []string{result.Items[0].NotePath, result.Items[1].NotePath})
}

func TestScopeReadOverlayTypeInstancesAppliesTypedBoolPredicate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md", "specs/product-b.md"]) {
  ready: Boolean @field
}
`, `---
ready: true
---
# Product A
`)
	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
ready: true
---
# Product B
`)
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NotNil(t, runtime.Schema)

	ready := true
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": `---
ready: false
---
# Product A
`,
		}},
	})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "ProductSpec",
		Predicates: []codeanchor.OntologyFieldPredicate{{
			FieldName: "ready",
			Op:        readmodel.FieldOpEq,
			Values:    []readmodel.FieldValueRow{{ValueBool: &ready}},
		}},
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, []string{"specs/product-b.md"}, []string{result.Items[0].NotePath})
}

func TestScopeReadOverlayTypeInstancesPreservesTypedIntSort(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md", "specs/product-b.md"]) {
  rank: Int @field
}
`, `---
rank: 1
---
# Product A
`)
	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
rank: 2
---
# Product B
`)
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NotNil(t, runtime.Schema)

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": `---
rank: 10
---
# Product A
`,
		}},
	})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "ProductSpec",
		Sort:     []codeanchor.OntologyFieldSort{{FieldName: "rank", ValueKind: "int", NullsLast: true}},
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.Equal(t, []string{"specs/product-b.md", "specs/product.md"}, []string{result.Items[0].NotePath, result.Items[1].NotePath})
}

func TestScopeReadOverlayTypeInstancesSortsCommittedRowsBySchemaFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// specStatus is authored as spec-status, and b.md mentions an id:: inline
	// field in prose; the merged sort must still use the indexed schema values.
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md", "specs/b.md", "specs/c.md", "specs/d.md"]) {
  id: String @field
  specStatus: String @field
}
`, `---
id: S-1
spec-status: proposed
---
# A
`)
	writeFixtureNote(t, vaultDef.Path, "specs/b.md", "---\nid: S-2\nspec-status: proposed\n---\n# B\n\nPrefer a bullet such as `- id:: ^S-2-US1`. Stories follow it.\n")
	writeFixtureNote(t, vaultDef.Path, "specs/c.md", "---\nid: S-0\nspec-status: active\n---\n# C\n")
	writeFixtureNote(t, vaultDef.Path, "specs/d.md", "---\nid: S-9\nspec-status: proposed\n---\n# D\n")
	_, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": "---\nid: S-1\nspec-status: active\n---\n# A\n",
		}},
	})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "ProductSpec",
		Sort: []codeanchor.OntologyFieldSort{
			{FieldName: "specStatus", ValueKind: "string", NullsLast: true},
			{FieldName: "id", ValueKind: "string", NullsLast: true},
		},
	})
	require.NoError(t, err)
	paths := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		paths = append(paths, item.NotePath)
	}
	require.Equal(t, []string{"specs/c.md", "specs/product.md", "specs/b.md", "specs/d.md"}, paths)
}

func TestScopeReadOverlayTypeInstancesFiltersStagedRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md", "specs/b.md"]) {
  rank: Int @field
  specStatus: String @field
}
`, "---\nrank: 1\nspec-status: proposed\n---\n# A\n")
	writeFixtureNote(t, vaultDef.Path, "specs/b.md", "---\nrank: 5\nspec-status: proposed\n---\n# B\n")
	_, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	scope := func() *Scope {
		return NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
			ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
				"specs/product.md": "---\nrank: 9\nspec-status: active\n---\n# A\n",
			}},
		})
	}
	paths := func(predicate codeanchor.OntologyFieldPredicate) []string {
		result, err := scope().TypeInstances(ctx, TypeInstancesRequest{
			TypeName:   "ProductSpec",
			Predicates: []codeanchor.OntologyFieldPredicate{predicate},
		})
		require.NoError(t, err)
		out := []string{}
		for _, item := range result.Items {
			out = append(out, item.NotePath)
		}
		sort.Strings(out)
		return out
	}
	three := int64(3)
	active := codeanchor.IntelOntologyNodeFieldValue{ValueText: "active", ValueNorm: "active"}
	proposed := codeanchor.IntelOntologyNodeFieldValue{ValueText: "proposed", ValueNorm: "proposed"}

	require.Equal(t, []string{"specs/product.md"}, paths(codeanchor.OntologyFieldPredicate{
		FieldName: "specStatus", Op: codeanchor.OntologyFieldOpEq, Values: []codeanchor.IntelOntologyNodeFieldValue{active},
	}))
	require.Equal(t, []string{"specs/b.md"}, paths(codeanchor.OntologyFieldPredicate{
		FieldName: "specStatus", Op: codeanchor.OntologyFieldOpEq, Values: []codeanchor.IntelOntologyNodeFieldValue{proposed},
	}))
	require.Equal(t, []string{"specs/b.md", "specs/product.md"}, paths(codeanchor.OntologyFieldPredicate{
		FieldName: "rank", Op: codeanchor.OntologyFieldOpGT, Values: []codeanchor.IntelOntologyNodeFieldValue{{ValueInt: &three}},
	}))
	require.Equal(t, []string{}, paths(codeanchor.OntologyFieldPredicate{
		FieldName: "rank", Op: codeanchor.OntologyFieldOpLT, Values: []codeanchor.IntelOntologyNodeFieldValue{{ValueInt: &three}},
	}))

	// Ints compare at full int64 precision, beyond float64's exact range.
	big := int64(9007199254740992)
	precise := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": "---\nrank: 9007199254740993\nspec-status: active\n---\n# A\n",
		}},
	})
	result, err := precise.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "ProductSpec",
		Predicates: []codeanchor.OntologyFieldPredicate{{
			FieldName: "rank", Op: codeanchor.OntologyFieldOpGT, Values: []codeanchor.IntelOntologyNodeFieldValue{{ValueInt: &big}},
		}},
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "specs/product.md", result.Items[0].NotePath)

	// A malformed staged Int is null in the index, so it satisfies no range.
	malformed := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": "---\nrank: abc\nspec-status: active\n---\n# A\n",
		}},
	})
	result, err = malformed.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "ProductSpec",
		Predicates: []codeanchor.OntologyFieldPredicate{{
			FieldName: "rank", Op: codeanchor.OntologyFieldOpGT, Values: []codeanchor.IntelOntologyNodeFieldValue{{ValueInt: &three}},
		}},
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "specs/b.md", result.Items[0].NotePath)
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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
		ReadOverlay: &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{
			"specs/product.md": "# Product\n\n- [ ] First #task\n  status:: done\n\n- [ ] Second #task\n",
		}},
	})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName:   "Task",
		Predicates: []codeanchor.OntologyFieldPredicate{{FieldName: "status", Op: codeanchor.OntologyFieldOpExists}},
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "First", result.Items[0].Title)
	existsRef := result.Items[0].Ref
	for _, tc := range []struct {
		value string
		count int
	}{{"done", 1}, {"open", 0}} {
		result, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "Task", Predicates: []codeanchor.OntologyFieldPredicate{{
			FieldName: "status", Op: codeanchor.OntologyFieldOpEq,
			Values: []codeanchor.IntelOntologyNodeFieldValue{{ValueText: tc.value, ValueNorm: tc.value}},
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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
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
	_, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{
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

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{ReadOverlay: overlay})
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

func overlayIndexRecordForPath(t *testing.T, index *ReadOverlayIndex, path string) NodeRecord {
	t.Helper()
	for _, record := range index.RecordsByRef {
		if record.Ref.NotePath == path && record.Ref.Kind == ontology.NodeKindNote {
			return record
		}
	}
	t.Fatalf("overlay index missing note record for %s", path)
	return NodeRecord{}
}

func TestScopeResolveTargetParsingAvoidsNotePathCacheForExplicitTargets(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  title: String @field
}
`, `# Product
`)
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(context.Background(), ScopeOptions{})

	targets, diagnostics := scope.parseResolveTargets(context.Background(), ResolveRequest{
		Targets: []NodeTarget{
			{Input: `{"notePath":"specs/product.md","kind":"NOTE"}`},
			{Input: "specs/product.md#struct:abc123"},
			{Input: "http://localhost:5173/notes?note=specs/product.md#intro"},
		},
	})

	require.Empty(t, diagnostics)
	require.Len(t, targets, 3)
	require.Equal(t, "specs/product.md", targets[0].Ref.NotePath)
	require.Equal(t, "specs/product.md", targets[1].Ref.NotePath)
	require.Equal(t, ontology.NodeKindSection, targets[1].Ref.Kind)
	require.Equal(t, "abc123", targets[1].Ref.Structural)
	require.Equal(t, "specs/product.md", targets[2].Ref.NotePath)
	require.Equal(t, "intro", targets[2].Ref.Fragment)
	require.Equal(t, 0, reader.lists)
}

func TestScopeProjectionCacheSeparatesNoteAndNodeIDRefs(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  metrics: MetricsSection @contains(level: H2, heading: "Metrics")
}

type MetricsSection implements Section {
  metrics: [SpecMetric!] @contains(level: H3)
}

type SpecMetric implements Section @node(locator: EMBEDDED) {
  status: String @field
}
`, `# Product

## Metrics

### Throughput metric
status:: Planned
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{})

	root, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	require.NotNil(t, root)
	rootAgain, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	require.Same(t, root, rootAgain)

	instances, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: "SpecMetric"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 1)

	nodeIDOnly := ontology.NodeRef{
		NotePath: "specs/product.md",
		NodeID:   instances.Items[0].Ref.NodeID,
		Kind:     ontology.NodeKindEmbedded,
	}
	embedded, err := scope.Projection(context.Background(), nodeIDOnly)
	require.NoError(t, err)
	require.NotNil(t, embedded)
	require.Equal(t, ontology.NodeKindEmbedded, embedded.Ref.Kind)
	require.NotSame(t, root, embedded)
	listed, err := scope.Projection(context.Background(), instances.Items[0].Ref)
	require.NoError(t, err)
	require.NotNil(t, listed)
	listedAgain, err := scope.Projection(context.Background(), instances.Items[0].Ref)
	require.NoError(t, err)
	require.Same(t, listed, listedAgain)

	again, err := scope.Projection(context.Background(), nodeIDOnly)
	require.NoError(t, err)
	require.Same(t, embedded, again)
}

func TestScopeProjectionReturnsFallbackForUntypedNoteRoot(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type ReferenceDoc @node(paths: ["docs/*.md"]) {
  title: String @field
}
`, `# Product

Untyped root prose.

## Findings

Fallback section prose.
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{})

	projection, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	require.NotNil(t, projection)
	require.Equal(t, ontology.FallbackNoteTypeName, projection.ResolvedType)
	require.Equal(t, ontology.FallbackNoteTypeName, projection.Ref.TypeName)
	require.Equal(t, ontology.NodeKindNote, projection.Ref.Kind)
	require.Nil(t, projection.Assessment)
	require.Contains(t, strings.Join(ontology.FallbackNoteBodyParts(projection, 0), "\n"), "Untyped root prose.")
	require.NotContains(t, strings.Join(ontology.FallbackNoteBodyParts(projection, 0), "\n"), "Fallback section prose.")
}

func TestScopeProjectionDoesNotFallbackInvalidDeclaredType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, schema, body string }{
		{"unknown type", `type ProductSpec @node(paths: ["docs/*.md"]) { status: String @field }`, "---\ntype: MissingType\n---\n# Product\nInvalid type body.\n"},
		{"type outside path", `type ProductSpec @node(paths: ["docs/*.md"]) { status: String @field }`, "---\ntype: ProductSpec\n---\n# Product\nMismatched path body.\n"},
		{"ambiguous path types", "type ProductSpec @node(paths: [\"specs/*.md\"]) { status: String @field }\ntype ResearchNote @node(paths: [\"specs/*.md\"]) { status: String @field }", "# Product\nAmbiguous type body.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vaultDef, store, schema := buildFixture(t, tc.schema, tc.body)
			projection, err := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{}).Projection(context.Background(), ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote})
			require.NoError(t, err)
			require.NotNil(t, projection)
			require.Empty(t, projection.ResolvedType)
			require.NotEqual(t, ontology.FallbackNoteTypeName, projection.Ref.TypeName)
			require.NotNil(t, projection.Assessment)
			require.Empty(t, ontology.FallbackNoteBodyParts(projection, 0))
		})
	}
}

func TestRefHelpersPreserveNodeIdentity(t *testing.T) {
	t.Parallel()

	ref := NormalizeRef(ontology.NodeRef{NotePath: " docs/spec.md ", Fragment: " #^story-a "})
	require.Equal(t, ontology.NodeKindEmbedded, ref.Kind)
	require.Equal(t, "docs/spec.md", ref.NotePath)
	require.Equal(t, "^story-a", ref.Fragment)

	require.NotEqual(t,
		RefIdentityKey(ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote}),
		RefIdentityKey(ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, NodeID: "story-a"}),
	)
}

func TestScopeResolveResolvesEmbeddedBlockLinks(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type ProductSpec @node(paths: ["specs/product.md"]) {
  owner: Person @link
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  owner: Person @link
  summary: String @field
}
`, `# Product

## Stories

### Story A

^story-a
summary:: Ship precise context
`)
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(context.Background(), ScopeOptions{})

	result, err := scope.Resolve(context.Background(), ResolveRequest{
		Targets: []NodeTarget{{Input: "[[product#^story-a]]"}},
		Hydrate: HydrateOptions{Profile: HydrateContent},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	resolved := result.Resolved[0]
	require.Equal(t, ontology.NodeKindEmbedded, resolved.Ref.Kind)
	require.Equal(t, "UserStory", resolved.Ref.TypeName)
	require.Equal(t, "specs/product.md#^story-a", resolved.Locator.SourceLocator)
	require.Equal(t, "[[product#^story-a]]", resolved.Locator.LinkTarget.Wikilink)
	require.Contains(t, resolved.Record.Content, "Ship precise context")
	reader.lists = 0
	identityScope := NewService(vaultDef, reader, store, schema).NewScope(context.Background(), ScopeOptions{})
	identity, err := identityScope.Resolve(context.Background(), ResolveRequest{Targets: []NodeTarget{{Input: "[[product#^story-a]]"}}, OmitLocators: true, Hydrate: HydrateOptions{Profile: HydrateSummary}})
	require.NoError(t, err)
	require.Len(t, identity.Resolved, 1)
	require.Equal(t, "^story-a", identity.Resolved[0].Ref.Fragment)
	_, err = identityScope.Resolve(context.Background(), ResolveRequest{Targets: []NodeTarget{{Input: "[[product#^story-a]]"}}, OmitLocators: true, Hydrate: HydrateOptions{Profile: HydrateSummary}})
	require.NoError(t, err)
	require.Equal(t, 1, reader.lists)
}

func TestScopeResolveReportsAmbiguousAliasCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true)
}
`, `---
id: SPEC-A
aliases:
  - DUPE
---
# Product A
`)
	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
id: SPEC-B
aliases:
  - DUPE
---
# Product B
`)
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"specs/product.md", "specs/product-b.md"}, nil))
	_, err := ontology.SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, []string{"specs/product.md", "specs/product-b.md"}, nil)
	require.NoError(t, err)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})

	result, err := scope.Resolve(ctx, ResolveRequest{
		Targets: []NodeTarget{{Input: "[[DUPE]]"}},
		Hydrate: HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Empty(t, result.Resolved)
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, "target_ambiguous", result.Diagnostics[0].Code)
	require.Equal(t, []ontology.NodeRef{
		{NotePath: "specs/product-b.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"},
		{NotePath: "specs/product.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"},
	}, result.Diagnostics[0].Candidates)
}

func TestScopeResolveReportsAmbiguousPreferredIdentifierCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true)
}
`, `---
id: DUPE
---
# Product A
`)
	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
id: dupe
---
# Product B
`)
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"specs/product.md", "specs/product-b.md"}, nil))
	_, err := ontology.SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, []string{"specs/product.md", "specs/product-b.md"}, nil)
	require.NoError(t, err)

	for _, input := range []string{"DUPE", "[[DUPE]]"} {
		t.Run(input, func(t *testing.T) {
			scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
			result, err := scope.Resolve(ctx, ResolveRequest{
				Targets: []NodeTarget{{Input: input}},
				Hydrate: HydrateOptions{Profile: HydrateSummary},
			})
			require.NoError(t, err)
			require.Empty(t, result.Resolved)
			require.Len(t, result.Diagnostics, 1)
			require.Equal(t, "target_ambiguous", result.Diagnostics[0].Code)
			require.Equal(t, []ontology.NodeRef{
				{NotePath: "specs/product-b.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"},
				{NotePath: "specs/product.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"},
			}, result.Diagnostics[0].Candidates)
		})
	}

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	initial, err := scope.Resolve(ctx, ResolveRequest{Targets: []NodeTarget{{Input: "DUPE"}}})
	require.NoError(t, err)
	require.Empty(t, initial.Resolved)
	require.Len(t, initial.Diagnostics, 1)
	require.Equal(t, "target_ambiguous", initial.Diagnostics[0].Code)

	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
id: UNIQUE
---
# Product B
`)
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"specs/product-b.md"}, nil))
	_, err = ontology.SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, []string{"specs/product-b.md"}, nil)
	require.NoError(t, err)
	refreshed, err := scope.Resolve(ctx, ResolveRequest{Targets: []NodeTarget{{Input: "DUPE"}}})
	require.NoError(t, err)
	require.Empty(t, refreshed.Diagnostics)
	require.Len(t, refreshed.Resolved, 1)
	require.Equal(t, "specs/product.md", refreshed.Resolved[0].Ref.NotePath)
}

func TestScopeResolveUsesFallbackSectionSourceLocator(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type Spec @node(paths: ["typed/*.md"]) {
  summary: String @field
}
`, `# Product

Root fallback prose.

## Findings

Fallback section prose.
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{})

	result, err := scope.Resolve(context.Background(), ResolveRequest{
		Targets: []NodeTarget{{Input: "[[product#Findings]]"}},
		Hydrate: HydrateOptions{Profile: HydrateContent},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	resolved := result.Resolved[0]
	require.Equal(t, ontology.NodeKindSection, resolved.Ref.Kind)
	require.Equal(t, ontology.FallbackSectionTypeName, resolved.Ref.TypeName)
	require.Equal(t, "specs/product.md#findings-33", resolved.Locator.SourceLocator)
	require.Contains(t, resolved.Record.Content, "Fallback section prose.")

	rows, err := store.OntologyNodesBySourceLocators(context.Background(), []string{resolved.Locator.SourceLocator})
	require.NoError(t, err)
	require.Contains(t, rows, resolved.Locator.SourceLocator)
	require.Equal(t, ontology.FallbackSectionTypeName, rows[resolved.Locator.SourceLocator].TypeName)
}

func TestScopeResolveIndexOnlyCanonicalSelectorsUseCatalog(t *testing.T) {
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
  summary: String @field
}
`, `# Product

## Stories

### Story A
summary:: Ship precise context
`)
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	var canonical ontology.NodeRef
	for _, node := range nodes {
		if node.TypeName == "UserStory" {
			require.NoError(t, json.Unmarshal([]byte(node.NodeRefJSON), &canonical))
			break
		}
	}
	require.Equal(t, ontology.NodeKindEmbedded, canonical.Kind)
	require.NotEmpty(t, canonical.Structural)

	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(ctx, ScopeOptions{})
	inputs := []string{
		canonical.NotePath + "#struct:" + canonical.Structural,
		canonical.NotePath + "#node:" + canonical.NodeID,
		mustNodeRefJSON(t, ontology.NodeRef{NotePath: canonical.NotePath, Kind: ontology.NodeKindSection, Structural: canonical.Structural}),
	}
	for _, input := range inputs {
		result, err := scope.Resolve(ctx, ResolveRequest{
			Targets:      []NodeTarget{{Input: input}},
			IndexOnly:    true,
			OmitLocators: true,
			Hydrate:      HydrateOptions{Profile: HydrateSummary},
		})
		require.NoError(t, err)
		require.Empty(t, result.Diagnostics)
		require.Len(t, result.Resolved, 1)
		require.Equal(t, canonical, result.Resolved[0].Ref)
		require.Equal(t, "Story A", result.Resolved[0].Record.Title)
	}

	missing, err := scope.Resolve(ctx, ResolveRequest{
		Targets:      []NodeTarget{{Input: canonical.NotePath + "#struct:missing"}},
		IndexOnly:    true,
		OmitLocators: true,
		Hydrate:      HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Empty(t, missing.Resolved)
	require.Len(t, missing.Diagnostics, 1)
	require.Equal(t, "node_unresolved", missing.Diagnostics[0].Code)
	require.Zero(t, reader.contents)
	require.Zero(t, reader.lists)
}

func TestScopeResolveStructuralSelectorReturnsOverlayCanonicalRef(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, sourceSpanIdentitySchema, "# Product\n\n## Acceptance Criteria\n\n- Target item\n")
	initial := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	listed, err := initial.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	original := listed.Items[0].Ref

	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(ctx, ScopeOptions{ReadOverlay: &ReadOverlay{
		SourceFormat: "markdown",
		UpdatedContentByPath: map[string]string{
			original.NotePath: "# Product\n\n## Acceptance Criteria\n\n- New preceding item\n- Target item\n",
		},
		TouchedPaths: []string{original.NotePath},
	}})
	result, err := scope.Resolve(ctx, ResolveRequest{
		Targets:      []NodeTarget{{Input: original.NotePath + "#struct:" + original.Structural}},
		OmitLocators: true,
		Hydrate:      HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	require.Equal(t, "Target item", result.Resolved[0].Record.Title)
	require.Equal(t, result.Resolved[0].Record.Ref, result.Resolved[0].Ref)
	require.NotEqual(t, original.NodeID, result.Resolved[0].Ref.NodeID)
	require.Equal(t, original.Structural, result.Resolved[0].Ref.Structural)
	require.Zero(t, reader.contents)
}

func TestScopeResolveIndexOnlyRootNodeSelector(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	schemaSDL := strings.ReplaceAll(sourceSpanIdentitySchema, "specs/product.md", "product.md")
	vaultDef, store, _ := buildFixture(t, schemaSDL, "# Ignored\n")
	writeFixtureNote(t, vaultDef.Path, "product.md", "# Product\n\n## Acceptance Criteria\n\n- Target item\n")
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, runtime.Schema).NewScope(ctx, ScopeOptions{})
	listed, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	ref := listed.Items[0].Ref
	result, err := scope.Resolve(ctx, ResolveRequest{
		Targets:      []NodeTarget{{Input: ref.NotePath + "#node:" + ref.NodeID}},
		IndexOnly:    true,
		OmitLocators: true,
		Hydrate:      HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	require.Equal(t, ref, result.Resolved[0].Ref)
	require.Equal(t, "Target item", result.Resolved[0].Record.Title)
	require.Zero(t, reader.contents)
	require.Zero(t, reader.lists)
}

func TestScopeResolveUnwrapsCopiedNoteURLs(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type ProductSpec @node(paths: ["specs/product.md"]) {
  owner: Person @link
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
`, `# Product

## Stories

### Story A

^story-a
summary:: Ship precise context
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{})

	result, err := scope.Resolve(context.Background(), ResolveRequest{
		Targets: []NodeTarget{{Input: "http://localhost:5173/notes?note=specs/product.md#%5Estory-a"}},
		Hydrate: HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	require.Equal(t, ontology.NodeKindEmbedded, result.Resolved[0].Ref.Kind)
	require.Equal(t, "^story-a", result.Resolved[0].Ref.Fragment)
	require.Equal(t, "UserStory", result.Resolved[0].Ref.TypeName)
}

func TestScopeTypeInstancesHidesFallbackTypeFromPublicReadBoundary(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["typed/*.md"]) {
  summary: String
}
`, `# Untyped

Fallback note body.
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{})

	fallback, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: ontology.FallbackNoteTypeName})
	require.NoError(t, err)
	require.Nil(t, fallback.TypeDoc)
	require.Zero(t, fallback.Count)
	require.Empty(t, fallback.Items)

	allNotes, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: ontology.TypeScopeAll})
	require.NoError(t, err)
	require.Len(t, allNotes.Items, 1)
	require.Empty(t, allNotes.Items[0].ResolvedType, "public all-notes listings should not expose internal fallback schema semantics")

	nodes, err := store.OntologyNodesByPaths(context.Background(), []string{"specs/product.md"})
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, 1, countNodeReadTestNodesByType(nodes, ontology.FallbackNoteTypeName), "fallback note remains internal catalog/search evidence")
	require.Equal(t, 1, countNodeReadTestNodesByType(nodes, ontology.FallbackSectionTypeName), "fallback sections remain internal catalog/search evidence")
}

func TestScopeTypeInstancesShowsAuthoredDefaultNoteType(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type GeneralNote @node(default: true) {
  title: String
}
`, `# Untyped

Authored default note body.
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{})

	defaultNotes, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: "GeneralNote"})
	require.NoError(t, err)
	require.NotNil(t, defaultNotes.TypeDoc)
	require.Equal(t, 1, defaultNotes.Count)
	require.Len(t, defaultNotes.Items, 1)
	require.Equal(t, "GeneralNote", defaultNotes.Items[0].ResolvedType)

	allNotes, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: ontology.TypeScopeAll})
	require.NoError(t, err)
	require.Len(t, allNotes.Items, 1)
	require.Equal(t, "GeneralNote", allNotes.Items[0].ResolvedType)

	nodes, err := store.OntologyNodesByPaths(context.Background(), []string{"specs/product.md"})
	require.NoError(t, err)
	require.Equal(t, 1, countNodeReadTestNodesByType(nodes, "GeneralNote"))
	require.Equal(t, 0, countNodeReadTestNodesByType(nodes, ontology.FallbackNoteTypeName))
}

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
	scope := service.NewScope(ctx, ScopeOptions{})
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

func TestCloneNodeRecordPreservesNilFieldValues(t *testing.T) {
	cloned := cloneNodeRecord(NodeRecord{Path: "notes/plain.md", FieldValues: nil})
	require.Nil(t, cloned.FieldValues)

	cloned = cloneNodeRecord(NodeRecord{Path: "notes/catalog.md", FieldValues: map[string][]codeanchor.IntelOntologyNodeFieldValue{}})
	require.NotNil(t, cloned.FieldValues)
	require.Empty(t, cloned.FieldValues)
}

func TestScopeTypeInstancesUsesCatalogForEmbeddedTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  metrics: MetricsSection @contains(level: H2, heading: "Metrics")
}

type MetricsSection implements Section {
  metrics: [SpecMetric!] @contains(level: H3)
}

type SpecMetric implements Section @node(locator: EMBEDDED) {
  summary: String! @field
}
`, `# Product

## Metrics

### Throughput metric
summary:: Measures throughput
^throughput
`)
	counting := &countingStore{Store: store}
	scope := NewService(vaultDef, nil, counting, schema).NewScope(ctx, ScopeOptions{})

	instances, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "SpecMetric"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 1)
	require.Equal(t, "^throughput", instances.Items[0].Ref.Fragment)
	require.Equal(t, "Throughput metric", instances.Items[0].Title)
	require.Equal(t, 1, scope.Diagnostics().CatalogHits)
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
	scope := NewService(vaultDef, nil, store, schema).NewScope(ctx, ScopeOptions{})
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

func TestScopeGraphMergesOntologyEmbeddedAndUntypedEdges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	storyRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "story-a", Fragment: "^story-a"}
	criteriaSectionRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindSection, TypeName: "AcceptanceCriteriaSection", NodeID: "criteria-section", Fragment: "Acceptance Criteria"}
	criterionRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "AcceptanceCriterion", NodeID: "criterion-a", Fragment: "^criterion-a"}
	specRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote, TypeName: "TechnicalSpec"}
	storyRefJSON := mustNodeRefJSON(t, storyRef)
	criteriaSectionRefJSON := mustNodeRefJSON(t, criteriaSectionRef)
	criterionRefJSON := mustNodeRefJSON(t, criterionRef)
	specRefJSON := mustNodeRefJSON(t, specRef)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/effort.md", RelationName: "frozenSpecs", DstPath: "docs/spec.md", DstType: "TechnicalSpec", Provenance: "field", Structural: true},
			{SrcPath: "docs/effort.md", RelationName: "frozenStories", DstPath: "docs/spec.md", DstNodeID: ontology.OntologyNodeID(storyRef), DstType: "UserStory", Provenance: "field", Structural: true},
			{SrcPath: "docs/spec.md", SrcNodeID: "story-a", RelationName: "owner", DstPath: "docs/person.md", DstType: "ReferenceDoc", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []codeanchor.IntelOntologyNode{
		{NodeID: ontology.OntologyNodeID(specRef), NotePath: "docs/spec.md", NodeRefJSON: specRefJSON, NodeKind: string(ontology.NodeKindNote), TypeName: "TechnicalSpec", DisplayLabel: "Spec", SourceLocator: "docs/spec.md", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(storyRef), NotePath: "docs/spec.md", NodeRefJSON: storyRefJSON, NodeKind: string(ontology.NodeKindEmbedded), TypeName: "UserStory", ParentNodeID: ontology.OntologyNodeID(specRef), DisplayLabel: "Story A", SourceLocator: "docs/spec.md#Story A", Fragment: "Story A", BlockID: "story-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(criteriaSectionRef), NotePath: "docs/spec.md", NodeRefJSON: criteriaSectionRefJSON, NodeKind: string(ontology.NodeKindSection), TypeName: "AcceptanceCriteriaSection", ParentNodeID: ontology.OntologyNodeID(storyRef), DisplayLabel: "Acceptance Criteria", SourceLocator: "docs/spec.md#Acceptance Criteria", Fragment: "Acceptance Criteria", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(criterionRef), NotePath: "docs/spec.md", NodeRefJSON: criterionRefJSON, NodeKind: string(ontology.NodeKindEmbedded), TypeName: "AcceptanceCriterion", ParentNodeID: ontology.OntologyNodeID(criteriaSectionRef), DisplayLabel: "Criterion A", SourceLocator: "docs/spec.md#^criterion-a", Fragment: "^criterion-a", BlockID: "criterion-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/untyped.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/spec.md"}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/effort.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/spec.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ctx, ScopeOptions{})
	graph, err := scope.Graph(ctx, GraphRequest{
		Sources:     []ontology.NodeRef{{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote}, {NotePath: "docs/effort.md", Kind: ontology.NodeKindNote}, {NotePath: "docs/untyped.md", Kind: ontology.NodeKindNote}},
		Profile:     GraphProfileNotesOnly,
		Diagnostics: true,
	})
	require.NoError(t, err)
	storyEndpoint := "embedded:" + ontology.OntologyNodeID(storyRef)
	criterionEndpoint := "embedded:" + ontology.OntologyNodeID(criterionRef)
	require.Contains(t, graphEndpointIDs(graph.Nodes), "embedded:"+ontology.OntologyNodeID(storyRef))
	require.Contains(t, graphEndpointIDs(graph.Nodes), criterionEndpoint)
	require.Contains(t, graphEndpointIDs(graph.Nodes), "note:docs/untyped.md")
	require.Contains(t, graphEdgeKinds(graph.Edges), "embeds")
	require.Contains(t, graphEdgePairKinds(graph.Edges), storyEndpoint+"|"+criterionEndpoint+"|embeds")
	require.NotContains(t, graphEdgePairKinds(graph.Edges), "note:docs/spec.md|"+criterionEndpoint+"|embeds")
	require.Contains(t, graphEdgeRelationNames(graph.Edges), "frozenStories")
	require.Contains(t, graphEdgeRelationNames(graph.Edges), "frozenSpecs")
	require.Contains(t, graphEdgeRelationNames(graph.Edges), "owner")
	require.Contains(t, graphEdgePairKinds(graph.Edges), "embedded:"+ontology.OntologyNodeID(storyRef)+"|note:docs/person.md|ontology")
	require.NotContains(t, graphEdgePairKinds(graph.Edges), "note:docs/effort.md|note:docs/spec.md|wikilink")
	require.Contains(t, graphEdgePairKinds(graph.Edges), "note:docs/untyped.md|note:docs/spec.md|wikilink")
	require.Equal(t, "docs/spec.md#^story-a", graphEndpointByID(graph.Nodes, "embedded:"+ontology.OntologyNodeID(storyRef)).SourceLocator)
	require.NotNil(t, graph.Diagnostics)
	require.NotEmpty(t, graph.Diagnostics.Nodes)
	require.NotEmpty(t, graph.Diagnostics.Edges)
	require.NotEmpty(t, graph.Diagnostics.Dedupe)
}

func TestScopeGraphFactsCarriesTypedEmbeddedAndDocEdgeMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	storyRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "story-a", Fragment: "^story-a"}
	specRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote, TypeName: "TechnicalSpec"}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/effort.md", RelationName: "frozenStories", DstPath: "docs/spec.md", DstNodeID: "story-a", DstType: "UserStory", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []codeanchor.IntelOntologyNode{
		{NodeID: ontology.OntologyNodeID(specRef), NotePath: "docs/spec.md", NodeRefJSON: mustNodeRefJSON(t, specRef), NodeKind: string(ontology.NodeKindNote), TypeName: "TechnicalSpec", DisplayLabel: "Spec", SourceLocator: "docs/spec.md", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(storyRef), NotePath: "docs/spec.md", NodeRefJSON: mustNodeRefJSON(t, storyRef), NodeKind: string(ontology.NodeKindEmbedded), TypeName: "UserStory", ParentNodeID: ontology.OntologyNodeID(specRef), DisplayLabel: "Story A", SourceLocator: "docs/spec.md#^story-a", BlockID: "story-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/untyped.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/spec.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ctx, ScopeOptions{})
	facts, err := scope.GraphFacts(ctx, GraphFactsRequest{
		Paths:           []string{"docs/effort.md", "docs/spec.md", "docs/untyped.md"},
		IncludeOntology: true,
		IncludeDocLinks: true,
		IncludeEmbedded: true,
	})
	require.NoError(t, err)

	storyEndpoint := "embedded:" + ontology.OntologyNodeID(storyRef)
	require.Contains(t, graphFactPairKinds(facts.Edges), "note:docs/effort.md|"+storyEndpoint+"|ontology")
	require.Contains(t, graphFactPairKinds(facts.Edges), "note:docs/spec.md|"+storyEndpoint+"|embeds")
	require.Contains(t, graphFactPairKinds(facts.Edges), "note:docs/untyped.md|note:docs/spec.md|wikilink")
	frozen := graphFactByPairKind(facts.Edges, "note:docs/effort.md", storyEndpoint, "ontology")
	require.Equal(t, "frozenStories", frozen.RelationName)
	require.Equal(t, ontology.NodeKindEmbedded, frozen.TargetRef.Kind)
	require.Equal(t, "docs/spec.md#^story-a", frozen.TargetLocator)
	require.Equal(t, 1.0, frozen.Confidence)
}

func TestScopeGraphDoesNotLetEmbeddedRowsOverwriteNoteEndpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	embeddedRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "aaa-story", Fragment: "^story-a"}
	noteRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote, TypeName: "TechnicalSpec"}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []codeanchor.IntelOntologyNode{
		{
			NodeID:        "aaa-embedded",
			NotePath:      "docs/spec.md",
			NodeRefJSON:   mustNodeRefJSON(t, embeddedRef),
			NodeKind:      string(ontology.NodeKindEmbedded),
			TypeName:      "UserStory",
			ParentNodeID:  "zzz-note",
			DisplayLabel:  "Story A",
			SourceLocator: "docs/spec.md#^story-a",
			BlockID:       "story-a",
			LocatorStatus: string(ontology.NodeLocatorLinkable),
			UpdatedAt:     1,
		},
		{
			NodeID:        "zzz-note",
			NotePath:      "docs/spec.md",
			NodeRefJSON:   mustNodeRefJSON(t, noteRef),
			NodeKind:      string(ontology.NodeKindNote),
			TypeName:      "TechnicalSpec",
			DisplayLabel:  "Spec",
			SourceLocator: "docs/spec.md",
			LocatorStatus: string(ontology.NodeLocatorLinkable),
			UpdatedAt:     1,
		},
	}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ctx, ScopeOptions{})
	graph, err := scope.Graph(ctx, GraphRequest{
		Sources: []ontology.NodeRef{{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote}},
		Profile: GraphProfileNotesOnly,
	})
	require.NoError(t, err)

	note := graphEndpointByID(graph.Nodes, "note:docs/spec.md")
	require.Equal(t, "TechnicalSpec", note.TypeName)
	require.Equal(t, "zzz-note", note.NodeID)
	require.Equal(t, ontology.NodeKindNote, note.Ref.Kind)
	require.Equal(t, "docs/spec.md", note.SourceLocator)
}

func TestScopeGraphEmbeddedSourceIsEndpointScoped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	storyRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory", NodeID: "story-a", Fragment: "^story-a"}
	specRef := ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote, TypeName: "TechnicalSpec"}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/spec.md", RelationName: "related", DstPath: "docs/whole-note.md", DstType: "ReferenceDoc", Provenance: "field", Structural: true},
			{SrcPath: "docs/spec.md", SrcNodeID: "story-a", RelationName: "owner", DstPath: "docs/person.md", DstType: "ReferenceDoc", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{"docs/spec.md"}, []codeanchor.IntelOntologyNode{
		{NodeID: ontology.OntologyNodeID(specRef), NotePath: "docs/spec.md", NodeRefJSON: mustNodeRefJSON(t, specRef), NodeKind: string(ontology.NodeKindNote), TypeName: "TechnicalSpec", DisplayLabel: "Spec", SourceLocator: "docs/spec.md", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
		{NodeID: ontology.OntologyNodeID(storyRef), NotePath: "docs/spec.md", NodeRefJSON: mustNodeRefJSON(t, storyRef), NodeKind: string(ontology.NodeKindEmbedded), TypeName: "UserStory", ParentNodeID: ontology.OntologyNodeID(specRef), DisplayLabel: "Story A", SourceLocator: "docs/spec.md#^story-a", BlockID: "story-a", LocatorStatus: string(ontology.NodeLocatorLinkable), UpdatedAt: 1},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/spec.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/parent-link.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ctx, ScopeOptions{})
	graph, err := scope.Graph(ctx, GraphRequest{
		Sources: []ontology.NodeRef{storyRef},
		Profile: GraphProfileCodeAware,
	})
	require.NoError(t, err)

	pairs := graphEdgePairKinds(graph.Edges)
	storyEndpoint := "embedded:" + ontology.OntologyNodeID(storyRef)
	require.Contains(t, pairs, storyEndpoint+"|note:docs/person.md|ontology")
	require.Contains(t, pairs, "note:docs/spec.md|"+storyEndpoint+"|embeds")
	require.NotContains(t, pairs, "note:docs/spec.md|note:docs/whole-note.md|ontology")
	require.NotContains(t, pairs, "note:docs/spec.md|note:docs/parent-link.md|wikilink")
}

func TestScopeGraphUnresolvedEndpointSourceDoesNotWidenScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/spec.md", RelationName: "related", DstPath: "docs/whole-note.md", DstType: "ReferenceDoc", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/spec.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/parent-link.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ctx, ScopeOptions{})
	graph, err := scope.Graph(ctx, GraphRequest{
		Sources: []ontology.NodeRef{{NotePath: "docs/spec.md", Kind: ontology.NodeKindEmbedded, NodeID: "missing-story"}},
		Profile: GraphProfileCodeAware,
	})
	require.NoError(t, err)
	require.Empty(t, graph.Edges)
}

func TestScopeGraphFactsAppliesLimitsAfterKindFiltering(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "docs/a.md", RelationName: "typed", DstPath: "docs/b.md", DstType: "Spec", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/c.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/d.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ctx, ScopeOptions{})
	facts, err := scope.GraphFacts(ctx, GraphFactsRequest{
		IncludeOntology: false,
		IncludeDocLinks: true,
		EdgeLimit:       1,
	})
	require.NoError(t, err)
	require.Len(t, facts.Edges, 1)
	require.Equal(t, "wikilink", facts.Edges[0].Kind)
	require.Equal(t, "docs/c.md", facts.Edges[0].SourcePath)
}

func TestScopeCodeGraphLinksReadsIncidentCodeLinks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "pkg/app.go", []codeanchor.DocLink{
		{
			SrcType: "code",
			SrcPath: "pkg/app.go",
			DstKind: "note",
			DstPath: "docs/spec.md",
		},
	}))
	require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "docs/spec.md", semdb.GraphDocEdgeKindWikilink, []string{"docs/related.md"}))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ctx, ScopeOptions{})
	links, err := scope.CodeGraphLinks(ctx, ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote}, 50)
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, GraphCodeLink{
		Path:       "pkg/app.go",
		Kind:       "coderef",
		Provenance: "coderef",
		Weight:     1,
	}, links[0])
}

func TestScopeGraphFactsIncludesCodeCodeEdgesOnlyWhenCallsIncluded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/a.go", []codeanchor.IntelAnchor{{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/a.go",
		Symbol:      "A",
		FQN:         "pkg.A",
		Fingerprint: "fp-a",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/b.go", []codeanchor.IntelAnchor{{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/b.go",
		Symbol:      "B",
		FQN:         "pkg.B",
		Fingerprint: "fp-b",
	}}, []codeanchor.IntelEdge{{
		SrcID: "b1",
		DstID: "a1",
		Kind:  "calls",
	}}, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "docs/spec.md", []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "docs/spec.md",
		Title:       "Spec",
		Level:       1,
		Content:     "Calls A",
		Fingerprint: "fp-section",
	}}, []codeanchor.IntelEdge{{
		SrcID: "s1",
		DstID: "a1",
		Kind:  "mentions",
	}}, nil))

	scope := NewService(obsidian.VaultDefinition{Path: root}, nil, store, nil).NewScope(ctx, ScopeOptions{})
	withoutCalls, err := scope.GraphFacts(ctx, GraphFactsRequest{
		Paths:           []string{"docs/spec.md", "pkg/a.go", "pkg/b.go"},
		IncludeDocLinks: true,
		IncludeCode:     true,
	})
	require.NoError(t, err)
	require.Contains(t, graphFactPairKinds(withoutCalls.Edges), "note:docs/spec.md|code:pkg/a.go|mentions")
	require.NotContains(t, graphFactPairKinds(withoutCalls.Edges), "code:pkg/b.go|code:pkg/a.go|calls")

	withCalls, err := scope.GraphFacts(ctx, GraphFactsRequest{
		Paths:           []string{"docs/spec.md", "pkg/a.go", "pkg/b.go"},
		IncludeDocLinks: true,
		IncludeCode:     true,
		IncludeCalls:    true,
	})
	require.NoError(t, err)
	require.Contains(t, graphFactPairKinds(withCalls.Edges), "code:pkg/b.go|code:pkg/a.go|calls")
}

func TestScopeRelationCountsUsesBatchedEdgesWithoutHydration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "decisions", DstPath: "notes/c.md", DstType: "Decision", Structural: true},
			{SrcPath: "notes/c.md", RelationName: "back", DstPath: "notes/a.md", DstType: "Project", Structural: false},
			{SrcPath: "notes/b.md", SrcNodeID: "story", RelationName: "owner", DstPath: "notes/a.md", DstType: "Project", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})

	result, err := scope.RelationCounts(ctx, RelationCountsRequest{
		Sources:           []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}, {NotePath: "notes/b.md", Kind: ontology.NodeKindNote}},
		Direction:         TraversalDirectionBoth,
		IncludeStructural: true,
		IncludeAmbient:    true,
	})
	require.NoError(t, err)
	require.Equal(t, 3, result.BySource["notes/a.md"].Count)
	require.Equal(t, 1, result.BySource["notes/b.md"].Count)
	require.Equal(t, 1, scope.Diagnostics().EdgeLoads)
	require.Zero(t, scope.Diagnostics().RecordLoads)
}

func TestScopeRelationCountsMatchesUnboundedNeighborSelections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "efforts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "main.md"), []byte("# Main\n\n### Story ^story-a\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "efforts", "one.md"), []byte("# One\n\n[[main#^story-a]]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "efforts", "two.md"), []byte("# Two\n"), 0o644))
	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{}, Interfaces: map[string]*ontology.InterfaceType{}}
	decisions := &ontology.Field{Name: "decisions", Kind: ontology.FieldKindNeighbor, TypeName: "Decision", List: true, Direction: ontology.NeighborDirectionOutbound, Scope: ontology.NeighborScopeNote}
	efforts := &ontology.Field{Name: "efforts", Kind: ontology.FieldKindNeighbor, TypeName: "Effort", List: true, Direction: ontology.NeighborDirectionInbound, Scope: ontology.NeighborScopeSubtree}
	schema.Types["Project"] = &ontology.NoteType{Name: "Project", Role: ontology.TypeRoleNote, Fields: []*ontology.Field{decisions}, ByName: map[string]*ontology.Field{"decisions": decisions}}
	schema.Types["Decision"] = &ontology.NoteType{Name: "Decision", Role: ontology.TypeRoleNote, ByName: map[string]*ontology.Field{}}
	schema.Types["Person"] = &ontology.NoteType{Name: "Person", Role: ontology.TypeRoleNote, ByName: map[string]*ontology.Field{}}
	schema.Types["UserStory"] = &ontology.NoteType{Name: "UserStory", Role: ontology.TypeRoleEmbeddedNode, Fields: []*ontology.Field{efforts}, ByName: map[string]*ontology.Field{"efforts": efforts}}
	schema.Types["Effort"] = &ontology.NoteType{Name: "Effort", Role: ontology.TypeRoleNote, ByName: map[string]*ontology.Field{}}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{
			{NotePath: "projects/a.md", TypeName: "Project"},
			{NotePath: "projects/b.md", TypeName: "Project"},
			{NotePath: "decisions/one.md", TypeName: "Decision"},
			{NotePath: "people/alice.md", TypeName: "Person"},
			{NotePath: "efforts/one.md", TypeName: "Effort"},
			{NotePath: "efforts/two.md", TypeName: "Effort"},
		},
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "projects/a.md", RelationName: "decisions", DstPath: "decisions/one.md", DstType: "Decision", Provenance: "body_link"},
			{SrcPath: "projects/a.md", RelationName: "decisions", DstPath: "people/alice.md", DstType: "Person", Provenance: "body_link"},
			{SrcPath: "projects/b.md", RelationName: "decisions", DstPath: "decisions/one.md", DstType: "Decision", Provenance: "body_link"},
			{SrcPath: "efforts/two.md", RelationName: "stories", DstPath: "specs/main.md", DstNodeID: "specs/main.md#^story-a", DstType: "UserStory", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	projects := []ontology.NodeRef{
		{NotePath: "projects/a.md", Kind: ontology.NodeKindNote, TypeName: "Project"},
		{NotePath: "projects/b.md", Kind: ontology.NodeKindNote, TypeName: "Project"},
	}
	projectSelection, ok := RelationSelectionForField(schema, decisions)
	require.True(t, ok)
	projectSelection.Sources = projects
	list, err := scope.executeSingleRelationSelection(ctx, projectSelection, TraverseLimits{})
	require.NoError(t, err)
	counts, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: projects, Selections: []RelationSelection{projectSelection}})
	require.NoError(t, err)
	require.Equal(t, len(list.Sources[0].Edges), counts.BySource[RefIdentityKey(projects[0])].Count)
	require.Equal(t, 1, counts.BySource[RefIdentityKey(projects[0])].Count)
	require.Equal(t, 1, counts.BySource[RefIdentityKey(projects[1])].Count)

	story := ontology.NodeRef{NotePath: "specs/main.md", Fragment: "^story-a", NodeID: "specs/main.md#^story-a", Kind: ontology.NodeKindEmbedded, TypeName: "UserStory"}
	storySelection, ok := RelationSelectionForField(schema, efforts)
	require.True(t, ok)
	storySelection.Sources = []ontology.NodeRef{story}
	storyList, err := scope.executeSingleRelationSelection(ctx, storySelection, TraverseLimits{})
	require.NoError(t, err)
	storyCounts, err := scope.RelationCounts(ctx, RelationCountsRequest{Sources: []ontology.NodeRef{story}, Selections: []RelationSelection{storySelection}})
	require.NoError(t, err)
	require.Len(t, storyList.Edges, 2)
	require.Equal(t, len(storyList.Edges), storyCounts.BySource[RefIdentityKey(story)].Count)
	require.Equal(t, 3, scope.Diagnostics().EdgeLoads)
}

func TestScopeTraverseLimitPerSourceDoesNotStarveLaterSources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	edges := make([]semdb.OntologyEdgeRow, 0, 10)
	for i := 0; i < 9; i++ {
		edges = append(edges, semdb.OntologyEdgeRow{
			SrcPath:      "notes/a.md",
			RelationName: "linked",
			DstPath:      filepath.ToSlash(filepath.Join("notes", "a", "target"+string(rune('a'+i))+".md")),
			DstType:      "Reference",
			Provenance:   "outbound",
			Structural:   false,
		})
	}
	edges = append(edges, semdb.OntologyEdgeRow{
		SrcPath:      "notes/b.md",
		RelationName: "linked",
		DstPath:      "notes/b/target.md",
		DstType:      "Reference",
		Provenance:   "outbound",
		Structural:   false,
	})
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges:       edges,
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})

	result, err := scope.Traverse(ctx, TraverseRequest{
		Sources: []ontology.NodeRef{
			{NotePath: "notes/a.md", Kind: ontology.NodeKindNote},
			{NotePath: "notes/b.md", Kind: ontology.NodeKindNote},
		},
		Provenance:     map[string]struct{}{"outbound": {}},
		LimitPerSource: 1,
	})
	require.NoError(t, err)
	require.Len(t, result.EdgesBySource["notes/a.md"], 1)
	require.Len(t, result.EdgesBySource["notes/b.md"], 1)
	require.Equal(t, "notes/b/target.md", result.EdgesBySource["notes/b.md"][0].DstPath)
}

func TestScopeNeighborhoodSupportsDirectionsAndFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{
			{NotePath: "notes/in.md", TypeName: "ReferenceDoc", SchemaHash: "schema", UpdatedAt: 1},
			{NotePath: "notes/out.md", TypeName: "ReferenceDoc", SchemaHash: "schema", UpdatedAt: 1},
			{NotePath: "notes/skip.md", TypeName: "Decision", SchemaHash: "schema", UpdatedAt: 1},
		},
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "references", DstPath: "notes/out.md", DstType: "ReferenceDoc", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/in.md", RelationName: "references", DstPath: "notes/a.md", DstType: "ProductSpec", Provenance: "backlink", Structural: false},
			{SrcPath: "notes/a.md", RelationName: "owner", DstPath: "notes/owner.md", DstType: "Person", Provenance: "field", Structural: true},
			{SrcPath: "notes/a.md", RelationName: "references", DstPath: "notes/skip.md", DstType: "Decision", Provenance: "body_link", Structural: false},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})

	result, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources:        []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}},
		Direction:      TraversalDirectionBoth,
		RelationNames:  []string{"references"},
		Provenance:     map[string]struct{}{"body_link": {}, "backlink": {}},
		IncludeAmbient: true,
		TargetTypes:    []string{"ReferenceDoc"},
	})
	require.NoError(t, err)
	require.Len(t, result.Edges, 2)
	require.Equal(t, []string{"notes/in.md", "notes/out.md"}, []string{result.Edges[0].Target.NotePath, result.Edges[1].Target.NotePath})
	require.Len(t, result.BySource["notes/a.md"].Edges, 2)
	require.False(t, result.Edges[0].Structural)
	require.Equal(t, TraversalDirectionInbound, result.Edges[0].Direction)
	require.Equal(t, TraversalDirectionOutbound, result.Edges[1].Direction)
}

func TestScopeNeighborhoodKeepsNodeScopedEdgesOnMatchingEmbeddedSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/spec.md", RelationName: "owner", DstPath: "people/note-owner.md", DstType: "Person", Provenance: "field", Structural: true},
			{SrcPath: "notes/spec.md", SrcNodeID: "story-a", RelationName: "owner", DstPath: "people/alice.md", DstType: "Person", Provenance: "field", Structural: true},
			{SrcPath: "notes/spec.md", SrcNodeID: "story-b", RelationName: "owner", DstPath: "people/bob.md", DstType: "Person", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})

	result, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources:           []ontology.NodeRef{{NotePath: "notes/spec.md", Kind: ontology.NodeKindEmbedded, NodeID: "story-a"}},
		RelationNames:     []string{"owner"},
		IncludeStructural: true,
	})
	require.NoError(t, err)
	require.Len(t, result.Edges, 1)
	require.Equal(t, "people/alice.md", result.Edges[0].Target.NotePath)
	require.Equal(t, "story-a", result.Edges[0].Source.NodeID)
	require.Empty(t, result.Edges[0].Target.NodeID)

	noteResult, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources:           []ontology.NodeRef{{NotePath: "notes/spec.md", Kind: ontology.NodeKindNote}},
		RelationNames:     []string{"owner"},
		IncludeStructural: true,
	})
	require.NoError(t, err)
	require.Len(t, noteResult.Edges, 1)
	require.Equal(t, "people/note-owner.md", noteResult.Edges[0].Target.NotePath)
}

func TestScopeNeighborhoodKeepsEmbeddedSourcesInSameNoteDistinct(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/spec.md", SrcNodeID: "story-a", RelationName: "owner", DstPath: "people/alice.md", DstType: "Person", Provenance: "field", Structural: true},
			{SrcPath: "notes/spec.md", SrcNodeID: "story-b", RelationName: "owner", DstPath: "people/bob.md", DstType: "Person", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})

	result, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources: []ontology.NodeRef{
			{NotePath: "notes/spec.md", Kind: ontology.NodeKindEmbedded, NodeID: "story-a"},
			{NotePath: "notes/spec.md", Kind: ontology.NodeKindEmbedded, NodeID: "story-b"},
		},
		RelationNames:     []string{"owner"},
		IncludeStructural: true,
	})
	require.NoError(t, err)
	require.Len(t, result.Sources, 2)
	require.Len(t, result.Edges, 2)
	require.Equal(t, []string{"people/alice.md"}, neighborhoodTargetPaths(result.Sources[0].Edges))
	require.Equal(t, []string{"people/bob.md"}, neighborhoodTargetPaths(result.Sources[1].Edges))
}

func TestScopeNeighborhoodCanonicalizesHydratedEmbeddedTargetsWithBlockIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
  metrics: MetricsSection @contains(level: H2, heading: "Metrics")
}


type MetricsSection implements Section {
  metrics: [SpecMetric!] @contains(level: H3)
}

type SpecMetric implements Section @node(locator: EMBEDDED) {
  summary: String! @field
}
`, `---
summary: Product summary
---
# Product

## Metrics

### Throughput
summary:: Measures throughput
^throughput

### Latency
summary:: Measures latency
^latency
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	instances, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "SpecMetric"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 2)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "specs/product.md", RelationName: "features", DstPath: "specs/product.md", DstNodeID: instances.Items[0].Ref.NodeID, DstType: "SpecMetric", Provenance: "field", Structural: true},
			{SrcPath: "specs/product.md", RelationName: "features", DstPath: "specs/product.md", DstNodeID: instances.Items[1].Ref.NodeID, DstType: "SpecMetric", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))

	result, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources:           []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}},
		RelationNames:     []string{"features"},
		IncludeStructural: true,
		Hydrate:           HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Edges, 2)
	require.Equal(t, []string{"^latency", "^throughput"}, sortedFragments([]ontology.NodeRef{result.Edges[0].Target, result.Edges[1].Target}))
	require.Len(t, result.Nodes, 2)
	require.Equal(t, []string{"^latency", "^throughput"}, sortedRecordFragments(result.Nodes))
	require.NotNil(t, result.Nodes[0].NodeLocator)
	require.NotNil(t, result.Nodes[0].LinkTarget)
}

type noCatalogStore struct{ Store }

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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
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

	// A Store without catalog methods resolves the requested edge locators
	// through the existing projection fallback.
	fallback := NewService(vaultDef, &obsidian.Note{}, noCatalogStore{Store: store}, schema).NewScope(ctx, ScopeOptions{})
	fallbackList, err := fallback.executeSingleRelationSelection(ctx, selection, TraverseLimits{})
	require.NoError(t, err)
	require.Len(t, fallbackList.Edges, 2)
	for _, edge := range fallbackList.Edges {
		require.Equal(t, "ZStory", edge.Target.TypeName)
	}
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
	storyNodes := map[string]codeanchor.IntelOntologyNode{}
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

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
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

func TestScopeNeighborhoodEnforcesLimitsAndTruncation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "linked", DstPath: "notes/a1.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/a.md", RelationName: "linked", DstPath: "notes/a2.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/b.md", RelationName: "linked", DstPath: "notes/b1.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/b.md", RelationName: "linked", DstPath: "notes/b2.md", DstType: "Reference", Provenance: "body_link", Structural: false},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})

	result, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources:        []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}, {NotePath: "notes/b.md", Kind: ontology.NodeKindNote}},
		RelationNames:  []string{"linked"},
		IncludeAmbient: true,
		FirstPerSource: 1,
		FirstTotal:     2,
	})
	require.NoError(t, err)
	require.Len(t, result.Edges, 2)
	require.Len(t, result.BySource["notes/a.md"].Edges, 1)
	require.Len(t, result.BySource["notes/b.md"].Edges, 1)
	require.True(t, result.BySource["notes/a.md"].Truncated)
	require.True(t, result.BySource["notes/b.md"].Truncated)
}

func TestScopeNeighborhoodHydratesTargetsOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
}

type ReferenceDoc @node(paths: ["specs/reference.md"]) {
  summary: String!
}
`, `---
summary: Product summary
---
# Product
`)
	require.NoError(t, os.WriteFile(filepath.Join(vaultDef.Path, "specs", "reference.md"), []byte(`---
summary: Reference summary
---
# Reference
`), 0o644))
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"specs/reference.md"}, nil))
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Assessments: []semdb.OntologyNoteAssessmentRow{
			{NotePath: "specs/product.md", ResolvedType: "ProductSpec", SchemaHash: "schema", UpdatedAt: 1},
			{NotePath: "specs/reference.md", ResolvedType: "ReferenceDoc", SchemaHash: "schema", UpdatedAt: 1},
		},
		NoteTypes: []semdb.OntologyNoteTypeRow{
			{NotePath: "specs/product.md", TypeName: "ProductSpec", SchemaHash: "schema", UpdatedAt: 1},
			{NotePath: "specs/reference.md", TypeName: "ReferenceDoc", SchemaHash: "schema", UpdatedAt: 1},
		},
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "specs/product.md", RelationName: "references", DstPath: "specs/reference.md", DstType: "ReferenceDoc", Provenance: "body_link", Structural: false},
			{SrcPath: "specs/other.md", RelationName: "references", DstPath: "specs/reference.md", DstType: "ReferenceDoc", Provenance: "backlink", Structural: false},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	counting := &countingStore{Store: store}
	scope := NewService(vaultDef, &obsidian.Note{}, counting, schema).NewScope(ctx, ScopeOptions{})

	result, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources:        []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}, {NotePath: "specs/other.md", Kind: ontology.NodeKindNote}},
		Direction:      TraversalDirectionOutbound,
		RelationNames:  []string{"references"},
		IncludeAmbient: true,
		Hydrate:        HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Edges, 2)
	require.Len(t, result.Nodes, 1)
	require.Equal(t, "specs/reference.md", result.Nodes[0].Path)
	require.Equal(t, 1, counting.currentNoteMetadataRowsByPath)
}

func TestScopeNeighborhoodHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scope := NewService(obsidian.VaultDefinition{}, &obsidian.Note{}, nil, nil).NewScope(context.Background(), ScopeOptions{})

	_, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources: []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}},
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestScopeExpandTraversesBreadthFirstAcrossSources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "linked", DstPath: "notes/a1.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/b.md", RelationName: "linked", DstPath: "notes/b1.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/a1.md", RelationName: "linked", DstPath: "notes/a2.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/b1.md", RelationName: "linked", DstPath: "notes/b2.md", DstType: "Reference", Provenance: "body_link", Structural: false},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})

	result, err := scope.Expand(ctx, ExpansionPlan{
		Sources: []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}, {NotePath: "notes/b.md", Kind: ontology.NodeKindNote}},
		Steps: []ExpansionStep{{
			Direction:      TraversalDirectionOutbound,
			RelationNames:  []string{"linked"},
			IncludeAmbient: true,
		}},
		Limits: TraverseLimits{MaxDepth: 2},
	})
	require.NoError(t, err)
	require.Len(t, result.Edges, 4)
	require.Len(t, result.BySource["notes/a.md"].Edges, 2)
	require.Len(t, result.BySource["notes/b.md"].Edges, 2)
	require.Equal(t, "notes/a2.md", result.BySource["notes/a.md"].Edges[1].Target.NotePath)
	require.Equal(t, 2, scope.Diagnostics().EdgeLoads)
}

func TestScopeExpandKeepsSharedFrontierForEachRoot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "linked", DstPath: "notes/shared.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/b.md", RelationName: "linked", DstPath: "notes/shared.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/shared.md", RelationName: "linked", DstPath: "notes/final.md", DstType: "Reference", Provenance: "body_link", Structural: false},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})

	result, err := scope.Expand(ctx, ExpansionPlan{
		Sources: []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}, {NotePath: "notes/b.md", Kind: ontology.NodeKindNote}},
		Steps: []ExpansionStep{{
			Direction:      TraversalDirectionOutbound,
			RelationNames:  []string{"linked"},
			IncludeAmbient: true,
		}},
		Limits: TraverseLimits{MaxDepth: 2},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"notes/shared.md", "notes/final.md"}, []string{
		result.BySource["notes/a.md"].Edges[0].Target.NotePath,
		result.BySource["notes/a.md"].Edges[1].Target.NotePath,
	})
	require.Equal(t, []string{"notes/shared.md", "notes/final.md"}, []string{
		result.BySource["notes/b.md"].Edges[0].Target.NotePath,
		result.BySource["notes/b.md"].Edges[1].Target.NotePath,
	})
}

func TestScopeExpandEnforcesTotalLimitAcrossDepths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "linked", DstPath: "notes/a1.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/a1.md", RelationName: "linked", DstPath: "notes/a2.md", DstType: "Reference", Provenance: "body_link", Structural: false},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})

	result, err := scope.Expand(ctx, ExpansionPlan{
		Sources: []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}},
		Steps: []ExpansionStep{{
			Direction:      TraversalDirectionOutbound,
			RelationNames:  []string{"linked"},
			IncludeAmbient: true,
		}},
		Limits: TraverseLimits{MaxDepth: 2, FirstTotal: 1},
	})
	require.NoError(t, err)
	require.Len(t, result.Edges, 1)
	require.True(t, result.Truncated)
	require.True(t, result.BySource["notes/a.md"].Truncated)
}

func TestScopeExpandAllowsConcurrentWorkerScopes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "linked", DstPath: "notes/shared.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/b.md", RelationName: "linked", DstPath: "notes/shared.md", DstType: "Reference", Provenance: "body_link", Structural: false},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	service := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil)
	plan := func(path string) ExpansionPlan {
		return ExpansionPlan{
			Sources: []ontology.NodeRef{{NotePath: path, Kind: ontology.NodeKindNote}},
			Steps: []ExpansionStep{{
				Direction:      TraversalDirectionOutbound,
				RelationNames:  []string{"linked"},
				IncludeAmbient: true,
			}},
			Limits: TraverseLimits{MaxDepth: 1},
		}
	}

	var wg sync.WaitGroup
	for _, path := range []string{"notes/a.md", "notes/b.md"} {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			scope := service.NewScope(ctx, ScopeOptions{Budget: 10})
			result, err := scope.Expand(ctx, plan(path))
			require.NoError(t, err)
			require.Len(t, result.Edges, 1)
			require.Equal(t, "notes/shared.md", result.Edges[0].Target.NotePath)
		}(path)
	}
	wg.Wait()
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
	scope := NewService(vaultDef, &obsidian.Note{}, counting, schema).NewScope(ctx, ScopeOptions{})

	_, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "ProductSpec"})
	require.NoError(t, err)
	_, err = scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "ProductSpec"})
	require.NoError(t, err)
	require.Equal(t, 1, counting.currentNoteMetadataRows)
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

func TestScopeCachesTraversalBatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "decisions", DstPath: "notes/c.md", DstType: "Decision", Structural: true},
			{SrcPath: "notes/b.md", RelationName: "decisions", DstPath: "notes/d.md", DstType: "Decision", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	counting := &countingStore{Store: store}
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, counting, nil).NewScope(ctx, ScopeOptions{})
	req := TraverseRequest{
		Sources:    []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}, {NotePath: "notes/b.md", Kind: ontology.NodeKindNote}},
		Relation:   "decisions",
		Structural: true,
	}

	first, err := scope.Traverse(ctx, req)
	require.NoError(t, err)
	require.Len(t, first.EdgesBySource["notes/a.md"], 1)
	require.Len(t, first.EdgesBySource["notes/b.md"], 1)
	first.EdgesBySource["notes/a.md"][0].DstPath = "mutated.md"
	second, err := scope.Traverse(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "notes/c.md", second.EdgesBySource["notes/a.md"][0].DstPath)
	require.Equal(t, 1, counting.structuralEdgesBySources)
	require.Equal(t, 1, scope.Diagnostics().EdgeLoads)
	require.Equal(t, 1, scope.Diagnostics().EdgeHits)
}

func TestScopeExecuteBatchesRelationPlanAndHydratesUniqueTargets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type Decision @node(paths: ["decisions/*.md"]) {
  summary: String!
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  decisions: [Decision!] @link
}
`, `---
summary: unused fixture note
---
# Unused
`)
	writeFixtureNote(t, vaultDef.Path, "specs/a.md", `---
summary: Spec A
decisions:
  - decisions/shared.md
---
# Spec A
`)
	writeFixtureNote(t, vaultDef.Path, "specs/b.md", `---
summary: Spec B
decisions:
  - decisions/shared.md
---
# Spec B
`)
	writeFixtureNote(t, vaultDef.Path, "decisions/shared.md", `---
summary: Shared decision
---
# Shared
`)
	_, err := testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	rebuilt, err := ontology.BuildIndexWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, schema, "notes-hash-2")
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Assessments: rebuilt.AssessmentRows,
		NoteTypes:   rebuilt.NoteTypes,
		Edges:       rebuilt.Edges,
		SchemaState: semdb.OntologySchemaState{SchemaHash: schema.Hash, NotesHash: rebuilt.NotesHash, LoadedAt: 2, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, rebuilt.NodePaths, rebuilt.Nodes))

	counting := &countingStore{Store: store}
	scope := NewService(vaultDef, &obsidian.Note{}, counting, schema).NewScope(ctx, ScopeOptions{})
	plan := NodeReadPlan{
		Roots: []NodeReadRoot{
			{Ref: ontology.NodeRef{NotePath: "specs/a.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}},
			{Ref: ontology.NodeRef{NotePath: "specs/b.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}},
		},
		Relations: []RelationSelection{{
			Name:              "decisions",
			RelationNames:     []string{"decisions"},
			IncludeStructural: true,
			Hydrate:           HydrateOptions{Profile: HydrateSummary},
		}},
		Diagnostics: true,
	}

	first, err := scope.Execute(ctx, plan)
	require.NoError(t, err)
	require.Len(t, first.Groups, 2)
	require.Len(t, first.Groups[0].Edges, 1)
	require.Len(t, first.Groups[1].Edges, 1)
	require.Len(t, first.Nodes, 1)
	require.Equal(t, "decisions/shared.md", first.Nodes[0].Record.Path)
	require.Equal(t, 1, counting.structuralEdgesBySources)
	require.Equal(t, 1, first.Diagnostics.EdgeLoads)
	require.Zero(t, first.Diagnostics.EdgeHits)

	second, err := scope.Execute(ctx, plan)
	require.NoError(t, err)
	require.Len(t, second.Nodes, 1)
	require.Equal(t, 1, counting.structuralEdgesBySources)
	require.Equal(t, 1, second.Diagnostics.EdgeHits)
	require.Zero(t, second.Diagnostics.EdgeLoads)
}

func TestScopeExecuteEnforcesRelationAndPlanLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "decisions", DstPath: "notes/c.md", DstType: "Decision", Structural: true},
			{SrcPath: "notes/a.md", RelationName: "decisions", DstPath: "notes/d.md", DstType: "Decision", Structural: true},
			{SrcPath: "notes/b.md", RelationName: "decisions", DstPath: "notes/e.md", DstType: "Decision", Structural: true},
			{SrcPath: "notes/b.md", RelationName: "decisions", DstPath: "notes/f.md", DstType: "Decision", Structural: true},
			{SrcPath: "notes/c.md", RelationName: "next", DstPath: "notes/g.md", DstType: "Decision", Structural: true},
			{SrcPath: "notes/e.md", RelationName: "next", DstPath: "notes/h.md", DstType: "Decision", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ctx, ScopeOptions{})
	roots := []NodeReadRoot{
		{Ref: ontology.NodeRef{NotePath: "notes/a.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}},
		{Ref: ontology.NodeRef{NotePath: "notes/b.md", Kind: ontology.NodeKindNote, TypeName: "Spec"}},
	}
	relation := RelationSelection{
		Name:              "decisions",
		RelationNames:     []string{"decisions"},
		IncludeStructural: true,
		Children: []RelationSelection{{
			Name:              "next",
			RelationNames:     []string{"next"},
			IncludeStructural: true,
		}},
	}

	perSource, err := scope.Execute(ctx, NodeReadPlan{Roots: roots, Relations: []RelationSelection{withFirstPerSource(relation, 1)}})
	require.NoError(t, err)
	require.True(t, perSource.Truncated)
	require.Len(t, perSource.Edges, 4)
	for _, group := range perSource.Groups[:2] {
		require.Len(t, group.Edges, 1)
	}

	shallow := relation
	shallow.Children = nil
	firstTotal, err := scope.Execute(ctx, NodeReadPlan{Roots: roots, Relations: []RelationSelection{withFirstTotal(shallow, 1)}})
	require.NoError(t, err)
	require.True(t, firstTotal.Truncated)
	require.Len(t, firstTotal.Edges, 1)

	maxEdges, err := scope.Execute(ctx, NodeReadPlan{Roots: roots, Relations: []RelationSelection{relation}, Limits: TraverseLimits{MaxEdges: 1}})
	require.NoError(t, err)
	require.True(t, maxEdges.Truncated)
	require.Len(t, maxEdges.Edges, 1)

	maxNodes, err := scope.Execute(ctx, NodeReadPlan{Roots: roots, Relations: []RelationSelection{relation}, Limits: TraverseLimits{MaxNodes: 1}})
	require.NoError(t, err)
	require.True(t, maxNodes.Truncated)
	require.Len(t, uniqueNeighborhoodTargets(maxNodes.Edges), 1)

	maxDepth, err := scope.Execute(ctx, NodeReadPlan{Roots: roots, Relations: []RelationSelection{relation}, Limits: TraverseLimits{MaxDepth: 1}})
	require.NoError(t, err)
	require.True(t, maxDepth.Truncated)
	require.Equal(t, []string{"decisions", "decisions", "decisions", "decisions"}, relationNames(maxDepth.Edges))

	selectionDepth := relation
	selectionDepth.MaxDepth = 1
	maxSelectionDepth, err := scope.Execute(ctx, NodeReadPlan{Roots: roots, Relations: []RelationSelection{selectionDepth}})
	require.NoError(t, err)
	require.True(t, maxSelectionDepth.Truncated)
	require.Equal(t, []string{"decisions", "decisions", "decisions", "decisions"}, relationNames(maxSelectionDepth.Edges))
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
	scope := NewService(vaultDef, reader, store, schema).NewScope(ctx, ScopeOptions{})
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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
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
type ProductSpec @node(paths: ["specs/product.md"]) {
  summary: String!
}
`, `---
summary: Product summary
---
# Product
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	ref := ontology.NodeRef{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			records, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateSummary})
			require.NoError(t, err)
			require.Len(t, records, 1)
		}()
	}
	wg.Wait()
}

func buildFixture(t *testing.T, schemaSDL, note string) (obsidian.VaultDefinition, *semdb.Store, *ontology.Schema) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(schemaSDL), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "product.md"), []byte(note), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	vaultDef := obsidian.VaultDefinition{Name: "noderead-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NotNil(t, runtime.Schema)
	return vaultDef, store, runtime.Schema
}

func writeFixtureNote(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

func countNodeReadTestNodesByType(nodes []codeanchor.IntelOntologyNode, typeName string) int {
	count := 0
	for _, node := range nodes {
		if node.TypeName == typeName {
			count++
		}
	}
	return count
}

func withFirstPerSource(selection RelationSelection, first int) RelationSelection {
	selection.FirstPerSource = first
	return selection
}

func withFirstTotal(selection RelationSelection, first int) RelationSelection {
	selection.FirstTotal = first
	return selection
}

func relationNames(edges []NeighborhoodEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.RelationName)
	}
	sort.Strings(out)
	return out
}

func neighborhoodTargetPaths(edges []NeighborhoodEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Target.NotePath)
	}
	sort.Strings(out)
	return out
}

func sortedFragments(refs []ontology.NodeRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Fragment)
	}
	sort.Strings(out)
	return out
}

func nodeListItemPaths(items []ontology.NodeListItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.NotePath)
	}
	return out
}

func nodeListItemRefs(items []ontology.NodeListItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Ref.String())
	}
	sort.Strings(out)
	return out
}

func sortedRecordFragments(records []NodeRecord) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.Ref.Fragment)
	}
	sort.Strings(out)
	return out
}

func mustNodeRefJSON(t *testing.T, ref ontology.NodeRef) string {
	t.Helper()
	data, err := json.Marshal(ref)
	require.NoError(t, err)
	return string(data)
}

func graphEndpointIDs(nodes []GraphEndpoint) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.ID)
	}
	sort.Strings(out)
	return out
}

func graphEndpointByID(nodes []GraphEndpoint, id string) GraphEndpoint {
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}
	return GraphEndpoint{}
}

func graphEdgeKinds(edges []GraphReadEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Kind)
	}
	sort.Strings(out)
	return out
}

func graphEdgeRelationNames(edges []GraphReadEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		if edge.RelationName != "" {
			out = append(out, edge.RelationName)
		}
	}
	sort.Strings(out)
	return out
}

func graphEdgePairKinds(edges []GraphReadEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Source+"|"+edge.Target+"|"+edge.Kind)
	}
	sort.Strings(out)
	return out
}

func graphFactPairKinds(edges []GraphFactEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Source+"|"+edge.Target+"|"+edge.Kind)
	}
	sort.Strings(out)
	return out
}

func graphFactByPairKind(edges []GraphFactEdge, source, target, kind string) GraphFactEdge {
	for _, edge := range edges {
		if edge.Source == source && edge.Target == target && edge.Kind == kind {
			return edge
		}
	}
	return GraphFactEdge{}
}

type countingStore struct {
	*semdb.Store

	currentNoteMetadataRows       int
	ontologyAssessmentsByPaths    int
	ontologyAssessmentFlags       int
	ontologyTypesByPaths          int
	ontologyPathsByType           int
	structuralEdgesBySources      int
	ambientEdgesBySources         int
	currentNoteMetadataRowsByPath int
	currentNotePropertyValues     int
	currentNoteTags               int
}

type countingNoteReader struct {
	*obsidian.Note
	contents int
	lists    int
}

func (r *countingNoteReader) GetContents(vaultDef obsidian.VaultDefinition, path string) (string, error) {
	r.contents++
	return r.Note.GetContents(vaultDef, path)
}

func (r *countingNoteReader) GetNotesList(vaultDef obsidian.VaultDefinition) ([]string, error) {
	r.lists++
	return r.Note.GetNotesList(vaultDef)
}

func (s *countingStore) CurrentNoteMetadataRows(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	s.currentNoteMetadataRows++
	return s.Store.CurrentNoteMetadataRows(ctx)
}

func (s *countingStore) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	s.currentNoteMetadataRowsByPath++
	return s.Store.CurrentNoteMetadataRowsByPaths(ctx, paths)
}

func (s *countingStore) CurrentNotePropertyValues(ctx context.Context, paths []string, names []string, source semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	s.currentNotePropertyValues++
	return s.Store.CurrentNotePropertyValues(ctx, paths, names, source)
}

func (s *countingStore) CurrentNoteTags(ctx context.Context, paths []string) ([]semdb.NoteTagRow, error) {
	s.currentNoteTags++
	return s.Store.CurrentNoteTags(ctx, paths)
}

func (s *countingStore) OntologyAssessmentsByPaths(ctx context.Context, paths []string) (map[string]semdb.OntologyNoteAssessmentRow, error) {
	s.ontologyAssessmentsByPaths++
	return s.Store.OntologyAssessmentsByPaths(ctx, paths)
}

func (s *countingStore) OntologyAssessmentFlags(ctx context.Context) (map[string]semdb.OntologyAssessmentFlags, error) {
	s.ontologyAssessmentFlags++
	return s.Store.OntologyAssessmentFlags(ctx)
}

func (s *countingStore) OntologyTypesByPaths(ctx context.Context, paths []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	s.ontologyTypesByPaths++
	return s.Store.OntologyTypesByPaths(ctx, paths)
}

func (s *countingStore) OntologyPathsByType(ctx context.Context, typeName string, limit int) ([]string, error) {
	s.ontologyPathsByType++
	return s.Store.OntologyPathsByType(ctx, typeName, limit)
}

func (s *countingStore) OntologyStructuralEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]semdb.OntologyEdgeRow, error) {
	s.structuralEdgesBySources++
	return s.Store.OntologyStructuralEdgesBySources(ctx, paths, relation, limit)
}

func (s *countingStore) OntologyAmbientEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]semdb.OntologyEdgeRow, error) {
	s.ambientEdgesBySources++
	return s.Store.OntologyAmbientEdgesBySources(ctx, paths, relation, limit)
}

func markdownRuntime(t *testing.T) noteformat.Runtime {
	t.Helper()
	provider := markdown.New()
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	return runtime
}
