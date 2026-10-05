package serve

import (
	"context"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/web"
)

type pendingFailureRefresher struct{ called chan struct{} }

func (r *pendingFailureRefresher) Refresh(context.Context) error {
	r.called <- struct{}{}
	return nil
}

func TestPendingRefreshCanceledAfterReconcileFailure(t *testing.T) {
	c := NewReadinessCoordinator(nil)
	c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
	r := &pendingFailureRefresher{called: make(chan struct{}, 1)}
	c.live = &bootstrap.LiveRuntime{}
	c.validation = r
	// Deliver both events before starting the worker. This establishes the
	// offending order without depending on scheduler timing or a sleep.
	sink := c.GlobalEventSink()
	sink(web.GlobalEventValidationInvalidated, nil)
	sink(web.GlobalEventValidationInvalidated, map[string]any{
		"reason": web.GlobalEventReasonReconcileFailed,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.runValidationRefreshRequests(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case <-r.called:
		t.Fatal("validation refreshed after reconcile-failed: earlier queued refresh survived invalidation")
	case <-time.After(time.Second):
	}
}

func TestValidationRequestsRecoverAfterFailedReconcile(t *testing.T) {
	for _, trigger := range []string{"manual save", "successful event"} {
		t.Run(trigger, func(t *testing.T) {
			c := NewReadinessCoordinator(nil)
			c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
			r := &pendingFailureRefresher{called: make(chan struct{}, 2)}
			c.live = &bootstrap.LiveRuntime{}
			c.validation = r
			c.RequestValidationRefresh()
			c.GlobalEventSink()(web.GlobalEventValidationInvalidated, map[string]any{"reason": web.GlobalEventReasonReconcileFailed})
			if trigger == "manual save" {
				c.RequestValidationRefresh()
			} else {
				c.GlobalEventSink()(web.GlobalEventValidationInvalidated, nil)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() { defer close(done); c.runValidationRefreshRequests(ctx) }()
			defer func() { cancel(); <-done }()
			select {
			case <-r.called:
			case <-time.After(time.Second):
				t.Fatal("new request did not recover validation")
			}
		})
	}
}

func TestBackgroundValidationAdmissionPrecedesIndexWork(t *testing.T) {
	indexStarted, finishIndex := make(chan struct{}), make(chan struct{})
	c := NewReadinessCoordinator(func(context.Context, notemeta.Indexer, string, obsidian.VaultDefinition, bool) error {
		close(indexStarted)
		<-finishIndex
		return nil
	})
	c.live = &bootstrap.LiveRuntime{}
	close(c.livePublished)
	r := &recordingValidationRefresher{}
	c.validation = r
	done := make(chan error, 1)
	go func() {
		done <- c.BackgroundIndexer(t.Context(), notemeta.Indexer{}, "", obsidian.VaultDefinition{}, false)
	}()
	<-indexStarted
	c.GlobalEventSink()(web.GlobalEventValidationInvalidated, map[string]any{"reason": web.GlobalEventReasonReconcileFailed})
	close(finishIndex)
	require.NoError(t, <-done)
	require.Zero(t, r.calls.Load(), "boot work admitted before failure must not refresh afterward")
}

func TestValidationInvalidationFencesStoreBeforeServerBinding(t *testing.T) {
	live := validationInvalidationRuntime(t)
	c := NewReadinessCoordinator(nil)
	c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
	c.live = live
	store := live.IntelStore()
	ctx := t.Context()
	publishedGeneration, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	ok, err := store.PublishValidationSnapshot(ctx, semdb.ValidationSnapshot{
		VaultIdentity: "test", Generation: publishedGeneration, Completion: semdb.ValidationCompletionComplete,
	})
	require.NoError(t, err)
	require.True(t, ok)
	running, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	c.GlobalEventSink()(web.GlobalEventValidationInvalidated, map[string]any{"reason": web.GlobalEventReasonReconcileFailed})
	read, err := store.GetValidationStateSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, running+1, read.State.Generation, "invalidation has exactly one durable owner")
	require.Equal(t, publishedGeneration, read.State.PublishedGeneration)
	require.Equal(t, semdb.ValidationStatusOK, read.State.Status)
	require.Equal(t, "validation inputs changed", read.Snapshot.StaleReason)
}

func TestBridgeValidationAdmissionPrecedesStartupWaits(t *testing.T) {
	live := validationInvalidationRuntime(t)
	c := NewReadinessCoordinator(nil)
	c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
	c.live = live
	c.webRuntime = &web.Runtime{Live: live}
	r := &recordingValidationRefresher{}
	c.validation = r
	probe, resume := make(chan struct{}), make(chan struct{})
	c.usableNoteCount = func(context.Context, *bootstrap.LiveRuntime) (int64, error) {
		close(probe)
		<-resume
		return 1, nil
	}
	c.completeModelCount = func(context.Context, *bootstrap.LiveRuntime) (int64, error) { return 1, nil }
	done := make(chan struct{})
	go func() { defer close(done); c.Bridge(t.Context()) }()
	<-probe
	c.GlobalEventSink()(web.GlobalEventValidationInvalidated, map[string]any{"reason": web.GlobalEventReasonReconcileFailed})
	close(resume)
	<-done
	require.Zero(t, r.calls.Load(), "startup validation admitted before failure must not refresh afterward")
}

func validationInvalidationRuntime(t *testing.T) *bootstrap.LiveRuntime {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: ['**/*.md']\n"), 0o644))
	live, err := bootstrap.NewLiveRuntime(t.Context(), bootstrap.LiveOptions{
		VaultName: root, DisableLeaderWork: true, DisableWatchHub: true, SkipCacheWarmup: true, DisableSessionStore: true,
		Requirements: bootstrap.RequireRuntimeCapabilities(bootstrap.RuntimeCapabilityCodeIndex),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, live.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, live.WaitForCodeIndex(ctx))
	require.NotNil(t, live.IntelStore())
	return live
}

type admissionGateRefresher struct {
	entered  chan struct{}
	proceed  chan struct{}
	finished chan struct{}
	delegate validationRefresher
}

func (r *admissionGateRefresher) Refresh(ctx context.Context) error {
	defer close(r.finished)
	close(r.entered)
	<-r.proceed
	return r.delegate.Refresh(ctx)
}

func TestCanceledRequestCannotAllocateGenerationAfterInvalidation(t *testing.T) {
	live := validationInvalidationRuntime(t)
	c := NewReadinessCoordinator(nil)
	c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
	c.live = live
	store := live.IntelStore()
	generation, err := store.SetValidationRunning(t.Context())
	require.NoError(t, err)
	var runs atomic.Int32
	actual := validationproduct.NewRefreshCoordinator(validationproduct.RefreshCoordinatorOptions{
		Store: store, VaultDef: live.VaultDef,
		Run: func(context.Context) (validationproduct.AuthoritativeRun, error) {
			runs.Add(1)
			return validationproduct.AuthoritativeRun{}, nil
		},
	})
	gate := &admissionGateRefresher{entered: make(chan struct{}), proceed: make(chan struct{}), finished: make(chan struct{}), delegate: actual}
	c.validation = gate
	done := make(chan struct{})
	workerCtx, stopWorker := context.WithCancel(t.Context())
	defer stopWorker()
	go func() { defer close(done); c.runValidationRefreshRequests(workerCtx) }()
	c.RequestValidationRefresh()
	<-gate.entered
	c.GlobalEventSink()(web.GlobalEventValidationInvalidated, map[string]any{"reason": web.GlobalEventReasonReconcileFailed})
	close(gate.proceed)
	<-gate.finished
	stopWorker()
	<-done
	read, err := store.GetValidationStateSnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, generation+1, read.State.Generation, "canceled admission must not allocate a generation after the fence")
	require.Zero(t, runs.Load())
	require.Equal(t, semdb.ValidationStatusNeverRan, read.State.Status)
}
