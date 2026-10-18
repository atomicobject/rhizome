package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestScopeEdgesLimitPerSourceDoesNotStarveLaterSources(t *testing.T) {
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
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})

	result, err := scope.Edges(ctx, NeighborhoodRequest{
		Sources: []ontology.NodeRef{
			{NotePath: "notes/a.md", Kind: ontology.NodeKindNote},
			{NotePath: "notes/b.md", Kind: ontology.NodeKindNote},
		},
		Provenance:     map[string]struct{}{"outbound": {}},
		IncludeAmbient: true,
		FirstPerSource: 1,
	})
	require.NoError(t, err)
	require.Len(t, result.BySource["notes/a.md"].Edges, 1)
	require.Len(t, result.BySource["notes/b.md"].Edges, 1)
	require.Equal(t, "notes/b/target.md", result.BySource["notes/b.md"].Edges[0].Target.NotePath)
	require.True(t, result.BySource["notes/a.md"].Truncated)
	require.False(t, result.BySource["notes/b.md"].Truncated)
}
