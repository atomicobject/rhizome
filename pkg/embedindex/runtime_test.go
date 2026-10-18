package embedindex

import (
	"context"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"

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

func TestRuntimeGroupsCompatibleProviderWorkOntoOneLane(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	provider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000}
	info := embeddings.ProviderConfig{Provider: "voyage", Model: "voyage-3.5", Endpoint: "https://api.voyageai.com/v1", Dimensions: 8}
	packer := EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 512, MaxTexts: 512, MaxBytes: 384 << 10}

	codeLane, err := rt.EnsureLane(context.Background(), LaneRequest{Kind: LaneKindCode, Provider: provider, ProviderInfo: info, MaxConcurrent: 32, Packer: packer})
	require.NoError(t, err)
	noteLane, err := rt.EnsureLane(context.Background(), LaneRequest{Kind: LaneKindNote, Provider: provider, ProviderInfo: info, MaxConcurrent: 32, Packer: packer})
	require.NoError(t, err)

	require.Same(t, codeLane, noteLane)
	require.Equal(t, 1000, noteLane.Packer.MaxTexts)
	require.Equal(t, 3_600_000, noteLane.Packer.MaxBytes)
}

func TestRuntimePromotesCompatibleLanePolicy(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	provider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000}
	info := embeddings.ProviderConfig{Provider: "voyage", Model: "voyage-3.5", Dimensions: 8}

	low, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:          LaneKindNote,
		Provider:      provider,
		ProviderInfo:  info,
		MaxConcurrent: 4,
		Packer:        EmbedPackerOptions{MinTexts: 8, MaxTexts: 128, MaxBytes: 96 << 10},
	})
	require.NoError(t, err)
	burst, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:          LaneKindCode,
		Provider:      provider,
		ProviderInfo:  info,
		MaxConcurrent: 16,
		Packer:        EmbedPackerOptions{MinTexts: 64, MaxTexts: 256, MaxBytes: 192 << 10},
	})
	require.NoError(t, err)

	require.Same(t, low, burst)
	require.Equal(t, 16, burst.MaxConcurrent)
	require.Equal(t, 64, burst.Packer.MinTexts)
	require.Equal(t, 256, burst.Packer.MaxTexts)
	require.Equal(t, 192<<10, burst.Packer.MaxBytes)
}

func TestRuntimeExplicitBatchSizeCapsProviderDefault(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	provider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000}
	info := embeddings.ProviderConfig{Provider: "voyage", Model: "voyage-3.5", Dimensions: 8}

	lane, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:         LaneKindNote,
		Provider:     provider,
		ProviderInfo: info,
		BatchSize:    128,
		Packer:       EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 512, MaxTexts: 512, MaxBytes: 384 << 10},
	})
	require.NoError(t, err)

	require.Equal(t, 128, lane.Packer.MaxTexts)
	require.Equal(t, 128, lane.Packer.MinTexts)
	require.Equal(t, 128, lane.Packer.AdaptiveMinTexts)
}

func TestRuntimeExistingExplicitBatchSizeSurvivesLanePromotion(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	provider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000}
	info := embeddings.ProviderConfig{Provider: "voyage", Model: "voyage-3.5", Dimensions: 8}

	first, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:         LaneKindNote,
		Provider:     provider,
		ProviderInfo: info,
		BatchSize:    128,
		Packer:       EmbedPackerOptions{MinTexts: 8, MaxTexts: 128, MaxBytes: 96 << 10},
	})
	require.NoError(t, err)
	second, err := rt.EnsureLane(context.Background(), LaneRequest{
		Kind:         LaneKindCode,
		Provider:     provider,
		ProviderInfo: info,
		Packer:       EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 512, MaxTexts: 512, MaxBytes: 384 << 10},
	})
	require.NoError(t, err)

	require.Same(t, first, second)
	require.Equal(t, 128, second.Packer.MaxTexts)
	require.Equal(t, 128, second.Packer.MinTexts)
	require.Equal(t, 128, second.Packer.AdaptiveMinTexts)
}

func TestRuntimeKeepsDistinctProviderLanesIndependent(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	codeProvider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000, offset: 11}
	noteProvider := &runtimeProvider{batchSize: 1000, maxConcurrency: 32, maxBatchBytes: 3_600_000, offset: 21}
	packer := EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 512, MaxTexts: 512, MaxBytes: 384 << 10}

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
	codeVecs, err := codeLane.Node.Submit(context.Background(), "embed_code", []string{"code-lane"})
	require.NoError(t, err)
	noteVecs, err := noteLane.Node.Submit(context.Background(), "embed_notes", []string{"note-lane"})
	require.NoError(t, err)
	require.Equal(t, float32(11), codeVecs[0][0])
	require.Equal(t, float32(21), noteVecs[0][0])
	require.Equal(t, [][]string{{"code-lane"}}, codeProvider.batches)
	require.Equal(t, [][]string{{"note-lane"}}, noteProvider.batches)
}
