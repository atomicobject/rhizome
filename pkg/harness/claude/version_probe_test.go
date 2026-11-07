package claude

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

// A caller waiting on another caller's in-flight probe must still honor its
// own deadline, and the probe result must then be cached for everyone.
func TestVersionWaiterHonorsOwnDeadline(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	runner := &fakeRunner{run: func(context.Context, command.Spec) (command.Result, error) {
		calls.Add(1)
		<-release
		return command.Result{Stdout: "2.1.269"}, nil
	}}
	driver := &Driver{runner: runner}
	leader := make(chan error, 1)
	go func() {
		_, err := driver.checkedVersion(context.Background(), "fake-version")
		leader <- err
	}()
	require.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, time.Millisecond)

	follower := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := driver.checkedVersion(ctx, "fake-version")
		follower <- err
	}()
	select {
	case err := <-follower:
		require.ErrorIs(t, err, harness.ErrTimeout)
	case <-time.After(time.Second):
		close(release)
		t.Fatal("waiter ignored its own deadline while another probe ran")
	}

	close(release)
	require.NoError(t, <-leader)
	version, err := driver.checkedVersion(context.Background(), "fake-version")
	require.NoError(t, err)
	require.Equal(t, "2.1.269", version)
	require.Equal(t, int32(1), calls.Load(), "completed probe must stay cached")
}
