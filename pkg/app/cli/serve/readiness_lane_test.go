package serve

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// catchUpHarness runs the coordinator's boot catch-up on a real lane and lets a
// test displace it with an explicit index while the catch-up is mid-run.
type catchUpHarness struct {
	live    *bootstrap.LiveRuntime
	lane    *lane.Executor
	runs    atomic.Int32
	started chan struct{}
	coord   *ReadinessCoordinator
}

func newCatchUpHarness(t *testing.T) *catchUpHarness {
	t.Helper()
	h := &catchUpHarness{started: make(chan struct{}, 8)}
	h.lane = lane.New(lane.Options{LockPath: filepath.Join(t.TempDir(), "index.lock"), PriorityPoll: 10 * time.Millisecond})
	t.Cleanup(h.lane.Close)
	h.live = &bootstrap.LiveRuntime{}
	h.live.SetLane(h.lane)
	h.coord = NewReadinessCoordinator(func(ctx context.Context, _ notemeta.Indexer, _ string, _ obsidian.VaultDefinition, _ bool) error {
		// The first run blocks until displaced; a resubmitted run completes.
		if h.runs.Add(1) == 1 {
			h.started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	return h
}

func (h *catchUpHarness) runCatchUp() <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- h.coord.runIndexOnLane(context.Background(), h.live, notemeta.Indexer{}, "", obsidian.VaultDefinition{}, false)
	}()
	return done
}

func (h *catchUpHarness) displaceWith(t *testing.T, explicitErr error) {
	t.Helper()
	<-h.started
	_, _, err := h.lane.Submit(context.Background(), lane.Request{Kind: lane.KindExplicitIndex, Coalesce: true, Run: func(context.Context, lane.Reporter) error {
		return explicitErr
	}})
	require.NoError(t, err)
}

func requireCatchUpResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the boot catch-up never settled")
		return nil
	}
}

func TestCatchUpDisplacedByASuccessfulIndexIsDone(t *testing.T) {
	h := newCatchUpHarness(t)
	done := h.runCatchUp()
	h.displaceWith(t, nil)

	require.NoError(t, requireCatchUpResult(t, done))
	require.Equal(t, int32(1), h.runs.Load(), "the explicit index did the catch-up's work; re-running would only make indexed reads unavailable again")
}

func TestCatchUpDisplacedByAFailedIndexRunsAgain(t *testing.T) {
	h := newCatchUpHarness(t)
	done := h.runCatchUp()
	h.displaceWith(t, errors.New("explicit index failed"))

	require.NoError(t, requireCatchUpResult(t, done))
	require.Equal(t, int32(2), h.runs.Load(), "the vault still needs catching up after a failed explicit index")
}

func TestCatchUpRunsDirectlyWithoutALane(t *testing.T) {
	var ran atomic.Bool
	coord := NewReadinessCoordinator(func(context.Context, notemeta.Indexer, string, obsidian.VaultDefinition, bool) error {
		ran.Store(true)
		return nil
	})

	require.NoError(t, coord.runIndexOnLane(context.Background(), &bootstrap.LiveRuntime{}, notemeta.Indexer{}, "", obsidian.VaultDefinition{}, false))
	require.True(t, ran.Load())
}
