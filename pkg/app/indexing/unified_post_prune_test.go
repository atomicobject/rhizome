package indexing

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
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
	pruneObsoleteEmbeddings(ctx, store, runNow)
	require.Equal(t, 1, cached(), "pending derived work keeps unreferenced vectors")

	_, err = store.DB().ExecContext(ctx, `DELETE FROM derived_work`)
	require.NoError(t, err)
	pruneObsoleteEmbeddings(ctx, store, runNow)
	require.Zero(t, cached(), "with no pending work, an unreferenced earlier-format vector is pruned")
}
