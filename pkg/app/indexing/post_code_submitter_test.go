package indexing

import (
	"context"
	"errors"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestAsyncCodeBatchSubmitterEnqueueDoesNotBlockOnSlowSubmit(t *testing.T) {
	t.Parallel()

	blocked := make(chan struct{}, 1)
	release := make(chan struct{})
	submitter := newAsyncCodeBatchSubmitter(context.Background(), func(context.Context, []codeanchor.CodeIndexWork) error {
		select {
		case blocked <- struct{}{}:
		default:
		}
		<-release
		return nil
	}, nil)
	t.Cleanup(func() {
		close(release)
		_ = submitter.Close()
	})

	require.NoError(t, submitter.Enqueue(context.Background(), []codeanchor.CodeIndexWork{{Path: "a.go"}}))
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("expected background submit to start")
	}

	started := time.Now()
	require.NoError(t, submitter.Enqueue(context.Background(), []codeanchor.CodeIndexWork{{Path: "b.go"}}))
	require.Less(t, time.Since(started), 100*time.Millisecond)
}

func TestAsyncCodeBatchSubmitterPreservesOrder(t *testing.T) {
	t.Parallel()

	var got []string
	submitter := newAsyncCodeBatchSubmitter(context.Background(), func(_ context.Context, batch []codeanchor.CodeIndexWork) error {
		got = append(got, batch[0].Path)
		return nil
	}, nil)

	require.NoError(t, submitter.Enqueue(context.Background(), []codeanchor.CodeIndexWork{{Path: "a.go"}}))
	require.NoError(t, submitter.Enqueue(context.Background(), []codeanchor.CodeIndexWork{{Path: "b.go"}}))
	require.NoError(t, submitter.Close())
	require.Equal(t, []string{"a.go", "b.go"}, got)
}

func TestAsyncCodeBatchSubmitterPropagatesSubmitErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	failed := make(chan error, 1)
	submitter := newAsyncCodeBatchSubmitter(context.Background(), func(_ context.Context, _ []codeanchor.CodeIndexWork) error {
		return want
	}, func(err error) {
		failed <- err
	})

	require.NoError(t, submitter.Enqueue(context.Background(), []codeanchor.CodeIndexWork{{Path: "a.go"}}))
	require.ErrorIs(t, submitter.Close(), want)
	select {
	case err := <-failed:
		require.ErrorIs(t, err, want)
	case <-time.After(time.Second):
		t.Fatal("expected fail callback")
	}
}
