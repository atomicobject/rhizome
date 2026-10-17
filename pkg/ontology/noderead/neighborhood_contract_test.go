package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

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
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})

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
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})

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
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})

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
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(ScopeOptions{})
	instances, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "SpecMetric"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 2)
	catalog, err := store.OntologyNodesByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	nodeIDs := make([]string, 0, len(catalog))
	for _, row := range catalog {
		nodeIDs = append(nodeIDs, row.NodeID)
	}
	fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "specs/product.md", RelationName: "features", DstPath: "specs/product.md", DstNodeID: instances.Items[0].Ref.NodeID, DstType: "SpecMetric", Provenance: "field", Structural: true},
			{SrcPath: "specs/product.md", RelationName: "features", DstPath: "specs/product.md", DstNodeID: instances.Items[1].Ref.NodeID, DstType: "SpecMetric", Provenance: "field", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: schema.Hash, NotesHash: "n1", LoadedAt: 1, Ready: true, MaterializationVersion: ontology.OntologyMaterializationVersion},
	}))

	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, readmodel.NodeReadModel{NotePaths: []string{"specs/product.md"}, Nodes: catalog, FieldValues: fields}))

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
	require.Zero(t, reader.contents)
	require.Zero(t, reader.lists)
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
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})

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
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"specs/reference.md"}, nil))
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
	scope := NewService(vaultDef, &obsidian.Note{}, counting, schema).NewScope(ScopeOptions{})

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
	scope := NewService(obsidian.VaultDefinition{}, &obsidian.Note{}, nil, nil).NewScope(ScopeOptions{})

	_, err := scope.Neighborhood(ctx, NeighborhoodRequest{
		Sources: []ontology.NodeRef{{NotePath: "notes/a.md", Kind: ontology.NodeKindNote}},
	})
	require.ErrorIs(t, err, context.Canceled)
}
