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
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
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
	for {
		select {
		case <-w.runCtx.Done():
			return
		case <-tick:
		case <-w.ownershipWake:
		}
		if handle := w.processOwnershipBatch(); handle != nil {
			select {
			case <-w.runCtx.Done():
				return
			case <-handle.Done():
			}
		}
	}
}

// processOwnershipBatch queues one reconciliation on the indexing lane and
// returns its handle without waiting: the lane owns the index lock and the
// ordering, and a batch already queued or running absorbs this tick's changes.
// An explicit index job or an external priority request may cancel the batch,
// so a cancelled batch marks the cache stale and its drained paths are
// rediscovered on the next tick rather than lost (SPEC-0104 US3).
func (w *unifiedSemanticWatcher) processOwnershipBatch() lane.Handle {
	if w == nil || w.cacheService == nil || w.intelStore == nil || w.noteSvc == nil || w.lane == nil {
		return nil
	}
	handle, _, err := w.lane.Submit(w.runCtx, lane.Request{
		Kind:     lane.KindWatcherBatch,
		Coalesce: true,
		Run: func(ctx context.Context, _ lane.Reporter) error {
			err := w.reconcileBatchUnderLease(ctx)
			if err := ctx.Err(); err != nil {
				if w.debug {
					log.Printf("semantic watch: ownership batch interrupted: %v", err)
				}
				w.cacheService.MarkStale()
				return err
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
func (w *unifiedSemanticWatcher) reconcileBatchUnderLease(ctx context.Context) (batchErr error) {
	ctx, finish := watchPhase(ctx, "watcher_reconcile")
	defer func() {
		finish(batchErr)
		w.pendingMu.Lock()
		pendingPaths := len(w.pendingByRel)
		w.pendingMu.Unlock()
		indexingperf.SetGauge(ctx, "watcher.pending_input_paths", int64(pendingPaths))
	}()
	cacheStarted := time.Now()
	result, err := w.cacheService.RefreshWithResult(ctx)
	indexingperf.ObserveLatency(ctx, "watcher.cache_refresh", time.Since(cacheStarted))
	if err != nil {
		if w.debug {
			log.Printf("semantic watch: cache refresh failed: %v", err)
		}
		return err
	}
	pendingDirty, pendingResync, reasons := w.takePendingWatchInputs()
	if result.Resynced {
		reasons = addWatchReason(reasons, "cache_recrawl_completed")
	}
	indexingperf.AddCount(ctx, "watcher.pending_paths", int64(len(pendingDirty)))
	indexingperf.AddCount(ctx, "watcher.cache_drained_paths", int64(len(result.Drained)))
	indexingperf.AddCount(ctx, "watcher.cache_changed_paths", int64(len(result.Changed)))
	for reason, count := range reasons {
		indexingperf.AddCount(ctx, "watcher.trigger."+reason, count)
	}
	result.Resynced = result.Resynced || pendingResync
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
	completed := false
	defer func() {
		if !completed {
			w.restoreWatchInputs(dirty, result.Resynced, reasons)
			indexingperf.AddCount(ctx, "watcher.restored_paths", int64(len(dirty)))
			watcherEvent(ctx, slog.LevelWarn, "batch.restored", watcherReasonAttrs(reasons, slog.Int("paths", len(dirty)), slog.Bool("resync", result.Resynced))...)
			for rel, kind := range dirty {
				w.cacheService.MarkDirty(rel, kind)
			}
			w.cacheService.MarkStale()
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
	result.Resynced = result.Resynced || unready
	if unready {
		reasons = addWatchReason(reasons, "unready_derived_work")
		indexingperf.AddCount(ctx, "watcher.trigger.unready_derived_work", 1)
	}
	if len(dirty) == 0 && !result.Resynced && !pending {
		completed = true
		return nil
	}
	indexingperf.AddCount(ctx, "watcher.dirty_paths", int64(len(dirty)))
	if pending {
		indexingperf.AddCount(ctx, "watcher.pending_reconciliation", 1)
	}
	watcherEvent(ctx, slog.LevelInfo, "batch.inputs", watcherReasonAttrs(reasons, slog.Int("dirty_paths", len(dirty)), slog.Bool("resync", result.Resynced), slog.Bool("pending_reconciliation", pending), slog.Int64("generation", generation))...)

	epoch := w.health.begin(dirty, result.Resynced)
	err = w.reconcileOwnershipBatch(ctx, dirty, result.Resynced, pending, generation, now, epochID(epoch))
	if err != nil {
		w.health.fail(epochID(epoch), "ownership", err)
		w.cacheService.MarkStale()
		w.publishInvalidation("metadata-ontology")
		return err
	}
	completed = true
	watcherEvent(ctx, slog.LevelInfo, "batch.completed")
	return nil
}

func (w *unifiedSemanticWatcher) reconcileOwnershipBatch(ctx context.Context, dirty map[string]cache.DirtyKind, resynced, pending bool, generation int64, now time.Time, epoch int64) error {
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
				indexingperf.AddCount(ctx, "watcher.selection_config_reload", 1)
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
	indexingperf.AddCount(ctx, "watcher.internal_paths", int64(len(internal)))
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
		indexingperf.AddCount(ctx, "watcher.full_discovery."+reason, 1)
		watcherEvent(ctx, slog.LevelInfo, "discovery.full", slog.String("reason_code", reason), slog.Bool("resync", resynced), slog.Bool("pending_reconciliation", pending), slog.Bool("selection_changed", selectionChanged))
	} else {
		indexingperf.AddCount(ctx, "watcher.scoped_discovery", 1)
	}
	w.health.phase(epoch, "structural-index")
	discovery, err := indexcore.Discover(ctx, request, w.intelStore)
	if err != nil {
		return fmt.Errorf("discover live structural work: %w", err)
	}
	queue := w.structuralWriter(ctx)
	defer queue.Close()
	ctx = codeintel.WithWriteQueue(ctx, queue)
	result, err := indexcore.Publish(ctx, request, discovery, w.noteSvc, w.intelStore, queue, indexcore.PublishOptions{AfterPreparedOwnershipCommit: w.afterPreparedOwnershipCommit})
	if err != nil {
		return fmt.Errorf("publish live structural work: %w", err)
	}
	if err := queue.FlushAndWait(ctx); err != nil {
		return err
	}
	if result.Reconciliation.Required() {
		if err := queue.AcknowledgeOwnershipReconciliation(ctx, result.StructuralGeneration); err != nil {
			return err
		}
		indexingperf.AddCount(ctx, "watcher.reconciliation_acknowledged", 1)
		watcherEvent(ctx, slog.LevelInfo, "reconciliation.acknowledged", slog.Int64("generation", result.StructuralGeneration))
	}
	indexingperf.AddCount(ctx, "watcher.notes_ingested", int64(len(result.Notes.ChangedPaths)))
	indexingperf.AddCount(ctx, "watcher.code_ingested", int64(len(result.Code.IndexedPaths)))
	indexingperf.AddCount(ctx, "watcher.notes_deleted", int64(len(result.Notes.DeletedPaths)))
	indexingperf.AddCount(ctx, "watcher.code_retired", int64(len(result.CodeRetirements)))
	if w.derived != nil {
		w.derived.notify()
	}
	for _, path := range result.Notes.ChangedPaths {
		if w.onProcessedPath != nil {
			w.onProcessedPath(path)
		}
	}
	w.publishProcessedEvents(resynced, result.Notes.ChangedPaths, result.Notes.DeletedPaths, result.Code.IndexedPaths, result.CodeRetirements, internal, forceMetadata, forceOntology)
	w.health.complete(epoch)
	return nil
}
