package semantic

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

type defaultingProvider struct {
	batchSize      int
	maxConcurrency int
	maxBatchBytes  int

	mu      sync.Mutex
	batches [][]string
	started chan int
	block   <-chan struct{}
	permit  <-chan struct{}
	onEmbed func(context.Context)
}

func (p *defaultingProvider) Dimensions() int { return 8 }
func (p *defaultingProvider) DefaultBatchSize() int {
	return p.batchSize
}
func (p *defaultingProvider) DefaultMaxConcurrency() int {
	return p.maxConcurrency
}
func (p *defaultingProvider) DefaultMaxBatchBytes() int {
	return p.maxBatchBytes
}
func (p *defaultingProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.mu.Lock()
	p.batches = append(p.batches, append([]string(nil), texts...))
	p.mu.Unlock()
	if p.onEmbed != nil {
		p.onEmbed(ctx)
	}
	if p.started != nil {
		p.started <- len(texts)
	}
	if p.block != nil {
		<-p.block
	}
	if p.permit != nil {
		<-p.permit
	}
	out := make([]embeddings.Embedding, len(texts))
	for i := range out {
		out[i] = embeddings.Embedding{float32(i + 1)}
	}
	return out, nil
}
func (p *defaultingProvider) BatchSizes() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]int, 0, len(p.batches))
	for _, batch := range p.batches {
		out = append(out, len(batch))
	}
	return out
}

func TestEffectiveProviderDefaults(t *testing.T) {
	t.Parallel()

	prov := &defaultingProvider{batchSize: 1000, maxConcurrency: 32}
	require.Equal(t, 1000, EffectiveBatchSize(prov, 0))
	require.Equal(t, 32, EffectiveMaxConcurrent(prov, 0))
	require.Equal(t, 12, EffectiveBatchSize(prov, 12))
	require.Equal(t, 7, EffectiveMaxConcurrent(prov, 7))
}

func TestApplyProviderPackerLimitsRaisesThroughputCeilings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		provider *defaultingProvider
		in, want EmbedPackerOptions
	}{
		{"throughput lift", &defaultingProvider{batchSize: 1000, maxBatchBytes: 3_600_000}, EmbedPackerOptions{MinTexts: 256, MaxTexts: 512, MaxBytes: 384 << 10, MaxWait: time.Second}, EmbedPackerOptions{MinTexts: FullScanAdaptivePackerFloor, MaxTexts: 1000, MaxBytes: 3_600_000, MaxWait: time.Second}},
		{"adaptive floor without lift", &defaultingProvider{batchSize: 512, maxBatchBytes: 384 << 10}, EmbedPackerOptions{MinTexts: 256, MaxTexts: 512, MaxBytes: 384 << 10, MaxWait: time.Second}, EmbedPackerOptions{MinTexts: FullScanAdaptivePackerFloor, MaxTexts: 512, MaxBytes: 384 << 10, MaxWait: time.Second}},
		{"explicit adaptive floor", &defaultingProvider{batchSize: 1000, maxBatchBytes: 3_600_000}, EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 256, MaxTexts: 512, MaxBytes: 384 << 10, MaxWait: time.Second}, EmbedPackerOptions{MinTexts: 256, AdaptiveMinTexts: 256, MaxTexts: 1000, MaxBytes: 3_600_000, MaxWait: time.Second}},
		{"low latency", &defaultingProvider{batchSize: 1000, maxBatchBytes: 3_600_000}, EmbedPackerOptions{MinTexts: 8, MaxTexts: 128, MaxBytes: 96 << 10, MaxWait: 75 * time.Millisecond}, EmbedPackerOptions{MinTexts: 8, MaxTexts: 128, MaxBytes: 96 << 10, MaxWait: 75 * time.Millisecond}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ApplyProviderPackerLimits(tc.provider, tc.in)
			require.Equal(t, tc.want.MinTexts, got.MinTexts)
			require.Equal(t, tc.want.MaxTexts, got.MaxTexts)
			require.Equal(t, tc.want.MaxBytes, got.MaxBytes)
			require.Equal(t, tc.want.MaxWait, got.MaxWait)
		})
	}
}

func TestSharedEmbeddingNodePassesBatchPhaseToProvider(t *testing.T) {
	t.Parallel()

	ctx := indexingperf.WithCollector(context.Background(), indexingperf.New())
	prov := &defaultingProvider{
		batchSize:      1000,
		maxConcurrency: 1,
		onEmbed: func(providerCtx context.Context) {
			indexingperf.AddCount(providerCtx, "provider.request.voyage", 1)
			indexingperf.AddCount(providerCtx, "provider.request.voyage.attempt", 2)
			indexingperf.AddCount(providerCtx, "provider.request.voyage.http_retry", 1)
			indexingperf.AddBytes(providerCtx, "provider.request.voyage.body", 1024)
			indexingperf.ObserveSample(providerCtx, "provider.request.voyage.inputs", 3)
			indexingperf.ObserveSample(providerCtx, "provider.request.voyage.status", 429)
			indexingperf.ObserveSample(providerCtx, "provider.request.voyage.status", 200)
		},
	}
	node := NewSharedEmbeddingNode(ctx, prov, 1, nil, EmbedPackerOptions{
		MinTexts: 1,
		MaxTexts: 8,
		MaxBytes: 1 << 20,
		MaxWait:  time.Hour,
	})
	defer node.Close()

	_, err := node.Submit(ctx, "embed_ontology_nodes", []string{"one", "two", "three"})
	require.NoError(t, err)

	summary := indexingperf.FromContext(ctx).RenderSummary()
	require.Contains(t, summary, "embed_ontology_nodes")
	require.Contains(t, summary, "unspanned")
	require.Contains(t, summary, "voyage_requests=1")
	require.Contains(t, summary, "voyage_attempts=2")
	require.Contains(t, summary, "voyage_http_retries=1")
	require.Contains(t, summary, "voyage_responses=2")
	require.Contains(t, summary, "voyage_status_p50=200")
	require.Contains(t, summary, "voyage_status_p95=429")
	require.Contains(t, summary, "voyage_inputs_max=3")
	require.Contains(t, summary, "voyage_body_bytes=1.0KiB")
}

func TestSharedEmbeddingNodeKeepsSlowProviderVisibleInTimingWindows(t *testing.T) {
	t.Parallel()

	ctx := indexingperf.WithCollector(context.Background(), indexingperf.New())
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	prov := &defaultingProvider{
		batchSize:      1000,
		maxConcurrency: 1,
		started:        make(chan int, 1),
		block:          release,
	}
	node := NewSharedEmbeddingNode(ctx, prov, 1, nil, EmbedPackerOptions{
		MinTexts: 1,
		MaxTexts: 8,
		MaxBytes: 1 << 20,
		MaxWait:  time.Hour,
	})
	defer func() {
		releaseOnce.Do(func() { close(release) })
		node.Close()
	}()

	errs := make(chan error, 1)
	go func() {
		_, err := node.Submit(ctx, "embed_ontology_nodes", []string{"blocked"})
		errs <- err
	}()
	require.Equal(t, 1, <-prov.started)

	first := indexingperf.FromContext(ctx).RenderWindow(time.Second)
	second := indexingperf.FromContext(ctx).RenderWindow(time.Second)
	require.Contains(t, first, "phase=embed_ontology_nodes")
	require.Contains(t, first, "provider_inflight=1")
	require.Contains(t, second, "phase=embed_ontology_nodes")
	require.Contains(t, second, "provider_inflight=1")

	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-errs)
}

func TestSharedEmbeddingNodeCountsOnlyCallsPastSharedGate(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "embed_code")
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	releaseProvider := make(chan struct{})
	var releaseProviderOnce sync.Once
	prov := &defaultingProvider{
		batchSize:      1000,
		maxConcurrency: 1,
		started:        make(chan int, 1),
		block:          releaseProvider,
	}
	node := NewSharedEmbeddingNode(ctx, prov, 1, gate, EmbedPackerOptions{
		MinTexts: 1,
		MaxTexts: 8,
		MaxBytes: 1 << 20,
		MaxWait:  time.Hour,
	})
	t.Cleanup(func() {
		releaseProviderOnce.Do(func() { close(releaseProvider) })
		select {
		case <-gate:
		default:
		}
		node.Close()
	})

	errs := make(chan error, 1)
	go func() {
		_, err := node.Submit(ctx, "embed_ontology_nodes", []string{"blocked"})
		errs <- err
	}()

	require.Never(t, func() bool {
		select {
		case <-prov.started:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 5*time.Millisecond)
	first := collector.RenderWindow(time.Second)
	require.NotContains(t, first, "provider_inflight=1")

	<-gate
	require.Equal(t, 1, <-prov.started)
	second := collector.RenderWindow(time.Second)
	require.Contains(t, second, "phase=embed_ontology_nodes")
	require.Contains(t, second, "provider_inflight=1")
	require.NotContains(t, second, "phase=embed_code")

	releaseProviderOnce.Do(func() { close(releaseProvider) })
	require.NoError(t, <-errs)
	third := collector.RenderWindow(time.Second)
	require.NotContains(t, third, "provider_inflight=1")
}

func TestSharedEmbeddingNodeReportsOnePhysicalMixedBatch(t *testing.T) {
	t.Parallel()

	ctx := indexingperf.WithCollector(context.Background(), indexingperf.New())
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	prov := &defaultingProvider{
		batchSize:      1000,
		maxConcurrency: 1,
		started:        make(chan int, 1),
		block:          release,
		onEmbed: func(providerCtx context.Context) {
			indexingperf.AddCount(providerCtx, "provider.request.voyage", 1)
			indexingperf.AddCount(providerCtx, "provider.request.voyage.attempt", 1)
		},
	}
	node := NewSharedEmbeddingNode(ctx, prov, 1, nil, EmbedPackerOptions{
		MinTexts: 4,
		MaxTexts: 8,
		MaxBytes: 1 << 20,
		MaxWait:  time.Hour,
	})
	defer func() {
		releaseOnce.Do(func() { close(release) })
		node.Close()
	}()

	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, phase := range []string{"embed_code", "embed_ontology_nodes"} {
		phase := phase
		go func() {
			<-start
			_, err := node.Submit(ctx, phase, []string{phase + "-a", phase + "-b"})
			errs <- err
		}()
	}
	close(start)
	require.Equal(t, 4, <-prov.started)

	first := indexingperf.FromContext(ctx).RenderWindow(time.Second)
	second := indexingperf.FromContext(ctx).RenderWindow(time.Second)
	require.Contains(t, first, "phase=shared_provider")
	require.Contains(t, first, "provider_inflight=1")
	require.Contains(t, second, "phase=shared_provider")
	require.Contains(t, second, "provider_inflight=1")

	releaseOnce.Do(func() { close(release) })
	for range 2 {
		require.NoError(t, <-errs)
	}
	require.Equal(t, []int{4}, prov.BatchSizes())

	summary := indexingperf.FromContext(ctx).RenderSummary()
	require.Contains(t, summary, "shared_provider")
	require.Equal(t, 1, strings.Count(summary, "voyage_requests=1"))
	require.Contains(t, summary, "embed_code")
	require.Contains(t, summary, "embed_ontology_nodes")
	require.Contains(t, summary, "mixed_batches=1")
}

func TestSharedEmbeddingNodeDedupesIdenticalTextsInsideBatch(t *testing.T) {
	t.Parallel()

	prov := &defaultingProvider{batchSize: 1000, maxConcurrency: 4}
	node := NewSharedEmbeddingNode(context.Background(), prov, 1, nil, EmbedPackerOptions{
		MinTexts: 3,
		MaxTexts: 8,
		MaxBytes: 1 << 20,
		MaxWait:  time.Hour,
	})
	defer node.Close()

	vecs, err := node.Submit(context.Background(), "embed_code", []string{"same", "other", "same"})
	require.NoError(t, err)
	require.Len(t, vecs, 3)
	require.Equal(t, []int{2}, prov.BatchSizes())
	require.Equal(t, vecs[0], vecs[2])
	require.NotEqual(t, vecs[0], vecs[1])
}

func TestSharedEmbeddingNodeDispatchesAdaptiveFullScanFloor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		floor int
	}{{"default floor", FullScanAdaptivePackerFloor}, {"explicit floor", 256}} {
		t.Run(tc.name, func(t *testing.T) {
			prov := &defaultingProvider{batchSize: 1000, maxConcurrency: 4}
			opts := EmbedPackerOptions{MinTexts: tc.floor, MaxTexts: 1000, MaxBytes: 1 << 20, MaxWait: time.Hour}
			if tc.floor == 256 {
				opts.AdaptiveMinTexts = 256
			}
			node := NewSharedEmbeddingNode(context.Background(), prov, 4, nil, opts)
			defer node.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			vecs, err := node.Submit(ctx, "embed_code", numberedTexts("floor", tc.floor))
			require.NoError(t, err)
			require.Len(t, vecs, tc.floor)
			require.Equal(t, []int{tc.floor}, prov.BatchSizes())
		})
	}
}

func TestSharedEmbeddingNodeTargetsOpenSlotsAfterWarmup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		floor, cap int
	}{{"thousand text cap", 512, 1000}, {"five hundred text cap", 256, 512}} {
		t.Run(tc.name, func(t *testing.T) {
			const slots = 16
			permits := make(chan struct{})
			var release sync.Once
			prov := &defaultingProvider{batchSize: tc.cap, maxConcurrency: slots, started: make(chan int, slots+2), permit: permits}
			collector := indexingperf.New()
			ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "embed_code")
			opts := EmbedPackerOptions{MinTexts: tc.floor, MaxTexts: tc.cap, MaxBytes: 1 << 20, MaxWait: time.Hour}
			if tc.floor == 256 {
				opts.AdaptiveMinTexts = 256
			}
			node := NewSharedEmbeddingNode(ctx, prov, slots, nil, opts)
			defer func() { release.Do(func() { close(permits) }); node.Close() }()
			errs := make(chan error, slots+1)
			for i := 0; i < slots; i++ {
				go func(i int) {
					_, err := node.Submit(context.Background(), "embed_code", numberedTexts(fmt.Sprintf("warm-%d", i), tc.floor))
					errs <- err
				}(i)
			}
			for i := 0; i < slots; i++ {
				select {
				case got := <-prov.started:
					require.Equal(t, tc.floor, got)
				case <-time.After(time.Second):
					t.Fatal("warm slot did not start")
				}
			}
			go func() {
				_, err := node.Submit(context.Background(), "embed_code", numberedTexts("queued", tc.cap))
				errs <- err
			}()
			require.Eventually(t, func() bool {
				return strings.Contains(collector.RenderSummary(), fmt.Sprintf("ready_depth_peak=%d", tc.cap))
			}, time.Second, time.Millisecond)
			permits <- struct{}{}
			select {
			case got := <-prov.started:
				require.Equal(t, tc.cap, got, "newly open slot should target queued backlog")
			case <-time.After(time.Second):
				t.Fatal("queued backlog was not dispatched")
			}
			release.Do(func() { close(permits) })
			for i := 0; i < slots+1; i++ {
				require.NoError(t, <-errs)
			}
		})
	}
}

func numberedTexts(prefix string, count int) []string {
	texts := make([]string, count)
	for i := range texts {
		texts[i] = fmt.Sprintf("%s-%04d", prefix, i)
	}
	return texts
}
