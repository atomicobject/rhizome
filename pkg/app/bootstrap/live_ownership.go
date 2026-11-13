package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/logging"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

func (w *unifiedSemanticWatcher) run() {
	if w.watcherDone != nil {
		defer close(w.watcherDone)
	}
	tick := w.opts.Tick
	var ticker *time.Ticker
	if tick == nil {
		ticker = time.NewTicker(w.opts.TickInterval)
		defer ticker.Stop()
		tick = ticker.C
	}
	var retryAt time.Time
	var retryDelay time.Duration
	inputVersion := w.inputVersion.Load()
	for {
		select {
		case <-w.runCtx.Done():
			return
		case <-tick:
			if !w.hasPendingWork() {
				continue
			}
		case <-w.ownershipWake:
		}
		if current := w.inputVersion.Load(); current != inputVersion {
			inputVersion = current
			retryAt, retryDelay = time.Time{}, 0
		}
		if w.opts.Now().Before(retryAt) {
			continue
		}
		if handle := w.processOwnershipBatch(); handle != nil {
			select {
			case <-w.runCtx.Done():
				return
			case <-handle.Done():
			}
			retryDelay = min(max(2*retryDelay, w.opts.TickInterval), time.Minute)
			if err := handle.Err(); err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				retryDelay = 0
			}
			retryAt = w.opts.Now().Add(retryDelay)
		}
	}
}

// hasPendingWork reports whether a periodic tick has work for a batch. An idle
// tick admits no lane job, so routine no-ops leave no diagnostics history
// (SPEC-0116); a read error submits.
func (w *unifiedSemanticWatcher) hasPendingWork() bool {
	if w.cacheService == nil || w.intelStore == nil {
		return true
	}
	w.pendingMu.Lock()
	inputs := len(w.pendingByRel) > 0 || w.pendingResync
	seen := w.seenRecrawls
	w.pendingMu.Unlock()
	if inputs || w.cacheService.HasPendingWork(seen) {
		return true
	}
	if _, pending, err := w.intelStore.PendingOwnershipReconciliation(w.runCtx); err != nil || pending {
		return true
	}
	unready, err := w.intelStore.HasUnreadyDerivedWork(w.runCtx)
	return err != nil || unready
}

// processOwnershipBatch queues one reconciliation on the indexing lane and
// returns its handle without waiting: the lane owns the index lock and the
// ordering, and a batch already queued or running absorbs this tick's changes.
// An explicit index job or an external priority request may cancel the batch,
// so a cancelled batch restores its inputs for the next tick. Durable
// reconciliation and unready tickets decide whether recovery needs full scope.
func (w *unifiedSemanticWatcher) processOwnershipBatch() lane.Handle {
	if w == nil || w.cacheService == nil || w.intelStore == nil || w.noteSvc == nil || w.lane == nil {
		return nil
	}
	handle, _, err := w.lane.Submit(w.runCtx, lane.Request{
		Kind:     lane.KindWatcherBatch,
		Coalesce: true,
		Run: func(ctx context.Context, lease *indexlock.Lease, _ lane.Reporter) error {
			err := w.reconcileBatchUnderLease(ctx, lease)
			if err != nil && !errors.Is(err, ctx.Err()) {
				w.cacheService.MarkStale()
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				if w.debug {
					log.Printf("semantic watch: ownership batch interrupted: %v", ctxErr)
				}
				if err == nil {
					w.cacheService.MarkStale()
				}
				return ctxErr
			}
			return err
		},
	})
	if err != nil {
		if w.debug && !errors.Is(err, lane.ErrClosed) {
			log.Printf("semantic watch: submit ownership batch: %v", err)
		}
		return nil
	}
	return handle
}

// reconcileBatchUnderLease runs with the lane's job context and the
// index lock held.
func (w *unifiedSemanticWatcher) reconcileBatchUnderLease(ctx context.Context, lease *indexlock.Lease) (batchErr error) {
	ctx, finish := watchPhase(ctx, "watcher_reconcile")
	defer func() {
		finish(batchErr)
		w.pendingMu.Lock()
		pendingPaths := len(w.pendingByRel)
		w.pendingMu.Unlock()
		indexingperf.SetGauge(ctx, indexingperf.WatcherPendingInputPaths, int64(pendingPaths))
	}()
	cacheStarted := time.Now()
	result, err := w.cacheService.RefreshWithResult(ctx)
	indexingperf.ObserveLatency(ctx, indexingperf.WatcherCacheRefresh, time.Since(cacheStarted))
	if err != nil {
		if w.debug {
			log.Printf("semantic watch: cache refresh failed: %v", err)
		}
		return err
	}
	pendingDirty, pendingResync, reasons := w.takePendingWatchInputs()
	recrawled := w.observeRecrawls(result.Recrawls)
	if recrawled {
		reasons = addWatchReason(reasons, "cache_recrawl_completed")
	}
	indexingperf.AddCount(ctx, indexingperf.WatcherPendingPaths, int64(len(pendingDirty)))
	indexingperf.AddCount(ctx, indexingperf.WatcherCacheDrainedPaths, int64(len(result.Drained)))
	indexingperf.AddCount(ctx, indexingperf.WatcherCacheChangedPaths, int64(len(result.Changed)))
	result.Resynced = result.Resynced || recrawled
	dirty := cloneDirtyKinds(result.Drained)
	for rel, kind := range pendingDirty {
		dirty[rel] = kind
	}
	for rel, kind := range result.Changed {
		if _, exists := dirty[rel]; !exists {
			dirty[rel] = kind
		}
	}
	now := w.opts.Now()
	// Any failure after the refresh can precede the durable transition. Retain
	// drained events alongside reconciliation debt for the lane's next batch.
	// A resync deferred to an in-flight recrawl stays pending outside the lease.
	completed, deferred := false, false
	defer func() {
		if completed && deferred {
			w.restoreWatchInputs(nil, true, reasons)
		}
		if !completed {
			resync := result.Resynced || pendingResync
			w.restoreWatchInputs(dirty, resync, reasons)
			indexingperf.AddCount(ctx, indexingperf.WatcherRestoredPaths, int64(len(dirty)))
			watcherEvent(ctx, slog.LevelWarn, "batch.restored", watcherReasonAttrs(reasons, slog.Int("paths", len(dirty)), slog.Bool("resync", resync))...)
			for rel, kind := range dirty {
				w.cacheService.MarkDirty(rel, kind)
			}
		}
	}()
	generation, pending, err := w.intelStore.PendingOwnershipReconciliation(ctx)
	if err != nil {
		if w.debug {
			log.Printf("semantic watch: read ownership reconciliation: %v", err)
		}
		return err
	}
	unready, err := w.intelStore.HasUnreadyDerivedWork(ctx)
	if err != nil {
		return err
	}
	// A stale cache's recrawl will trigger a full discovery when it completes;
	// running one now for the same staleness would discover twice.
	deferred = w.deferResync(pendingResync && !result.Resynced && !pending && !unready, result.RecrawlInFlight)
	if !deferred {
		for reason, count := range reasons {
			indexingperf.AddCount(ctx, indexingperf.WatcherTrigger.Select(reason), count)
		}
		result.Resynced = result.Resynced || pendingResync
	}
	result.Resynced = result.Resynced || unready
	if unready {
		reasons = addWatchReason(reasons, "unready_derived_work")
		indexingperf.AddCount(ctx, indexingperf.WatcherTriggerUnreadyDerivedWork, 1)
	}
	if len(dirty) == 0 && !result.Resynced && !pending {
		completed = true
		return nil
	}
	indexingperf.AddCount(ctx, indexingperf.WatcherDirtyPaths, int64(len(dirty)))
	if pending {
		indexingperf.AddCount(ctx, indexingperf.WatcherPendingReconciliation, 1)
	}
	watcherEvent(ctx, slog.LevelInfo, "batch.inputs", watcherReasonAttrs(reasons, slog.Int("dirty_paths", len(dirty)), slog.Bool("resync", result.Resynced), slog.Bool("pending_reconciliation", pending), slog.Int64("generation", generation))...)

	epoch := w.health.begin(dirty, result.Resynced)
	err = w.reconcileOwnershipBatch(ctx, lease, dirty, result.Resynced, pending, generation, now, epochID(epoch))
	if err != nil {
		w.health.fail(epochID(epoch), "ownership", err)
		w.publishInvalidation("metadata-ontology")
		return err
	}
	completed = true
	watcherEvent(ctx, slog.LevelInfo, "batch.completed")
	return nil
}

func (w *unifiedSemanticWatcher) reconcileOwnershipBatch(ctx context.Context, lease *indexlock.Lease, dirty map[string]cache.DirtyKind, resynced, pending bool, generation int64, now time.Time, epoch int64) error {
	forceAll := resynced || pending
	forceMetadata := false
	forceOntology := false
	selectionChanged := false
	processed := make(map[string]cache.DirtyKind, len(dirty))
	affected := make(map[string]struct{}, len(dirty))
	internal := make([]string, 0)
	for rel, kind := range dirty {
		rel = filepath.ToSlash(strings.TrimSpace(rel))
		if rel == "" {
			continue
		}

		if effect := classifyInternalChange(rel); effect.any() {
			internal = append(internal, rel)
			processed[rel] = kind
			forceMetadata = forceMetadata || effect.refreshMeta
			forceOntology = forceOntology || effect.refreshOntology
			if effect.reloadVaultDef || rel == ".rhizome/ignore" || rel == ".obsidianignore" {
				if err := w.reloadOwnershipSelection(); err != nil {
					return err
				}
				forceAll = true
				selectionChanged = true
				indexingperf.AddCount(ctx, indexingperf.WatcherSelectionConfigReload, 1)
			}
			continue
		}
		clean, err := paths.CleanRelPath(rel)
		if err != nil || clean.String() != rel {
			continue
		}
		affected[clean.String()] = struct{}{}
		processed[clean.String()] = kind
	}
	indexingperf.AddCount(ctx, indexingperf.WatcherInternalPaths, int64(len(internal)))
	if len(affected) == 0 && len(internal) == 0 && !resynced && !pending {
		w.health.phase(epoch, "deferred")
		return nil
	}

	request := indexcore.Request{VaultDefinition: w.vaultDef, CodeConfig: w.codeCfg, NoteMetadata: w.noteMetadataIndexer, ForceMetadata: forceMetadata, ForceOntology: forceOntology}
	if !forceAll && !forceMetadata && !forceOntology {
		request.Paths = make([]paths.RelPath, 0, len(affected))
		for path := range affected {
			request.Paths = append(request.Paths, paths.RelPath(path))
		}
		sort.Slice(request.Paths, func(i, j int) bool { return request.Paths[i] < request.Paths[j] })
	}
	if request.Paths == nil {
		reason := "resync"
		switch {
		case pending:
			reason = "pending_reconciliation"
		case selectionChanged:
			reason = "selection_config"
		case forceMetadata:
			reason = "metadata_change"
		case forceOntology:
			reason = "ontology_change"
		}
		indexingperf.AddCount(ctx, indexingperf.WatcherFullDiscovery.Select(reason), 1)
		watcherEvent(ctx, slog.LevelInfo, "discovery.full", slog.String("reason_code", reason), slog.Bool("resync", resynced), slog.Bool("pending_reconciliation", pending), slog.Bool("selection_changed", selectionChanged))
	} else {
		indexingperf.AddCount(ctx, indexingperf.WatcherScopedDiscovery, 1)
	}
	w.health.phase(epoch, "structural-index")
	discovery, err := indexcore.Discover(ctx, request, w.intelStore)
	if err != nil {
		return fmt.Errorf("discover live structural work: %w", err)
	}
	queue := w.structuralWriter(ctx)
	defer queue.Close()
	result, err := indexcore.Publish(ctx, lease, request, discovery, w.noteSvc, w.intelStore, queue, indexcore.PublishOptions{AfterPreparedOwnershipCommit: w.afterPreparedOwnershipCommit})
	if err != nil {
		return fmt.Errorf("publish live structural work: %w", err)
	}
	if err := queue.FlushAndWait(ctx); err != nil {
		return err
	}

	indexingperf.AddCount(ctx, indexingperf.WatcherNotesIngested, int64(len(result.Notes.ChangedPaths)))
	indexingperf.AddCount(ctx, indexingperf.WatcherCodeIngested, int64(len(result.Code.IndexedPaths)))
	indexingperf.AddCount(ctx, indexingperf.WatcherNotesDeleted, int64(len(result.Notes.DeletedPaths)))
	indexingperf.AddCount(ctx, indexingperf.WatcherCodeRetired, int64(len(result.CodeRetirements)))
	if w.derived != nil {
		w.derived.notify()
	}
	for _, path := range result.Notes.ChangedPaths {
		if w.onProcessedPath != nil {
			w.onProcessedPath(path)
		}
	}
	// Reads take note bytes from the cache, which otherwise refreshes only on
	// watch events. This batch may have published disk content whose event has
	// not arrived (a resync, or FSEvents lagging under load), so bring those
	// entries up to date before node.changed announces them. Unchanged entries
	// cost one stat; watcher inputs live apart from the cache's dirty set. A
	// failed refresh leaves the committed publication standing, as saves do.
	for _, path := range append(result.Notes.ChangedPaths, result.Notes.DeletedPaths...) {
		w.cacheService.MarkDirty(path, cache.DirtyModified)
	}
	if err := w.cacheService.Refresh(ctx); err != nil {
		watcherEvent(ctx, slog.LevelWarn, "cache.refresh_failed", slog.String("error_class", logging.ClassifyError(err)))
	}
	w.publishProcessedEvents(resynced, result.Notes.ChangedPaths, result.Notes.DeletedPaths, result.Code.IndexedPaths, result.CodeRetirements, internal, forceMetadata, forceOntology)
	w.health.complete(epoch)
	return nil
}
