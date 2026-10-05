package semantic

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

type embedPipelineOwnerState interface {
	pipelineReadyAt() time.Time
	pipelinePending() int
	pipelineSetPending(int)
}

type embedPipelineJobState[K comparable] interface {
	pipelineOwnerKey() K
	pipelineText() string
	pipelineBytes() int
}

type embedPipelineBatchResult[Owner any, Job any] struct {
	jobs []queuedEmbedJob[Owner, Job]
	vecs []embeddings.Embedding
	err  error
}

type queuedEmbedJob[Owner any, Job any] struct {
	owner      Owner
	job        Job
	enqueuedAt time.Time
}

type embedPipelineCore[K comparable, Prepared any, Owner embedPipelineOwnerState, Job embedPipelineJobState[K]] struct {
	ctx           context.Context
	provider      embeddings.Provider
	sharedNode    *SharedEmbeddingNode
	maxConcurrent int
	globalSem     chan struct{}
	opts          EmbedPackerOptions

	preparedCh chan Prepared
	resultCh   chan embedPipelineBatchResult[Owner, Job]

	readyMaxTexts int
	readyMaxBytes int
	completeStat  string

	acceptPrepared func(Prepared) (K, Owner, []Job)
	applyResult    func(Owner, Job, embeddings.Embedding) error
	finalize       func(Owner) error
}

func newEmbedPipelineCoreWithNode[K comparable, Prepared any, Owner embedPipelineOwnerState, Job embedPipelineJobState[K]](
	ctx context.Context,
	provider embeddings.Provider,
	sharedNode *SharedEmbeddingNode,
	maxConcurrent int,
	globalSem chan struct{},
	opts EmbedPackerOptions,
	preparedCh chan Prepared,
	acceptPrepared func(Prepared) (K, Owner, []Job),
	applyResult func(Owner, Job, embeddings.Embedding) error,
	finalize func(Owner) error,
) *embedPipelineCore[K, Prepared, Owner, Job] {
	if maxConcurrent < 1 {
		maxConcurrent = EffectiveMaxConcurrent(provider, 0)
	}
	opts = normalizeEmbedPackerOptions(opts)
	return &embedPipelineCore[K, Prepared, Owner, Job]{
		ctx:            ctx,
		provider:       provider,
		sharedNode:     sharedNode,
		maxConcurrent:  maxConcurrent,
		globalSem:      globalSem,
		opts:           opts,
		preparedCh:     preparedCh,
		resultCh:       make(chan embedPipelineBatchResult[Owner, Job], maxConcurrent*2),
		readyMaxTexts:  defaultCodePipelineReadyTexts,
		readyMaxBytes:  defaultCodePipelineReadyBytes,
		completeStat:   "codeembed.complete_wait",
		acceptPrepared: acceptPrepared,
		applyResult:    applyResult,
		finalize:       finalize,
	}
}

func (p *embedPipelineCore[K, Prepared, Owner, Job]) run() error {
	var (
		pending      []queuedEmbedJob[Owner, Job]
		pendingBytes int
		active       int
		timer        *time.Timer
		timerCh      <-chan time.Time
		dispatchWG   sync.WaitGroup
		finalizeWG   sync.WaitGroup
		finalizeErr  error
		finalizeMu   sync.Mutex
		preparedOpen = true
		readyState   = "empty"
		readySince   = time.Now()
		slotBucket   = "provider.slots.0"
		slotSince    = time.Now()

		adaptiveWarmups int
	)

	recordReadyInterval := func(next string, now time.Time) {
		if !readySince.IsZero() && now.After(readySince) {
			indexingperf.ObserveInterval(p.ctx, "embed.ready_state."+readyState, readySince, now.Sub(readySince))
		}
		readyState = next
		readySince = now
	}
	updateReadyState := func(now time.Time) {
		next := "empty"
		if len(pending) > 0 {
			next = "nonzero"
		}
		if next != readyState {
			recordReadyInterval(next, now)
		}
	}
	providerSlotBucket := func(active int) string {
		switch {
		case active <= 0:
			return "provider.slots.0"
		case active <= 2:
			return "provider.slots.1_2"
		case active <= 6:
			return "provider.slots.3_6"
		case active <= 12:
			return "provider.slots.7_12"
		default:
			return "provider.slots.13_plus"
		}
	}
	recordSlotInterval := func(next string, now time.Time) {
		if !slotSince.IsZero() && now.After(slotSince) {
			indexingperf.ObserveInterval(p.ctx, slotBucket, slotSince, now.Sub(slotSince))
		}
		slotBucket = next
		slotSince = now
	}
	updateSlotBucket := func(now time.Time) {
		next := providerSlotBucket(active)
		if next != slotBucket {
			recordSlotInterval(next, now)
		}
	}

	setReadyDepth := func() {
		indexingperf.SetGauge(p.ctx, "embed.ready_depth", int64(len(pending)))
		updateReadyState(time.Now())
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
		timer = time.NewTimer(p.opts.MaxWait)
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
		return adaptiveDispatch(adaptiveDispatchInput{
			Opts:          p.opts,
			Pending:       len(pending),
			PendingBytes:  pendingBytes,
			Active:        active,
			MaxConcurrent: p.maxConcurrent,
			Warmups:       adaptiveWarmups,
			OldestAge:     oldestAge(),
			Draining:      draining,
		})
	}
	readyForImmediateDispatch := func() bool {
		return decision(false).Ready
	}
	prepareDispatchDecision := func(draining bool) adaptiveDispatchDecision {
		dec := decision(draining)
		if len(pending) >= p.opts.MaxTexts {
			dec.Warmup = false
		}
		return dec
	}
	takeBatch := func(limit int) []queuedEmbedJob[Owner, Job] {
		if len(pending) == 0 {
			return nil
		}
		if limit <= 0 || limit > p.opts.MaxTexts {
			limit = p.opts.MaxTexts
		}
		n := 0
		bytes := 0
		for n < len(pending) && n < limit {
			nextBytes := bytes + pending[n].job.pipelineBytes()
			if n > 0 && nextBytes > p.opts.MaxBytes {
				break
			}
			bytes = nextBytes
			n++
		}
		if n == 0 {
			n = 1
			bytes = pending[0].job.pipelineBytes()
		}
		out := append([]queuedEmbedJob[Owner, Job](nil), pending[:n]...)
		pending = pending[n:]
		pendingBytes -= bytes
		if pendingBytes < 0 {
			pendingBytes = 0
		}
		setReadyDepth()
		return out
	}
	dispatchBatch := func(jobs []queuedEmbedJob[Owner, Job], dec adaptiveDispatchDecision) {
		if len(jobs) == 0 {
			return
		}
		useSharedNode := p.sharedNode != nil
		if dec.Warmup {
			adaptiveWarmups++
			indexingperf.AddCount(p.ctx, "provider.adaptive_warmup_calls", 1)
		}
		active++
		updateSlotBucket(time.Now())
		if !useSharedNode {
			indexingperf.SetGauge(p.ctx, "provider.inflight", int64(active))
		}
		dispatchWG.Add(1)
		go func(jobs []queuedEmbedJob[Owner, Job]) {
			defer dispatchWG.Done()
			batchTexts := make([]string, 0, len(jobs))
			totalBytes := 0
			callStarted := time.Now()
			oldestAge := time.Duration(0)
			for _, qjob := range jobs {
				job := qjob.job
				batchTexts = append(batchTexts, job.pipelineText())
				totalBytes += job.pipelineBytes()
				age := callStarted.Sub(qjob.enqueuedAt)
				if age < 0 {
					age = 0
				}
				indexingperf.ObserveLatency(p.ctx, "embed.queue_age", age)
				if age > oldestAge {
					oldestAge = age
				}
			}
			indexingperf.ObserveLatency(p.ctx, "embed.batch_oldest_age", oldestAge)

			gateStarted := time.Now()
			if !useSharedNode && p.globalSem != nil {
				select {
				case <-p.ctx.Done():
					p.resultCh <- embedPipelineBatchResult[Owner, Job]{jobs: jobs, err: p.ctx.Err()}
					return
				case p.globalSem <- struct{}{}:
				}
			}
			if !useSharedNode {
				indexingperf.ObserveLatency(p.ctx, "embed.gate_wait", time.Since(gateStarted))
				if dec.Threshold > 0 {
					indexingperf.ObserveSample(p.ctx, "provider.dispatch_threshold_texts", int64(dec.Threshold))
				}
				indexingperf.ObserveSample(p.ctx, "provider.batch_texts", int64(len(batchTexts)))
				indexingperf.ObserveSample(p.ctx, "provider.batch_bytes", int64(totalBytes))
				indexingperf.AddCount(p.ctx, "provider.calls", 1)
				indexingperf.AddCount(p.ctx, "provider.texts", int64(len(batchTexts)))
				indexingperf.AddBytes(p.ctx, "provider.text", int64(totalBytes))
			}

			var vecs []embeddings.Embedding
			var err error
			if useSharedNode {
				vecs, err = p.sharedNode.Submit(p.ctx, indexingperf.PhaseFromContext(p.ctx), batchTexts)
			} else {
				vecs, err = p.provider.EmbedTexts(p.ctx, batchTexts)
			}
			callDur := time.Since(callStarted)
			if !useSharedNode {
				indexingperf.ObserveLatency(p.ctx, "provider.latency", callDur)
				indexingperf.ObserveInterval(p.ctx, "provider.latency", callStarted, callDur)
			}
			if err != nil {
				err = fmt.Errorf("embed provider batch texts=%d bytes=%d: %w", len(batchTexts), totalBytes, err)
			}
			if err == nil && len(vecs) != len(batchTexts) {
				err = fmt.Errorf("embed: unexpected vector count %d for %d texts", len(vecs), len(batchTexts))
			}
			if !useSharedNode && p.globalSem != nil {
				<-p.globalSem
			}
			p.resultCh <- embedPipelineBatchResult[Owner, Job]{jobs: jobs, vecs: vecs, err: err}
		}(jobs)
	}
	dispatchReady := func(force bool) {
		draining := force
		for active < p.maxConcurrent && len(pending) > 0 {
			if !force && !readyForImmediateDispatch() {
				return
			}
			// IMPORTANT: force means "drain now", not "ignore provider limits".
			// It is used at barriers/close to avoid stranded work while still
			// respecting MaxTexts/MaxBytes and maxConcurrent.
			dec := prepareDispatchDecision(draining)
			dispatchBatch(takeBatch(dec.BatchTexts), dec)
			if !draining {
				force = false
			}
		}
	}
	sendComplete := func(owner Owner) {
		finalizeWG.Add(1)
		go func(owner Owner) {
			defer finalizeWG.Done()
			indexingperf.ObserveLatency(p.ctx, p.completeStat, time.Since(owner.pipelineReadyAt()))
			if err := p.finalize(owner); err != nil {
				finalizeMu.Lock()
				if finalizeErr == nil {
					finalizeErr = err
				}
				finalizeMu.Unlock()
			}
		}(owner)
	}
	acceptPrepared := func(prepared Prepared) {
		_, owner, jobs := p.acceptPrepared(prepared)
		indexingperf.AddCount(p.ctx, "node.embed.in", int64(len(jobs)))
		owner.pipelineSetPending(len(jobs))
		for _, job := range jobs {
			pending = append(pending, queuedEmbedJob[Owner, Job]{owner: owner, job: job, enqueuedAt: time.Now()})
			pendingBytes += job.pipelineBytes()
		}
		setReadyDepth()
		if owner.pipelinePending() == 0 {
			sendComplete(owner)
		}
	}

	defer func() {
		stopTimer()
		dispatchWG.Wait()
		finalizeWG.Wait()
		now := time.Now()
		recordReadyInterval(readyState, now)
		recordSlotInterval(slotBucket, now)
		indexingperf.SetGauge(p.ctx, "embed.ready_depth", 0)
		if p.sharedNode == nil {
			indexingperf.SetGauge(p.ctx, "provider.inflight", 0)
		}
	}()

	for {
		if len(pending) > 0 && readyForImmediateDispatch() {
			stopTimer()
			dispatchReady(false)
			if len(pending) > 0 {
				startTimer()
			}
		}

		preparedInput := (<-chan Prepared)(nil)
		if preparedOpen && len(pending) < p.readyMaxTexts && pendingBytes < p.readyMaxBytes {
			preparedInput = p.preparedCh
		}
		if !preparedOpen && len(pending) == 0 && active == 0 {
			break
		}

		waitMetric := ""
		waitStarted := time.Now()
		switch {
		case preparedOpen && (len(pending) >= p.readyMaxTexts || pendingBytes >= p.readyMaxBytes):
			waitMetric = "node.embed.ready_cap_wait"
		case len(pending) > 0 && active >= p.maxConcurrent:
			waitMetric = "node.embed.provider_slots_wait"
		case len(pending) == 0 && preparedOpen && active < p.maxConcurrent:
			waitMetric = "node.embed.starved"
		case len(pending) > 0 && active < p.maxConcurrent && !readyForImmediateDispatch():
			waitMetric = "node.embed.pack_wait"
		}

		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case prepared, ok := <-preparedInput:
			if waitMetric != "" {
				indexingperf.ObserveLatency(p.ctx, waitMetric, time.Since(waitStarted))
				if waitMetric == "node.embed.pack_wait" {
					indexingperf.ObserveLatency(p.ctx, "node.embed.free_slots_under_mintexts", time.Since(waitStarted))
				}
				indexingperf.ObserveLatency(p.ctx, "provider.dispatch_idle", time.Since(waitStarted))
			}
			if !ok {
				preparedOpen = false
				stopTimer()
				dispatchReady(true)
				continue
			}
			acceptPrepared(prepared)
			startTimer()
			dispatchReady(false)
		case res := <-p.resultCh:
			if waitMetric != "" {
				indexingperf.ObserveLatency(p.ctx, waitMetric, time.Since(waitStarted))
				if waitMetric == "node.embed.pack_wait" {
					indexingperf.ObserveLatency(p.ctx, "node.embed.free_slots_under_mintexts", time.Since(waitStarted))
				}
				indexingperf.ObserveLatency(p.ctx, "provider.dispatch_idle", time.Since(waitStarted))
			}
			active--
			updateSlotBucket(time.Now())
			if p.sharedNode == nil {
				indexingperf.SetGauge(p.ctx, "provider.inflight", int64(active))
			}
			if res.err != nil {
				return res.err
			}
			for idx, qjob := range res.jobs {
				job := qjob.job
				owner := qjob.owner
				if idx < len(res.vecs) {
					if err := p.applyResult(owner, job, res.vecs[idx]); err != nil {
						return err
					}
				}
				owner.pipelineSetPending(owner.pipelinePending() - 1)
				if owner.pipelinePending() == 0 {
					sendComplete(owner)
				}
			}
			indexingperf.AddCount(p.ctx, "node.embed.out", int64(len(res.jobs)))
			dispatchReady(false)
			if len(pending) > 0 {
				startTimer()
			}
		case <-timerCh:
			if waitMetric != "" {
				indexingperf.ObserveLatency(p.ctx, waitMetric, time.Since(waitStarted))
				if waitMetric == "node.embed.pack_wait" {
					indexingperf.ObserveLatency(p.ctx, "node.embed.free_slots_under_mintexts", time.Since(waitStarted))
				}
				indexingperf.ObserveLatency(p.ctx, "provider.dispatch_idle", time.Since(waitStarted))
			}
			stopTimer()
			dispatchReady(true)
			if len(pending) > 0 {
				startTimer()
			}
		}
	}

	finalizeMu.Lock()
	defer finalizeMu.Unlock()
	return finalizeErr
}
