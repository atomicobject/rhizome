package indexing

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeemb "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

// Vectors look unreferenced while their derived work awaits republication, so
// pending derived work blocks pruning even when unreferenced rows dominate.
func TestPruneObsoleteEmbeddingsWaitsForPendingDerivedWork(t *testing.T) {
	runNow := func(ctx context.Context, run func(context.Context) error) error { return run(ctx) }
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertOntologyEmbeddingCache(ctx, []semdb.OntologyEmbeddingCacheEntry{
		{Provider: "test", Model: "m", TextHash: "awaiting-republication", Vector: embeddings.Embedding{1, 0}, Format: "earlier"},
	}))
	cached := func() int {
		got, err := store.OntologyEmbeddingCacheVectors(ctx, "test", "m", 2, []string{"awaiting-republication"})
		require.NoError(t, err)
		return len(got)
	}

	_, err = store.MarkDerivedDirty(ctx, []codeanchor.DerivedScope{{Kind: codeanchor.DerivedOntology, Path: "notes/a.md"}})
	require.NoError(t, err)
	require.NoError(t, pruneObsoleteEmbeddings(ctx, store, nil, nil, runNow))
	require.Equal(t, 1, cached(), "pending derived work keeps unreferenced vectors")

	_, err = store.DB().ExecContext(ctx, `DELETE FROM derived_work`)
	require.NoError(t, err)
	require.NoError(t, pruneObsoleteEmbeddings(ctx, store, nil, nil, runNow))
	require.Zero(t, cached(), "with no pending work, an unreferenced earlier-format vector is pruned")
}

// The note and code content caches follow the same guard: pending derived work
// of any kind keeps their earlier-format vectors.
func TestPruneObsoleteEmbeddingsWaitsForPendingDerivedWorkInContentCaches(t *testing.T) {
	runNow := func(ctx context.Context, run func(context.Context) error) error { return run(ctx) }
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := semdb.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	notes, err := codeemb.OpenNotes(ctx, codeemb.File(path), codeemb.Options{Access: codeemb.WriteIndex, Dimensions: 2})
	require.NoError(t, err)
	t.Cleanup(func() { _ = notes.Close() })
	code, err := codeemb.OpenCode(ctx, codeemb.File(path), codeemb.Options{Access: codeemb.WriteIndex, Dimensions: 2})
	require.NoError(t, err)
	t.Cleanup(func() { _ = code.Close() })
	require.NoError(t, notes.CacheEmbedding(ctx, "awaiting-republication", embeddings.Embedding{1, 0}))
	require.NoError(t, code.CacheEmbedding(ctx, "awaiting-republication", embeddings.Embedding{1, 0}))
	for _, table := range []string{"emb_embedding_cache", "code_embedding_cache"} {
		_, err = store.DB().ExecContext(ctx, `UPDATE `+table+` SET format = 'earlier'`)
		require.NoError(t, err)
	}
	cached := func() int {
		n := 0
		for _, index := range []interface {
			EmbeddingByHash(context.Context, string) (embeddings.Embedding, bool, error)
		}{notes, code} {
			_, ok, err := index.EmbeddingByHash(ctx, "awaiting-republication")
			require.NoError(t, err)
			if ok {
				n++
			}
		}
		return n
	}

	_, err = store.MarkDerivedDirty(ctx, []codeanchor.DerivedScope{{Kind: codeanchor.DerivedCode, Path: "src/a.go"}})
	require.NoError(t, err)
	require.NoError(t, pruneObsoleteEmbeddings(ctx, store, notes, code, runNow))
	require.Equal(t, 2, cached(), "pending derived work keeps unreferenced vectors")

	_, err = store.DB().ExecContext(ctx, `DELETE FROM derived_work`)
	require.NoError(t, err)
	require.NoError(t, pruneObsoleteEmbeddings(ctx, store, notes, code, runNow))
	require.Zero(t, cached(), "with no pending work, unreferenced earlier-format vectors are pruned")
}

// A failed prune must not fail the maintenance callback: an error returned
// there stops the writer, and the index's later writes would then fail.
func TestPruneObsoleteEmbeddingsFailureKeepsTheWriterRunning(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.DB().ExecContext(ctx, `DROP TABLE ontology_embedding_cache`)
	require.NoError(t, err)
	var callbackErr error
	ran := false
	require.NoError(t, pruneObsoleteEmbeddings(ctx, store, nil, nil, func(ctx context.Context, run func(context.Context) error) error {
		ran = true
		callbackErr = run(ctx)
		return nil
	}))
	require.True(t, ran)
	require.NoError(t, callbackErr, "a prune failure is reported, not returned to the writer")
}
