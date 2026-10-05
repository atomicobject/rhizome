package cache

import (
	"context"
	"path/filepath"
	"runtime"

	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// SubscribeWatchHub wires a cache.Service to a watchhub.Hub.
// The cache watcher is expected to be disabled when using the hub.
func SubscribeWatchHub(hub *watchhub.Hub, svc *Service) func() {
	if hub == nil || svc == nil {
		return func() {}
	}
	filter := watchhub.Filter{
		IncludeDirs:   true,
		IncludeFiles:  true,
		IncludeHidden: true,
	}
	return hub.Subscribe("cache", filter, func(ctx context.Context, events []watchhub.WatchEvent) {
		_ = ctx
		for _, ev := range events {
			handleCacheEvent(svc, ev)
		}
	}, func(ctx context.Context, ev watchhub.StaleEvent) {
		_ = ctx
		_ = ev
		svc.MarkStale()
	})
}

func handleCacheEvent(svc *Service, ev watchhub.WatchEvent) {
	rel := ev.RelPath
	if rel == "" {
		return
	}
	// Treat explicit remove+create as recreation. On Windows, remove+write
	// can appear for directory deletes and should remain a removal.
	if ev.Op.Has(watchhub.OpRemove) && ev.Op.Has(watchhub.OpCreate) {
		svc.MarkDirty(rel, DirtyRecreated)
		if runtime.GOOS == "windows" {
			parent := filepath.Dir(rel)
			if parent != "." && parent != "" {
				svc.MarkDirty(parent, DirtyModified)
			}
		}
		return
	}
	if ev.Op.Has(watchhub.OpRename) {
		svc.MarkDirty(rel, DirtyRenamed)
		parent := filepath.Dir(rel)
		if parent != "." && parent != "" {
			// Rename events often report only one side of the move. Mark the
			// parent so Refresh rescans for the new child name.
			svc.MarkDirty(parent, DirtyModified)
		}
		if ev.IsDir {
			// Directory renames can move an arbitrary subtree; a full resync is
			// cheaper than trying to synthesize every old/new child path.
			svc.MarkStale()
		}
		return
	}
	if ev.Op.Has(watchhub.OpRemove) {
		svc.MarkDirty(rel, DirtyRemoved)
		if runtime.GOOS == "windows" {
			parent := filepath.Dir(rel)
			if parent != "." && parent != "" {
				svc.MarkDirty(parent, DirtyModified)
			}
		}
		return
	}
	if ev.Op.Has(watchhub.OpCreate) {
		svc.MarkDirty(rel, DirtyCreated)
		return
	}
	if ev.Op.Has(watchhub.OpWrite) || ev.Op.Has(watchhub.OpChmod) {
		svc.MarkDirty(rel, DirtyModified)
	}
}
