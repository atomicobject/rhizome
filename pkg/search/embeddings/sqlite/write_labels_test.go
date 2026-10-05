package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestStoreCacheEmbeddingExposesNamedOp(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	store, err := Open(filepath.Join(t.TempDir(), "labels.db"), 4)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.CacheEmbedding(ctx, "note-hash", embeddings.Embedding{1, 2, 3, 4}))

	summary := collector.RenderSummary()
	require.Contains(t, summary, "noteemb.cache_embedding")
	require.NotContains(t, summary, "unknown")
}
