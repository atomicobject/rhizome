package noderead

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestExplicitTargetsCanonicalizeToTheIndexedSpelling(t *testing.T) {
	vaultDef, store, schema := buildFixture(t, `
type Spec @node(paths: ["specs/*.md"]) { title: String }
`, "---\ntitle: Product\n---\n\n# Product\n")
	actual, err := os.Stat(filepath.Join(vaultDef.Path, "specs"))
	require.NoError(t, err)
	alias, err := os.Stat(filepath.Join(vaultDef.Path, "Specs"))
	fold := err == nil && os.SameFile(actual, alias)
	t.Logf("vault filesystem folds case: %v", fold)

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
	stored, err := scope.StoredNotePaths(context.Background(), []string{"Specs/Product.md"})
	require.NoError(t, err)
	if fold {
		require.Equal(t, "specs/product.md", stored[0])
	} else {
		require.Equal(t, "Specs/Product.md", stored[0], "exact: an unindexed spelling stays as authored")
	}
	indexed, err := scope.StoredNotePaths(context.Background(), []string{"specs/product.md"})
	require.NoError(t, err)
	require.Equal(t, "specs/product.md", indexed[0], "an indexed spelling always wins")
	missing, err := scope.StoredNotePaths(context.Background(), []string{"specs/absent.md"})
	require.NoError(t, err)
	require.Equal(t, "specs/absent.md", missing[0], "a missing file never gains a spelling")

	result, err := scope.Resolve(context.Background(), ResolveRequest{
		Targets: []NodeTarget{{Input: "Specs/Product.md"}, {Input: `{"notePath":"Specs/Product.md","kind":"NOTE"}`}},
		Hydrate: HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	resolvedPaths := make(map[string]int)
	for _, node := range result.Resolved {
		if node.Record.Path != "" {
			resolvedPaths[node.Ref.NotePath]++
		}
	}
	if fold {
		require.Equal(t, 2, resolvedPaths["specs/product.md"], "bare and JSON explicit refs resolve to the indexed spelling: %+v", result)
		for _, node := range result.Resolved {
			require.Equal(t, ontology.NodeKindNote, node.Ref.Kind)
			require.Equal(t, "specs/product.md", node.Record.Path, "hydrated records carry the indexed spelling")
		}
		return
	}
	require.Empty(t, resolvedPaths, "exact: a differently cased spelling does not select the indexed note: %+v", result)
}

func TestExplicitTargetMembershipChecksAreBatched(t *testing.T) {
	ctx := context.Background()
	vault, store, _ := buildFixture(t, `type Document @node(paths:["notes/*.md"]) { title:String }`, "# Host\n")
	targets := make([]NodeTarget, 25)
	for i := range targets {
		path := fmt.Sprintf("notes/%02d.md", i)
		writeFixtureNote(t, vault.Path, path, "# Note\n")
		targets[i] = NodeTarget{Input: path}
	}
	runtime, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	for _, overlay := range []*ReadOverlay{nil, {SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"notes/00.md": "# Preview\n"}}} {
		var before []int
		for _, count := range []int{1, 25} {
			counting := &countingStore{Store: store}
			scope := NewService(vault, &obsidian.Note{}, counting, runtime.Schema).NewScope(ScopeOptions{ReadOverlay: overlay})
			result, err := scope.Resolve(ctx, ResolveRequest{Targets: targets[:count], Hydrate: HydrateOptions{Profile: HydrateSummary}})
			require.NoError(t, err)
			require.Len(t, result.Resolved, count)
			calls := []int{counting.currentNoteMetadataRowsByPath, counting.currentNotePropertyValues, counting.currentNoteTags, counting.ontologyTypesByPaths, counting.ontologyAssessmentFlagsByPath}
			if before == nil {
				before = calls
			} else {
				require.Equal(t, before, calls, "calls must not grow with target count, overlay=%v", overlay != nil)
			}
		}
	}
}
