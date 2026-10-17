package embedindex

import (
	"context"

	"fmt"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

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
		indexingperf.SetGauge(metricsCtx, indexingperf.ProviderInflight, int64(providerMetrics.active))
		if phase == "" {
			return
		}
		providerMetrics.activeByPhase[phase] += delta
		indexingperf.SetGauge(indexingperf.WithPhase(metricsCtx, phase), indexingperf.ProviderInflight, int64(providerMetrics.activeByPhase[phase]))
	}
	resetProviderInflight := func() {
		providerMetrics.Lock()
		defer providerMetrics.Unlock()

		providerMetrics.active = 0
		indexingperf.SetGauge(metricsCtx, indexingperf.ProviderInflight, 0)
		for phase := range providerMetrics.activeByPhase {
			providerMetrics.activeByPhase[phase] = 0
			indexingperf.SetGauge(indexingperf.WithPhase(metricsCtx, phase), indexingperf.ProviderInflight, 0)
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
	var waitMetric indexingperf.MetricName
	var waitCtx context.Context
	var waitStarted time.Time
	setWait := func(metric indexingperf.MetricName) {
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
		indexingperf.SetGauge(n.ctx, indexingperf.EmbedReadyDepth, int64(len(pending)))
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
			indexingperf.AddCount(n.ctx, indexingperf.ProviderAdaptiveWarmupCalls, 1)
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
					indexingperf.AddCount(indexingperf.WithPhase(n.ctx, job.req.phase), indexingperf.EmbedDedupeSaved, 1)
					indexingperf.AddCount(indexingperf.WithPhase(n.ctx, job.req.phase), indexingperf.EmbedDedupeSavedBytes, int64(job.bytes))
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
				indexingperf.ObserveLatency(indexingperf.WithPhase(n.ctx, job.req.phase), indexingperf.EmbedQueueAge, started.Sub(job.enqueuedAt))
			}
			batchCtx := n.ctx
			if phase != "" {
				batchCtx = indexingperf.WithPhase(n.ctx, phase)
			}
			if cap := EffectiveBatchSize(n.provider, 0); cap > 0 {
				indexingperf.ObserveSample(batchCtx, indexingperf.ProviderCapTexts, int64(cap))
			}
			cfg := n.config()
			if cfg.opts.MinTexts > 0 {
				indexingperf.ObserveSample(batchCtx, indexingperf.ProviderDispatchFloorTexts, int64(cfg.opts.MinTexts))
			}
			if dec.Threshold > 0 {
				indexingperf.ObserveSample(batchCtx, indexingperf.ProviderDispatchThresholdTexts, int64(dec.Threshold))
			}
			indexingperf.ObserveSample(batchCtx, indexingperf.ProviderCapacity, int64(cfg.maxConcurrent))
			indexingperf.ObserveSample(batchCtx, indexingperf.ProviderBatchTexts, int64(len(texts)))
			indexingperf.ObserveSample(batchCtx, indexingperf.ProviderBatchBytes, int64(requestBytes))
			indexingperf.ObserveSample(batchCtx, indexingperf.ProviderInputTexts, int64(len(jobs)))
			indexingperf.ObserveSample(batchCtx, indexingperf.ProviderInputBytes, int64(totalBytes))
			if cfg.opts.MaxTexts > 0 {
				indexingperf.ObserveSample(batchCtx, indexingperf.ProviderBatchFillRatio, int64(len(texts)*100/cfg.opts.MaxTexts))
			}
			if cfg.opts.MaxBytes > 0 {
				indexingperf.ObserveSample(batchCtx, indexingperf.ProviderBytesFillRatio, int64(requestBytes*100/cfg.opts.MaxBytes))
			}
			indexingperf.ObserveSample(batchCtx, indexingperf.ProviderBatchDomains, int64(len(phases)))
			if len(phases) > 1 {
				indexingperf.AddCount(batchCtx, indexingperf.ProviderMixedBatches, 1)
				for phase := range phases {
					phaseCtx := indexingperf.WithPhase(n.ctx, phase)
					indexingperf.AddCount(phaseCtx, indexingperf.ProviderMixedBatches, 1)
					indexingperf.AddCount(phaseCtx, indexingperf.ProviderCalls, 1)
					indexingperf.AddCount(phaseCtx, indexingperf.ProviderTexts, int64(phaseInputTexts[phase]))
					if cap := EffectiveBatchSize(n.provider, 0); cap > 0 {
						indexingperf.ObserveSample(phaseCtx, indexingperf.ProviderCapTexts, int64(cap))
					}
					if cfg.opts.MinTexts > 0 {
						indexingperf.ObserveSample(phaseCtx, indexingperf.ProviderDispatchFloorTexts, int64(cfg.opts.MinTexts))
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
			indexingperf.ObserveLatency(batchCtx, indexingperf.EmbedGateWait, time.Since(gateStarted))
			indexingperf.AddCount(batchCtx, indexingperf.ProviderCalls, 1)
			indexingperf.AddCount(batchCtx, indexingperf.ProviderTexts, int64(len(texts)))
			callStarted := time.Now()
			setProviderInflight(phase, 1)
			vecs, err := n.provider.EmbedTexts(batchCtx, texts)
			setProviderInflight(phase, -1)
			callDur := time.Since(callStarted)
			indexingperf.ObserveLatency(batchCtx, indexingperf.ProviderLatency, callDur)
			indexingperf.ObserveInterval(batchCtx, indexingperf.ProviderLatency, callStarted, callDur)
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
		indexingperf.SetGauge(n.ctx, indexingperf.EmbedReadyDepth, 0)
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
			setWait(indexingperf.EmbedNodeSlotsWait)
		case !ready():
			setWait(indexingperf.EmbedNodePackWait)
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
			indexingperf.SetGauge(n.ctx, indexingperf.EmbedReadyDepth, int64(len(pending)))
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
