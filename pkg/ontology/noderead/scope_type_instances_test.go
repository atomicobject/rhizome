package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

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
	_, err := testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
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
	model := readmodel.NodeReadModel{FullReplace: true}
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

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

	result, err := scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "SpecLike",
		Limit:    2,
		Sort:     []readmodel.FieldSort{{FieldName: "priority", ValueKind: "int"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"specs/product-b.md", "technical-a.md"}, nodeListItemPaths(result.Items))

	result, err = scope.TypeInstances(ctx, TypeInstancesRequest{
		TypeName: "SpecLike",
		Limit:    2,
		Offset:   1,
		Sort:     []readmodel.FieldSort{{FieldName: "priority", ValueKind: "int"}},
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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

	_, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{
		TypeName: "SpecLike",
		Limit:    10,
		Sort:     []readmodel.FieldSort{{FieldName: "reviewers", ValueKind: "string"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-common or non-indexed sort fields")
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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

	fallback, err := scope.TypeInstances(context.Background(), TypeInstancesRequest{TypeName: ontoschema.FallbackNoteTypeName})
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
	require.Equal(t, 1, countNodeReadTestNodesByType(nodes, ontoschema.FallbackNoteTypeName), "fallback note remains internal catalog/search evidence")
	require.Equal(t, 1, countNodeReadTestNodesByType(nodes, ontoschema.FallbackSectionTypeName), "fallback sections remain internal catalog/search evidence")
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
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

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
	require.Equal(t, 0, countNodeReadTestNodesByType(nodes, ontoschema.FallbackNoteTypeName))
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
	scope := NewService(vaultDef, nil, counting, schema).NewScope(ScopeOptions{})

	instances, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "SpecMetric"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 1)
	require.Equal(t, "^throughput", instances.Items[0].Ref.Fragment)
	require.Equal(t, "Throughput metric", instances.Items[0].Title)
	require.Equal(t, 1, scope.Diagnostics().CatalogHits)
}
