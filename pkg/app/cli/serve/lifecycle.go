package serve

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
)

// lifecyclePollInterval bounds how quickly a headless runtime notices that it
// has gone idle, that its vault root disappeared, or that its configuration
// changed. Idle exit is an hour by default, so a coarse poll is enough.
const lifecyclePollInterval = 5 * time.Second

// rootPollInterval is how often either mode checks that its vault root still
// exists.
const rootPollInterval = 30 * time.Second

// IdleTracker records the last moment a client touched the runtime and how
// many UI event streams are open. Every request counts, and the runtime is
// never idle while a stream is open, so an idle runtime is one no client is
// using (SPEC-0104 US5).
type IdleTracker struct {
	last atomic.Int64
	open atomic.Int64
	now  func() time.Time
}

// NewIdleTracker starts the idle clock at the current time.
func NewIdleTracker() *IdleTracker {
	t := &IdleTracker{now: time.Now}
	t.Touch()
	return t
}

// Touch records client activity.
func (t *IdleTracker) Touch() {
	if t == nil {
		return
	}
	t.last.Store(t.clock().UnixNano())
}

// Hold marks a client stream open until release, which also records activity.
func (t *IdleTracker) Hold() (release func()) {
	if t == nil {
		return func() {}
	}
	t.open.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			t.open.Add(-1)
			t.Touch()
		})
	}
}

// IdleFor reports how long the runtime has gone without client activity.
func (t *IdleTracker) IdleFor() time.Duration {
	if t == nil || t.open.Load() > 0 {
		return 0
	}
	return t.clock().Sub(time.Unix(0, t.last.Load()))
}

func (t *IdleTracker) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// laneIdle reports whether the runtime's indexing lane has nothing to do. A
// runtime that never became owner has no lane and therefore no work in flight.
func laneIdle(rt *bootstrap.LiveRuntime) bool {
	l := rt.Lane()
	if l == nil {
		return true
	}
	status := l.Status()
	return !status.Busy && status.Queued == 0
}

// idleExitConfig carries the knobs an idle watcher needs. Tests shorten both.
type idleExitConfig struct {
	timeout time.Duration
	poll    time.Duration
}

// watchIdleExit shuts a headless runtime down once it has been idle for the
// configured timeout with an idle lane. Attached runtimes never call this.
func watchIdleExit(ctx context.Context, tracker *IdleTracker, rt *bootstrap.LiveRuntime, cfg idleExitConfig, shutdown func(reason string)) {
	if cfg.timeout <= 0 {
		return
	}
	poll := cfg.poll
	if poll <= 0 {
		poll = lifecyclePollInterval
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !laneIdle(rt) {
				// Indexing work is client-visible progress; do not exit under it.
				tracker.Touch()
				continue
			}
			if tracker.IdleFor() >= cfg.timeout {
				shutdown("idle for " + cfg.timeout.String())
				return
			}
		}
	}
}

// watchVaultRoot shuts the runtime down when its vault root disappears. A
// runtime whose root is gone cannot index, watch, or serve it, in either mode.
func watchVaultRoot(ctx context.Context, vaultPath string, poll time.Duration, shutdown func(reason string)) {
	if poll <= 0 {
		poll = rootPollInterval
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if info, err := os.Stat(vaultPath); err != nil || !info.IsDir() {
				shutdown("vault root " + vaultPath + " is gone")
				return
			}
		}
	}
}
