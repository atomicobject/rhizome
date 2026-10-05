package serve

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestReadinessCoordinatorWaitsForLivePublicationAndOpensGates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	indexStarted := make(chan struct{})
	coordinator := NewReadinessCoordinator(func(context.Context, notemeta.Indexer, string, obsidian.VaultDefinition, bool) error {
		close(indexStarted)
		return nil
	})
	indexDone := make(chan error, 1)
	go func() {
		indexDone <- coordinator.BackgroundIndexer(ctx, notemeta.Indexer{}, "", obsidian.VaultDefinition{}, false)
	}()

	requireChannelOpen(t, coordinator.livePublished)
	runtime := coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	<-indexStarted
	require.NoError(t, <-indexDone)
	require.True(t, runtime.NoteReadReady())
	require.True(t, runtime.IndexReady())
}

func TestReadinessCoordinatorRefreshesValidationAfterSuccessfulBackgroundIndex(t *testing.T) {
	ctx := context.Background()
	coordinator := NewReadinessCoordinator(func(context.Context, notemeta.Indexer, string, obsidian.VaultDefinition, bool) error {
		return nil
	})
	coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	refresher := &recordingValidationRefresher{}
	coordinator.validation = refresher

	require.NoError(t, coordinator.BackgroundIndexer(ctx, notemeta.Indexer{}, "", obsidian.VaultDefinition{}, false))
	require.Equal(t, int32(1), refresher.calls.Load())
}

func TestReadinessCoordinatorDoesNotRefreshValidationAfterFailedBackgroundIndex(t *testing.T) {
	ctx := context.Background()
	coordinator := NewReadinessCoordinator(func(context.Context, notemeta.Indexer, string, obsidian.VaultDefinition, bool) error {
		return errors.New("index failed")
	})
	coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	refresher := &recordingValidationRefresher{}
	coordinator.validation = refresher

	require.Error(t, coordinator.BackgroundIndexer(ctx, notemeta.Indexer{}, "", obsidian.VaultDefinition{}, false))
	require.Zero(t, refresher.calls.Load())
}

func TestReadinessCoordinatorCoalescesManualSaveAndWatcherRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	coordinator := NewReadinessCoordinator(nil)
	coordinator.indexAttemptDoneOnce.Do(func() { close(coordinator.indexAttemptDone) })
	coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	refresher := &recordingValidationRefresher{}
	coordinator.validation = refresher

	coordinator.RequestValidationRefresh()
	coordinator.RequestValidationRefresh()

	require.Eventually(t, func() bool { return refresher.calls.Load() == 1 }, time.Second, 10*time.Millisecond)
	time.Sleep(2 * validationRefreshDebounce)
	require.Equal(t, int32(1), refresher.calls.Load())
}

func TestReadinessCoordinatorGlobalEventSinkDebouncesValidationInvalidated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	coordinator := NewReadinessCoordinator(nil)
	coordinator.indexAttemptDoneOnce.Do(func() { close(coordinator.indexAttemptDone) })
	coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	refresher := &recordingValidationRefresher{}
	coordinator.validation = refresher

	sink := coordinator.GlobalEventSink()
	sink(web.GlobalEventValidationInvalidated, nil)
	sink(web.GlobalEventValidationInvalidated, nil)

	require.Eventually(t, func() bool { return refresher.calls.Load() == 1 }, time.Second, 10*time.Millisecond)
	time.Sleep(2 * validationRefreshDebounce)
	require.Equal(t, int32(1), refresher.calls.Load())
}

func TestReadinessCoordinatorGlobalEventSinkKeepsValidationStaleAfterFailedReconcile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	coordinator := NewReadinessCoordinator(nil)
	coordinator.indexAttemptDoneOnce.Do(func() { close(coordinator.indexAttemptDone) })
	coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	refresher := &recordingValidationRefresher{}
	coordinator.validation = refresher

	coordinator.GlobalEventSink()(web.GlobalEventValidationInvalidated, map[string]any{
		"source": "watcher",
		"reason": web.GlobalEventReasonReconcileFailed,
	})

	time.Sleep(3 * validationRefreshDebounce)
	require.Zero(t, refresher.calls.Load())
}

func TestReadinessCoordinatorGlobalEventSinkIgnoresOtherKinds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	coordinator := NewReadinessCoordinator(nil)
	coordinator.indexAttemptDoneOnce.Do(func() { close(coordinator.indexAttemptDone) })
	coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	refresher := &recordingValidationRefresher{}
	coordinator.validation = refresher

	coordinator.GlobalEventSink()(web.GlobalEventIndexChanged, nil)

	time.Sleep(3 * validationRefreshDebounce)
	require.Zero(t, refresher.calls.Load())
}

type recordingValidationRefresher struct {
	calls atomic.Int32
	err   error
}

func (r *recordingValidationRefresher) Refresh(context.Context) error {
	r.calls.Add(1)
	return r.err
}

func TestReadinessCoordinatorObservesExternalModelAfterIndexFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	coordinator := NewReadinessCoordinator(func(context.Context, notemeta.Indexer, string, obsidian.VaultDefinition, bool) error {
		return errors.New("index deferred")
	})
	var countCalls atomic.Int32
	coordinator.completeModelCount = func(context.Context, *bootstrap.LiveRuntime) (int64, error) {
		if countCalls.Add(1) < 3 {
			return 0, nil
		}
		return 1, nil
	}
	runtime := coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})

	require.Error(t, coordinator.BackgroundIndexer(ctx, notemeta.Indexer{}, "", obsidian.VaultDefinition{}, false))
	require.Eventually(t, runtime.IndexReady, time.Second, time.Millisecond)
	require.GreaterOrEqual(t, countCalls.Load(), int32(3))
}

func TestReadinessCoordinatorOpensNoteGateBeforeSearchReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	coordinator := NewReadinessCoordinator(nil)
	coordinator.indexAttemptDoneOnce.Do(func() { close(coordinator.indexAttemptDone) })
	var countCalls atomic.Int32
	coordinator.usableNoteCount = func(context.Context, *bootstrap.LiveRuntime) (int64, error) {
		if countCalls.Add(1) < 3 {
			return 0, nil
		}
		return 1, nil
	}
	var completeCountCalls atomic.Int32
	coordinator.completeModelCount = func(context.Context, *bootstrap.LiveRuntime) (int64, error) {
		completeCountCalls.Add(1)
		return 0, nil
	}
	runtime := coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	go coordinator.Bridge(ctx)

	require.Eventually(t, runtime.NoteReadReady, time.Second, time.Millisecond)
	require.False(t, runtime.IndexReady())
	require.Eventually(t, func() bool { return completeCountCalls.Load() > 0 }, time.Second, time.Millisecond)
}

func TestReadinessCoordinatorOpensCompleteGateBeforeSearchReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	coordinator := NewReadinessCoordinator(nil)
	coordinator.indexAttemptDoneOnce.Do(func() { close(coordinator.indexAttemptDone) })
	coordinator.usableNoteCount = func(context.Context, *bootstrap.LiveRuntime) (int64, error) {
		return 0, nil
	}
	var completeCountCalls atomic.Int32
	coordinator.completeModelCount = func(context.Context, *bootstrap.LiveRuntime) (int64, error) {
		if completeCountCalls.Add(1) < 3 {
			return 0, nil
		}
		return 1, nil
	}
	runtime := coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	go coordinator.Bridge(ctx)

	require.Eventually(t, runtime.IndexReady, time.Second, time.Millisecond)
	require.True(t, runtime.NoteReadReady())
	require.GreaterOrEqual(t, completeCountCalls.Load(), int32(3))
}

func requireChannelOpen(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("channel closed before live runtime publication")
	default:
	}
}

func TestWaitForUsableIndexModelOpensAfterExternalIndexerCompletes(t *testing.T) {
	runtime := &web.Runtime{}
	runtime.EnableIndexGate()
	var calls atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ready := waitForUsableIndexModel(ctx, runtime, time.Millisecond, func(context.Context) (int64, error) {
		if calls.Add(1) < 3 {
			return 0, nil
		}
		return 1, nil
	})

	require.True(t, ready)
	require.True(t, runtime.IndexReady())
	require.GreaterOrEqual(t, calls.Load(), int32(3))
}

func TestWaitForUsableIndexModelSurvivesTransientCountFailure(t *testing.T) {
	runtime := &web.Runtime{}
	runtime.EnableIndexGate()
	var calls atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ready := waitForUsableIndexModel(ctx, runtime, time.Millisecond, func(context.Context) (int64, error) {
		if calls.Add(1) == 1 {
			return 0, errors.New("database busy")
		}
		return 2, nil
	})

	require.True(t, ready)
	require.True(t, runtime.IndexReady())
}

func TestWaitForUsableIndexModelStopsOnCancellation(t *testing.T) {
	runtime := &web.Runtime{}
	runtime.EnableIndexGate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ready := waitForUsableIndexModel(ctx, runtime, time.Millisecond, func(context.Context) (int64, error) {
		return 0, nil
	})

	require.False(t, ready)
	require.False(t, runtime.IndexReady())
}

func TestMarkUsableNoteModelOpensOnlyTheNoteReadGate(t *testing.T) {
	runtime := &web.Runtime{}
	runtime.EnableIndexGate()

	ready := markUsableNoteModel(context.Background(), runtime, func(context.Context) (int64, error) {
		return 3, nil
	})

	require.True(t, ready)
	require.True(t, runtime.NoteReadReady())
	require.False(t, runtime.IndexReady())
}

func TestMarkUsableNoteModelKeepsColdOrUnreadableStoresGated(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		count modelCountFunc
	}{
		{
			name: "cold",
			count: func(context.Context) (int64, error) {
				return 0, nil
			},
		},
		{
			name: "read failure",
			count: func(context.Context) (int64, error) {
				return 0, errors.New("database busy")
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			runtime := &web.Runtime{}
			runtime.EnableIndexGate()

			require.False(t, markUsableNoteModel(context.Background(), runtime, testCase.count))
			require.False(t, runtime.NoteReadReady())
			require.False(t, runtime.IndexReady())
		})
	}
}

func TestNoteModelPollOpensBeforeCompleteIndex(t *testing.T) {
	runtime := &web.Runtime{}
	runtime.EnableIndexGate()
	var calls atomic.Int32

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ready := waitForUsableNoteModel(ctx, runtime, time.Millisecond, func(context.Context) (int64, error) {
		if calls.Add(1) < 3 {
			return 0, nil
		}
		return 1, nil
	})

	require.True(t, ready)
	require.True(t, runtime.NoteReadReady())
	require.False(t, runtime.IndexReady())
	require.GreaterOrEqual(t, calls.Load(), int32(3))
}

func TestNoteModelPollStopsOnCancellation(t *testing.T) {
	runtime := &web.Runtime{}
	runtime.EnableIndexGate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ready := waitForUsableNoteModel(ctx, runtime, time.Millisecond, func(context.Context) (int64, error) {
		return 0, nil
	})

	require.False(t, ready)
	require.False(t, runtime.NoteReadReady())
}

func TestNoteModelPollBacksOffRepeatedMetadataScans(t *testing.T) {
	runtime := &web.Runtime{}
	runtime.EnableIndexGate()
	// The timeout is a watchdog; normal completion cancels after five scans.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var scans []time.Time

	ready := waitForUsableNoteModel(ctx, runtime, 5*time.Millisecond, func(context.Context) (int64, error) {
		scans = append(scans, time.Now())
		if len(scans) == 5 {
			cancel()
		}
		return 0, nil
	})

	require.False(t, ready)
	require.False(t, runtime.NoteReadReady())
	require.Len(t, scans, 5)
	// Scheduling can delay a scan, so assert minimum backoff between observed
	// scans instead of requiring a scan count within a wall-clock deadline.
	for i, minimum := range []time.Duration{5, 10, 20, 40} {
		require.GreaterOrEqual(t, scans[i+1].Sub(scans[i]), minimum*time.Millisecond,
			"polling should back off between scans %d and %d", i+1, i+2)
	}
}

func TestQueuedValidationWaitsForBootIndexAttempt(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := make(chan struct{})
	finish := make(chan struct{})
	coordinator := NewReadinessCoordinator(func(ctx context.Context, _ notemeta.Indexer, _ string, _ obsidian.VaultDefinition, _ bool) error {
		close(started)
		select {
		case <-finish:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	refresher := &recordingValidationRefresher{}
	coordinator.validation = refresher
	coordinator.BindLive(ctx, &bootstrap.LiveRuntime{})
	indexed := make(chan error, 1)
	go func() {
		indexed <- coordinator.BackgroundIndexer(ctx, notemeta.Indexer{}, "", obsidian.VaultDefinition{}, false)
	}()
	<-started
	coordinator.RequestValidationRefresh()
	require.Never(t, func() bool { return refresher.calls.Load() > 0 }, 2*validationRefreshDebounce, time.Millisecond,
		"validation must not publish partial projection state ahead of boot indexing")
	close(finish)
	require.NoError(t, <-indexed)
	require.Eventually(t, func() bool { return refresher.calls.Load() >= 1 }, time.Second, time.Millisecond)
}
