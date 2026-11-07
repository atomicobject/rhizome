package embedindex

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
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

func TestOntologyComputeDerivedSplitsByBatchSize(t *testing.T) {
	t.Parallel()

	prov := &slowBatchProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}

	texts := []string{"a", "b", "c", "d", "e"}
	result, err := (OntologyNodeSyncer{Provider: prov, BatchSize: 2, MaxConcurrent: 1}).ComputeDerived(context.Background(), OntologyDerivedPlan{texts: texts})
	require.NoError(t, err)
	want, err := prov.inner.EmbedTexts(context.Background(), texts)
	require.NoError(t, err)
	require.Equal(t, want, result.vectors)
	batchSizes := prov.BatchSizes()
	slices.Sort(batchSizes)
	require.Equal(t, []int{1, 2, 2}, batchSizes)
}

func TestOntologyComputeDerivedUsesConcurrency(t *testing.T) {
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
		derived, err := (OntologyNodeSyncer{Provider: prov, BatchSize: 2, MaxConcurrent: 3}).ComputeDerived(context.Background(), OntologyDerivedPlan{texts: texts})
		finished <- result{derived.vectors, err}
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

func TestOntologyComputeDerivedStopsAfterBatchError(t *testing.T) {
	t.Parallel()

	prov := &failFirstBatchProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}

	result, err := (OntologyNodeSyncer{Provider: prov, BatchSize: 1, MaxConcurrent: 1}).ComputeDerived(context.Background(), OntologyDerivedPlan{ids: []string{"a", "b", "c"}, texts: []string{"a", "b", "c"}})
	require.ErrorContains(t, err, "boom")
	require.Empty(t, result.vectors)
	require.Equal(t, int64(1), prov.calls.Load())
}

func TestEmbedProviderBatchPreservesOrderAndMetrics(t *testing.T) {
	t.Parallel()
	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "embed_ontology_nodes")
	provider := &defaultingProvider{batchSize: 8}
	gate := make(chan struct{}, 1)
	vectors, err := embedProviderBatch(ctx, provider, []string{"one", "two", "three"}, 4, gate)
	require.NoError(t, err)
	require.Equal(t, []embeddings.Embedding{{1}, {2}, {3}}, vectors)
	require.Equal(t, []int{3}, provider.BatchSizes())
	require.Empty(t, gate)

	snapshot := collector.Snapshot()
	counts := map[string]int64{}
	for _, counter := range snapshot.Counters {
		require.Equal(t, "embed_ontology_nodes", counter.Phase)
		counts[counter.Name] = counter.Total
	}
	require.EqualValues(t, 1, counts[string(indexingperf.ProviderCalls)])
	require.EqualValues(t, 3, counts[string(indexingperf.ProviderTexts)])
	require.EqualValues(t, 3, counts[string(indexingperf.ProviderLogicalTexts)])
	require.EqualValues(t, 11, counts[string(indexingperf.ProviderTextBytes)])
	samples := map[string]int64{}
	for _, sample := range snapshot.Samples {
		samples[sample.Name] = sample.Total
	}
	require.EqualValues(t, 8, samples[string(indexingperf.ProviderCapTexts)])
	require.EqualValues(t, 4, samples[string(indexingperf.ProviderDispatchFloorTexts)])
	require.EqualValues(t, 1, samples[string(indexingperf.ProviderCapacity)])
	require.EqualValues(t, 3, samples[string(indexingperf.ProviderBatchTexts)])
	require.EqualValues(t, 11, samples[string(indexingperf.ProviderBatchBytes)])
	require.EqualValues(t, 75, samples[string(indexingperf.ProviderBatchFillRatio)])
	for _, gauge := range snapshot.Gauges {
		if gauge.Name == string(indexingperf.ProviderInflight) {
			require.EqualValues(t, 0, gauge.Current)
			require.EqualValues(t, 1, gauge.Peak)
		}
	}
}

type observedDoneContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestEmbedProviderBatchCancelsWhileWaitingForGate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := &observedDoneContext{Context: ctx, observed: make(chan struct{})}
	provider := &defaultingProvider{batchSize: 8}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	done := make(chan error, 1)
	go func() {
		_, err := embedProviderBatch(waiting, provider, []string{"waiting"}, 1, gate)
		done <- err
	}()
	select {
	case <-waiting.observed:
	case <-time.After(time.Second):
		t.Fatal("provider batch did not wait for the shared gate")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("provider batch did not cancel while the gate remained occupied")
	}
	require.Empty(t, provider.BatchSizes())
	require.Len(t, gate, 1, "cancellation must not release another lane's gate slot")
}

type fixedVectorProvider []embeddings.Embedding

func (fixedVectorProvider) Dimensions() int { return 8 }
func (p fixedVectorProvider) EmbedTexts(context.Context, []string) ([]embeddings.Embedding, error) {
	return p, nil
}

func TestEmbedProviderBatchRejectsMalformedVectorCount(t *testing.T) {
	t.Parallel()
	for _, vectors := range []fixedVectorProvider{{{1}}, {{1}, {2}, {3}}} {
		gate := make(chan struct{}, 1)
		got, err := embedProviderBatch(context.Background(), vectors, []string{"one", "two"}, 2, gate)
		require.ErrorContains(t, err, "for 2 texts")
		require.Nil(t, got)
		require.Empty(t, gate)
	}
}
