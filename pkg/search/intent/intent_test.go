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

type stubProvider struct {
	dims  int
	vecs  map[string]embeddings.Embedding
	calls int
	texts [][]string
}

func (p *stubProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.calls++
	p.texts = append(p.texts, append([]string(nil), texts...))
	out := make([]embeddings.Embedding, len(texts))
	for i, t := range texts {
		if vec, ok := p.vecs[t]; ok {
			out[i] = vec
			continue
		}
		out[i] = make(embeddings.Embedding, p.dims)
	}
	return out, nil
}

func (p *stubProvider) Dimensions() int {
	return p.dims
}

type defaultBatchStubProvider struct {
	stubProvider
	batchSize int
}

func (p *defaultBatchStubProvider) DefaultBatchSize() int {
	return p.batchSize
}

type flakyProvider struct {
	dims      int
	vecs      map[string]embeddings.Embedding
	failCalls int
	calls     int
}

func (p *flakyProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.calls++
	if p.failCalls > 0 {
		p.failCalls--
		return nil, errors.New("embed failed")
	}
	out := make([]embeddings.Embedding, len(texts))
	for i, t := range texts {
		if vec, ok := p.vecs[t]; ok {
			out[i] = vec
			continue
		}
		out[i] = make(embeddings.Embedding, p.dims)
	}
	return out, nil
}

func (p *flakyProvider) Dimensions() int {
	return p.dims
}

type stubIntentStore struct {
	fingerprint string
	rows        []intentstore.EmbeddingRecord
	seen        []intentstore.EmbeddingRecord
	replaces    int
	err         error
}

func (s *stubIntentStore) IntentEmbeddings(ctx context.Context) ([]intentstore.EmbeddingRecord, error) {
	return s.rows, s.err
}

func (s *stubIntentStore) IntentEmbeddingSnapshot(context.Context) (intentstore.Snapshot, error) {
	return intentstore.Snapshot{ProviderFingerprint: s.fingerprint, Rows: s.rows}, s.err
}

func (s *stubIntentStore) ReplaceIntentEmbeddingSnapshot(_ context.Context, snapshot intentstore.Snapshot) error {
	s.replaces++
	s.fingerprint = snapshot.ProviderFingerprint
	s.seen = append([]intentstore.EmbeddingRecord(nil), snapshot.Rows...)
	s.rows = append([]intentstore.EmbeddingRecord(nil), snapshot.Rows...)
	return nil
}

type stubEmbeddingNode struct {
	calls  int
	phases []string
	texts  [][]string
	vecs   map[string]embeddings.Embedding
}

func (n *stubEmbeddingNode) Submit(ctx context.Context, phase string, texts []string) ([]embeddings.Embedding, error) {
	n.calls++
	n.phases = append(n.phases, phase)
	n.texts = append(n.texts, append([]string(nil), texts...))
	out := make([]embeddings.Embedding, len(texts))
	for i, text := range texts {
		if vec, ok := n.vecs[text]; ok {
			out[i] = vec
		} else {
			out[i] = embeddings.Embedding{0, 0}
		}
	}
	return out, nil
}

func TestDetectorDetectsBestIntent(t *testing.T) {
	prov := &stubProvider{
		dims: 2,
		vecs: map[string]embeddings.Embedding{
			"alpha": {1, 0},
			"beta":  {0, 1},
		},
	}
	detector := NewDetector(prov, embeddings.ProviderConfig{Provider: "test", Model: "model", Dimensions: 2}, map[search.Intent][]string{
		search.IntentSearch:      {"alpha"},
		search.IntentDocsForCode: {"beta"},
	})

	intent, score, ok, err := detector.Detect(context.Background(), "alpha")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, search.IntentSearch, intent)
	require.Greater(t, score, 0.9)
}

func TestDetectorRespectsMargin(t *testing.T) {
	prov := &stubProvider{
		dims: 2,
		vecs: map[string]embeddings.Embedding{
			"alpha":    {1, 0},
			"beta":     {0, 1},
			"balanced": {0.7, 0.7},
		},
	}
	detector := NewDetector(prov, embeddings.ProviderConfig{Provider: "test", Model: "model", Dimensions: 2}, map[search.Intent][]string{
		search.IntentSearch:      {"alpha"},
		search.IntentDocsForCode: {"beta"},
	})

	_, _, ok, err := detector.Detect(context.Background(), "balanced")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDetectorDetectWithStoreMissingEmbeddings(t *testing.T) {
	prov := &stubProvider{
		dims: 2,
		vecs: map[string]embeddings.Embedding{
			"alpha": {1, 0},
		},
	}
	detector := NewDetector(prov, embeddings.ProviderConfig{Provider: "test", Model: "model", Dimensions: 2}, map[search.Intent][]string{
		search.IntentSearch: {"alpha"},
	})

	store := &stubIntentStore{}
	intent, _, ok, err := detector.DetectWithStore(context.Background(), "alpha", store)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, search.IntentSearch, intent)
}

func TestDetectorRespectsThreshold(t *testing.T) {
	prov := &stubProvider{
		dims: 2,
		vecs: map[string]embeddings.Embedding{
			"alpha": {1, 0},
			"beta":  {0, 1},
			"low":   {0, 0},
		},
	}
	detector := NewDetector(prov, embeddings.ProviderConfig{Provider: "test", Model: "model", Dimensions: 2}, map[search.Intent][]string{
		search.IntentSearch:      {"alpha"},
		search.IntentDocsForCode: {"beta"},
	})

	_, score, ok, err := detector.Detect(context.Background(), "low")
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, 0.0, score)
}

func TestDetectorRetriesAfterEmbedFailure(t *testing.T) {
	prov := &flakyProvider{
		dims:      2,
		failCalls: 1,
		vecs: map[string]embeddings.Embedding{
			"alpha": {1, 0},
			"beta":  {0, 1},
			"query": {1, 0},
		},
	}
	detector := NewDetector(prov, embeddings.ProviderConfig{Provider: "test", Model: "model", Dimensions: 2}, map[search.Intent][]string{
		search.IntentSearch:      {"alpha"},
		search.IntentDocsForCode: {"beta"},
	})

	_, _, _, err := detector.Detect(context.Background(), "query")
	require.Error(t, err)

	intent, score, ok, err := detector.Detect(context.Background(), "query")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, search.IntentSearch, intent)
	require.Greater(t, score, 0.9)
	require.Equal(t, 2, prov.calls)
}

func TestDefaultExemplarsIncludesP2(t *testing.T) {
	exemplars := DefaultExemplars()
	for _, intent := range []search.Intent{
		search.IntentOverview, search.IntentCodeForDocs,
		search.IntentConsolidation, search.IntentFindUsages, search.IntentCallers,
		search.IntentCallees, search.IntentGoToDef, search.IntentExplainSymbol,
		search.IntentTestsForCode, search.IntentRefactorImpact,
		search.IntentImplementers, search.IntentOverrides, search.IntentImports,
		search.IntentDataFlow, search.IntentSecurityAudit,
	} {
		require.NotEmpty(t, exemplars[intent], "expected exemplars for %s", intent)
	}
}

func TestSyncEmbeddingsUsesSharedNodeForMissingExemplars(t *testing.T) {
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "model", Dimensions: 2}
	missing := []string{"ex-09", "ex-08", "ex-07", "ex-06", "ex-05", "ex-04", "ex-03", "ex-02", "ex-01", "ex-00"}
	store := &stubIntentStore{
		fingerprint: ProviderFingerprint(providerInfo),
		rows: []intentstore.EmbeddingRecord{
			{Intent: string(search.IntentDocsForCode), Exemplar: "existing", Embedding: embeddings.Embedding{1, 0}, Dimensions: 2},
		},
	}
	prov := &stubProvider{dims: 2}
	node := &stubEmbeddingNode{}

	err := SyncEmbeddingsWithOptions(context.Background(), prov, store, map[search.Intent][]string{
		search.IntentSearch:      missing,
		search.IntentDocsForCode: {"existing"},
	}, SyncOptions{Node: node, ProviderInfo: providerInfo})
	require.NoError(t, err)

	require.Zero(t, prov.calls)
	require.Equal(t, []string{"sync_intent_embeddings"}, node.phases)
	require.Equal(t, [][]string{{"ex-00", "ex-01", "ex-02", "ex-03", "ex-04", "ex-05", "ex-06", "ex-07", "ex-08", "ex-09"}}, node.texts)
	require.Len(t, store.seen, len(missing)+1)
	require.Equal(t, "existing", store.seen[0].Exemplar)
	require.Equal(t, "ex-09", store.seen[len(store.seen)-1].Exemplar)
}

func TestSyncEmbeddingsUsesConfiguredSharedNodePhase(t *testing.T) {
	store := &stubIntentStore{}
	prov := &stubProvider{dims: 2}
	node := &stubEmbeddingNode{}

	err := SyncEmbeddingsWithOptions(context.Background(), prov, store, map[search.Intent][]string{
		search.IntentSearch: {"alpha"},
	}, SyncOptions{Node: node, Phase: "embed_code"})
	require.NoError(t, err)

	require.Equal(t, []string{"embed_code"}, node.phases)
	require.Equal(t, [][]string{{"alpha"}}, node.texts)
}

func TestSyncEmbeddingsFallsBackToProviderWithoutNode(t *testing.T) {
	store := &stubIntentStore{}
	prov := &stubProvider{dims: 2, vecs: map[string]embeddings.Embedding{
		"alpha": {1, 0},
	}}

	err := SyncEmbeddings(context.Background(), prov, store, map[search.Intent][]string{
		search.IntentSearch: {"alpha"},
	})
	require.NoError(t, err)

	require.Equal(t, 1, prov.calls)
	require.Equal(t, [][]string{{"alpha"}}, prov.texts)
	require.Equal(t, []intentstore.EmbeddingRecord{
		{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{1, 0}, Dimensions: 2},
	}, store.seen)
}

func TestSyncEmbeddingsSkipsMatchingDimensions(t *testing.T) {
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "model", Dimensions: 2}
	store := &stubIntentStore{
		fingerprint: ProviderFingerprint(providerInfo),
		rows: []intentstore.EmbeddingRecord{
			{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{1, 0}, Dimensions: 2},
		},
	}
	prov := &stubProvider{dims: 2}
	node := &stubEmbeddingNode{}

	err := SyncEmbeddingsWithOptions(context.Background(), prov, store, map[search.Intent][]string{
		search.IntentSearch: {"alpha"},
	}, SyncOptions{Node: node, ProviderInfo: providerInfo})
	require.NoError(t, err)

	require.Zero(t, node.calls)
	require.Zero(t, prov.calls)
	require.Empty(t, store.seen)
}

func TestProviderFingerprintTracksOnlyEmbeddingIdentity(t *testing.T) {
	base := embeddings.ProviderConfig{
		Provider:       " Voyage ",
		Model:          "voyage-3.5",
		APIKey:         "secret-a",
		Endpoint:       "https://api.voyageai.com/v1/",
		Dimensions:     1024,
		MaxConcurrency: 4,
		BatchSize:      32,
	}
	operationalChange := base
	operationalChange.APIKey = "secret-b"
	operationalChange.MaxConcurrency = 20
	operationalChange.BatchSize = 1000
	operationalChange.Endpoint = "https://api.voyageai.com/v1"
	operationalChange.Provider = "voyage"

	require.Equal(t, ProviderFingerprint(base), ProviderFingerprint(operationalChange))
	require.Empty(t, ProviderFingerprint(embeddings.ProviderConfig{}))

	modelChange := base
	modelChange.Model = "voyage-3-large"
	require.NotEqual(t, ProviderFingerprint(base), ProviderFingerprint(modelChange))
	dimensionChange := base
	dimensionChange.Dimensions = 512
	require.NotEqual(t, ProviderFingerprint(base), ProviderFingerprint(dimensionChange))
}

func TestSyncEmbeddingsProviderChangeRegeneratesCompleteSnapshot(t *testing.T) {
	oldInfo := embeddings.ProviderConfig{Provider: "test", Model: "old", Dimensions: 2}
	newInfo := embeddings.ProviderConfig{Provider: "test", Model: "new", Dimensions: 2}
	store := &stubIntentStore{
		fingerprint: ProviderFingerprint(oldInfo),
		rows: []intentstore.EmbeddingRecord{
			{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{1, 0}, Dimensions: 2},
		},
	}
	node := &stubEmbeddingNode{vecs: map[string]embeddings.Embedding{"alpha": {0, 1}}}

	err := SyncEmbeddingsWithOptions(context.Background(), &stubProvider{dims: 2}, store, map[search.Intent][]string{
		search.IntentSearch: {"alpha"},
	}, SyncOptions{Node: node, ProviderInfo: newInfo})
	require.NoError(t, err)

	require.Equal(t, 1, node.calls)
	require.Equal(t, ProviderFingerprint(newInfo), store.fingerprint)
	require.Equal(t, embeddings.Embedding{0, 1}, store.rows[0].Embedding)
}

func TestSyncEmbeddingsMatchingSnapshotRemovesStaleExemplarsWithoutEmbedding(t *testing.T) {
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "model", Dimensions: 2}
	store := &stubIntentStore{
		fingerprint: ProviderFingerprint(providerInfo),
		rows: []intentstore.EmbeddingRecord{
			{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{1, 0}, Dimensions: 2},
			{Intent: string(search.IntentSearch), Exemplar: "stale", Embedding: embeddings.Embedding{0, 1}, Dimensions: 2},
		},
	}
	node := &stubEmbeddingNode{}

	err := SyncEmbeddingsWithOptions(context.Background(), &stubProvider{dims: 2}, store, map[search.Intent][]string{
		search.IntentSearch: {"alpha"},
	}, SyncOptions{Node: node, ProviderInfo: providerInfo})
	require.NoError(t, err)

	require.Zero(t, node.calls)
	require.Equal(t, 1, store.replaces)
	require.Len(t, store.rows, 1)
	require.Equal(t, "alpha", store.rows[0].Exemplar)
}

func TestSyncEmbeddingsUnknownDimensionsLearnsThenReusesSnapshot(t *testing.T) {
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "dynamic"}
	store := &stubIntentStore{}
	firstNode := &stubEmbeddingNode{vecs: map[string]embeddings.Embedding{"alpha": {1, 2, 3}}}

	err := SyncEmbeddingsWithOptions(context.Background(), &stubProvider{}, store, map[search.Intent][]string{
		search.IntentSearch: {"alpha"},
	}, SyncOptions{Node: firstNode, ProviderInfo: providerInfo})
	require.NoError(t, err)
	require.Equal(t, 3, store.rows[0].Dimensions)
	require.Equal(t, 1, store.replaces)

	secondNode := &stubEmbeddingNode{}
	err = SyncEmbeddingsWithOptions(context.Background(), &stubProvider{}, store, map[search.Intent][]string{
		search.IntentSearch: {"alpha"},
	}, SyncOptions{Node: secondNode, ProviderInfo: providerInfo})
	require.NoError(t, err)
	require.Zero(t, secondNode.calls)
	require.Equal(t, 1, store.replaces)
}

func TestSyncEmbeddingsInvalidUnknownDimensionSnapshotRegeneratesAtNewDimension(t *testing.T) {
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "dynamic"}
	store := &stubIntentStore{
		fingerprint: ProviderFingerprint(providerInfo),
		rows: []intentstore.EmbeddingRecord{
			{Intent: string(search.IntentSearch), Exemplar: "alpha", Embedding: embeddings.Embedding{1}, Dimensions: 2},
		},
	}
	node := &stubEmbeddingNode{vecs: map[string]embeddings.Embedding{"alpha": {1, 2, 3}}}

	err := SyncEmbeddingsWithOptions(context.Background(), &stubProvider{}, store, map[search.Intent][]string{
		search.IntentSearch: {"alpha"},
	}, SyncOptions{Node: node, ProviderInfo: providerInfo})
	require.NoError(t, err)

	require.Equal(t, 1, node.calls)
	require.Equal(t, 3, store.rows[0].Dimensions)
}
