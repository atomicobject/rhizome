package watchhub

import (
	"context"
	"log"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type fsnotifyBackend struct {
	watcher   *fsnotify.Watcher
	events    chan BackendEvent
	errors    chan error
	ctx       context.Context
	cancel    context.CancelFunc
	loopDone  chan struct{}
	closeOnce sync.Once
	closed    bool
	closedMu  sync.RWMutex
}

func newFSNotifyBackend(eventBuffer int) (Backend, error) {
	if eventBuffer <= 0 {
		eventBuffer = defaultBackendEventBuffer
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fsnotifyBackend{
		watcher: w,
		events:  make(chan BackendEvent, eventBuffer),
		errors:  make(chan error, 16),
	}, nil
}

func (b *fsnotifyBackend) Start(ctx context.Context) error {
	// Windows watch installation shares the native reader with event delivery.
	// Keep draining it until Close, even after hub work has been cancelled.
	b.ctx, b.cancel = context.WithCancel(context.WithoutCancel(ctx))
	b.loopDone = make(chan struct{})
	go func() {
		defer close(b.loopDone)
		b.loop()
	}()
	return nil
}

func (b *fsnotifyBackend) loop() {
	for {
		select {
		case <-b.ctx.Done():
			return
		case ev, ok := <-b.watcher.Events:
			if !ok {
				b.closedMu.RLock()
				closed := b.closed
				b.closedMu.RUnlock()
				if !closed {
					// Channel closed unexpectedly
					select {
					case b.errors <- ErrEventOverflow:
					default:
					}
				}
				return
			}
			// Filter out internal high-churn paths at backend ingress.
			if shouldDropBackendPath(ev.Name) {
				continue
			}
			select {
			case b.events <- BackendEvent{
				Path:       ev.Name,
				Op:         opFromFSNotify(ev.Op),
				IsDir:      false, // fsnotify doesn't tell us; Hub will stat
				IsDirKnown: false,
			}:
			default:
				// Buffer full - send overflow error
				log.Printf("watchhub/fsnotify: event buffer full, dropping: %s", ev.Name)
				select {
				case b.errors <- ErrEventOverflow:
				default:
				}
			}
		case err, ok := <-b.watcher.Errors:
			if !ok {
				return
			}
			select {
			case b.errors <- err:
			default:
			}
		}
	}
}

func (b *fsnotifyBackend) Close() error {
	var err error
	b.closeOnce.Do(func() {
		b.closedMu.Lock()
		b.closed = true
		b.closedMu.Unlock()
		if b.cancel != nil {
			b.cancel()
		}
		err = b.watcher.Close()
		if b.loopDone != nil {
			<-b.loopDone
		}
	})
	return err
}

func (b *fsnotifyBackend) AddPath(path string) error {
	return b.watcher.Add(path)
}

func (b *fsnotifyBackend) RemovePath(path string) error {
	return b.watcher.Remove(path)
}

func (b *fsnotifyBackend) Events() <-chan BackendEvent {
	return b.events
}

func (b *fsnotifyBackend) Errors() <-chan error {
	return b.errors
}

func (b *fsnotifyBackend) IsRecursive() bool {
	return false
}

func (b *fsnotifyBackend) WatchList() []string {
	return b.watcher.WatchList()
}

func opFromFSNotify(op fsnotify.Op) Op {
	var out Op
	if op&fsnotify.Create != 0 {
		out |= OpCreate
	}
	if op&fsnotify.Write != 0 {
		out |= OpWrite
	}
	if op&fsnotify.Remove != 0 {
		out |= OpRemove
	}
	if op&fsnotify.Rename != 0 {
		out |= OpRename
	}
	if op&fsnotify.Chmod != 0 {
		out |= OpChmod
	}
	return out
}
