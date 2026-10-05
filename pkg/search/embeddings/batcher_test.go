package embeddings

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchExecutorFixedBatchSize(t *testing.T) {
	var batchCalls int32
	var batchSizes []int

	fn := func(ctx context.Context, texts []string) ([]Embedding, error) {
		atomic.AddInt32(&batchCalls, 1)
		batchSizes = append(batchSizes, len(texts))
		result := make([]Embedding, len(texts))
		for i := range result {
			result[i] = Embedding{float32(texts[i][0])}
		}
		return result, nil
	}

	executor := &BatchExecutor{
		BatchSize:      3,
		MaxConcurrency: 1,
	}

	// 7 texts with batch size 3 = 3 batches (3+3+1)
	texts := []string{"a", "b", "c", "d", "e", "f", "g"}
	results, err := executor.Execute(context.Background(), texts, fn)

	require.NoError(t, err)
	require.Len(t, results, 7)
	require.Equal(t, int32(3), atomic.LoadInt32(&batchCalls))
	require.Equal(t, []int{3, 3, 1}, batchSizes)
	require.Equal(t, []Embedding{{'a'}, {'b'}, {'c'}, {'d'}, {'e'}, {'f'}, {'g'}}, results)

	t.Run("single batch", func(t *testing.T) {
		calls := 0
		one := &BatchExecutor{BatchSize: 10, MaxConcurrency: 5}
		got, err := one.Execute(context.Background(), texts[:3], func(_ context.Context, batch []string) ([]Embedding, error) {
			calls++
			require.Equal(t, texts[:3], batch)
			return []Embedding{{'a'}, {'b'}, {'c'}}, nil
		})
		require.NoError(t, err)
		require.Equal(t, 1, calls)
		require.Equal(t, []Embedding{{'a'}, {'b'}, {'c'}}, got)
	})
}

func TestBatchExecutorDynamicBatchSize(t *testing.T) {
	var batchCalls int32
	var batchSizes []int

	fn := func(ctx context.Context, texts []string) ([]Embedding, error) {
		atomic.AddInt32(&batchCalls, 1)
		batchSizes = append(batchSizes, len(texts))
		result := make([]Embedding, len(texts))
		for i := range result {
			result[i] = Embedding{float32(i)}
		}
		return result, nil
	}

	// Dynamic batch size: alternates between 2 and 3
	callNum := 0
	executor := &BatchExecutor{
		MaxConcurrency: 1,
		NextBatchSizeFn: func(remaining []string) int {
			callNum++
			if callNum%2 == 1 {
				return 2
			}
			return 3
		},
	}

	// 10 texts: batches of 2, 3, 2, 3 = 10
	texts := make([]string, 10)
	for i := range texts {
		texts[i] = "text"
	}
	results, err := executor.Execute(context.Background(), texts, fn)

	require.NoError(t, err)
	require.Len(t, results, 10)
	require.Equal(t, int32(4), atomic.LoadInt32(&batchCalls))
	require.Equal(t, []int{2, 3, 2, 3}, batchSizes)
}

func TestBatchExecutorConcurrency(t *testing.T) {
	var maxInflight int32
	var inflight int32
	started := make(chan struct{}, 5)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)

	fn := func(ctx context.Context, texts []string) ([]Embedding, error) {
		cur := atomic.AddInt32(&inflight, 1)
		defer atomic.AddInt32(&inflight, -1)
		started <- struct{}{}
		<-release

		// Track max concurrency
		for {
			max := atomic.LoadInt32(&maxInflight)
			if cur <= max {
				break
			}
			if atomic.CompareAndSwapInt32(&maxInflight, max, cur) {
				break
			}
		}

		result := make([]Embedding, len(texts))
		for i := range result {
			result[i] = Embedding{float32(texts[i][0])}
		}
		return result, nil
	}

	executor := &BatchExecutor{
		BatchSize:      2,
		MaxConcurrency: 3,
	}

	// 10 texts with batch size 2 = 5 batches, max 3 concurrent
	texts := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	type outcome struct {
		results []Embedding
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := executor.Execute(context.Background(), texts, fn)
		done <- outcome{results, err}
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("three batches never started")
		}
	}
	select {
	case <-started:
		t.Fatal("fourth batch started before release")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	var got outcome
	select {
	case got = <-done:
	case <-time.After(time.Second):
		t.Fatal("batches did not finish")
	}

	require.NoError(t, got.err)
	require.Equal(t, []Embedding{{'a'}, {'b'}, {'c'}, {'d'}, {'e'}, {'f'}, {'g'}, {'h'}, {'i'}, {'j'}}, got.results)
	require.Equal(t, int32(3), atomic.LoadInt32(&maxInflight))
}

func TestBatchExecutorErrorCancelsRemaining(t *testing.T) {
	var batchCalls int32
	expectedErr := errors.New("test error")

	fn := func(ctx context.Context, texts []string) ([]Embedding, error) {
		call := atomic.AddInt32(&batchCalls, 1)
		if call == 2 {
			return nil, expectedErr
		}
		result := make([]Embedding, len(texts))
		for i := range result {
			result[i] = Embedding{float32(i)}
		}
		return result, nil
	}

	executor := &BatchExecutor{
		BatchSize:      1,
		MaxConcurrency: 1, // Sequential so error happens predictably
	}

	texts := []string{"a", "b", "c", "d", "e"}
	_, err := executor.Execute(context.Background(), texts, fn)

	require.Error(t, err)
	require.Equal(t, expectedErr, err)
	// Should have stopped after the error (2 calls total for sequential)
	require.Equal(t, int32(2), atomic.LoadInt32(&batchCalls))
}

func TestBatchExecutorEmptyInput(t *testing.T) {
	fn := func(ctx context.Context, texts []string) ([]Embedding, error) {
		t.Fatal("should not be called for empty input")
		return nil, nil
	}

	executor := &BatchExecutor{
		BatchSize:      3,
		MaxConcurrency: 2,
	}

	results, err := executor.Execute(context.Background(), nil, fn)
	require.NoError(t, err)
	require.Nil(t, results)

	results, err = executor.Execute(context.Background(), []string{}, fn)
	require.NoError(t, err)
	require.Nil(t, results)
}

func TestBatchExecutorTokenAwareBatching(t *testing.T) {
	for _, tc := range []struct {
		name, prefix      string
		batchSize, tokens int
		want              []int
	}{
		{"token budget", strings.Repeat("x", 99), 1000, 100, []int{4, 4, 2}},
		{"item cap", "", 3, 1000000, []int{3, 3, 3, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sizes []int
			fn := func(_ context.Context, batch []string) ([]Embedding, error) {
				sizes = append(sizes, len(batch))
				vectors := make([]Embedding, len(batch))
				for i, text := range batch {
					vectors[i] = Embedding{float32(text[len(text)-1])}
				}
				return vectors, nil
			}
			texts := make([]string, 10)
			want := make([]Embedding, 10)
			for i := range texts {
				texts[i] = tc.prefix + string(rune('a'+i))
				want[i] = Embedding{float32('a' + i)}
			}
			executor := &BatchExecutor{BatchSize: tc.batchSize, MaxTokensPerBatch: tc.tokens, MaxConcurrency: 1}
			got, err := executor.Execute(context.Background(), texts, fn)
			require.NoError(t, err)
			require.Equal(t, tc.want, sizes)
			require.Equal(t, want, got)
		})
	}
}
