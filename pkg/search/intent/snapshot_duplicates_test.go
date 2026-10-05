package intent_test

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/intent"
	"github.com/stretchr/testify/require"
)

type duplicateExemplarProvider struct{ texts []string }

func (p *duplicateExemplarProvider) Dimensions() int { return 2 }
func (p *duplicateExemplarProvider) EmbedTexts(_ context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.texts = append(p.texts, texts...)
	vectors := make([]embeddings.Embedding, len(texts))
	for i := range vectors {
		vectors[i] = embeddings.Embedding{1, 0}
	}
	return vectors, nil
}

func TestSnapshotDeduplicatesExemplarsBeforeEmbeddingAndPublication(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intent.db"))
	require.NoError(t, err)
	defer store.Close()
	provider := &duplicateExemplarProvider{}
	exemplars := map[search.Intent][]string{search.IntentSearch: {" alpha ", "alpha", "beta", "beta"}}
	opts := intent.SyncOptions{ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "duplicates", Dimensions: 2}}
	require.NoError(t, intent.SyncEmbeddingsWithOptions(ctx, provider, store, exemplars, opts))
	require.Equal(t, []string{"alpha", "beta"}, provider.texts)
	snapshot, err := store.IntentEmbeddingSnapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snapshot.Rows, 2)
	require.NoError(t, intent.SyncEmbeddingsWithOptions(ctx, provider, store, exemplars, opts))
	require.Equal(t, []string{"alpha", "beta"}, provider.texts)
}
