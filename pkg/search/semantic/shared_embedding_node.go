package semantic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
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
	indexingperf.ObserveLatency(n.ctx, "embed.node_close_drain", time.Since(started))
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
	indexingperf.AddCount(phaseCtx, "provider.logical_texts", int64(len(texts)))
	indexingperf.ObserveLatency(phaseCtx, "embed.submit_wait", time.Since(submitStarted))
	indexingperf.ObserveLatency(phaseCtx, "embed.enqueue_wait", time.Since(submitStarted))
	futureStarted := time.Now()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-req.done:
		indexingperf.ObserveLatency(phaseCtx, "embed.future_wait", time.Since(futureStarted))
		if req.err != nil {
			return nil, req.err
		}
		return req.vecs, nil
	}
}

func (n *SharedEmbeddingNode) run() {
	defer n.wg.Done()
	var (
		pending []sharedEmbeddingJob
		bytes   int
		active  int
		timer   *time.Timer
		timerCh <-chan time.Time
		wg      sync.WaitGroup

		adaptiveWarmups int
	)
	// The run loop's active count tracks scheduled batches, including batches
	// waiting for the shared gate. Provider inflight instead tracks only calls
	// that have reached EmbedTexts, so it must be maintained by the dispatch
	// goroutines themselves.
	metricsCtx := indexingperf.WithCollector(context.Background(), indexingperf.FromContext(n.ctx))
	var providerMetrics struct {
		sync.Mutex
		activeByPhase map[string]int
		active        int
	}
	providerMetrics.activeByPhase = make(map[string]int)
	setProviderInflight := func(phase string, delta int) {
		providerMetrics.Lock()
		defer providerMetrics.Unlock()

		providerMetrics.active += delta
		indexingperf.SetGauge(metricsCtx, "provider.inflight", int64(providerMetrics.active))
		if phase == "" {
			return
		}
		providerMetrics.activeByPhase[phase] += delta
		indexingperf.SetGauge(indexingperf.WithPhase(metricsCtx, phase), "provider.inflight", int64(providerMetrics.activeByPhase[phase]))
	}
	resetProviderInflight := func() {
		providerMetrics.Lock()
		defer providerMetrics.Unlock()

		providerMetrics.active = 0
		indexingperf.SetGauge(metricsCtx, "provider.inflight", 0)
		for phase := range providerMetrics.activeByPhase {
			providerMetrics.activeByPhase[phase] = 0
			indexingperf.SetGauge(indexingperf.WithPhase(metricsCtx, phase), "provider.inflight", 0)
		}
	}
	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer = nil
		timerCh = nil
	}
	startTimer := func() {
		if timer != nil || len(pending) == 0 {
			return
		}
		timer = time.NewTimer(n.config().opts.MaxWait)
		timerCh = timer.C
	}
	oldestAge := func() time.Duration {
		if len(pending) == 0 {
			return 0
		}
		age := time.Since(pending[0].enqueuedAt)
		if age < 0 {
			return 0
		}
		return age
	}
	decision := func(draining bool) adaptiveDispatchDecision {
		cfg := n.config()
		return adaptiveDispatch(adaptiveDispatchInput{
			Opts:          cfg.opts,
			Pending:       len(pending),
			PendingBytes:  bytes,
			Active:        active,
			MaxConcurrent: cfg.maxConcurrent,
			Warmups:       adaptiveWarmups,
			OldestAge:     oldestAge(),
			Draining:      draining,
		})
	}
	ready := func() bool {
		return decision(false).Ready
	}
	prepareDispatchDecision := func(draining bool) adaptiveDispatchDecision {
		dec := decision(draining)
		if len(pending) >= n.config().opts.MaxTexts {
			dec.Warmup = false
		}
		return dec
	}
	phaseCtxForPending := func() context.Context {
		if len(pending) == 0 {
			return n.ctx
		}
		phase := pending[0].req.phase
		if phase == "" {
			return n.ctx
		}
		for _, job := range pending[1:] {
			if job.req.phase != phase {
				return n.ctx
			}
		}
		return indexingperf.WithPhase(n.ctx, phase)
	}
	var waitMetric string
	var waitCtx context.Context
	var waitStarted time.Time
	setWait := func(metric string) {
		if metric == waitMetric {
			return
		}
		if waitMetric != "" {
			indexingperf.ObserveInterval(waitCtx, waitMetric, waitStarted, time.Since(waitStarted))
		}
		waitMetric = metric
		if waitMetric != "" {
			waitCtx = phaseCtxForPending()
			waitStarted = time.Now()
		}
	}
	take := func(limit int) []sharedEmbeddingJob {
		if len(pending) == 0 {
			return nil
		}
		opts := n.config().opts
		if limit <= 0 || limit > opts.MaxTexts {
			limit = opts.MaxTexts
		}
		count := 0
		totalBytes := 0
		for count < len(pending) && count < limit {
			nextBytes := totalBytes + pending[count].bytes
			if count > 0 && nextBytes > opts.MaxBytes {
				break
			}
			totalBytes = nextBytes
			count++
		}
		if count == 0 {
			count = 1
			totalBytes = pending[0].bytes
		}
		out := append([]sharedEmbeddingJob(nil), pending[:count]...)
		pending = pending[count:]
		bytes -= totalBytes
		if bytes < 0 {
			bytes = 0
		}
		indexingperf.SetGauge(n.ctx, "embed.ready_depth", int64(len(pending)))
		return out
	}
	batchPhase := func(jobs []sharedEmbeddingJob) string {
		if len(jobs) == 0 {
			return ""
		}
		phase := jobs[0].req.phase
		if phase == "" {
			return ""
		}
		for _, job := range jobs[1:] {
			if job.req.phase != phase {
				// A shared provider call has one physical request even when it
				// carries multiple domain batches. Keep that request in a separate
				// phase so domain-attributed counters are not mistaken for a sum of
				// distinct HTTP calls.
				return sharedProviderPhase
			}
		}
		return phase
	}
	dispatch := func(jobs []sharedEmbeddingJob, dec adaptiveDispatchDecision) {
		if len(jobs) == 0 {
			return
		}
		phase := batchPhase(jobs)
		if dec.Warmup {
			adaptiveWarmups++
			indexingperf.AddCount(n.ctx, "provider.adaptive_warmup_calls", 1)
		}
		active++
		wg.Add(1)
		go func(phase string) {
			defer wg.Done()
			texts := make([]string, 0, len(jobs))
			jobTextIndexes := make([]int, 0, len(jobs))
			seenTexts := make(map[string]int, len(jobs))
			totalBytes := 0
			requestBytes := 0
			phases := map[string]struct{}{}
			phaseInputTexts := make(map[string]int)
			started := time.Now()
			for _, job := range jobs {
				if idx, ok := seenTexts[job.text]; ok {
					jobTextIndexes = append(jobTextIndexes, idx)
					// IMPORTANT: dedupe is intra-provider-batch only. Durable
					// hash reuse still belongs to the code/note planners because
					// it can avoid provider submission entirely.
					indexingperf.AddCount(indexingperf.WithPhase(n.ctx, job.req.phase), "embed.dedupe.saved", 1)
					indexingperf.AddCount(indexingperf.WithPhase(n.ctx, job.req.phase), "embed.dedupe.saved_bytes", int64(job.bytes))
				} else {
					idx := len(texts)
					seenTexts[job.text] = idx
					jobTextIndexes = append(jobTextIndexes, idx)
					texts = append(texts, job.text)
					requestBytes += job.bytes
				}
				totalBytes += job.bytes
				phases[job.req.phase] = struct{}{}
				phaseInputTexts[job.req.phase]++
				indexingperf.ObserveLatency(indexingperf.WithPhase(n.ctx, job.req.phase), "embed.queue_age", started.Sub(job.enqueuedAt))
			}
			batchCtx := n.ctx
			if phase != "" {
				batchCtx = indexingperf.WithPhase(n.ctx, phase)
			}
			if cap := EffectiveBatchSize(n.provider, 0); cap > 0 {
				indexingperf.ObserveSample(batchCtx, "provider.cap_texts", int64(cap))
			}
			cfg := n.config()
			if cfg.opts.MinTexts > 0 {
				indexingperf.ObserveSample(batchCtx, "provider.dispatch_floor_texts", int64(cfg.opts.MinTexts))
			}
			if dec.Threshold > 0 {
				indexingperf.ObserveSample(batchCtx, "provider.dispatch_threshold_texts", int64(dec.Threshold))
			}
			indexingperf.ObserveSample(batchCtx, "provider.capacity", int64(cfg.maxConcurrent))
			indexingperf.ObserveSample(batchCtx, "provider.batch_texts", int64(len(texts)))
			indexingperf.ObserveSample(batchCtx, "provider.batch_bytes", int64(requestBytes))
			indexingperf.ObserveSample(batchCtx, "provider.input_texts", int64(len(jobs)))
			indexingperf.ObserveSample(batchCtx, "provider.input_bytes", int64(totalBytes))
			if cfg.opts.MaxTexts > 0 {
				indexingperf.ObserveSample(batchCtx, "provider.batch_fill_ratio", int64(len(texts)*100/cfg.opts.MaxTexts))
			}
			if cfg.opts.MaxBytes > 0 {
				indexingperf.ObserveSample(batchCtx, "provider.bytes_fill_ratio", int64(requestBytes*100/cfg.opts.MaxBytes))
			}
			indexingperf.ObserveSample(batchCtx, "provider.batch_domains", int64(len(phases)))
			if len(phases) > 1 {
				indexingperf.AddCount(batchCtx, "provider.mixed_batches", 1)
				for phase := range phases {
					phaseCtx := indexingperf.WithPhase(n.ctx, phase)
					indexingperf.AddCount(phaseCtx, "provider.mixed_batches", 1)
					indexingperf.AddCount(phaseCtx, "provider.calls", 1)
					indexingperf.AddCount(phaseCtx, "provider.texts", int64(phaseInputTexts[phase]))
					if cap := EffectiveBatchSize(n.provider, 0); cap > 0 {
						indexingperf.ObserveSample(phaseCtx, "provider.cap_texts", int64(cap))
					}
					if cfg.opts.MinTexts > 0 {
						indexingperf.ObserveSample(phaseCtx, "provider.dispatch_floor_texts", int64(cfg.opts.MinTexts))
					}
				}
			}
			gateStarted := time.Now()
			if cfg.gate != nil {
				select {
				case <-n.ctx.Done():
					select {
					case n.results <- sharedEmbeddingBatchResult{jobs: jobs, err: n.ctx.Err()}:
					case <-n.ctx.Done():
					}
					return
				case cfg.gate <- struct{}{}:
				}
			}
			indexingperf.ObserveLatency(batchCtx, "embed.gate_wait", time.Since(gateStarted))
			indexingperf.AddCount(batchCtx, "provider.calls", 1)
			indexingperf.AddCount(batchCtx, "provider.texts", int64(len(texts)))
			callStarted := time.Now()
			setProviderInflight(phase, 1)
			vecs, err := n.provider.EmbedTexts(batchCtx, texts)
			setProviderInflight(phase, -1)
			callDur := time.Since(callStarted)
			indexingperf.ObserveLatency(batchCtx, "provider.latency", callDur)
			indexingperf.ObserveInterval(batchCtx, "provider.latency", callStarted, callDur)
			if cfg.gate != nil {
				<-cfg.gate
			}
			if err == nil && len(vecs) != len(texts) {
				err = fmt.Errorf("embed: unexpected vector count %d for %d texts", len(vecs), len(texts))
			}
			if err == nil && len(texts) != len(jobs) {
				expanded := make([]embeddings.Embedding, len(jobs))
				for i, textIdx := range jobTextIndexes {
					expanded[i] = vecs[textIdx]
				}
				vecs = expanded
			}
			select {
			case n.results <- sharedEmbeddingBatchResult{jobs: jobs, vecs: vecs, err: err}:
			case <-n.ctx.Done():
			}
		}(phase)
	}
	dispatchReady := func(force bool) {
		draining := force
		for active < n.config().maxConcurrent && len(pending) > 0 {
			if !force && !ready() {
				return
			}
			setWait("")
			dec := prepareDispatchDecision(draining)
			dispatch(take(dec.BatchTexts), dec)
			if !draining {
				force = false
			}
		}
	}
	complete := func(req *sharedEmbeddingRequest, err error) {
		if req.err == nil && err != nil {
			req.err = err
		}
		req.left--
		if req.left <= 0 {
			close(req.done)
		}
	}
	defer func() {
		setWait("")
		stopTimer()
		for _, job := range pending {
			complete(job.req, n.ctx.Err())
		}
		wg.Wait()
		indexingperf.SetGauge(n.ctx, "embed.ready_depth", 0)
		resetProviderInflight()
	}()
	for {
		if len(pending) > 0 && ready() {
			stopTimer()
			dispatchReady(false)
			if len(pending) > 0 {
				startTimer()
			}
		}
		switch {
		case len(pending) == 0:
			setWait("")
		case active >= n.config().maxConcurrent:
			setWait("embed.node_slots_wait")
		case !ready():
			setWait("embed.node_pack_wait")
		default:
			setWait("")
		}
		if n.requests == nil && len(pending) == 0 && active == 0 {
			return
		}
		select {
		case <-n.ctx.Done():
			return
		case req, ok := <-n.requests:
			if !ok {
				n.requests = nil
				stopTimer()
				dispatchReady(true)
				continue
			}
			for i, text := range req.texts {
				pending = append(pending, sharedEmbeddingJob{req: req, index: i, text: text, bytes: len(text), enqueuedAt: time.Now()})
				bytes += len(text)
			}
			indexingperf.SetGauge(n.ctx, "embed.ready_depth", int64(len(pending)))
			startTimer()
			dispatchReady(false)
		case res := <-n.results:
			active--
			for i, job := range res.jobs {
				if res.err == nil && i < len(res.vecs) {
					job.req.vecs[job.index] = res.vecs[i]
				}
				complete(job.req, res.err)
			}
			dispatchReady(false)
			if len(pending) > 0 {
				startTimer()
			}
		case <-timerCh:
			stopTimer()
			dispatchReady(true)
			if len(pending) > 0 {
				startTimer()
			}
		}
	}
}
