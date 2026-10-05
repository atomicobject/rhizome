package codeintel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunAdaptiveBatchWriter_FlushesOnIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan int, 1)
	errCh := make(chan error, 1)
	applied := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunAdaptiveBatchWriter(ctx, cancel, in, errCh, func(batch []int) error {
			applied <- len(batch)
			return nil
		})
	}()

	in <- 1
	select {
	case size := <-applied:
		require.Equal(t, 1, size)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for idle flush")
	}

	close(in)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not exit")
	}
	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}
}

func TestRunAdaptiveBatchWriter_FlushesAtBatchCap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan int, indexWriterBatchCap)
	errCh := make(chan error, 1)
	applied := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunAdaptiveBatchWriter(ctx, cancel, in, errCh, func(batch []int) error {
			applied <- len(batch)
			return nil
		})
	}()

	for i := 0; i < indexWriterBatchCap; i++ {
		in <- i
	}
	select {
	case size := <-applied:
		require.Equal(t, indexWriterBatchCap, size)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for batch-cap flush")
	}

	close(in)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not exit")
	}
}

func TestRunAdaptiveBatchWriter_ApplyErrorCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan int, indexWriterBatchCap)
	errCh := make(chan error, 1)
	done := make(chan struct{})
	wantErr := errors.New("boom")

	go func() {
		defer close(done)
		RunAdaptiveBatchWriter(ctx, cancel, in, errCh, func(batch []int) error {
			return wantErr
		})
	}()

	for i := 0; i < indexWriterBatchCap; i++ {
		in <- i
	}

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, wantErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for writer error")
	}

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("context was not canceled on apply error")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not exit")
	}
}
