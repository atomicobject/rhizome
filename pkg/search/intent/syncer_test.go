package intent

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
	"github.com/stretchr/testify/require"
)

func TestSyncEmbeddingsPublishesPartialFinalBatchWithCachedRows(t *testing.T) {
	info := embeddings.ProviderConfig{Provider: "test", Model: "dynamic"}
	existing := intentstore.EmbeddingRecord{
		Intent: string(search.IntentDocsForCode), Exemplar: "existing", Embedding: embeddings.Embedding{9, 0}, Dimensions: 2,
	}
	store := &stubIntentStore{fingerprint: ProviderFingerprint(info), rows: []intentstore.EmbeddingRecord{existing}}
	provider := &defaultBatchStubProvider{
		stubProvider: stubProvider{vecs: map[string]embeddings.Embedding{
			"alpha": {1, 0}, "beta": {2, 0}, "gamma": {3, 0},
		}},
		batchSize: 2,
	}

	err := SyncEmbeddingsWithOptions(context.Background(), provider, store, map[search.Intent][]string{
		search.IntentSearch:      {"gamma", "alpha", "beta"},
		search.IntentDocsForCode: {"existing"},
	}, SyncOptions{ProviderInfo: info})
	require.NoError(t, err)

	require.Equal(t, [][]string{{"alpha", "beta"}, {"gamma"}}, provider.texts)
	require.Equal(t, 1, store.replaces)
	require.Equal(t, ProviderFingerprint(info), store.fingerprint)
	require.Equal(t, []intentstore.EmbeddingRecord{
		existing,
		{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{1, 0}, Dimensions: 2},
		{Intent: string(search.IntentSearch), Exemplar: "beta", Embedding: embeddings.Embedding{2, 0}, Dimensions: 2},
		{Intent: string(search.IntentSearch), Exemplar: "gamma", Embedding: embeddings.Embedding{3, 0}, Dimensions: 2},
	}, store.rows)
}

type failingSyncBackend struct {
	stubProvider
	respond func(int, []embeddings.Embedding) ([]embeddings.Embedding, error)
}

func (p *failingSyncBackend) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	vecs, _ := p.stubProvider.EmbedTexts(ctx, texts)
	return p.respond(p.calls, vecs)
}

func (p *failingSyncBackend) Submit(ctx context.Context, _ string, texts []string) ([]embeddings.Embedding, error) {
	return p.EmbedTexts(ctx, texts)
}

func (*failingSyncBackend) DefaultBatchSize() int { return 2 }

func TestSyncEmbeddingsFailurePreservesSnapshot(t *testing.T) {
	embedErr := errors.New("provider unavailable")
	for _, mode := range []string{"provider", "shared node"} {
		for _, failure := range []struct {
			name    string
			respond func([]embeddings.Embedding) ([]embeddings.Embedding, error)
			message string
		}{
			{"provider error", func([]embeddings.Embedding) ([]embeddings.Embedding, error) { return nil, embedErr }, "intent exemplar embeddings: provider unavailable"},
			{"vector count", func(vecs []embeddings.Embedding) ([]embeddings.Embedding, error) { return vecs[:len(vecs)-1], nil }, "intent exemplar embeddings: expected"},
			{"empty vector", func(vecs []embeddings.Embedding) ([]embeddings.Embedding, error) {
				vecs[len(vecs)-1] = nil
				return vecs, nil
			}, "exemplar \"gamma\" returned an empty vector"},
			{"dimensions", func(vecs []embeddings.Embedding) ([]embeddings.Embedding, error) {
				vecs[len(vecs)-1] = embeddings.Embedding{1, 2, 3}
				return vecs, nil
			}, "exemplar \"gamma\" returned 3 dimensions, expected 2"},
		} {
			t.Run(mode+"/"+failure.name, func(t *testing.T) {
				info := embeddings.ProviderConfig{Provider: "test", Model: "new", Dimensions: 2}
				store := &stubIntentStore{
					fingerprint: "previous-provider",
					rows: []intentstore.EmbeddingRecord{{
						Intent: string(search.IntentSearch), Exemplar: "previous", Embedding: embeddings.Embedding{9, 8}, Dimensions: 2,
					}},
				}
				backend := &failingSyncBackend{stubProvider: stubProvider{dims: 2}}
				backend.respond = func(call int, vecs []embeddings.Embedding) ([]embeddings.Embedding, error) {
					if mode == "provider" && call == 1 {
						return vecs, nil
					}
					return failure.respond(vecs)
				}
				opts := SyncOptions{ProviderInfo: info}
				var provider embeddings.Provider = backend
				if mode == "shared node" {
					opts.Node = backend
					provider = &stubProvider{dims: 2}
				}

				err := SyncEmbeddingsWithOptions(context.Background(), provider, store, map[search.Intent][]string{
					search.IntentSearch: {"gamma", "alpha", "beta"},
				}, opts)
				require.ErrorContains(t, err, failure.message)
				if failure.name == "provider error" {
					require.ErrorIs(t, err, embedErr)
				}
				if mode == "provider" {
					require.Equal(t, [][]string{{"alpha", "beta"}, {"gamma"}}, backend.texts)
				} else {
					require.Equal(t, [][]string{{"alpha", "beta", "gamma"}}, backend.texts)
				}
				require.Zero(t, store.replaces)
				require.Equal(t, "previous-provider", store.fingerprint)
				require.Equal(t, []intentstore.EmbeddingRecord{{
					Intent: string(search.IntentSearch), Exemplar: "previous", Embedding: embeddings.Embedding{9, 8}, Dimensions: 2,
				}}, store.rows)
			})
		}
	}
}
