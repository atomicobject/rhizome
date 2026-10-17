package embedindex

import (
	"context"
	"errors"

	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type SharedEmbeddingNode struct {
	ctx           context.Context
	cancel        context.CancelFunc
	provider      embeddings.Provider
	configMu      sync.RWMutex
	maxConcurrent int
	gate          chan struct{}
	opts          EmbedPackerOptions

	submitMu sync.RWMutex
	closed   bool
	requests chan *sharedEmbeddingRequest
	results  chan sharedEmbeddingBatchResult
	wg       sync.WaitGroup
}

type sharedEmbeddingNodeConfig struct {
	opts          EmbedPackerOptions
	maxConcurrent int
	gate          chan struct{}
}

type sharedEmbeddingRequest struct {
	ctx   context.Context
	phase string
	texts []string
	vecs  []embeddings.Embedding
	err   error
	done  chan struct{}
	left  int
}

type sharedEmbeddingJob struct {
	req        *sharedEmbeddingRequest
	index      int
	text       string
	bytes      int
	enqueuedAt time.Time
}

type sharedEmbeddingBatchResult struct {
	jobs []sharedEmbeddingJob
	vecs []embeddings.Embedding
	err  error
}

const sharedProviderPhase = "shared_provider"

// NewSharedEmbeddingNode starts a provider-packing node shared by compatible
// semantic lanes. Submitters keep their own phase labels, but actual provider
// calls can be coalesced, deduped, and capped together.
func NewSharedEmbeddingNode(ctx context.Context, provider embeddings.Provider, maxConcurrent int, gate chan struct{}, opts EmbedPackerOptions) *SharedEmbeddingNode {
	if provider == nil {
		return nil
	}
	if maxConcurrent <= 0 {
		maxConcurrent = EffectiveMaxConcurrent(provider, 0)
	}
	opts = normalizeEmbedPackerOptions(opts)
	nodeCtx, cancel := context.WithCancel(ctx)
	n := &SharedEmbeddingNode{
		ctx:           nodeCtx,
		cancel:        cancel,
		provider:      provider,
		maxConcurrent: maxConcurrent,
		gate:          gate,
		opts:          opts,
		requests:      make(chan *sharedEmbeddingRequest, maxConcurrent*opts.MaxTexts),
		results:       make(chan sharedEmbeddingBatchResult, maxConcurrent*2),
	}
	n.wg.Add(1)
	go n.run()
	return n
}

// Close stops accepting submissions and drains pending provider work.
func (n *SharedEmbeddingNode) Close() {
	if n == nil {
		return
	}
	started := time.Now()
	n.submitMu.Lock()
	if n.closed {
		n.submitMu.Unlock()
		return
	}
	n.closed = true
	close(n.requests)
	n.submitMu.Unlock()
	n.wg.Wait()
	indexingperf.ObserveLatency(n.ctx, indexingperf.EmbedNodeCloseDrain, time.Since(started))
	n.cancel()
}

// Configure promotes lane capacity/packing when a compatible later caller asks
// for more throughput. It never lowers max concurrency because an active full
// scan may already depend on those slots.
func (n *SharedEmbeddingNode) Configure(maxConcurrent int, opts EmbedPackerOptions) {
	if n == nil {
		return
	}
	opts = normalizeEmbedPackerOptions(opts)
	if maxConcurrent <= 0 {
		maxConcurrent = EffectiveMaxConcurrent(n.provider, 0)
	}
	n.configMu.Lock()
	if maxConcurrent > n.maxConcurrent {
		n.maxConcurrent = maxConcurrent
	}
	n.opts = opts
	n.configMu.Unlock()
}

func (n *SharedEmbeddingNode) config() sharedEmbeddingNodeConfig {
	n.configMu.RLock()
	defer n.configMu.RUnlock()
	return sharedEmbeddingNodeConfig{
		opts:          n.opts,
		maxConcurrent: n.maxConcurrent,
		gate:          n.gate,
	}
}

func (n *SharedEmbeddingNode) Submit(ctx context.Context, phase string, texts []string) ([]embeddings.Embedding, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if n == nil {
		return nil, errors.New("shared embedding node is nil")
	}
	req := &sharedEmbeddingRequest{
		ctx:   ctx,
		phase: phase,
		texts: append([]string(nil), texts...),
		vecs:  make([]embeddings.Embedding, len(texts)),
		done:  make(chan struct{}),
		left:  len(texts),
	}
	submitStarted := time.Now()
	n.submitMu.RLock()
	if n.closed {
		n.submitMu.RUnlock()
		return nil, errors.New("shared embedding node is closed")
	}
	select {
	case <-ctx.Done():
		n.submitMu.RUnlock()
		return nil, ctx.Err()
	case <-n.ctx.Done():
		n.submitMu.RUnlock()
		return nil, n.ctx.Err()
	case n.requests <- req:
		n.submitMu.RUnlock()
	}
	// NOTE: submit_wait includes backpressure on the shared request queue. A
	// high value is usually provider packing/capacity pressure, not SQLite write
	// pressure.
	phaseCtx := indexingperf.WithPhase(ctx, phase)
	indexingperf.AddCount(phaseCtx, indexingperf.ProviderLogicalTexts, int64(len(texts)))
	indexingperf.ObserveLatency(phaseCtx, indexingperf.EmbedSubmitWait, time.Since(submitStarted))
	indexingperf.ObserveLatency(phaseCtx, indexingperf.EmbedEnqueueWait, time.Since(submitStarted))
	futureStarted := time.Now()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-req.done:
		indexingperf.ObserveLatency(phaseCtx, indexingperf.EmbedFutureWait, time.Since(futureStarted))
		if req.err != nil {
			return nil, req.err
		}
		return req.vecs, nil
	}
}
