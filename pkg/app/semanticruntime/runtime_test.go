package semanticruntime

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	searchintent "github.com/atomicobject/rhizome/pkg/search/intent"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

type runtimeProvider struct {
	batchSize      int
	maxConcurrency int
	maxBatchBytes  int
	offset         float32
	mu             sync.Mutex
	batches        [][]string
}

func (p *runtimeProvider) EmbedTexts(_ context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.mu.Lock()
	p.batches = append(p.batches, append([]string(nil), texts...))
	p.mu.Unlock()
	out := make([]embeddings.Embedding, len(texts))
	for i := range out {
		out[i] = make(embeddings.Embedding, p.Dimensions())
		out[i][0] = p.offset + float32(i)
	}
	return out, nil
}

func (p *runtimeProvider) Dimensions() int {
	return 8
}

func (p *runtimeProvider) DefaultBatchSize() int {
	return p.batchSize
}

func (p *runtimeProvider) DefaultMaxConcurrency() int {
	return p.maxConcurrency
}

func (p *runtimeProvider) DefaultMaxBatchBytes() int {
	return p.maxBatchBytes
}

func (p *runtimeProvider) batchSizes() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]int, 0, len(p.batches))
	for _, batch := range p.batches {
		out = append(out, len(batch))
	}
	return out
}

func TestRuntimeGroupsCompatibleProviderWorkOntoOneLane(t *testing.T) {
	rt := New()
	defer rt.Close()
	provider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000}
	info := embeddings.ProviderConfig{Provider: "voyage", Model: "voyage-3.5", Endpoint: "https://api.voyageai.com/v1", Dimensions: 8}
	packer := semantic.EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 512, MaxTexts: 512, MaxBytes: 384 << 10}

	codeLane, err := rt.EnsureLane(context.Background(), LaneRequest{Kind: LaneKindCode, Provider: provider, ProviderInfo: info, MaxConcurrent: 32, Packer: packer})
	require.NoError(t, err)
	noteLane, err := rt.EnsureLane(context.Background(), LaneRequest{Kind: LaneKindNote, Provider: provider, ProviderInfo: info, MaxConcurrent: 32, Packer: packer})
	require.NoError(t, err)

	require.Same(t, codeLane, noteLane)
	require.Same(t, noteLane, rt.IntentLane())
	require.Equal(t, 1000, noteLane.Packer.MaxTexts)
	require.Equal(t, 3_600_000, noteLane.Packer.MaxBytes)
}

func TestRuntimePromotesCompatibleLanePolicy(t *testing.T) {
	rt := New()
	defer rt.Close()
	provider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000}
	info := embeddings.ProviderConfig{Provider: "voyage", Model: "voyage-3.5", Dimensions: 8}

	low, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:          LaneKindNote,
		Provider:      provider,
		ProviderInfo:  info,
		MaxConcurrent: 4,
		Packer:        semantic.EmbedPackerOptions{MinTexts: 8, MaxTexts: 128, MaxBytes: 96 << 10},
	})
	require.NoError(t, err)
	burst, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:          LaneKindCode,
		Provider:      provider,
		ProviderInfo:  info,
		MaxConcurrent: 16,
		Packer:        semantic.EmbedPackerOptions{MinTexts: 64, MaxTexts: 256, MaxBytes: 192 << 10},
	})
	require.NoError(t, err)

	require.Same(t, low, burst)
	require.Equal(t, 16, burst.MaxConcurrent)
	require.Equal(t, 64, burst.Packer.MinTexts)
	require.Equal(t, 256, burst.Packer.MaxTexts)
	require.Equal(t, 192<<10, burst.Packer.MaxBytes)
}

func TestRuntimeExplicitBatchSizeCapsProviderDefault(t *testing.T) {
	rt := New()
	defer rt.Close()
	provider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000}
	info := embeddings.ProviderConfig{Provider: "voyage", Model: "voyage-3.5", Dimensions: 8}

	lane, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:         LaneKindNote,
		Provider:     provider,
		ProviderInfo: info,
		BatchSize:    128,
		Packer:       semantic.EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 512, MaxTexts: 512, MaxBytes: 384 << 10},
	})
	require.NoError(t, err)

	require.Equal(t, 128, lane.Packer.MaxTexts)
	require.Equal(t, 128, lane.Packer.MinTexts)
	require.Equal(t, 128, lane.Packer.AdaptiveMinTexts)
}

func TestRuntimeExistingExplicitBatchSizeSurvivesLanePromotion(t *testing.T) {
	rt := New()
	defer rt.Close()
	provider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000}
	info := embeddings.ProviderConfig{Provider: "voyage", Model: "voyage-3.5", Dimensions: 8}

	first, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:         LaneKindNote,
		Provider:     provider,
		ProviderInfo: info,
		BatchSize:    128,
		Packer:       semantic.EmbedPackerOptions{MinTexts: 8, MaxTexts: 128, MaxBytes: 96 << 10},
	})
	require.NoError(t, err)
	second, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:         LaneKindCode,
		Provider:     provider,
		ProviderInfo: info,
		Packer:       semantic.EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 512, MaxTexts: 512, MaxBytes: 384 << 10},
	})
	require.NoError(t, err)

	require.Same(t, first, second)
	require.Equal(t, 128, second.Packer.MaxTexts)
	require.Equal(t, 128, second.Packer.MinTexts)
	require.Equal(t, 128, second.Packer.AdaptiveMinTexts)
}

func TestRuntimePrefersCodeLaneForIntentMetricsWhenBothLanesExist(t *testing.T) {
	rt := New()
	defer rt.Close()
	codeProvider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000, offset: 11}
	noteProvider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000, offset: 21}
	packer := semantic.EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 512, MaxTexts: 512, MaxBytes: 384 << 10}

	codeLane, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:         LaneKindCode,
		Provider:     codeProvider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "voyage", Model: "code-model", Dimensions: 8},
		Packer:       packer,
	})
	require.NoError(t, err)
	noteLane, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:         LaneKindNote,
		Provider:     noteProvider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "voyage", Model: "note-model", Dimensions: 8},
		Packer:       packer,
	})
	require.NoError(t, err)

	require.NotSame(t, codeLane, noteLane)
	require.Same(t, codeLane, rt.IntentLane())
	codeVecs, err := codeLane.Node.Submit(context.Background(), "embed_code", []string{"code-lane"})
	require.NoError(t, err)
	noteVecs, err := noteLane.Node.Submit(context.Background(), "embed_notes", []string{"note-lane"})
	require.NoError(t, err)
	require.Equal(t, float32(11), codeVecs[0][0])
	require.Equal(t, float32(21), noteVecs[0][0])
	require.Equal(t, [][]string{{"code-lane"}}, codeProvider.batches)
	require.Equal(t, [][]string{{"note-lane"}}, noteProvider.batches)
}

func TestSyncIntentExemplarsRequiresSharedLane(t *testing.T) {
	rt := New()
	err := rt.SyncIntentExemplars(context.Background(), &runtimeIntentStore{})
	require.ErrorContains(t, err, "no shared lane")
}

func TestSyncIntentExemplarsUsesExistingSharedLane(t *testing.T) {
	rt := New()
	defer rt.Close()
	provider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000}
	info := embeddings.ProviderConfig{Provider: "voyage", Model: "voyage-3.5", Endpoint: "https://api.voyageai.com/v1", Dimensions: 8}
	packer := semantic.EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 512, MaxTexts: 512, MaxBytes: 384 << 10, MaxWait: 150_000_000}
	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	codeLane, err := rt.EnsureLane(ctx, LaneRequest{Kind: LaneKindCode, Provider: provider, ProviderInfo: info, MaxConcurrent: 32, Packer: packer})
	require.NoError(t, err)
	noteLane, err := rt.EnsureLane(ctx, LaneRequest{Kind: LaneKindNote, Provider: provider, ProviderInfo: info, MaxConcurrent: 32, Packer: packer})
	require.NoError(t, err)
	require.Same(t, codeLane, noteLane)

	store := &runtimeIntentStore{}
	doneIntent := indexingperf.StartSpan(indexingperf.WithPhase(ctx, "sync_intent_embeddings"), "sync_intent_embeddings")
	doneCode := indexingperf.StartSpan(indexingperf.WithPhase(ctx, "embed_code"), "embed_code")
	err = rt.SyncIntentExemplars(indexingperf.WithPhase(ctx, "sync_intent_embeddings"), store)
	doneIntent(err)
	doneCode(nil)
	require.NoError(t, err)

	expected := map[string]struct{}{}
	for intent, exemplars := range searchintent.DefaultExemplars() {
		for _, exemplar := range exemplars {
			expected[string(intent)+"\x00"+exemplar] = struct{}{}
		}
	}
	require.Equal(t, []int{len(expected)}, provider.batchSizes())
	require.Len(t, store.rows, len(expected))
	for _, row := range store.rows {
		key := row.Intent + "\x00" + row.Exemplar
		require.Contains(t, expected, key)
		delete(expected, key)
		require.Len(t, row.Embedding, provider.Dimensions())
	}
	require.Empty(t, expected)
	require.Equal(t, searchintent.ProviderFingerprint(info), store.fingerprint)
	summary := collector.RenderSummary()
	require.Contains(t, summary, "embed_code")
	require.Contains(t, summary, "calls=1")
	for _, line := range strings.Split(summary, "\n") {
		if strings.Contains(line, "sync_intent_embeddings") {
			require.NotContains(t, line, "calls=")
		}
	}
}

type runtimeIntentStore struct {
	fingerprint string
	rows        []intentstore.EmbeddingRecord
}

func (s *runtimeIntentStore) IntentEmbeddings(context.Context) ([]intentstore.EmbeddingRecord, error) {
	return s.rows, nil
}

func (s *runtimeIntentStore) IntentEmbeddingSnapshot(context.Context) (intentstore.Snapshot, error) {
	return intentstore.Snapshot{ProviderFingerprint: s.fingerprint, Rows: s.rows}, nil
}

func (s *runtimeIntentStore) ReplaceIntentEmbeddingSnapshot(_ context.Context, snapshot intentstore.Snapshot) error {
	s.fingerprint = snapshot.ProviderFingerprint
	s.rows = append([]intentstore.EmbeddingRecord(nil), snapshot.Rows...)
	return nil
}
