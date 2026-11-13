package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

var errDerivedSuperseded = fmt.Errorf("derived work superseded: %w", lane.ErrSkipped)

// A bounded group keeps preparation below the lane's one-second priority poll.
const derivedGroupSize = 64

type derivedScheduler struct {
	watcher *unifiedSemanticWatcher
	ctx     context.Context
	cancel  context.CancelFunc
	wake    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	running codeanchor.DerivedKind
	witness map[string]map[string]string
	single  map[codeanchor.DerivedScope]int64
	members []codeanchor.DerivedWork
}

func newDerivedScheduler(w *unifiedSemanticWatcher) *derivedScheduler {
	ctx, cancel := context.WithCancel(w.runCtx)
	s := &derivedScheduler{watcher: w, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), single: map[codeanchor.DerivedScope]int64{}}
	go s.run()
	return s
}
func (s *derivedScheduler) close() { s.cancel(); <-s.done }
func (s *derivedScheduler) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *derivedScheduler) run() {
	defer close(s.done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		for _, kind := range []codeanchor.DerivedKind{codeanchor.DerivedNotes, codeanchor.DerivedOntology, codeanchor.DerivedCode, codeanchor.DerivedGraph} {
			work, err := s.watcher.intelStore.PendingDerivedWorkForKind(s.ctx, kind, time.Now(), derivedGroupSize)
			if err != nil {
				if s.ctx.Err() != nil {
					return
				}
				continue
			}
			var groups [][]codeanchor.DerivedWork
			var paths []codeanchor.DerivedWork
			for _, ticket := range work {
				if ticket.Path == "" {
					for scope := range s.single {
						if scope.Kind == kind {
							delete(s.single, scope)
						}
					}
				}
				if ticket.Path == "" || s.single[ticket.DerivedScope] == ticket.Generation {
					groups = append(groups, []codeanchor.DerivedWork{ticket})
					delete(s.single, ticket.DerivedScope)
				} else {
					delete(s.single, ticket.DerivedScope)
					paths = append(paths, ticket)
				}
			}
			if len(paths) > 0 {
				groups = append(groups, paths)
			}
			for _, group := range groups {
				if s.ctx.Err() != nil {
					return
				}
				s.mu.Lock()
				s.running = kind
				s.mu.Unlock()
				watcherEvent(s.ctx, slog.LevelInfo, "derived.pending", slog.Int("ready_tickets", len(group)))
				s.members = group
				err = s.execute(group...)
				s.mu.Lock()
				s.running = ""
				s.mu.Unlock()
				if err != nil && s.ctx.Err() == nil && len(s.members) > 0 {
					group = s.members
					failed := !errors.Is(err, errDerivedSuperseded) && !errors.Is(err, context.Canceled) && !errors.Is(err, lane.ErrPreempted)
					delay := time.Second
					if failed {
						attempt := 0
						for _, ticket := range group {
							attempt = max(attempt, ticket.Attempt)
						}
						delay = 250 * time.Millisecond << min(attempt, 6)
						if len(group) > 1 && errors.Is(err, embeddings.ErrInvalidInput) {
							for _, ticket := range group {
								s.single[ticket.DerivedScope] = ticket.Generation
							}
						}
					}
					_ = s.inLane(s.ctx, "derived_retry", func(ctx context.Context) error {
						return s.watcher.intelStore.RetryDerivedWorkBatch(ctx, group, time.Now().Add(delay), failed)
					})
				}
			}
		}
	}
}

func (s *derivedScheduler) inLane(parent context.Context, phase string, run func(context.Context) error) error {
	handle, _, err := s.watcher.lane.Submit(parent, lane.Request{Kind: lane.KindEmbedCycle, Run: func(ctx context.Context, _ *indexlock.Lease, _ lane.Reporter) (jobErr error) {
		ctx, finish := watchPhase(ctx, phase)
		defer func() {
			if panicked := recover(); panicked != nil {
				finish(errors.New("diagnostic boundary panicked"))
				panic(panicked)
			}
			finish(jobErr)
		}()
		defer watchDuration(parent, phase)()
		return run(ctx)
	}})
	if err != nil {
		return err
	}
	select {
	case <-parent.Done():
		handle.Cancel()
		<-handle.Done()
		return parent.Err()
	case <-handle.Done():
		return handle.Err()
	}
}

func (s *derivedScheduler) computeContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				status := s.watcher.lane.Status()
				if status.JobKind == lane.KindExplicitIndex {
					cancel(lane.ErrPreempted)
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(context.Canceled); <-done }
}

func (w *unifiedSemanticWatcher) closeDerived() {
	if w.derived != nil {
		w.derived.close()
	}
	if w.watcherDone != nil {
		<-w.watcherDone
	}
}
