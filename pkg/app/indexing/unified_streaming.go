package indexing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	codeemb "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

const (
	unifiedStreamingQueueSize = 128
	unifiedStreamingBatchIdle = 75 * time.Millisecond
	// Early code work is intentionally coalesced before it enters the shared
	// embed lane. Very small streamed batches improved overlap but regressed
	// provider packing and created extra writer churn, so the default here
	// favors throughput over “embed immediately after every file”.
	unifiedEarlyCodeTaskFlush    = 768
	unifiedEarlyCodeMaxWait      = 250 * time.Millisecond
	unifiedReverseIndexReadyPoll = 2 * time.Second
)

var errUnifiedStreamingCodeQueueFull = errors.New("semantic code stream queue full")

// shouldFlushEarlyPrepared decides when call-insensitive code embedding work is
// large or old enough to enter the provider lane before final call-edge rebuild.
func shouldFlushEarlyPrepared(taskCount int, queuedAt time.Time, force bool, now time.Time) bool {
	if taskCount <= 0 {
		return false
	}
	if force || taskCount >= unifiedEarlyCodeTaskFlush {
		return true
	}
	if queuedAt.IsZero() {
		return false
	}
	return now.Sub(queuedAt) >= unifiedEarlyCodeMaxWait
}

// codeStreamingBatch is the unit exchanged between code ingest/writeback and
// semantic follow-up work. It deliberately carries both durable footprints and
// prepared semantic work so expensive preparation can happen before the final
// correctness barrier, while call-sensitive structural retrieval waits until call edges are
// rebuilt.
type codeStreamingBatch struct {
	indexedPaths        []string
	changedCallerPaths  []string
	footprints          []codeanchor.DurableRefFootprint
	scopeReady          []codeanchor.ScopeReadyFootprint
	defDeltas           codeanchor.DefDeltas
	noteScopePaths      []string
	forceFullRebuild    bool
	prepareIndexedPaths bool
	prepared            semantic.PreparedCodePaths
	prePersistWorks     []codeanchor.CodeIndexWork
	freshWorks          []codeanchor.CodeIndexWork
}

// noteStreamingBatch carries changed/deleted note paths into the note semantic
// lane. In ontology-ready vaults this means ontology body chunks, not raw
// authored-section embeddings.
type noteStreamingBatch struct {
	changedPaths []string
	deletedPaths []string
	fullSync     bool
}

// unifiedStreamingCoordinator overlaps code semantic preparation/embedding,
// ontology body sync, call-edge rebuild, and scope rebuild while preserving the
// explicit final drains required before sync marks are written.
type unifiedStreamingCoordinator struct {
	ctx            context.Context
	cancel         context.CancelFunc
	progress       ProgressSink
	fallbackReason string
	closing        bool
	started        bool

	mu            sync.Mutex
	err           error
	closeOnce     sync.Once
	seenCodePath  map[string]struct{}
	noteProcessed bool

	codeCh   chan codeStreamingBatch
	noteCh   chan noteStreamingBatch
	codeDone chan struct{}
	wg       sync.WaitGroup

	prepareCodePaths    func(context.Context, []string) (semantic.PreparedCodePaths, error)
	prepareCodeWorks    func(context.Context, []codeanchor.CodeIndexWork) (semantic.PreparedCodePaths, error)
	planEarlyCode       func(context.Context, semantic.PreparedCodePaths) (semantic.SyncPlan, error)
	planPreparedCode    func(context.Context, semantic.PreparedCodePaths) (semantic.SyncPlan, error)
	planDeferredCode    func(context.Context, semantic.PreparedCodePaths) (semantic.SyncPlan, error)
	embedCodePlan       func(context.Context, semantic.SyncPlan) error
	markCodeLastSync    func(context.Context) error
	syncNotePaths       func(context.Context, []string) error
	syncNoteBodyPaths   func(context.Context, []string, []string, bool) error
	markNoteLastSync    func(context.Context) error
	planRebuild         func(context.Context, []string, codeanchor.DefDeltas, codeanchor.CallEdgeResidual, bool) (codeanchor.CallEdgeRebuildPlan, error)
	reverseIndexReady   func(context.Context) (bool, error)
	rebuildCallEdges    func(context.Context, []string) error
	rebuildGoPackages   func(context.Context, []string) error
	rebuildScopeIDs     func(context.Context, []int64) error
	recomputeScopes     func(context.Context) error
	dirtyScopeIDs       func() (bool, []int64)
	scopeAnchorsByPath  func(context.Context, []string) (map[string][]codeanchor.Anchor, error)
	noteRuntimeCode     func([]codeanchor.CodeIndexWork)
	runtimeCodeVersions func([]string) map[string]uint64
	workProgress        *fixedTotalFileProgress
	finalDrain          *finalDrainProgress
	initialScopeAnchors map[string][]codeanchor.Anchor
}

func newUnifiedStreamingCoordinator(
	ctx context.Context,
	cancel context.CancelFunc,
	progress ProgressSink,
	service *codeanchor.Service,
	codeSyncer *semantic.Syncer,
	noteSyncer *semantic.NoteSyncer,
	codeStore *codeemb.Store,
	fallbackReason string,
) *unifiedStreamingCoordinator {
	if fallbackReason != "" {
		indexingperf.AddCount(ctx, "calledge.fallback."+fallbackReason, 1)
		indexEvent(ctx, slog.LevelInfo, "call_edges.fallback", slog.String("reason_code", fallbackReason))
	}
	c := &unifiedStreamingCoordinator{
		ctx:            ctx,
		cancel:         cancel,
		progress:       withProgressSink(progress),
		fallbackReason: strings.TrimSpace(fallbackReason),
		seenCodePath:   make(map[string]struct{}),
		codeCh:         make(chan codeStreamingBatch, unifiedStreamingQueueSize),
		codeDone:       make(chan struct{}),
		reverseIndexReady: func(context.Context) (bool, error) {
			return strings.TrimSpace(fallbackReason) == "", nil
		},
		planRebuild: func(stageCtx context.Context, changedPaths []string, deltas codeanchor.DefDeltas, residual codeanchor.CallEdgeResidual, backfillComplete bool) (codeanchor.CallEdgeRebuildPlan, error) {
			return service.PlanCallEdgeRebuildIncremental(stageCtx, changedPaths, deltas, residual, backfillComplete)
		},
		rebuildCallEdges:    service.RebuildParserCallEdgesForPaths,
		rebuildGoPackages:   service.RebuildGoPackageRelationshipsForPaths,
		rebuildScopeIDs:     service.RebuildAnchorScopesForIDs,
		recomputeScopes:     service.RecomputeAnchorScopes,
		dirtyScopeIDs:       service.DirtyAnchorIDsSnapshot,
		scopeAnchorsByPath:  service.AnchorsByNotePaths,
		noteRuntimeCode:     service.NoteRuntimeCodeIndexWorks,
		runtimeCodeVersions: service.RuntimeCodeSourceVersions,
	}
	if codeSyncer != nil {
		c.prepareCodePaths = codeSyncer.PreparePathsEarly
		c.prepareCodeWorks = codeSyncer.PrepareCodeIndexWorksEarly
		c.planEarlyCode = codeSyncer.PlanPreparedPathsEarly
		c.planPreparedCode = codeSyncer.PlanPreparedPaths
		c.planDeferredCode = codeSyncer.PlanPreparedPathsDeferred
		c.embedCodePlan = codeSyncer.EmbedWithoutSyncMark
		c.markCodeLastSync = codeSyncer.MarkLastSync
	}
	if noteSyncer != nil {
		c.noteCh = make(chan noteStreamingBatch, unifiedStreamingQueueSize)
		c.syncNotePaths = func(stageCtx context.Context, paths []string) error {
			paths = normalizeStringSet(paths)
			if len(paths) == 0 {
				return nil
			}
			var plan semantic.NotePlan
			if err := runPhase(stageCtx, "plan_note_embeddings", func(phaseCtx context.Context) error {
				var err error
				plan, err = noteSyncer.PlanPaths(phaseCtx, paths)
				return err
			}); err != nil {
				return err
			}
			return runPhase(stageCtx, "embed_notes", func(phaseCtx context.Context) error {
				return noteSyncer.EmbedWithoutSyncMark(phaseCtx, plan)
			})
		}
		c.markNoteLastSync = noteSyncer.MarkLastSync
	}
	_ = codeStore
	return c
}

// Start launches workers only after the coordinator's production callbacks
// and progress sinks have been configured. CloseAndWait before Start is a
// supported no-op shutdown path for early RunUnifiedCore failures.
func (c *unifiedStreamingCoordinator) Start() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.started || c.closing {
		c.mu.Unlock()
		return
	}
	c.started = true
	startCode := c.codeCh != nil
	startNotes := c.noteCh != nil
	if startCode {
		c.wg.Add(1)
	}
	if startNotes {
		c.wg.Add(1)
	}
	c.mu.Unlock()
	if startCode {
		go c.runCode()
	}
	if startNotes {
		go c.runNotes()
	}
}

func (c *unifiedStreamingCoordinator) SubmitCodeBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	if c == nil || c.codeCh == nil {
		return nil
	}
	summary := summarizeCodeStreamingBatch(batch)
	if len(summary.indexedPaths) == 0 && len(summary.changedCallerPaths) == 0 && !summary.defDeltas.HasChanges() {
		return nil
	}
	c.mu.Lock()
	for _, path := range summary.indexedPaths {
		c.seenCodePath[path] = struct{}{}
	}
	c.mu.Unlock()
	return c.sendCode(ctx, summary)
}

func (c *unifiedStreamingCoordinator) SubmitPreparedCodeBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	if c == nil || c.codeCh == nil || len(batch) == 0 {
		return nil
	}
	prePersist := append([]codeanchor.CodeIndexWork(nil), batch...)
	return c.sendCode(ctx, codeStreamingBatch{prePersistWorks: prePersist})
}

func (c *unifiedStreamingCoordinator) SubmitCodePaths(ctx context.Context, paths []string) error {
	if c == nil || c.codeCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	c.mu.Lock()
	for _, path := range paths {
		c.seenCodePath[path] = struct{}{}
	}
	c.mu.Unlock()
	return c.sendCode(ctx, codeStreamingBatch{indexedPaths: paths, prepareIndexedPaths: true})
}

func (c *unifiedStreamingCoordinator) SubmitDeletedCodePaths(ctx context.Context, paths []string) error {
	if c == nil || c.codeCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	c.mu.Lock()
	for _, path := range paths {
		c.seenCodePath[path] = struct{}{}
	}
	c.mu.Unlock()
	return c.sendCode(ctx, codeStreamingBatch{indexedPaths: paths})
}

func (c *unifiedStreamingCoordinator) SubmitCodeFullRebuild(ctx context.Context, paths []string) error {
	if c == nil || c.codeCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	c.mu.Lock()
	for _, path := range paths {
		c.seenCodePath[path] = struct{}{}
	}
	c.mu.Unlock()
	return c.sendCode(ctx, codeStreamingBatch{
		indexedPaths:        paths,
		forceFullRebuild:    true,
		prepareIndexedPaths: true,
	})
}

func (c *unifiedStreamingCoordinator) SubmitNotePaths(ctx context.Context, paths []string) error {
	if c == nil || c.noteCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case c.noteCh <- noteStreamingBatch{changedPaths: paths}:
		return nil
	}
}

func (c *unifiedStreamingCoordinator) SubmitDeletedNotePaths(ctx context.Context, paths []string) error {
	if c == nil || c.noteCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case c.noteCh <- noteStreamingBatch{deletedPaths: paths}:
		return nil
	}
}

func (c *unifiedStreamingCoordinator) SubmitNoteFullRebuild(ctx context.Context, paths []string) error {
	if c == nil || c.noteCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case c.noteCh <- noteStreamingBatch{changedPaths: paths, fullSync: true}:
		return nil
	}
}

func (c *unifiedStreamingCoordinator) SubmitScopeNotePaths(ctx context.Context, paths []string) error {
	if c == nil || c.codeCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	return c.sendCode(ctx, codeStreamingBatch{noteScopePaths: paths})
}

func (c *unifiedStreamingCoordinator) TrySubmitScopeNotePaths(ctx context.Context, paths []string) error {
	if c == nil || c.codeCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	return c.trySendCode(ctx, codeStreamingBatch{noteScopePaths: paths})
}

func (c *unifiedStreamingCoordinator) CloseAndWait() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	c.closeOnce.Do(func() {
		if c.codeCh != nil {
			close(c.codeCh)
		}
		if c.noteCh != nil {
			close(c.noteCh)
		}
	})
	c.wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *unifiedStreamingCoordinator) MarkNoteLastSync(ctx context.Context) error {
	if c == nil || c.markNoteLastSync == nil {
		return nil
	}
	c.mu.Lock()
	processed := c.noteProcessed
	failed := c.err != nil
	c.mu.Unlock()
	if !processed || failed || c.ctx.Err() != nil {
		return nil
	}
	return c.markNoteLastSync(ctx)
}

func (c *unifiedStreamingCoordinator) runCode() {
	defer c.wg.Done()
	if c.codeDone != nil {
		defer close(c.codeDone)
	}
	timer := time.NewTimer(unifiedStreamingBatchIdle)
	defer timer.Stop()
	var pending []codeStreamingBatch
	readyIndex := newCallEdgeReadyIndex()
	membership := newCallEdgeMembershipIndex()
	scopeReady := newScopeReadyIndex()
	scopeReady.seed(c.initialScopeAnchors)
	var prepared []semantic.PreparedCodePaths
	preparedPaths := make(map[string]struct{})
	var earlyPrepared []semantic.PreparedCodePaths
	earlyTaskCount := 0
	var earlyQueuedAt time.Time
	var callEdgePending codeStreamingBatch
	lastFreshRebuildVersions := make(map[string]uint64)
	goPackagesRebuilt := false
	didProcess := false
	backfillReady := strings.TrimSpace(c.fallbackReason) == ""
	lastReverseIndexPoll := time.Time{}
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
	scheduleEmbed := func(plan semantic.SyncPlan) bool {
		if plan.ShouldSkipEmbed() || plan.TaskCount() == 0 {
			return true
		}
		if c.embedCodePlan == nil {
			return true
		}
		didProcess = true
		startEmbedSpan()
		indexingperf.AddCount(embedPhaseCtx, "semantic.stream.embed_plans", 1)
		indexingperf.AddCount(embedPhaseCtx, "semantic.stream.embed_tasks", int64(plan.TaskCount()))
		indexingperf.AddCount(embedPhaseCtx, "semantic.stream.embed_work", int64(plan.TotalWork))
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
		indexingperf.ObserveLatency(embedPhaseCtx, "semantic.stream.final_drain_wait", time.Since(started))
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
	callEdgeBatchFor := func(batch codeStreamingBatch) codeStreamingBatch {
		return codeStreamingBatch{
			indexedPaths:       append([]string(nil), batch.indexedPaths...),
			changedCallerPaths: append([]string(nil), batch.changedCallerPaths...),
			footprints:         append([]codeanchor.DurableRefFootprint(nil), batch.footprints...),
			defDeltas:          batch.defDeltas,
			forceFullRebuild:   batch.forceFullRebuild,
		}
	}
	var flushCallEdges func(batch codeStreamingBatch, final bool) bool
	flushGoPackages := func(final bool) bool {
		if !final || goPackagesRebuilt || c.rebuildGoPackages == nil {
			return true
		}
		paths := c.snapshotSeenCodePaths()
		if len(paths) > 0 {
			if err := runPhase(c.ctx, "rebuild_go_package_relationships", func(phaseCtx context.Context) error {
				return c.rebuildGoPackages(phaseCtx, paths)
			}); err != nil && c.ctx.Err() == nil {
				c.fail(err)
				return false
			}
		}
		goPackagesRebuilt = true
		return true
	}
	flushEarly := func(force bool) bool {
		if !shouldFlushEarly(force) {
			return true
		}
		// Only the call-insensitive slice of code semantic work is allowed to run
		// before rebuild. Unified full scans keep the actual call-edge rebuild behind
		// the final drain barrier so the expensive reverse-index pass runs once with
		// the complete changed-path set.
		mergedEarly := semantic.MergePreparedCodePaths(earlyPrepared)
		earlyPrepared = nil
		earlyTaskCount = 0
		earlyQueuedAt = time.Time{}
		if mergedEarly.EarlyTaskCount() == 0 || c.planEarlyCode == nil {
			return true
		}
		didProcess = true
		var earlyPlan semantic.SyncPlan
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
	flushCallEdgePending := func(force bool) bool {
		if callEdgePending.callEdgeWorkCount() == 0 {
			if force {
				return flushCallEdges(codeStreamingBatch{}, true)
			}
			return true
		}
		if !force {
			return true
		}
		batch := callEdgePending
		callEdgePending = codeStreamingBatch{}
		return flushCallEdges(batch, force)
	}
	flushCallEdges = func(batch codeStreamingBatch, final bool) bool {
		if batch.forceFullRebuild {
			rebuildPaths := normalizeStringSet(batch.indexedPaths)
			if len(rebuildPaths) == 0 || c.rebuildCallEdges == nil {
				readyIndex.clearResidual()
				return true
			}
			rebuildPaths = filterRuntimeRebuildPaths(rebuildPaths, c.runtimeCodeVersions, lastFreshRebuildVersions)
			if len(rebuildPaths) == 0 {
				readyIndex.clearResidual()
				return true
			}
			indexingperf.AddCount(c.ctx, "calledge.full_recompute_batches", 1)
			indexingperf.AddCount(c.ctx, "calledge.full_recompute_paths", int64(len(rebuildPaths)))
			indexEvent(c.ctx, slog.LevelInfo, "call_edges.full_recompute", slog.String("reason_code", "forced_code_batch"), slog.Int("paths", len(rebuildPaths)))
			if c.workProgress != nil {
				c.workProgress.AddEdgeWork(rebuildPaths)
			}
			if err := runPhase(c.ctx, "rebuild_call_edges", func(phaseCtx context.Context) error {
				return c.rebuildCallEdges(phaseCtx, rebuildPaths)
			}); err != nil && c.ctx.Err() == nil {
				c.fail(err)
				return false
			}
			if c.workProgress != nil {
				c.workProgress.CompleteEdgeWork(rebuildPaths)
			}
			readyIndex.clearResidual()
			readyIndex.markRebuilt(rebuildPaths)
			return true
		}

		if c.reverseIndexReady != nil && (!backfillReady || final) && (final || lastReverseIndexPoll.IsZero() || time.Since(lastReverseIndexPoll) >= unifiedReverseIndexReadyPoll) {
			ready, err := c.reverseIndexReady(c.ctx)
			if err != nil {
				c.fail(err)
				return false
			}
			backfillReady = ready
			lastReverseIndexPoll = time.Now()
		}
		exactRebuild := backfillReady || final
		if len(batch.changedCallerPaths) == 0 && !batch.defDeltas.HasChanges() && len(batch.footprints) == 0 && !(readyIndex.hasDeferred() && exactRebuild) {
			return true
		}
		readyIndex.enqueueDirect(batch.changedCallerPaths)
		if c.planRebuild != nil && batch.defDeltas.HasChanges() {
			plan, err := c.planRebuild(c.ctx, nil, batch.defDeltas, codeanchor.CallEdgeResidual{}, exactRebuild)
			if err != nil {
				c.fail(err)
				return false
			}
			if !exactRebuild {
				readyIndex.seedResidual(plan.Residual)
			}
			readyIndex.enqueuePlannerReady(plan.Paths)
		}
		membership.upsertFootprints(batch.footprints)
		readyIndex.noteDurableFootprints(batch.footprints)
		if readyIndex.hasDeferred() && exactRebuild {
			if final && c.finalDrain != nil {
				if setter, ok := c.progress.(interface{ SetStage(string) }); ok {
					setter.SetStage("Planning residual call edges")
				}
				c.finalDrain.reset()
			}
			var rebuildPaths []string
			if final {
				rebuildPaths = membership.resolveResidual(readyIndex.residual(), c.finalDrain)
			} else if c.planRebuild != nil {
				planCtx := c.ctx
				if c.finalDrain != nil {
					planCtx = codeanchor.WithCallEdgePlanProgress(planCtx, c.finalDrain)
				}
				plan, err := c.planRebuild(planCtx, nil, codeanchor.DefDeltas{}, readyIndex.residual(), true)
				if err != nil {
					c.fail(err)
					return false
				}
				rebuildPaths = plan.Paths
			}
			readyIndex.clearResidual()
			readyIndex.enqueuePlannerReady(rebuildPaths)
		}
		rebuildPaths := readyIndex.drainReady()
		if len(rebuildPaths) == 0 || c.rebuildCallEdges == nil {
			return true
		}
		rebuildPaths = filterRuntimeRebuildPaths(rebuildPaths, c.runtimeCodeVersions, lastFreshRebuildVersions)
		if len(rebuildPaths) == 0 {
			return true
		}
		metricsCtx := indexingperf.WithPhase(c.ctx, "rebuild_call_edges")
		indexingperf.AddCount(metricsCtx, "calledge.incremental_recompute_batches", 1)
		indexingperf.AddCount(metricsCtx, "calledge.flush.count", 1)
		indexingperf.ObserveSample(metricsCtx, "calledge.flush.paths", int64(len(rebuildPaths)))
		indexingperf.ObserveSample(metricsCtx, "calledge.flush.work", int64(batch.callEdgeWorkCount()))
		rebuildCtx := c.ctx
		if final && c.finalDrain != nil {
			if setter, ok := c.progress.(interface{ SetStage(string) }); ok {
				setter.SetStage("Rebuilding residual call edges")
			}
			rebuildCtx = codeanchor.WithCallEdgeProgress(rebuildCtx, c.finalDrain)
		}
		if c.workProgress != nil {
			c.workProgress.AddEdgeWork(rebuildPaths)
		}
		if err := runPhase(rebuildCtx, "rebuild_call_edges", func(phaseCtx context.Context) error {
			return c.rebuildCallEdges(phaseCtx, rebuildPaths)
		}); err != nil && c.ctx.Err() == nil {
			c.fail(err)
			return false
		}
		if c.workProgress != nil {
			c.workProgress.CompleteEdgeWork(rebuildPaths)
		}
		readyIndex.markRebuilt(rebuildPaths)
		return true
	}
	flushScopes := func(batch codeStreamingBatch, final bool) bool {
		if c.rebuildScopeIDs == nil {
			return true
		}
		scopeReady.noteCodeFootprints(batch.scopeReady)
		if len(batch.noteScopePaths) > 0 && c.scopeAnchorsByPath != nil {
			anchorsByPath, err := c.scopeAnchorsByPath(c.ctx, batch.noteScopePaths)
			if err != nil {
				c.fail(err)
				return false
			}
			for _, path := range normalizeStringSet(batch.noteScopePaths) {
				anchors := anchorsByPath[path]
				scopeReady.replaceNoteAnchors(path, anchors)
				ids := make([]int64, 0, len(anchors))
				for _, anchor := range anchors {
					ids = append(ids, anchor.ID)
				}
				scopeReady.enqueueDirect(ids)
			}
		}
		if final {
			if c.dirtyScopeIDs != nil {
				overflow, dirtyIDs := c.dirtyScopeIDs()
				if scopeReady.hasGlobalFallback() || overflow {
					reason := "global_scope_dependency"
					if overflow {
						reason = "dirty_scope_overflow"
					}
					if c.recomputeScopes == nil {
						c.fail(fmt.Errorf("scope fallback requires full recompute"))
						return false
					}
					indexingperf.AddCount(c.ctx, "scope.full_recompute."+reason, 1)
					indexEvent(c.ctx, slog.LevelInfo, "anchor_scopes.full_recompute", slog.String("reason_code", reason), slog.Bool("overflow", overflow))
					rebuildCtx := c.ctx
					if c.finalDrain != nil {
						if setter, ok := c.progress.(interface{ SetStage(string) }); ok {
							setter.SetStage("Recomputing residual anchor scopes")
						}
						c.finalDrain.reset()
						rebuildCtx = codeanchor.WithScopeProgress(rebuildCtx, c.finalDrain)
					}
					if err := runPhase(rebuildCtx, "rebuild_anchor_scopes", func(phaseCtx context.Context) error {
						return c.recomputeScopes(phaseCtx)
					}); err != nil && c.ctx.Err() == nil {
						c.fail(err)
						return false
					}
					return true
				}
				scopeReady.enqueueDirect(dirtyIDs)
			}
		}
		readyIDs := normalizeScopeAnchorIDs(scopeReady.drainReady())
		if len(readyIDs) == 0 {
			return true
		}
		indexingperf.AddCount(c.ctx, "scope.incremental_recompute_batches", 1)
		indexingperf.AddCount(c.ctx, "scope.incremental_anchor_ids", int64(len(readyIDs)))
		rebuildCtx := c.ctx
		if final && c.finalDrain != nil {
			if setter, ok := c.progress.(interface{ SetStage(string) }); ok {
				setter.SetStage("Rebuilding residual anchor scopes")
			}
			c.finalDrain.reset()
			rebuildCtx = codeanchor.WithScopeProgress(rebuildCtx, c.finalDrain)
		}
		if c.workProgress != nil {
			c.workProgress.AddScopeWork(readyIDs)
		}
		if err := runPhase(rebuildCtx, "rebuild_anchor_scopes", func(phaseCtx context.Context) error {
			return c.rebuildScopeIDs(phaseCtx, readyIDs)
		}); err != nil && c.ctx.Err() == nil {
			c.fail(err)
			return false
		}
		if c.workProgress != nil {
			c.workProgress.CompleteScopeWork(readyIDs)
		}
		scopeReady.markRebuilt(readyIDs)
		return true
	}
	flush := func(final bool) bool {
		if len(pending) == 0 {
			if !flushEarly(final) {
				return false
			}
			if !flushCallEdgePending(final) {
				return false
			}
			if !flushGoPackages(final) {
				return false
			}
			if !final {
				return true
			}
			return flushScopes(codeStreamingBatch{}, true)
		}
		batch := mergeCodeStreamingBatches(pending)
		pending = pending[:0]
		if len(batch.freshWorks) > 0 && c.noteRuntimeCode != nil {
			c.noteRuntimeCode(batch.freshWorks)
		}

		if !batch.prepared.Empty() {
			didProcess = true
			for _, path := range batch.prepared.Paths() {
				preparedPaths[path] = struct{}{}
			}
			if batch.prepared.EarlyTaskCount() > 0 {
				earlyPrepared = append(earlyPrepared, batch.prepared)
				earlyTaskCount += batch.prepared.EarlyTaskCount()
				if earlyQueuedAt.IsZero() {
					earlyQueuedAt = time.Now()
				}
			}
			if batch.prepared.HasDeferredPaths() {
				prepared = append(prepared, batch.prepared)
				for _, path := range batch.prepared.Paths() {
					preparedPaths[path] = struct{}{}
				}
			}
		}

		if len(batch.prePersistWorks) > 0 && c.prepareCodeWorks != nil {
			var preparedBatch semantic.PreparedCodePaths
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
		if batch.prepareIndexedPaths && len(preparePaths) > 0 && c.prepareCodePaths != nil {
			var preparedBatch semantic.PreparedCodePaths
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
		if !flushEarly(batch.forceFullRebuild || final) {
			return false
		}
		callEdgeBatch := callEdgeBatchFor(batch)
		callEdgePending = mergeCodeStreamingBatches([]codeStreamingBatch{callEdgePending, callEdgeBatch})
		if final && !flushCallEdgePending(true) {
			return false
		}
		if !flushGoPackages(final) {
			return false
		}
		if !flushScopes(batch, final) {
			return false
		}
		if c.planDeferredCode == nil && c.planPreparedCode == nil {
			return true
		}

		mergedPrepared := semantic.MergePreparedCodePaths(prepared)
		prepared = nil
		preparedPaths = make(map[string]struct{})
		if mergedPrepared.Empty() {
			return true
		}
		didProcess = true

		// Deferred code work contains call-sensitive or barrier-sensitive chunks.
		// It is planned after the scope/call rebuild opportunities above so
		// embedding does not race ahead of durable graph facts.
		var plan semantic.SyncPlan
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

func (c *unifiedStreamingCoordinator) runNotes() {
	defer c.wg.Done()
	timer := time.NewTimer(unifiedStreamingBatchIdle)
	defer timer.Stop()
	var pending []noteStreamingBatch
	flush := func(force bool) bool {
		if len(pending) == 0 {
			return true
		}
		batch := mergeNoteStreamingBatches(pending)
		pending = pending[:0]
		c.mu.Lock()
		c.noteProcessed = true
		c.mu.Unlock()
		if c.syncNoteBodyPaths != nil {
			// IMPORTANT: typed-note body chunks are the normal semantic surface
			// when ontology is available. Raw note-section sync is only the
			// compatibility path below.
			if err := runPhase(c.ctx, "sync_ontology_body", func(phaseCtx context.Context) error {
				return c.syncNoteBodyPaths(phaseCtx, batch.changedPaths, batch.deletedPaths, batch.fullSync)
			}); err != nil && c.ctx.Err() == nil {
				c.fail(err)
				return false
			}
			return true
		}
		paths := append([]string(nil), batch.changedPaths...)
		paths = append(paths, batch.deletedPaths...)
		paths = normalizeStringSet(paths)
		if c.syncNotePaths != nil {
			if err := runPhase(c.ctx, "embed_notes", func(phaseCtx context.Context) error {
				indexingperf.AddCount(phaseCtx, "note_raw.compat_paths", int64(len(paths)))
				return c.syncNotePaths(phaseCtx, paths)
			}); err != nil && c.ctx.Err() == nil {
				c.fail(err)
				return false
			}
		}
		return true
	}
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.codeDone:
			c.codeDone = nil
			if !flush(false) {
				return
			}
		case batch, ok := <-c.noteCh:
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

func mergeNoteStreamingBatches(batches []noteStreamingBatch) noteStreamingBatch {
	out := noteStreamingBatch{}
	for _, batch := range batches {
		out.changedPaths = append(out.changedPaths, batch.changedPaths...)
		out.deletedPaths = append(out.deletedPaths, batch.deletedPaths...)
		out.fullSync = out.fullSync || batch.fullSync
	}
	out.changedPaths = normalizeStringSet(out.changedPaths)
	out.deletedPaths = normalizeStringSet(out.deletedPaths)
	return out
}

func (c *unifiedStreamingCoordinator) sendCode(ctx context.Context, batch codeStreamingBatch) error {
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case c.codeCh <- batch:
		return nil
	}
}

func (c *unifiedStreamingCoordinator) trySendCode(ctx context.Context, batch codeStreamingBatch) error {
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case c.codeCh <- batch:
		return nil
	default:
		return fmt.Errorf("%w (depth=%d capacity=%d)", errUnifiedStreamingCodeQueueFull, len(c.codeCh), cap(c.codeCh))
	}
}

func (c *unifiedStreamingCoordinator) snapshotSeenCodePaths() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	paths := make([]string, 0, len(c.seenCodePath))
	for path := range c.seenCodePath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (c *unifiedStreamingCoordinator) fail(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

func summarizeCodeStreamingBatch(batch []codeanchor.CodeIndexWork) codeStreamingBatch {
	out := codeStreamingBatch{}
	var deltaAcc codeanchor.DefDeltaAccumulator
	for _, work := range batch {
		path := strings.TrimSpace(work.Path)
		if path != "" {
			out.indexedPaths = append(out.indexedPaths, path)
			if work.ReplaceIndex && work.HasRefSignals {
				out.changedCallerPaths = append(out.changedCallerPaths, path)
			}
		}
		if !work.RefFootprint.Empty() {
			out.footprints = append(out.footprints, work.RefFootprint)
		}
		if work.ScopeReady.Path != "" {
			out.scopeReady = append(out.scopeReady, work.ScopeReady)
		}
		if work.DefDeltas.HasChanges() {
			deltaAcc.Add(work.DefDeltas)
		}
		out.freshWorks = append(out.freshWorks, work)
	}
	out.indexedPaths = normalizeStringSet(out.indexedPaths)
	out.changedCallerPaths = normalizeStringSet(out.changedCallerPaths)
	out.footprints = mergeDurableRefFootprints(out.footprints)
	out.scopeReady = mergeScopeReadyFootprints(out.scopeReady)
	out.defDeltas = deltaAcc.Finalize()
	return out
}

func mergeCodeStreamingBatches(batches []codeStreamingBatch) codeStreamingBatch {
	out := codeStreamingBatch{}
	var prepared []semantic.PreparedCodePaths
	var deltaAcc codeanchor.DefDeltaAccumulator
	for _, batch := range batches {
		out.indexedPaths = append(out.indexedPaths, batch.indexedPaths...)
		out.changedCallerPaths = append(out.changedCallerPaths, batch.changedCallerPaths...)
		out.footprints = append(out.footprints, batch.footprints...)
		out.scopeReady = append(out.scopeReady, batch.scopeReady...)
		deltaAcc.Add(batch.defDeltas)
		out.noteScopePaths = append(out.noteScopePaths, batch.noteScopePaths...)
		out.forceFullRebuild = out.forceFullRebuild || batch.forceFullRebuild
		out.prepareIndexedPaths = out.prepareIndexedPaths || batch.prepareIndexedPaths
		out.prePersistWorks = append(out.prePersistWorks, batch.prePersistWorks...)
		out.freshWorks = append(out.freshWorks, batch.freshWorks...)
		if !batch.prepared.Empty() {
			prepared = append(prepared, batch.prepared)
		}
	}
	out.indexedPaths = normalizeStringSet(out.indexedPaths)
	out.changedCallerPaths = normalizeStringSet(out.changedCallerPaths)
	out.footprints = mergeDurableRefFootprints(out.footprints)
	out.scopeReady = mergeScopeReadyFootprints(out.scopeReady)
	out.noteScopePaths = normalizeStringSet(out.noteScopePaths)
	out.defDeltas = deltaAcc.Finalize()
	out.prepared = semantic.MergePreparedCodePaths(prepared)
	return out
}

func (b codeStreamingBatch) callEdgeWorkCount() int {
	return len(b.changedCallerPaths) +
		len(b.footprints) +
		len(b.defDeltas.AddedSymbols) +
		len(b.defDeltas.RemovedSymbols) +
		len(b.defDeltas.AddedModules) +
		len(b.defDeltas.RemovedModules)
}

func filterRuntimeRebuildPaths(paths []string, current func([]string) map[string]uint64, rebuilt map[string]uint64) []string {
	if len(paths) == 0 || current == nil {
		return paths
	}
	versions := current(paths)
	if len(versions) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		version := versions[path]
		if version == 0 {
			out = append(out, path)
			continue
		}
		if rebuilt[path] >= version {
			continue
		}
		rebuilt[path] = version
		out = append(out, path)
	}
	return out
}

func mergeDurableRefFootprints(values []codeanchor.DurableRefFootprint) []codeanchor.DurableRefFootprint {
	if len(values) == 0 {
		return nil
	}
	type footprintSet struct {
		symbols map[string]struct{}
		modules map[string]struct{}
	}
	merged := make(map[string]footprintSet, len(values))
	order := make([]string, 0, len(values))
	for _, value := range values {
		path := strings.TrimSpace(strings.ReplaceAll(value.Path, "\\", "/"))
		if path == "" {
			continue
		}
		current, ok := merged[path]
		if !ok {
			current = footprintSet{
				symbols: make(map[string]struct{}, len(value.SymbolKeys)),
				modules: make(map[string]struct{}, len(value.Modules)),
			}
			order = append(order, path)
		}
		for _, key := range value.SymbolKeys {
			key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
			if key != "" {
				current.symbols[key] = struct{}{}
			}
		}
		for _, module := range value.Modules {
			module = strings.TrimSpace(strings.ReplaceAll(module, "\\", "/"))
			if module != "" {
				current.modules[module] = struct{}{}
			}
		}
		merged[path] = current
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]codeanchor.DurableRefFootprint, 0, len(order))
	for _, path := range order {
		current := merged[path]
		footprint := codeanchor.DurableRefFootprint{Path: path}
		if len(current.symbols) > 0 {
			footprint.SymbolKeys = make([]string, 0, len(current.symbols))
			for key := range current.symbols {
				footprint.SymbolKeys = append(footprint.SymbolKeys, key)
			}
			sort.Strings(footprint.SymbolKeys)
		}
		if len(current.modules) > 0 {
			footprint.Modules = make([]string, 0, len(current.modules))
			for module := range current.modules {
				footprint.Modules = append(footprint.Modules, module)
			}
			sort.Strings(footprint.Modules)
		}
		out = append(out, footprint)
	}
	return out
}

func mergeScopeReadyFootprints(values []codeanchor.ScopeReadyFootprint) []codeanchor.ScopeReadyFootprint {
	if len(values) == 0 {
		return nil
	}
	type footprintSet struct {
		symbols     map[string]struct{}
		annotations map[string]struct{}
	}
	merged := make(map[string]footprintSet, len(values))
	order := make([]string, 0, len(values))
	for _, value := range values {
		path := strings.TrimSpace(strings.ReplaceAll(value.Path, "\\", "/"))
		if path == "" {
			continue
		}
		current, ok := merged[path]
		if !ok {
			current = footprintSet{
				symbols:     make(map[string]struct{}, len(value.SymbolKeys)),
				annotations: make(map[string]struct{}, len(value.AnnotationKeys)),
			}
			order = append(order, path)
		}
		for _, key := range value.SymbolKeys {
			key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
			if key != "" {
				current.symbols[key] = struct{}{}
			}
		}
		for _, key := range value.AnnotationKeys {
			key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
			if key != "" {
				current.annotations[key] = struct{}{}
			}
		}
		merged[path] = current
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]codeanchor.ScopeReadyFootprint, 0, len(order))
	for _, path := range order {
		current := merged[path]
		footprint := codeanchor.ScopeReadyFootprint{Path: path}
		for key := range current.symbols {
			footprint.SymbolKeys = append(footprint.SymbolKeys, key)
		}
		for key := range current.annotations {
			footprint.AnnotationKeys = append(footprint.AnnotationKeys, key)
		}
		sort.Strings(footprint.SymbolKeys)
		sort.Strings(footprint.AnnotationKeys)
		out = append(out, footprint)
	}
	return out
}

func normalizeStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func staleCodeEmbeddingPaths(ctx context.Context, store *codeemb.Store, keepRel []string) ([]string, error) {
	if store == nil {
		return nil, nil
	}
	items, err := store.ListItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list code embedding items: %w", err)
	}
	keep := make(map[string]struct{}, len(keepRel))
	for _, path := range keepRel {
		path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
		if path != "" {
			keep[path] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	var stale []string
	for _, item := range items {
		path := strings.TrimSpace(strings.ReplaceAll(item.Path, "\\", "/"))
		if path == "" {
			continue
		}
		if _, ok := keep[path]; ok {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		stale = append(stale, path)
	}
	sort.Strings(stale)
	return stale, nil
}

// submitStaleCodeEmbeddingCleanup schedules path-scoped code embedding cleanup
// from the complete ownership result. It intentionally runs even when the
// current full index has no code roots: an empty keep set means every stored
// code embedding path is stale. The streaming coordinator finishes this work
// before its final close barrier and before reconciliation acknowledgement.
func submitStaleCodeEmbeddingCleanup(
	ctx context.Context,
	streaming *unifiedStreamingCoordinator,
	store *codeemb.Store,
	keepPaths []paths.CodePath,
) error {
	keepRel := make([]string, 0, len(keepPaths))
	for _, path := range keepPaths {
		if value := strings.TrimSpace(path.String()); value != "" {
			keepRel = append(keepRel, value)
		}
	}
	stalePaths, err := staleCodeEmbeddingPaths(ctx, store, keepRel)
	if err != nil {
		return err
	}
	return streaming.SubmitCodePaths(ctx, stalePaths)
}
