package indexing

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestValidationProjectionLaneRetriesPreemptionAndReleasesLock(t *testing.T) {
	root := writeValidationProjectionVault(t)
	lockPath := obsidian.IndexLockPath(root)
	executor := lane.New(lane.Options{LockPath: lockPath, PriorityPoll: 10 * time.Millisecond})
	defer executor.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{})
	var attempts atomic.Int32
	var lockWasHeld atomic.Bool
	done := make(chan error, 1)
	request := ValidationProjectionRequest{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t), Target: ValidationProjectionLive, Lane: executor,
		BeforeMutation: func(ctx context.Context) error {
			release, acquired, err := indexlock.TryAcquire(lockPath)
			if err != nil {
				return err
			}
			if acquired {
				_ = release()
			} else {
				lockWasHeld.Store(true)
			}
			if attempts.Add(1) == 1 {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		},
	}
	go func() {
		result, err := RefreshValidationProjection(ctx, request)
		if result != nil {
			err = result.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	explicit, _, err := executor.Submit(ctx, lane.Request{Kind: lane.KindExplicitIndex})
	require.NoError(t, err)
	<-explicit.Done()
	require.NoError(t, <-done)
	require.Equal(t, int32(2), attempts.Load())
	require.True(t, lockWasHeld.Load(), "projection barrier must execute under the lane lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, release())
}

func TestValidationProjectionLaneCallerCancellationStopsWork(t *testing.T) {
	root := writeValidationProjectionVault(t)
	executor := lane.New(lane.Options{LockPath: obsidian.IndexLockPath(root)})
	defer executor.Close()
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	done := make(chan error, 1)
	request := ValidationProjectionRequest{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t), Target: ValidationProjectionLive, Lane: executor,
		BeforeMutation: func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() },
	}
	go func() { _, err := RefreshValidationProjection(ctx, request); done <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.False(t, executor.Status().Busy)
}

func TestValidationProjectionLaneCancelsQueuedRequestWithoutWaitingForUnrelatedJob(t *testing.T) {
	root := writeValidationProjectionVault(t)
	executor := lane.New(lane.Options{LockPath: obsidian.IndexLockPath(root)})
	defer executor.Close()
	blockerStarted := make(chan struct{})
	blocker, _, err := executor.Submit(t.Context(), lane.Request{
		Kind: lane.KindExplicitIndex,
		Run: func(ctx context.Context, _ lane.Reporter) error {
			close(blockerStarted)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	require.NoError(t, err)
	defer blocker.Cancel()
	<-blockerStarted
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := ValidationProjectionRequest{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t), Target: ValidationProjectionLive, Lane: executor,
	}
	done := make(chan error, 1)
	go func() { _, err := RefreshValidationProjection(ctx, request); done <- err }()
	require.Eventually(t, func() bool { return executor.Status().Queued == 1 }, time.Second, time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancelled validation waited for unrelated explicit indexing")
	}
	select {
	case <-blocker.Done():
		t.Fatal("validation cancellation interrupted the unrelated index job")
	default:
	}
	require.Zero(t, executor.Status().Queued)
}
