package noderead

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestTraversalUsesNarrowerLimitAndBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "a.md", RelationName: "linked", DstPath: "b.md", DstType: "Reference", Structural: true},
			{SrcPath: "a.md", RelationName: "linked", DstPath: "c.md", DstType: "Reference", Structural: true},
			{SrcPath: "a.md", RelationName: "linked", DstPath: "d.md", DstType: "Reference", Structural: true},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	service := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil)
	source := ontology.NodeRef{NotePath: "a.md", Kind: ontology.NodeKindNote}
	for _, tc := range []struct {
		name         string
		limits       TraverseLimits
		budget       TraverseBudget
		expandEdges  int
		executeEdges int
	}{
		{"edge budget tighter", TraverseLimits{MaxEdges: 3}, TraverseBudget{MaxEdges: 1}, 1, 1},
		{"edge limit tighter", TraverseLimits{MaxEdges: 1}, TraverseBudget{MaxEdges: 3}, 1, 1},
		{"edge limit alone", TraverseLimits{MaxEdges: 1}, TraverseBudget{}, 1, 1},
		{"edge budget alone", TraverseLimits{}, TraverseBudget{MaxEdges: 1}, 1, 1},
		{"node budget tighter", TraverseLimits{MaxNodes: 4}, TraverseBudget{MaxNodes: 2}, 1, 2},
		{"node limit tighter", TraverseLimits{MaxNodes: 2}, TraverseBudget{MaxNodes: 4}, 1, 2},
		{"node budget alone", TraverseLimits{}, TraverseBudget{MaxNodes: 2}, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("Expand", func(t *testing.T) {
				result, err := service.NewScope(ctx, ScopeOptions{}).Expand(ctx, ExpansionPlan{
					Sources: []ontology.NodeRef{source},
					Steps:   []ExpansionStep{{Direction: TraversalDirectionOutbound, RelationNames: []string{"linked"}, IncludeStructural: true}},
					Limits:  tc.limits,
					Budget:  tc.budget,
				})
				require.NoError(t, err)
				require.Len(t, result.Edges, tc.expandEdges)
				require.True(t, result.Truncated)
				require.True(t, result.BySource["a.md"].Truncated)
			})
			t.Run("Execute", func(t *testing.T) {
				result, err := service.NewScope(ctx, ScopeOptions{}).Execute(ctx, NodeReadPlan{
					Roots:     []NodeReadRoot{{Ref: source}},
					Relations: []RelationSelection{{Direction: TraversalDirectionOutbound, RelationNames: []string{"linked"}, IncludeStructural: true}},
					Limits:    tc.limits,
					Budget:    tc.budget,
				})
				require.NoError(t, err)
				require.Len(t, result.Edges, tc.executeEdges)
				require.True(t, result.Truncated)
			})
		})
	}
}
