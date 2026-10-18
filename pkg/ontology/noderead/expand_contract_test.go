package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"sync"
	"testing"
)

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
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})

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
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})

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
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})

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
			scope := service.NewScope(ScopeOptions{})
			result, err := scope.Expand(ctx, plan(path))
			require.NoError(t, err)
			require.Len(t, result.Edges, 1)
			require.Equal(t, "notes/shared.md", result.Edges[0].Target.NotePath)
		}(path)
	}
	wg.Wait()
}
