package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

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
	_, err := testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	rebuilt, err := ontology.BuildIndexWithStore(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, schema, "notes-hash-2")
	require.NoError(t, err)
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Assessments: rebuilt.AssessmentRows,
		NoteTypes:   rebuilt.NoteTypes,
		Edges:       rebuilt.Edges,
		SchemaState: semdb.OntologySchemaState{SchemaHash: schema.Hash, NotesHash: rebuilt.NotesHash, LoadedAt: 2, Ready: true},
	}))
	require.NoError(t, store.ReplaceOntologyNodes(ctx, rebuilt.NodePaths, rebuilt.Nodes))

	counting := &countingStore{Store: store}
	scope := NewService(vaultDef, &obsidian.Note{}, counting, schema).NewScope(ScopeOptions{})
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
	require.Equal(t, "decisions/shared.md", first.Nodes[0].Path)
	require.Equal(t, 1, counting.ontologyNeighborhood)
	require.Equal(t, 1, first.Diagnostics.EdgeLoads)
	require.Zero(t, first.Diagnostics.EdgeHits)

	second, err := scope.Execute(ctx, plan)
	require.NoError(t, err)
	require.Len(t, second.Nodes, 1)
	require.Equal(t, 1, counting.ontologyNeighborhood)
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
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})
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
