//go:build darwin && !fsnotify_fallback

package watchhub

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsevents"
)

type fseventsBackend struct {
	stream        *fsevents.EventStream
	events        chan BackendEvent
	errors        chan error
	paths         map[string]struct{}
	pathsMu       sync.RWMutex
	streamMu      sync.Mutex
	streamUpdates chan *fsevents.EventStream // swap active stream without spawning new loops
	restartMu     sync.Mutex
	loopOnce      sync.Once
	ctx           context.Context
	cancel        context.CancelFunc
	closeOnce     sync.Once
	started       bool
	startedMu     sync.RWMutex
}

func newFSEventsBackend(eventBuffer int) (Backend, error) {
	if eventBuffer <= 0 {
		eventBuffer = defaultBackendEventBuffer
	}
	return &fseventsBackend{
		events:        make(chan BackendEvent, eventBuffer),
		errors:        make(chan error, 16),
		paths:         make(map[string]struct{}),
		streamUpdates: make(chan *fsevents.EventStream, 1),
	}, nil
}

func newBackend(eventBuffer int) (Backend, error) {
	return newFSEventsBackend(eventBuffer)
}

func (b *fseventsBackend) Start(ctx context.Context) error {
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.startedMu.Lock()
	b.started = true
	b.startedMu.Unlock()
	b.loopOnce.Do(func() {
		go b.loop()
	})
	return b.startStream()
}

func (b *fseventsBackend) startStream() error {
	b.restartMu.Lock()
	defer b.restartMu.Unlock()

	b.pathsMu.RLock()
	paths := make([]string, 0, len(b.paths))
	for p := range b.paths {
		paths = append(paths, p)
	}
	b.pathsMu.RUnlock()

	if len(paths) == 0 {
		b.swapStream(nil)
		return nil
	}

	stream := &fsevents.EventStream{
		Paths:   paths,
		Flags:   fsevents.FileEvents | fsevents.WatchRoot,
		Latency: 50 * time.Millisecond, // Slight coalescing to reduce noise
	}

	if err := stream.Start(); err != nil {
		return err
	}

	b.swapStream(stream)
	return nil
}

func (b *fseventsBackend) loop() {
	var events <-chan []fsevents.Event
	for {
		select {
		case <-b.ctx.Done():
			return
		case stream := <-b.streamUpdates:
			if stream == nil {
				events = nil
				continue
			}
			events = stream.Events
		case batch, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			for _, ev := range batch {
				// Filter out internal high-churn paths at backend ingress.
				if shouldDropBackendPath(ev.Path) {
					continue
				}
				bev := BackendEvent{
					Path:       ev.Path,
					Op:         opFromFSEvents(ev.Flags),
					IsDir:      ev.Flags&fsevents.ItemIsDir != 0,
					IsDirKnown: true, // FSEvents tells us
					MustRescan: ev.Flags&fsevents.MustScanSubDirs != 0,
				}
				select {
				case b.events <- bev:
				default:
					// Buffer full - send overflow error
					log.Printf("watchhub/fsevents: event buffer full, dropping: %s", ev.Path)
					select {
					case b.errors <- ErrEventOverflow:
					default:
					}
				}
			}
		}
	}
}

func (b *fseventsBackend) Close() error {
	b.closeOnce.Do(func() {
		if b.cancel != nil {
			b.cancel()
		}
		b.swapStream(nil)
	})
	return nil
}

func (b *fseventsBackend) AddPath(path string) error {
	// Resolve symlinks for FSEvents (it reports real paths)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			resolved = path
		} else {
			return err
		}
	}

	b.pathsMu.Lock()
	b.paths[resolved] = struct{}{}
	b.pathsMu.Unlock()

	// If already started, restart the stream to pick up new path
	b.startedMu.RLock()
	started := b.started
	b.startedMu.RUnlock()

	if started {
		if err := b.startStream(); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (b *fseventsBackend) RemovePath(path string) error {
	resolved, _ := filepath.EvalSymlinks(path)
	if resolved == "" {
		resolved = path
	}

	b.pathsMu.Lock()
	delete(b.paths, resolved)
	b.pathsMu.Unlock()

	// Restart stream if running
	b.startedMu.RLock()
	started := b.started
	b.startedMu.RUnlock()

	if started {
		return b.startStream()
	}
	return nil
}

func (b *fseventsBackend) Events() <-chan BackendEvent {
	return b.events
}

func (b *fseventsBackend) Errors() <-chan error {
	return b.errors
}

func (b *fseventsBackend) IsRecursive() bool {
	return true
}

func (b *fseventsBackend) WatchList() []string {
	b.pathsMu.RLock()
	defer b.pathsMu.RUnlock()
	paths := make([]string, 0, len(b.paths))
	for p := range b.paths {
		paths = append(paths, p)
	}
	return paths
}

func opFromFSEvents(flags fsevents.EventFlags) Op {
	var out Op
	if flags&fsevents.ItemCreated != 0 {
		out |= OpCreate
	}
	if flags&fsevents.ItemModified != 0 {
		out |= OpWrite
	}
	if flags&fsevents.ItemRemoved != 0 {
		out |= OpRemove
	}
	if flags&fsevents.ItemRenamed != 0 {
		out |= OpRename
	}
	if flags&fsevents.ItemInodeMetaMod != 0 ||
		flags&fsevents.ItemChangeOwner != 0 ||
		flags&fsevents.ItemXattrMod != 0 {
		out |= OpChmod
	}
	return out
}

func (b *fseventsBackend) swapStream(next *fsevents.EventStream) {
	b.streamMu.Lock()
	prev := b.stream
	b.stream = next
	b.streamMu.Unlock()

	if prev != nil && prev != next {
		prev.Stop()
	}

	b.publishStream(next)
}

func (b *fseventsBackend) publishStream(next *fsevents.EventStream) {
	select {
	case b.streamUpdates <- next:
		return
	default:
	}
	select {
	case <-b.streamUpdates:
	default:
	}
	b.streamUpdates <- next
}
