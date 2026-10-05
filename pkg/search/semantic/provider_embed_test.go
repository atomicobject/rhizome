package semantic

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

type slowBatchProvider struct {
	inner      embeddings.Provider
	delay      time.Duration
	active     atomic.Int64
	maxActive  atomic.Int64
	mu         sync.Mutex
	batchSizes []int
	entered    chan struct{}
	release    chan struct{}
}

func (p *slowBatchProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *slowBatchProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	active := p.active.Add(1)
	for {
		max := p.maxActive.Load()
		if active <= max || p.maxActive.CompareAndSwap(max, active) {
			break
		}
	}
	p.mu.Lock()
	p.batchSizes = append(p.batchSizes, len(texts))
	p.mu.Unlock()
	defer p.active.Add(-1)
	if p.entered != nil {
		p.entered <- struct{}{}
		<-p.release
	}
	time.Sleep(p.delay)
	return p.inner.EmbedTexts(ctx, texts)
}

func (p *slowBatchProvider) BatchSizes() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.batchSizes...)
}

func TestEmbedTextsWithProviderSplitsByBatchSize(t *testing.T) {
	t.Parallel()

	prov := &slowBatchProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}

	texts := []string{"a", "b", "c", "d", "e"}
	vecs, err := embedTextsWithProvider(context.Background(), prov, texts, 2, 1, nil)
	require.NoError(t, err)
	want, err := prov.inner.EmbedTexts(context.Background(), texts)
	require.NoError(t, err)
	require.Equal(t, want, vecs)
	batchSizes := prov.BatchSizes()
	slices.Sort(batchSizes)
	require.Equal(t, []int{1, 2, 2}, batchSizes)
}

func TestEmbedTextsWithProviderUsesConcurrency(t *testing.T) {
	t.Parallel()

	prov := &slowBatchProvider{
		inner:   embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
		entered: make(chan struct{}, 8),
		release: make(chan struct{}),
	}
	texts := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	type result struct {
		vecs []embeddings.Embedding
		err  error
	}
	finished := make(chan result, 1)
	go func() {
		vecs, err := embedTextsWithProvider(context.Background(), prov, texts, 2, 3, nil)
		finished <- result{vecs, err}
	}()
	t.Cleanup(func() {
		select {
		case <-prov.release:
		default:
			close(prov.release)
		}
	})
	for i := 0; i < 3; i++ {
		select {
		case <-prov.entered:
		case <-time.After(time.Second):
			t.Fatal("fewer than three batches entered")
		}
	}
	select {
	case <-prov.entered:
		t.Fatal("fourth batch exceeded cap")
	case <-time.After(30 * time.Millisecond):
	}
	close(prov.release)
	got := <-finished
	vecs, err := got.vecs, got.err
	require.NoError(t, err)
	want, err := prov.inner.EmbedTexts(context.Background(), texts)
	require.NoError(t, err)
	require.Equal(t, want, vecs)
	require.Equal(t, int64(3), prov.maxActive.Load())
}

type failFirstBatchProvider struct {
	inner embeddings.Provider
	calls atomic.Int64
}

func (p *failFirstBatchProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *failFirstBatchProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	if p.calls.Add(1) == 1 {
		return nil, errors.New("boom")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return p.inner.EmbedTexts(ctx, texts)
}

func TestEmbedTextsWithProviderCancelsRemainingBatchesOnError(t *testing.T) {
	t.Parallel()

	prov := &failFirstBatchProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}

	vecs, err := embedTextsWithProvider(context.Background(), prov, []string{"a", "b", "c"}, 1, 1, nil)
	require.ErrorContains(t, err, "boom")
	require.Nil(t, vecs)
	require.Equal(t, int64(1), prov.calls.Load())
}
