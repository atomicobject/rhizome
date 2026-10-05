package indexing

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIndexerStyleNodeReadScopesRunConcurrentlyWithoutSharedMutableCache(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "linked", DstPath: "notes/shared.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/a.md", RelationName: "linked", DstPath: "notes/a-specific.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/b.md", RelationName: "linked", DstPath: "notes/shared.md", DstType: "Reference", Provenance: "body_link", Structural: false},
			{SrcPath: "notes/b.md", RelationName: "linked", DstPath: "notes/b-specific.md", DstType: "Reference", Provenance: "body_link", Structural: false},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", Ready: true},
	}))
	service := noderead.NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil)
	sources := []string{"notes/a.md", "notes/b.md"}

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for worker := 0; worker < 8; worker++ {
		for _, source := range sources {
			wg.Add(1)
			go func(source string) {
				defer wg.Done()
				scope := service.NewScope(ctx, noderead.ScopeOptions{Budget: 4})
				result, err := scope.Expand(ctx, noderead.ExpansionPlan{
					Sources: []ontology.NodeRef{{NotePath: source, Kind: ontology.NodeKindNote}},
					Steps: []noderead.ExpansionStep{{
						Direction:      noderead.TraversalDirectionOutbound,
						RelationNames:  []string{"linked"},
						IncludeAmbient: true,
					}},
					Limits: noderead.TraverseLimits{MaxDepth: 1, FirstPerSource: 2, FirstTotal: 2},
				})
				if err != nil {
					errs <- err
					return
				}
				if len(result.Edges) != 2 {
					errs <- fmt.Errorf("expected 2 edges for %s, got %d", source, len(result.Edges))
					return
				}
				paths := []string{result.Edges[0].Target.NotePath, result.Edges[1].Target.NotePath}
				if !containsPath(paths, "notes/shared.md") {
					errs <- fmt.Errorf("missing shared target for %s in %v", source, paths)
					return
				}
				if !containsPath(paths, sourceSpecificTarget(source)) {
					errs <- fmt.Errorf("missing source target for %s in %v", source, paths)
				}
			}(source)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func sourceSpecificTarget(source string) string {
	switch source {
	case "notes/a.md":
		return "notes/a-specific.md"
	case "notes/b.md":
		return "notes/b-specific.md"
	default:
		return ""
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
