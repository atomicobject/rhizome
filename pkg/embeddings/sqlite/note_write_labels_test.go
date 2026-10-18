package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func TestStoreCacheEmbeddingExposesNamedOp(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	store, err := OpenNotes(context.Background(), File(filepath.Join(t.TempDir(), "labels.db")), Options{Access: WriteIndex, Dimensions: 4})
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.CacheEmbedding(ctx, "note-hash", embeddings.Embedding{1, 2, 3, 4}))

	summary := collector.RenderSummary()
	require.Contains(t, summary, "noteemb.cache_embedding")
	require.NotContains(t, summary, "unknown")
}
