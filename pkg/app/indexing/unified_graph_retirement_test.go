package indexing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	graphindex "github.com/atomicobject/rhizome/pkg/graph/index"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestUnifiedIndexRecomputesGraphAfterCodeRetirement(t *testing.T) {
	for _, removeAll := range []bool{false, true} {
		name := "one file"
		if removeAll {
			name = "all files"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/config.yml"), []byte("code:\n  enabled: true\n  goRoots: [src]\nnoteEmbeddings: {enabled: false}\ncodeEmbeddings: {enabled: false}\n"), 0o600))
			require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "src/caller.go"), []byte("package example\nfunc Caller() int { return Target() }\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, "src/target.go"), []byte("package example\nfunc Target() int { return 1 }\n"), 0o600))
			run := func() {
				t.Helper()
				require.NoError(t, runUnifiedCoreForTest(t, t.Context(), UnifiedOptions{
					VaultPath: root, VaultDef: obsidian.VaultDefinition{Root: root},
					NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true,
				}))
			}
			run()
			store := openTouchParityStore(t, root)
			before, err := store.GraphDocScoresByPaths(t.Context(), []string{"src/caller.go"})
			require.NoError(t, err)
			require.Contains(t, before, "src/caller.go")
			require.NoError(t, os.Remove(filepath.Join(root, "src/caller.go")))
			if removeAll {
				require.NoError(t, os.Remove(filepath.Join(root, "src/target.go")))
			}
			run()
			scores, err := store.GraphDocScoresByPaths(t.Context(), []string{"src/caller.go", "src/target.go"})
			require.NoError(t, err)
			require.NotContains(t, scores, "src/caller.go", "retired paths must not retain graph scores")
			want, err := graphindex.ComputeDerivedScores(t.Context(), store, graphindex.DocScoresOptions{VaultRoot: root})
			require.NoError(t, err)
			for _, score := range want.Documents {
				got := scores[score.DocPath]
				got.UpdatedAt = score.UpdatedAt
				require.Equal(t, score, got)
			}
			var anchorRows int
			require.NoError(t, store.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM graph_anchor_scores`).Scan(&anchorRows))
			require.Len(t, want.Anchors, anchorRows)
			for _, score := range want.Anchors {
				actual, err := store.AnchorScoresByIDs(t.Context(), []string{score.AnchorID})
				require.NoError(t, err)
				require.Equal(t, score.PageRank, actual[score.AnchorID])
			}
		})
	}
}
