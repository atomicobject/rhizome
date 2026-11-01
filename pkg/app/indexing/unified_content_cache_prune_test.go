package indexing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// A chunking or embedded-text change orphans a whole generation of cached code
// vectors. A complete index prunes that generation and keeps every vector a
// current item uses and the current format's history, so an edit then revert
// still sends nothing. The unified pipeline writes note vectors to Intel, not
// to the note cache, so only the code cache holds a generation here.
func TestCompleteIndexPrunesEarlierCodeCacheGenerations(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	foo := "package fixture\n\n// Foo computes a result.\nfunc Foo() int { return 1 }\n\n// Bar computes another result.\nfunc Bar() int { return 2 }\n"
	write(".rhizome/config.yml", "code:\n  enabled: true\n  goRoots: [src]\nnoteEmbeddings:\n  enabled: false\ncodeEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\n")
	write("src/foo.go", foo)

	definition, err := repoconfig.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	opts := UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true}
	providerTexts := func() (int64, map[string]int64) {
		t.Helper()
		collector := indexingperf.New()
		require.NoError(t, runUnifiedCoreForTest(t, indexingperf.WithCollector(context.Background(), collector), opts))
		var total int64
		byPhase := map[string]int64{}
		for _, counter := range collector.Snapshot().Counters {
			if counter.Name == string(indexingperf.ProviderTexts) {
				total += counter.Total
				byPhase[counter.Phase] += counter.Total
			}
		}
		return total, byPhase
	}
	const refs = `SELECT content_hash FROM code_chunk_embeddings UNION SELECT content_hash FROM code_item_embeddings`
	withDB := func(fn func(exec func(query string, args ...any), count func(query string) int)) {
		t.Helper()
		store, closeStore, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
		require.NoError(t, err)
		defer closeStore()
		fn(func(query string, args ...any) {
			_, err := store.DB().ExecContext(context.Background(), query, args...)
			require.NoError(t, err)
		}, func(query string) int {
			var n int
			require.NoError(t, store.DB().QueryRowContext(context.Background(), query).Scan(&n))
			return n
		})
	}
	rows := func() (live, orphaned int) {
		withDB(func(_ func(string, ...any), count func(string) int) {
			live = count(`SELECT COUNT(*) FROM code_embedding_cache WHERE content_hash IN (` + refs + `)`)
			orphaned = count(`SELECT COUNT(*) FROM code_embedding_cache WHERE content_hash NOT IN (` + refs + `)`)
		})
		return live, orphaned
	}

	initial, _ := providerTexts()
	require.Positive(t, initial, "the first index must embed the fixture")
	write("src/foo.go", strings.Replace(foo, "another result", "a different result", 1))
	_, _ = providerTexts()

	live, history := rows()
	require.Positive(t, live)
	require.Positive(t, history, "the edit left current-format history to keep")
	withDB(func(exec func(string, ...any), _ func(string) int) {
		for i := range live + 1 {
			exec(`INSERT INTO code_embedding_cache (content_hash, embedding, norm, dimensions, created_at, format)
				SELECT ?, embedding, norm, dimensions, created_at, 'earlier-format' FROM code_embedding_cache LIMIT 1`, fmt.Sprintf("old-generation-%d", i))
		}
	})

	unchanged, byPhase := providerTexts()
	require.Zero(t, unchanged, "provider texts by phase: %v", byPhase)
	afterLive, orphaned := rows()
	require.Equal(t, history, orphaned, "an earlier format's unreferenced vectors are pruned; current-format history stays")

	// Vectors cached before formats were recorded have an empty format; the
	// first complete index after the upgrade reclaims those no item uses.
	withDB(func(exec func(string, ...any), _ func(string) int) {
		exec(`INSERT INTO code_embedding_cache (content_hash, embedding, norm, dimensions, created_at, format)
			SELECT 'pre-upgrade', embedding, norm, dimensions, created_at, '' FROM code_embedding_cache LIMIT 1`)
	})
	_, _ = providerTexts()
	_, orphaned = rows()
	require.Equal(t, history, orphaned, "an unreferenced pre-upgrade vector is reclaimed")
	require.Equal(t, live, afterLive, "vectors current chunks use survive pruning")

	write("src/foo.go", foo)
	reverted, byPhase := providerTexts()
	require.Zero(t, reverted, "reverting a code edit must reuse cached vectors; provider texts by phase: %v", byPhase)
}
