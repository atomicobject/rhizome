package claude

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

func TestCanceledVersionProbeDoesNotPoisonCache(t *testing.T) {
	calls := 0
	runner := &fakeRunner{}
	driver := &Driver{runner: runner}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.run = func(ctx context.Context, _ command.Spec) (command.Result, error) {
		calls++
		if calls == 1 {
			cancel()
			return command.Result{}, ctx.Err()
		}
		return command.Result{Stdout: "2.1.269"}, nil
	}
	_, err := driver.checkedVersion(ctx, "fake-version")
	require.ErrorIs(t, err, harness.ErrTimeout)
	require.ErrorIs(t, err, context.Canceled)
	version, err := driver.checkedVersion(context.Background(), "fake-version")
	require.NoError(t, err)
	require.Equal(t, "2.1.269", version)
	require.Equal(t, 2, calls, "interrupted probe must be retried")
	version, err = driver.checkedVersion(context.Background(), "fake-version")
	require.NoError(t, err)
	require.Equal(t, "2.1.269", version)
	require.Equal(t, 2, calls, "healthy version must remain cached")
}

func TestVersionProbeCachesCompletedFailure(t *testing.T) {
	runner := &fakeRunner{results: []command.Result{{ExitCode: 7, Stderr: "version unavailable"}}}
	driver := &Driver{runner: runner}
	for i := 0; i < 2; i++ {
		_, err := driver.checkedVersion(context.Background(), "fake-version")
		require.ErrorIs(t, err, harness.ErrCommandFailed)
		require.Contains(t, err.Error(), "version unavailable")
	}
	require.Len(t, runner.commands, 1)
}
