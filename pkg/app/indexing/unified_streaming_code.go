package indexing

import (
	"context"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"

	"github.com/atomicobject/rhizome/pkg/embedindex"
)

func (c *unifiedStreamingCoordinator) runCode() {
	defer c.wg.Done()
	if c.codeDone != nil {
		defer close(c.codeDone)
	}
	timer := time.NewTimer(unifiedStreamingBatchIdle)
	defer timer.Stop()
	var pending []codeStreamingBatch
	var prepared []embedindex.PreparedCodePaths
	preparedPaths := make(map[string]struct{})
	var earlyPrepared []embedindex.PreparedCodePaths
	earlyTaskCount := 0
	var earlyQueuedAt time.Time
	didProcess := false
	embedPhaseCtx := indexingperf.WithPhase(c.ctx, "embed_code")
	var embedWG sync.WaitGroup
	var embedMu sync.Mutex
	var embedErr error
	var embedSpanDone func(error)
	startEmbedSpan := func() {
		if embedSpanDone == nil {
			embedSpanDone = indexingperf.StartSpan(embedPhaseCtx, "embed_code")
		}
	}
	setEmbedErr := func(err error) {
		if err == nil {
			return
		}
		embedMu.Lock()
		if embedErr == nil {
			embedErr = err
		}
		embedMu.Unlock()
		c.fail(err)
	}
	scheduleEmbed := func(plan embedindex.SyncPlan) bool {
		if plan.ShouldSkipEmbed() || plan.TaskCount() == 0 {
			return true
		}
		if c.embedCodePlan == nil {
			return true
		}
		didProcess = true
		startEmbedSpan()
		indexingperf.AddCount(embedPhaseCtx, indexingperf.SemanticStreamEmbedPlans, 1)
		indexingperf.AddCount(embedPhaseCtx, indexingperf.SemanticStreamEmbedTasks, int64(plan.TaskCount()))
		indexingperf.AddCount(embedPhaseCtx, indexingperf.SemanticStreamEmbedWork, int64(plan.TotalWork))
		// WHY: embedding plans run concurrently with ongoing ingest and final
		// graph planning, but their writes still flow through the shared writer
		// lane. This is the performance win without adding SQLite write
		// concurrency.
		embedWG.Add(1)
		go func() {
			defer embedWG.Done()
			if err := c.embedCodePlan(embedPhaseCtx, plan); err != nil && c.ctx.Err() == nil {
				setEmbedErr(err)
			}
		}()
		return true
	}
	drainEmbeds := func() error {
		if embedSpanDone == nil {
			return nil
		}
		started := time.Now()
		embedWG.Wait()
		indexingperf.ObserveLatency(embedPhaseCtx, indexingperf.SemanticStreamFinalDrainWait, time.Since(started))
		embedMu.Lock()
		err := embedErr
		embedMu.Unlock()
		return err
	}
	closeEmbedSpan := func() {
		if embedSpanDone == nil {
			return
		}
		embedMu.Lock()
		err := embedErr
		embedMu.Unlock()
		embedSpanDone(err)
		embedSpanDone = nil
	}
	shouldFlushEarly := func(force bool) bool {
		if len(earlyPrepared) == 0 {
			return false
		}
		return shouldFlushEarlyPrepared(earlyTaskCount, earlyQueuedAt, force, time.Now())
	}
	flushEarly := func(force bool) bool {
		if !shouldFlushEarly(force) {
			return true
		}
		// Only the call-insensitive slice of code semantic work is allowed to run
		// before rebuild. Unified full scans keep the actual call-edge rebuild behind
		// the final drain barrier so the expensive reverse-index pass runs once with
		// the complete changed-path set.
		mergedEarly := embedindex.MergePreparedCodePaths(earlyPrepared)
		earlyPrepared = nil
		earlyTaskCount = 0
		earlyQueuedAt = time.Time{}
		if mergedEarly.EarlyTaskCount() == 0 || c.planEarlyCode == nil {
			return true
		}
		didProcess = true
		var earlyPlan embedindex.SyncPlan
		if err := runPhase(c.ctx, "plan_code_embeddings", func(phaseCtx context.Context) error {
			var err error
			earlyPlan, err = c.planEarlyCode(phaseCtx, mergedEarly)
			return err
		}); err != nil && c.ctx.Err() == nil {
			c.fail(err)
			return false
		}
		return scheduleEmbed(earlyPlan)
	}
	flush := func(final bool) bool {
		if len(pending) == 0 {
			return flushEarly(final)
		}
		batch := mergeCodeStreamingBatches(pending)
		pending = pending[:0]

		if len(batch.prePersistWorks) > 0 && c.prepareCodeWorks != nil {
			var preparedBatch embedindex.PreparedCodePaths
			if err := runPhase(c.ctx, "plan_code_embeddings", func(phaseCtx context.Context) error {
				var err error
				preparedBatch, err = c.prepareCodeWorks(phaseCtx, batch.prePersistWorks)
				return err
			}); err != nil && c.ctx.Err() == nil {
				c.fail(err)
				return false
			}
			if !preparedBatch.Empty() {
				didProcess = true
				for _, path := range preparedBatch.Paths() {
					preparedPaths[path] = struct{}{}
				}
				if preparedBatch.EarlyTaskCount() > 0 {
					earlyPrepared = append(earlyPrepared, preparedBatch)
					earlyTaskCount += preparedBatch.EarlyTaskCount()
					if earlyQueuedAt.IsZero() {
						earlyQueuedAt = time.Now()
					}
				}
				if preparedBatch.HasDeferredPaths() {
					prepared = append(prepared, preparedBatch)
				}
			}
		}

		preparePaths := make([]string, 0, len(batch.indexedPaths))
		for _, path := range batch.indexedPaths {
			if _, ok := preparedPaths[path]; ok {
				continue
			}
			preparePaths = append(preparePaths, path)
		}
		if len(preparePaths) > 0 && c.prepareCodePaths != nil {
			var preparedBatch embedindex.PreparedCodePaths
			if err := runPhase(c.ctx, "plan_code_embeddings", func(phaseCtx context.Context) error {
				var err error
				preparedBatch, err = c.prepareCodePaths(phaseCtx, preparePaths)
				return err
			}); err != nil && c.ctx.Err() == nil {
				c.fail(err)
				return false
			}
			if !preparedBatch.Empty() {
				didProcess = true
				if preparedBatch.EarlyTaskCount() > 0 {
					earlyPrepared = append(earlyPrepared, preparedBatch)
					earlyTaskCount += preparedBatch.EarlyTaskCount()
					if earlyQueuedAt.IsZero() {
						earlyQueuedAt = time.Now()
					}
				}
				if preparedBatch.HasDeferredPaths() {
					prepared = append(prepared, preparedBatch)
					for _, path := range preparedBatch.Paths() {
						preparedPaths[path] = struct{}{}
					}
				}
			}
		}
		if !flushEarly(final) {
			return false
		}
		if c.planDeferredCode == nil && c.planPreparedCode == nil {
			return true
		}

		mergedPrepared := embedindex.MergePreparedCodePaths(prepared)
		prepared = nil
		preparedPaths = make(map[string]struct{})
		if mergedPrepared.Empty() {
			return true
		}
		didProcess = true

		// Preparation captures primary chunks before persistence; deferred plans
		// load any structural context available through the shared publication.
		var plan embedindex.SyncPlan
		if err := runPhase(c.ctx, "plan_code_embeddings", func(phaseCtx context.Context) error {
			var err error
			if c.planDeferredCode != nil {
				plan, err = c.planDeferredCode(phaseCtx, mergedPrepared)
			} else {
				plan, err = c.planPreparedCode(phaseCtx, mergedPrepared)
			}
			return err
		}); err != nil && c.ctx.Err() == nil {
			c.fail(err)
			return false
		}
		return scheduleEmbed(plan)
	}
	defer func() {
		defer closeEmbedSpan()
		if err := drainEmbeds(); err != nil && c.ctx.Err() == nil {
			c.fail(err)
			return
		}
		if !didProcess || c.markCodeLastSync == nil || c.ctx.Err() != nil {
			return
		}
		if err := c.markCodeLastSync(c.ctx); err != nil && c.ctx.Err() == nil {
			c.fail(err)
		}
	}()
	for {
		select {
		case <-c.ctx.Done():
			return
		case batch, ok := <-c.codeCh:
			if !ok {
				_ = flush(true)
				return
			}
			pending = append(pending, batch)
			if len(pending) == 1 {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(unifiedStreamingBatchIdle)
			}
		case <-timer.C:
			if !flush(false) {
				return
			}
		}
	}
}
