package agentapi

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCodeResourcesShareReadsAndIsolateRuntime(t *testing.T) {
	resources := NewCodeResources()
	release, err := resources.Acquire(t.Context(), "code_symbol")
	require.NoError(t, err)
	other, err := resources.Acquire(t.Context(), "code_references")
	require.NoError(t, err)
	other()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = resources.Acquire(ctx, "files")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	// Cancellation of the waiting writer must let new readers proceed.
	other, err = resources.Acquire(t.Context(), "code_symbol")
	require.NoError(t, err)
	other()
	release()
	release, err = resources.Acquire(t.Context(), "files")
	require.NoError(t, err)
	// Provider and path checking don't touch the live vault runtime.
	for _, name := range []string{"evaluate", "evaluate_batch", "check_paths"} {
		done, err := resources.Acquire(t.Context(), name)
		require.NoError(t, err)
		done()
	}
	ctx2, cancel2 := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel2()
	_, err = resources.Acquire(ctx2, "code_symbol")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	release()
}
