package bootstrap

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// Cache readers can refresh before the semantic tick. Keep the watcher's inputs
// independently until its destinations finish, including changes during a batch.
func (w *unifiedSemanticWatcher) subscribeWatchHub() {
	w.stopWatchHub = func() {}
	if w.watchHub == nil {
		return
	}
	w.stopWatchHub = w.watchHub.Subscribe("semantic", watchhub.Filter{IncludeFiles: true, IncludeDirs: true, IncludeHidden: true}, func(_ context.Context, events []watchhub.WatchEvent) {
		dirty := make(map[string]cache.DirtyKind, len(events))
		reasons := make(map[string]int64)
		resync := false
		for _, event := range events {
			if event.IsDir {
				resync = true
				reasons["directory_event"]++
				if event.Op.Has(watchhub.OpRename) {
					reasons["directory_rename"]++
				}
				if event.Op.Has(watchhub.OpCreate) {
					reasons["directory_create"]++
				}
				continue
			}
			kind := cache.DirtyModified
			switch {
			case event.Op.Has(watchhub.OpRemove) && event.Op.Has(watchhub.OpCreate):
				kind = cache.DirtyRecreated
			case event.Op.Has(watchhub.OpRename):
				kind = cache.DirtyRenamed
				resync = true
				reasons["file_rename"]++
			case event.Op.Has(watchhub.OpRemove):
				kind = cache.DirtyRemoved
			case event.Op.Has(watchhub.OpCreate):
				kind = cache.DirtyCreated
			}
			dirty[event.RelPath] = kind
			reasons["file_"+string(kind)]++
		}
		w.retainWatchInputs(dirty, resync, reasons)
	}, func(_ context.Context, event watchhub.StaleEvent) {
		w.retainWatchInputs(nil, true, map[string]int64{"stale_" + watchStaleReason(event.Reason): 1, "source_" + watchSource(event.Source): 1})
	})
}

func (w *unifiedSemanticWatcher) retainWatchEvents(dirty map[string]cache.DirtyKind, resync bool) {
	var reasons map[string]int64
	if resync {
		reasons = map[string]int64{"unspecified_resync": 1}
	}
	w.retainWatchInputs(dirty, resync, reasons)
}

func (w *unifiedSemanticWatcher) retainWatchInputs(dirty map[string]cache.DirtyKind, resync bool, reasons map[string]int64) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	if w.pendingByRel == nil {
		w.pendingByRel = make(map[string]cache.DirtyKind, len(dirty))
	}
	if w.eventCount == nil {
		w.eventCount = make(map[string]uint64, len(dirty))
	}
	for path, kind := range dirty {
		w.pendingByRel[path] = kind
		w.eventCount[path]++
	}
	w.pendingResync = w.pendingResync || resync
	w.mergeWatchReasonsLocked(reasons)
	if w.ownershipWake != nil {
		select {
		case w.ownershipWake <- struct{}{}:
		default:
		}
	}
}

// pruneEvents drops event counts for paths outside keep. A dropped path that
// returns starts counting again with no remembered stat to trust.
func (w *unifiedSemanticWatcher) pruneEvents(keep map[string]struct{}) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	for rel := range w.eventCount {
		if _, ok := keep[rel]; !ok {
			delete(w.eventCount, rel)
		}
	}
}

// events reports how many filesystem events the watcher has received for rel.
func (w *unifiedSemanticWatcher) events(rel string) uint64 {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	return w.eventCount[rel]
}

func (w *unifiedSemanticWatcher) takePendingWatchEvents() (map[string]cache.DirtyKind, bool) {
	dirty, resync, _ := w.takePendingWatchInputs()
	return dirty, resync
}

func (w *unifiedSemanticWatcher) takePendingWatchInputs() (map[string]cache.DirtyKind, bool, map[string]int64) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	dirty, resync, reasons := w.pendingByRel, w.pendingResync, w.pendingReasons
	w.pendingByRel = nil
	w.pendingResync = false
	w.pendingReasons = nil
	return dirty, resync, reasons
}

// Retry old work without overwriting events received while it was running.
func (w *unifiedSemanticWatcher) restoreWatchEvents(dirty map[string]cache.DirtyKind, resync bool) {
	w.restoreWatchInputs(dirty, resync, nil)
}

func (w *unifiedSemanticWatcher) restoreWatchInputs(dirty map[string]cache.DirtyKind, resync bool, reasons map[string]int64) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	if w.pendingByRel == nil {
		w.pendingByRel = make(map[string]cache.DirtyKind, len(dirty))
	}
	for path, kind := range dirty {
		if _, newer := w.pendingByRel[path]; !newer {
			w.pendingByRel[path] = kind
		}
	}
	w.pendingResync = w.pendingResync || resync
	w.mergeWatchReasonsLocked(reasons)
}

func (w *unifiedSemanticWatcher) mergeWatchReasonsLocked(reasons map[string]int64) {
	if w.pendingReasons == nil {
		w.pendingReasons = make(map[string]int64)
	}
	for reason, count := range reasons {
		w.pendingReasons[reason] += count
	}
}

func watchStaleReason(reason watchhub.StaleReason) string {
	switch reason {
	case watchhub.StaleWatcherError, watchhub.StaleOverflow, watchhub.StaleIgnoreChanged, watchhub.StaleDirRenamed, watchhub.StaleDirCreated, watchhub.StaleHintResync:
		return string(reason)
	default:
		return "unknown"
	}
}

func watchSource(source watchhub.Source) string {
	switch source {
	case watchhub.SourceFSNotify, watchhub.SourceHint:
		return string(source)
	default:
		return "unknown"
	}
}
