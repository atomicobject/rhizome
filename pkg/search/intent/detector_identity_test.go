package intent

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
	"github.com/stretchr/testify/require"
)

func TestDetectorRejectsStoredEmbeddingsFromDifferentSameDimensionModel(t *testing.T) {
	currentInfo := embeddings.ProviderConfig{Provider: "test", Model: "current", Dimensions: 2}
	for name, fingerprint := range map[string]string{
		"different model":         ProviderFingerprint(embeddings.ProviderConfig{Provider: "test", Model: "old", Dimensions: 2}),
		"legacy missing identity": "",
	} {
		t.Run(name, func(t *testing.T) {
			provider := &stubProvider{dims: 2, vecs: map[string]embeddings.Embedding{
				"query": {1, 0},
				"alpha": {1, 0},
				"beta":  {0, 1},
			}}
			detector := NewDetector(provider, currentInfo, map[search.Intent][]string{
				search.IntentSearch:      {"alpha"},
				search.IntentDocsForCode: {"beta"},
			})
			store := &stubIntentStore{
				fingerprint: fingerprint,
				rows: []intentstore.EmbeddingRecord{
					{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{0, 1}, Dimensions: 2},
					{Intent: string(search.IntentDocsForCode), Exemplar: "beta", Embedding: embeddings.Embedding{1, 0}, Dimensions: 2},
				},
			}

			detected, _, ok, err := detector.DetectWithStore(context.Background(), "query", store)

			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, search.IntentSearch, detected)
			require.Equal(t, 2, provider.calls, "incompatible snapshot must fall back to current-provider exemplars")
		})
	}
}

func TestDetectorUsesMatchingStoredEmbeddingSnapshot(t *testing.T) {
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "current", Dimensions: 2}
	provider := &stubProvider{dims: 2, vecs: map[string]embeddings.Embedding{
		"query": {1, 0},
		"alpha": {0, 1},
		"beta":  {1, 0},
	}}
	detector := NewDetector(provider, providerInfo, map[search.Intent][]string{
		search.IntentSearch:      {"alpha"},
		search.IntentDocsForCode: {"beta"},
	})
	store := &stubIntentStore{
		fingerprint: ProviderFingerprint(providerInfo),
		rows: []intentstore.EmbeddingRecord{
			{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{1, 0}, Dimensions: 2},
			{Intent: string(search.IntentDocsForCode), Exemplar: "beta", Embedding: embeddings.Embedding{0, 1}, Dimensions: 2},
		},
	}

	detected, score, ok, err := detector.DetectWithStore(context.Background(), "query", store)

	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, search.IntentSearch, detected)
	require.Greater(t, score, 0.9)
	require.Equal(t, 1, provider.calls, "matching snapshot should avoid embedding exemplars")
}

func TestDetectorRejectsMalformedStoredEmbeddingDimensions(t *testing.T) {
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "current", Dimensions: 2}
	provider := &stubProvider{dims: 2, vecs: map[string]embeddings.Embedding{
		"query": {1, 0},
		"alpha": {1, 0},
		"beta":  {0, 1},
	}}
	detector := NewDetector(provider, providerInfo, map[search.Intent][]string{
		search.IntentSearch:      {"alpha"},
		search.IntentDocsForCode: {"beta"},
	})
	store := &stubIntentStore{
		fingerprint: ProviderFingerprint(providerInfo),
		rows: []intentstore.EmbeddingRecord{
			{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{1}, Dimensions: 2},
			{Intent: string(search.IntentDocsForCode), Exemplar: "beta", Embedding: embeddings.Embedding{0, 1}, Dimensions: 2},
		},
	}

	detected, _, ok, err := detector.DetectWithStore(context.Background(), "query", store)

	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, search.IntentSearch, detected)
	require.Equal(t, 2, provider.calls)
}

func TestDetectorCacheIncludesProviderFingerprint(t *testing.T) {
	provider := &stubProvider{dims: 2}
	firstInfo := embeddings.ProviderConfig{Provider: "test", Model: "first", Dimensions: 2}
	secondInfo := embeddings.ProviderConfig{Provider: "test", Model: "second", Dimensions: 2}

	first := DetectorForProvider(provider, firstInfo)
	require.Same(t, first, DetectorForProvider(provider, firstInfo))
	require.NotSame(t, first, DetectorForProvider(provider, secondInfo))
}

func TestDetectorRejectsIncompleteOrDuplicateExemplarSnapshot(t *testing.T) {
	info := embeddings.ProviderConfig{Provider: "test", Model: "current", Dimensions: 2}
	alpha := intentstore.EmbeddingRecord{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{0, 1}, Dimensions: 2}
	beta := intentstore.EmbeddingRecord{Intent: string(search.IntentDocsForCode), Exemplar: "beta", Embedding: embeddings.Embedding{1, 0}, Dimensions: 2}
	stale := alpha
	stale.Exemplar = "removed"
	for name, rows := range map[string][]intentstore.EmbeddingRecord{
		"missing competing intent":            {beta},
		"duplicate replaces missing exemplar": {beta, beta},
		"removed exemplar replaces current":   {stale, beta},
		"extra obsolete exemplar":             {alpha, beta, stale},
	} {
		t.Run(name, func(t *testing.T) {
			provider := &stubProvider{dims: 2, vecs: map[string]embeddings.Embedding{
				"query": {1, 0}, "alpha": {1, 0}, "beta": {0, 1},
			}}
			detector := NewDetector(provider, info, map[search.Intent][]string{
				search.IntentSearch: {"alpha"}, search.IntentDocsForCode: {"beta"},
			})
			store := &stubIntentStore{fingerprint: ProviderFingerprint(info), rows: rows}
			detected, _, ok, err := detector.DetectWithStore(context.Background(), "query", store)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, search.IntentSearch, detected)
			require.Equal(t, 2, provider.calls, "incomplete corpus must fall back to current exemplars")
		})
	}
}
