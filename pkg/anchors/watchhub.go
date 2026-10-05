package codeanchor

import (
	"context"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
	"github.com/fsnotify/fsnotify"
)

// NewHubSubscriber constructs a watcher that consumes WatchHub events instead of fsnotify.
func NewHubSubscriber(service *Service, noteRoots, codeRoots []string, sourceFactory NoteSourceFactory, opts ...WatcherOption) (*Watcher, error) {
	return newWatcher(service, noteRoots, codeRoots, sourceFactory, opts...)
}

// HandleWatchEvents feeds WatchHub events into the watcher loop.
func (w *Watcher) HandleWatchEvents(ctx context.Context, events []watchhub.WatchEvent) {
	if w == nil {
		return
	}
	for _, ev := range events {
		path := ev.AbsPath
		if path == "" {
			root := ""
			if w.service != nil {
				root = w.service.vault.Root()
			}
			if root == "" {
				continue
			}
			path = filepath.Join(root, filepath.FromSlash(ev.RelPath))
		}
		fsEv := fsnotify.Event{
			Name: path,
			Op:   toFSNotifyOp(ev.Op),
		}
		select {
		case w.events <- fsEv:
		default:
			w.handleEventOverflow()
		}
	}
	if ctx != nil {
		_ = ctx
	}
}

// HandleWatchStale triggers a full rescan on stale signals.
func (w *Watcher) HandleWatchStale(ctx context.Context, ev watchhub.StaleEvent) {
	if w == nil {
		return
	}
	if ctx == nil {
		ctx = w.ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = ev
	w.scheduleStaleRescan(ctx)
}

func toFSNotifyOp(op watchhub.Op) fsnotify.Op {
	var out fsnotify.Op
	if op.Has(watchhub.OpCreate) {
		out |= fsnotify.Create
	}
	if op.Has(watchhub.OpWrite) {
		out |= fsnotify.Write
	}
	if op.Has(watchhub.OpRemove) {
		out |= fsnotify.Remove
	}
	if op.Has(watchhub.OpRename) {
		out |= fsnotify.Rename
	}
	if op.Has(watchhub.OpChmod) {
		out |= fsnotify.Chmod
	}
	return out
}
