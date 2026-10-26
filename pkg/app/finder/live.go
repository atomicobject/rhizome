package finder

import (
	"context"
	"sync"
	"time"
)

// Loader reads a fresh catalog from the index.
type Loader func(ctx context.Context) (*Catalog, error)

// Live serves the latest catalog and rebuilds it in the background. Requests
// never wait for a rebuild once a catalog exists; only the very first request
// waits for the initial build, because there is nothing older to serve.
type Live struct {
	load     Loader
	debounce time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	mu       sync.Mutex
	current  *Catalog
	err      error
	ready    chan struct{} // closed after the first build attempt
	building bool
	dirty    bool
	timer    *time.Timer
	// retry is the delay before retrying a failed rebuild; zero after a
	// success. It doubles up to maxRetryDelay.
	retry time.Duration
}

const (
	firstRetryDelay = time.Second
	maxRetryDelay   = 30 * time.Second
)

// NewLive returns a holder that builds lazily and rebuilds debounce after
// each Invalidate.
func NewLive(load Loader, debounce time.Duration) *Live {
	ctx, cancel := context.WithCancel(context.Background())
	return &Live{load: load, debounce: debounce, ctx: ctx, cancel: cancel, ready: make(chan struct{})}
}

// Catalog returns the current catalog, starting the first build if needed.
func (l *Live) Catalog(ctx context.Context) (*Catalog, error) {
	l.mu.Lock()
	if l.current != nil {
		c := l.current
		l.mu.Unlock()
		return c, nil
	}
	if !l.building && l.ctx.Err() == nil {
		l.startLocked()
	}
	ready := l.ready
	l.mu.Unlock()
	select {
	case <-ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current, l.err
}

// Invalidate schedules a debounced rebuild. Bursts of index events coalesce
// into one build, and a build already running is followed by exactly one more.
func (l *Live) Invalidate() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ctx.Err() != nil {
		return
	}
	if l.building {
		l.dirty = true
		return
	}
	l.scheduleLocked(l.debounce)
}

func (l *Live) startLocked() {
	l.building = true
	l.dirty = false
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		catalog, err := l.load(l.ctx)
		l.mu.Lock()
		defer l.mu.Unlock()
		l.building = false
		if err == nil {
			l.current, l.err, l.retry = catalog, nil, 0
		} else if l.current == nil {
			l.err = err
		}
		select {
		case <-l.ready:
		default:
			close(l.ready)
		}
		if l.current == nil && l.err != nil {
			// Let the next request retry a failed first build.
			l.ready = make(chan struct{})
		}
		switch {
		case l.ctx.Err() != nil:
		case l.dirty:
			l.startLocked()
		case err != nil && l.current != nil:
			// Keep serving the older catalog, but don't let one failed reload
			// leave it stale until the next index event.
			l.retry = min(max(2*l.retry, firstRetryDelay), maxRetryDelay)
			l.scheduleLocked(l.retry)
		}
	}()
}

// scheduleLocked starts a build after delay unless one is already running.
func (l *Live) scheduleLocked(delay time.Duration) {
	if l.timer != nil {
		l.timer.Stop()
	}
	l.timer = time.AfterFunc(delay, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if !l.building && l.ctx.Err() == nil {
			l.startLocked()
		}
	})
}

// Close stops pending and running builds and waits for them to exit.
func (l *Live) Close() {
	l.mu.Lock()
	l.cancel()
	if l.timer != nil {
		l.timer.Stop()
	}
	l.mu.Unlock()
	l.wg.Wait()
}
