package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type fakeDerivedResult int

func (fakeDerivedResult) RetainedBytes() int { return 1 }

type publicationLane struct {
	lane.Lane
	submits atomic.Int64
}

func (l *publicationLane) Submit(ctx context.Context, req lane.Request) (lane.Handle, bool, error) {
	l.submits.Add(1)
	return l.Lane.Submit(ctx, req)
}

func derivedPublicationFixture(t *testing.T) (*derivedScheduler, *publicationLane, codeanchor.DerivedWork) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# note\n"), 0o600))
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { c.Close(); store.Close() })
	counted := &publicationLane{Lane: w.lane}
	w.lane = counted
	tickets, err := store.MarkDerivedDirty(t.Context(), []codeanchor.DerivedScope{{Kind: codeanchor.DerivedCode}})
	require.NoError(t, err)
	require.NoError(t, store.ActivateDerivedWork(t.Context(), tickets))
	return &derivedScheduler{watcher: w, ctx: t.Context()}, counted, tickets[0]
}

// produce calls sink concurrently, as the pipeline's bounded finalizers do.
func produce(n int, sink func(context.Context, fakeDerivedResult) error) error {
	var wg sync.WaitGroup
	errs := make(chan error, n)
	next := atomic.Int64{}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := next.Add(1); i <= int64(n); i = next.Add(1) {
				if err := sink(context.Background(), fakeDerivedResult(i)); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	return <-errs
}

func TestDerivedPublicationBatchesResultsIntoFewLaneJobs(t *testing.T) {
	s, counted, ticket := derivedPublicationFixture(t)
	published := map[fakeDerivedResult]bool{}
	err := computeAndPublish(s, t.Context(), ticket, func(_ context.Context, sink func(context.Context, fakeDerivedResult) error) error {
		return produce(1000, sink)
	}, func(_ context.Context, r fakeDerivedResult) error {
		published[r] = true
		return nil
	})
	require.NoError(t, err)
	require.Len(t, published, 1000, "every computed result is published before return")
	maxJobs := (1000+derivedPublishBatchResults-1)/derivedPublishBatchResults + 2
	require.LessOrEqual(t, counted.submits.Load(), int64(maxJobs))
}

func TestDerivedPublicationStopsWhenWorkIsSupersededMidPass(t *testing.T) {
	s, _, ticket := derivedPublicationFixture(t)
	store := s.watcher.intelStore
	var published atomic.Int64
	var computeStopped bool
	err := computeAndPublish(s, t.Context(), ticket, func(ctx context.Context, sink func(context.Context, fakeDerivedResult) error) error {
		err := produce(1000, sink)
		computeStopped = ctx.Err() != nil
		return err
	}, func(ctx context.Context, _ fakeDerivedResult) error {
		if published.Add(1) == 1 {
			// A newer generation arrives while the first batch publishes.
			_, err := store.MarkDerivedDirty(ctx, []codeanchor.DerivedScope{{Kind: codeanchor.DerivedCode}})
			return err
		}
		return nil
	})
	require.ErrorIs(t, err, errDerivedSuperseded)
	require.Less(t, published.Load(), int64(1000), "later batches must not publish superseded work")
	require.True(t, computeStopped, "a failed batch cancels compute so the pass stops calling the provider")
	pending, err := store.HasDerivedWork(t.Context(), codeanchor.DerivedCode)
	require.NoError(t, err)
	require.True(t, pending, "superseded work is never acknowledged")
}

func TestDerivedPublicationYieldsToExplicitIndexBetweenBatches(t *testing.T) {
	s, _, ticket := derivedPublicationFixture(t)
	entered := make(chan struct{})
	var batches atomic.Int64
	var once sync.Once
	errCh := make(chan error, 1)
	go func() {
		errCh <- computeAndPublish(s, t.Context(), ticket, func(_ context.Context, sink func(context.Context, fakeDerivedResult) error) error {
			return produce(1000, sink)
		}, func(ctx context.Context, _ fakeDerivedResult) error {
			batches.Add(1)
			once.Do(func() { close(entered) })
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-entered
	explicit, _, err := s.watcher.lane.Submit(t.Context(), lane.Request{Kind: lane.KindExplicitIndex, Run: func(context.Context, *indexlock.Lease, lane.Reporter) error { return nil }})
	require.NoError(t, err)
	select {
	case err := <-errCh:
		require.True(t, errors.Is(err, lane.ErrPreempted) || errors.Is(err, context.Canceled), "got %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("publication did not yield to the explicit index")
	}
	<-explicit.Done()
	require.NoError(t, explicit.Err())
	require.EqualValues(t, 1, batches.Load(), "no further batch publishes after preemption")
}
