package lane

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecutorDoesNotJoinCancelledQueuedJob(t *testing.T) {
	for _, kind := range []Kind{KindExplicitIndex, KindWatcherBatch} {
		t.Run(string(kind), func(t *testing.T) {
			executor := newTestLane(t, nil)
			release := make(chan struct{})
			defer close(release)
			started := make(chan struct{})
			_, _, err := executor.Submit(context.Background(), Request{Kind: KindBootCatchUp, Run: func(context.Context, Reporter) error {
				close(started)
				<-release
				return nil
			}})
			require.NoError(t, err)
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("blocking job did not start")
			}

			cancelled, _, err := executor.Submit(context.Background(), Request{Kind: kind, Coalesce: true, Run: func(context.Context, Reporter) error {
				t.Error("cancelled queued job must not run")
				return nil
			}})
			require.NoError(t, err)
			cancelled.Cancel()
			select {
			case <-cancelled.Done():
			case <-time.After(time.Second):
				t.Fatal("queued cancellation waited for unrelated running work")
			}
			require.ErrorIs(t, cancelled.Err(), context.Canceled)

			ran := make(chan struct{})
			replacement, joined, err := executor.Submit(context.Background(), Request{Kind: kind, Coalesce: true, Run: func(context.Context, Reporter) error {
				close(ran)
				return nil
			}})
			require.NoError(t, err)
			require.False(t, joined, "new work must not join a cancelled queued job")
			require.NotEqual(t, cancelled.ID(), replacement.ID())

			// Let the blocker finish without closing its channel twice on failure.
			release <- struct{}{}
			select {
			case <-replacement.Done():
			case <-time.After(time.Second):
				t.Fatal("replacement job did not finish")
			}
			require.NoError(t, replacement.Err())
			require.ErrorIs(t, cancelled.Err(), context.Canceled)
			select {
			case <-ran:
			default:
				t.Fatal("replacement job did not run")
			}
		})
	}
}
