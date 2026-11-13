package embedindex

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func (p *embedPipelineCore[K, Prepared, Owner, Job]) run() (runErr error) {
	var (
		incomplete   = map[K]Owner{}
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
		slotBucket   = indexingperf.ProviderSlots0
		slotSince    = time.Now()

		adaptiveWarmups int
	)

	recordReadyInterval := func(next string, now time.Time) {
		if !readySince.IsZero() && now.After(readySince) {
			indexingperf.ObserveInterval(p.ctx, indexingperf.EmbeddingReadyState.Select(readyState), readySince, now.Sub(readySince))
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
	providerSlotBucket := func(active int) indexingperf.MetricName {
		switch {
		case active <= 0:
			return indexingperf.ProviderSlots0
		case active <= 2:
			return indexingperf.ProviderSlots12
		case active <= 6:
			return indexingperf.ProviderSlots36
		case active <= 12:
			return indexingperf.ProviderSlots712
		default:
			return indexingperf.ProviderSlots13Plus
		}
	}
	recordSlotInterval := func(next indexingperf.MetricName, now time.Time) {
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
		indexingperf.SetGauge(p.ctx, indexingperf.EmbedReadyDepth, int64(len(pending)))
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
			indexingperf.AddCount(p.ctx, indexingperf.ProviderAdaptiveWarmupCalls, 1)
		}
		active++
		updateSlotBucket(time.Now())
		if !useSharedNode {
			indexingperf.SetGauge(p.ctx, indexingperf.ProviderInflight, int64(active))
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
				indexingperf.ObserveLatency(p.ctx, indexingperf.EmbedQueueAge, age)
				if age > oldestAge {
					oldestAge = age
				}
			}
			indexingperf.ObserveLatency(p.ctx, indexingperf.EmbedBatchOldestAge, oldestAge)

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
				indexingperf.ObserveLatency(p.ctx, indexingperf.EmbedGateWait, time.Since(gateStarted))
				if dec.Threshold > 0 {
					indexingperf.ObserveSample(p.ctx, indexingperf.ProviderDispatchThresholdTexts, int64(dec.Threshold))
				}
				indexingperf.ObserveSample(p.ctx, indexingperf.ProviderBatchTexts, int64(len(batchTexts)))
				indexingperf.ObserveSample(p.ctx, indexingperf.ProviderBatchBytes, int64(totalBytes))
				indexingperf.AddCount(p.ctx, indexingperf.ProviderCalls, 1)
				indexingperf.AddCount(p.ctx, indexingperf.ProviderTexts, int64(len(batchTexts)))
				indexingperf.AddBytes(p.ctx, indexingperf.ProviderTextBytes, int64(totalBytes))
			}

			var vecs []embeddings.Embedding
			var err error
			if useSharedNode {
				// A run that can cache unfinalized owners keeps in-flight results, so a
				// cancelled pass does not discard vectors the provider already returned.
				vecs, err = p.sharedNode.submit(p.ctx, indexingperf.PhaseFromContext(p.ctx), batchTexts, p.abandon != nil)
			} else {
				providerCtx := p.ctx
				if p.abandon != nil {
					providerCtx = context.WithoutCancel(p.ctx)
				}
				if err = p.ctx.Err(); err == nil {
					vecs, err = p.provider.EmbedTexts(providerCtx, batchTexts)
				}
				if err != nil {
					vecs = nil
				}
			}
			callDur := time.Since(callStarted)
			if !useSharedNode {
				indexingperf.ObserveLatency(p.ctx, indexingperf.ProviderLatency, callDur)
				indexingperf.ObserveInterval(p.ctx, indexingperf.ProviderLatency, callStarted, callDur)
			}
			if err != nil {
				err = fmt.Errorf("embed provider batch texts=%d bytes=%d: %w", len(batchTexts), totalBytes, err)
			}
			if err == nil && len(vecs) != len(batchTexts) {
				err = fmt.Errorf("embed: unexpected vector count %d for %d texts", len(vecs), len(batchTexts))
				vecs = nil
			}
			if !useSharedNode && p.globalSem != nil {
				<-p.globalSem
			}
			p.resultCh <- embedPipelineBatchResult[Owner, Job]{jobs: jobs, vecs: vecs, err: err}
		}(jobs)
	}
	dispatchReady := func(force bool) {
		if p.ctx.Err() != nil {
			return
		}
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
	// Finalizers share the provider concurrency bound. A full set blocks the
	// loop, so a slow sink backpressures dispatch instead of piling up owners.
	finalizeSlots := make(chan struct{}, p.maxConcurrent)
	sendComplete := func(key K, owner Owner) {
		select {
		case finalizeSlots <- struct{}{}:
		case <-p.ctx.Done():
			finalizeMu.Lock()
			if finalizeErr == nil {
				finalizeErr = p.ctx.Err()
			}
			finalizeMu.Unlock()
			return
		}
		delete(incomplete, key)
		finalizeWG.Add(1)
		go func(owner Owner) {
			defer finalizeWG.Done()
			defer func() { <-finalizeSlots }()
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
		key, owner, jobs := p.acceptPrepared(prepared)
		indexingperf.AddCount(p.ctx, indexingperf.NodeEmbedIn, int64(len(jobs)))
		owner.pipelineSetPending(len(jobs))
		for _, job := range jobs {
			pending = append(pending, queuedEmbedJob[Owner, Job]{owner: owner, job: job, enqueuedAt: time.Now()})
			pendingBytes += job.pipelineBytes()
		}
		setReadyDepth()
		incomplete[key] = owner
		if owner.pipelinePending() == 0 {
			sendComplete(key, owner)
		}
	}

	defer func() {
		stopTimer()
		dispatchWG.Wait()
		finalizeWG.Wait()
		if runErr != nil && p.abandon != nil {
			// Batches in flight when the run failed have all reported now.
			for drained := false; !drained; {
				select {
				case res := <-p.resultCh:
					for idx, qjob := range res.jobs {
						if idx < len(res.vecs) && len(res.vecs[idx]) > 0 {
							_ = p.applyResult(qjob.owner, qjob.job, res.vecs[idx])
						}
					}
				default:
					drained = true
				}
			}
			if len(incomplete) > 0 {
				p.abandon(slices.Collect(maps.Values(incomplete)))
			}
		}
		now := time.Now()
		recordReadyInterval(readyState, now)
		recordSlotInterval(slotBucket, now)
		indexingperf.SetGauge(p.ctx, indexingperf.EmbedReadyDepth, 0)
		if p.sharedNode == nil {
			indexingperf.SetGauge(p.ctx, indexingperf.ProviderInflight, 0)
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

		var waitMetric indexingperf.MetricName
		waitStarted := time.Now()
		switch {
		case preparedOpen && (len(pending) >= p.readyMaxTexts || pendingBytes >= p.readyMaxBytes):
			waitMetric = indexingperf.NodeEmbedReadyCapWait
		case len(pending) > 0 && active >= p.maxConcurrent:
			waitMetric = indexingperf.NodeEmbedProviderSlotsWait
		case len(pending) == 0 && preparedOpen && active < p.maxConcurrent:
			waitMetric = indexingperf.NodeEmbedStarved
		case len(pending) > 0 && active < p.maxConcurrent && !readyForImmediateDispatch():
			waitMetric = indexingperf.NodeEmbedPackWait
		}

		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case prepared, ok := <-preparedInput:
			if waitMetric != "" {
				indexingperf.ObserveLatency(p.ctx, waitMetric, time.Since(waitStarted))
				if waitMetric == "node.embed.pack_wait" {
					indexingperf.ObserveLatency(p.ctx, indexingperf.NodeEmbedFreeSlotsUnderMintexts, time.Since(waitStarted))
				}
				indexingperf.ObserveLatency(p.ctx, indexingperf.ProviderDispatchIdle, time.Since(waitStarted))
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
					indexingperf.ObserveLatency(p.ctx, indexingperf.NodeEmbedFreeSlotsUnderMintexts, time.Since(waitStarted))
				}
				indexingperf.ObserveLatency(p.ctx, indexingperf.ProviderDispatchIdle, time.Since(waitStarted))
			}
			active--
			updateSlotBucket(time.Now())
			if p.sharedNode == nil {
				indexingperf.SetGauge(p.ctx, indexingperf.ProviderInflight, int64(active))
			}
			if res.err != nil {
				if p.abandon != nil {
					for i, job := range res.jobs {
						if i < len(res.vecs) && len(res.vecs[i]) > 0 {
							_ = p.applyResult(job.owner, job.job, res.vecs[i])
						}
					}
				}
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
					sendComplete(job.pipelineOwnerKey(), owner)
				}
			}
			indexingperf.AddCount(p.ctx, indexingperf.NodeEmbedOut, int64(len(res.jobs)))
			dispatchReady(false)
			if len(pending) > 0 {
				startTimer()
			}
		case <-timerCh:
			if waitMetric != "" {
				indexingperf.ObserveLatency(p.ctx, waitMetric, time.Since(waitStarted))
				if waitMetric == "node.embed.pack_wait" {
					indexingperf.ObserveLatency(p.ctx, indexingperf.NodeEmbedFreeSlotsUnderMintexts, time.Since(waitStarted))
				}
				indexingperf.ObserveLatency(p.ctx, indexingperf.ProviderDispatchIdle, time.Since(waitStarted))
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
	if p.ctx.Err() != nil {
		return p.ctx.Err()
	}
	return finalizeErr
}
