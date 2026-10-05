package semantic

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

type controlledQueryProvider struct {
	calls atomic.Int32
	embed func(context.Context, string, int32) (embeddings.Embedding, error)
}

func (p *controlledQueryProvider) Dimensions() int { return 4 }
func (p *controlledQueryProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	vector, err := p.embed(ctx, texts[0], p.calls.Add(1))
	return []embeddings.Embedding{vector}, err
}

// Done observes when a caller waits, without a callback in production code.
type observedQueryContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *observedQueryContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

type queryEmbeddingReply struct {
	vectors QueryEmbeddings
	err     error
}

type queryTimingCollector struct {
	mu     sync.Mutex
	events map[string]TimingEvent
}

func (c *queryTimingCollector) Add(event TimingEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events[event.Name] = event
}

func startQueryEmbedding(e *QueryEmbedder, ctx context.Context, text string) <-chan queryEmbeddingReply {
	done := make(chan queryEmbeddingReply, 1)
	go func() {
		vectors, err := e.Embed(ctx, text)
		done <- queryEmbeddingReply{vectors, err}
	}()
	return done
}

func receiveQueryEmbedding(t *testing.T, done <-chan queryEmbeddingReply) queryEmbeddingReply {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("query embedding did not settle")
		return queryEmbeddingReply{}
	}
}

func waitForQueryFollower(t *testing.T, ctx *observedQueryContext) {
	t.Helper()
	select {
	case <-ctx.waiting:
	case <-time.After(time.Second):
		t.Fatal("the query follower did not join the pending attempt")
	}
}

func TestQueryEmbeddingMemoSharesConcurrentVectorsAndIsolatesCopies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	ctx, memo := WithQueryEmbeddingMemo(ctx, nil)
	started, release := make(chan struct{}), make(chan struct{})
	providerVector := embeddings.Embedding{1, 2, 3, 4}
	provider := &controlledQueryProvider{embed: func(ctx context.Context, _ string, call int32) (embeddings.Embedding, error) {
		if call == 1 {
			close(started)
		}
		select {
		case <-release:
			return providerVector, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	embedder := QueryEmbedder{CodeProvider: provider, NoteProvider: provider}
	leader := startQueryEmbedding(&embedder, ctx, " query ")
	<-started
	followerContext := &observedQueryContext{Context: ctx, waiting: make(chan struct{})}
	follower := startQueryEmbedding(&embedder, followerContext, "query")
	waitForQueryFollower(t, followerContext)
	close(release)
	first, second := receiveQueryEmbedding(t, leader), receiveQueryEmbedding(t, follower)
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, int32(1), provider.calls.Load())
	require.Equal(t, first.vectors, second.vectors)
	require.Equal(t, QueryEmbeddings{Code: embeddings.Embedding{1, 2, 3, 4}, Note: embeddings.Embedding{1, 2, 3, 4}}, first.vectors)
	first.vectors.Code[0] = 99
	require.Equal(t, float32(1), first.vectors.Note[0])
	require.Equal(t, float32(1), second.vectors.Code[0])
	providerVector[0] = 88
	snapshot := memo.Snapshot()
	require.Equal(t, float32(1), snapshot[0].Code[0])
	snapshot[0].Code[0] = 77
	require.Equal(t, float32(1), memo.Snapshot()[0].Code[0])
	seed := memo.Snapshot()
	continued, _ := WithQueryEmbeddingMemo(ctx, seed)
	seed[0].Note[0] = 66
	third, err := embedder.Embed(continued, "query")
	require.NoError(t, err)
	require.Equal(t, float32(1), third.Note[0])
	require.Equal(t, int32(1), provider.calls.Load(), "seeded continuations never call the provider")
}

func TestQueryEmbeddingMemoFollowerCancellationPreservesLeader(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	ctx, memo := WithQueryEmbeddingMemo(ctx, nil)
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	provider := &controlledQueryProvider{embed: func(ctx context.Context, _ string, _ int32) (embeddings.Embedding, error) {
		started <- ctx
		select {
		case <-release:
			return embeddings.Embedding{1, 0, 0, 0}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	embedder := QueryEmbedder{CodeProvider: provider, NoteProvider: provider}
	leader := startQueryEmbedding(&embedder, ctx, "query")
	providerContext := <-started
	followerBase, cancelFollower := context.WithCancel(ctx)
	followerContext := &observedQueryContext{Context: followerBase, waiting: make(chan struct{})}
	follower := startQueryEmbedding(&embedder, followerContext, "query")
	waitForQueryFollower(t, followerContext)
	cancelFollower()
	require.ErrorIs(t, receiveQueryEmbedding(t, follower).err, context.Canceled)
	require.NoError(t, providerContext.Err())
	close(release)
	require.NoError(t, receiveQueryEmbedding(t, leader).err)
	require.Equal(t, int32(1), provider.calls.Load())
	require.Len(t, memo.Snapshot(), 1)
}

func TestQueryEmbeddingMemoCanceledLeaderAllowsLiveFollowerRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	ctx, memo := WithQueryEmbeddingMemo(ctx, nil)
	started := make(chan struct{})
	provider := &controlledQueryProvider{embed: func(ctx context.Context, _ string, call int32) (embeddings.Embedding, error) {
		if call == 1 {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return embeddings.Embedding{2, 0, 0, 0}, nil
	}}
	embedder := QueryEmbedder{CodeProvider: provider, NoteProvider: provider}
	leaderContext, cancelLeader := context.WithCancel(ctx)
	leader := startQueryEmbedding(&embedder, leaderContext, "query")
	<-started
	followerContext := &observedQueryContext{Context: ctx, waiting: make(chan struct{})}
	follower := startQueryEmbedding(&embedder, followerContext, "query")
	waitForQueryFollower(t, followerContext)
	cancelLeader()
	require.ErrorIs(t, receiveQueryEmbedding(t, leader).err, context.Canceled)
	result := receiveQueryEmbedding(t, follower)
	require.NoError(t, result.err)
	require.Equal(t, embeddings.Embedding{2, 0, 0, 0}, result.vectors.Code)
	require.Equal(t, int32(2), provider.calls.Load())
	require.Equal(t, result.vectors.Code, memo.Snapshot()[0].Code)
}

func TestQueryEmbeddingMemoDoesNotRetryProviderFailureForParticipants(t *testing.T) {
	for _, failure := range []error{errors.New("synthetic provider outage"), context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			t.Cleanup(cancel)
			ctx, memo := WithQueryEmbeddingMemo(ctx, nil)
			started, release := make(chan struct{}), make(chan struct{})
			provider := &controlledQueryProvider{embed: func(ctx context.Context, _ string, call int32) (embeddings.Embedding, error) {
				if call != 1 {
					return embeddings.Embedding{2, 0, 0, 0}, nil
				}
				close(started)
				select {
				case <-release:
					return nil, failure
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
			embedder := QueryEmbedder{CodeProvider: provider, NoteProvider: provider}
			leader := startQueryEmbedding(&embedder, ctx, "query")
			<-started
			followerContext := &observedQueryContext{Context: ctx, waiting: make(chan struct{})}
			follower := startQueryEmbedding(&embedder, followerContext, "query")
			waitForQueryFollower(t, followerContext)
			close(release)
			require.ErrorIs(t, receiveQueryEmbedding(t, leader).err, failure)
			require.ErrorIs(t, receiveQueryEmbedding(t, follower).err, failure)
			require.Equal(t, int32(1), provider.calls.Load())
			require.NoError(t, ctx.Err(), "provider cancellation is not caller cancellation")
			require.Empty(t, memo.Snapshot())
			result, err := embedder.Embed(ctx, "query")
			require.NoError(t, err)
			require.Equal(t, embeddings.Embedding{2, 0, 0, 0}, result.Code)
			require.Equal(t, int32(2), provider.calls.Load(), "a later call may retry a failure")
		})
	}
}

func TestQueryEmbeddingMemoDifferentTextsRunConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	ctx, memo := WithQueryEmbeddingMemo(ctx, nil)
	started := make(chan string, 2)
	release := make(chan struct{})
	provider := &controlledQueryProvider{embed: func(ctx context.Context, text string, _ int32) (embeddings.Embedding, error) {
		started <- text
		select {
		case <-release:
			if text == "alpha" {
				return embeddings.Embedding{1, 0, 0, 0}, nil
			}
			return embeddings.Embedding{0, 1, 0, 0}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	embedder := QueryEmbedder{NoteProvider: provider}
	alpha := startQueryEmbedding(&embedder, ctx, "alpha")
	beta := startQueryEmbedding(&embedder, ctx, "beta")
	require.ElementsMatch(t, []string{"alpha", "beta"}, []string{<-started, <-started})
	close(release)
	a, b := receiveQueryEmbedding(t, alpha), receiveQueryEmbedding(t, beta)
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	require.Equal(t, embeddings.Embedding{1, 0, 0, 0}, a.vectors.Note)
	require.Equal(t, embeddings.Embedding{0, 1, 0, 0}, b.vectors.Note)
	require.Len(t, memo.Snapshot(), 2)
}

func TestQueryEmbeddingMemoNeverCachesPartialProviderPair(t *testing.T) {
	ctx, memo := WithQueryEmbeddingMemo(context.Background(), nil)
	codeReady := make(chan struct{})
	failure := errors.New("synthetic note provider outage")
	code := &controlledQueryProvider{embed: func(_ context.Context, _ string, call int32) (embeddings.Embedding, error) {
		if call == 1 {
			close(codeReady)
		}
		return embeddings.Embedding{1, 0, 0, 0}, nil
	}}
	note := &controlledQueryProvider{embed: func(_ context.Context, _ string, call int32) (embeddings.Embedding, error) {
		<-codeReady
		if call == 1 {
			return nil, failure
		}
		return embeddings.Embedding{0, 1, 0, 0}, nil
	}}
	embedder := QueryEmbedder{CodeProvider: code, NoteProvider: note}
	result, err := embedder.Embed(ctx, "query")
	require.ErrorIs(t, err, failure)
	require.Equal(t, QueryEmbeddings{}, result)
	require.Empty(t, memo.Snapshot())
	result, err = embedder.Embed(ctx, "query")
	require.NoError(t, err)
	require.Equal(t, QueryEmbeddings{Code: embeddings.Embedding{1, 0, 0, 0}, Note: embeddings.Embedding{0, 1, 0, 0}}, result)
	require.Equal(t, int32(2), code.calls.Load())
	require.Equal(t, int32(2), note.calls.Load())
}

func TestQueryEmbeddingMemoExpiredStageAllowsLiveFollowerRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	ctx, memo := WithQueryEmbeddingMemo(ctx, nil)
	started := make(chan struct{})
	provider := &controlledQueryProvider{embed: func(ctx context.Context, _ string, call int32) (embeddings.Embedding, error) {
		if call == 1 {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return embeddings.Embedding{2, 0, 0, 0}, nil
	}}
	embedder := QueryEmbedder{CodeProvider: provider, NoteProvider: provider}
	stage, cancelStage := context.WithTimeout(ctx, time.Second)
	defer cancelStage()
	leader := startQueryEmbedding(&embedder, stage, "query")
	<-started
	follower := startQueryEmbedding(&embedder, ctx, "query")
	require.ErrorIs(t, receiveQueryEmbedding(t, leader).err, context.DeadlineExceeded)
	result := receiveQueryEmbedding(t, follower)
	require.NoError(t, result.err)
	require.Equal(t, embeddings.Embedding{2, 0, 0, 0}, result.vectors.Code)
	require.Equal(t, result.vectors.Code, memo.Snapshot()[0].Code)
}

func TestQueryEmbedderPreservesCausalFailureAndProviderTimings(t *testing.T) {
	started := make(chan struct{})
	failure := errors.New("synthetic note provider outage")
	code := &controlledQueryProvider{embed: func(ctx context.Context, _ string, _ int32) (embeddings.Embedding, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	note := &controlledQueryProvider{embed: func(ctx context.Context, _ string, _ int32) (embeddings.Embedding, error) {
		select {
		case <-started:
			return nil, failure
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	timings := &queryTimingCollector{events: make(map[string]TimingEvent)}
	ctx = WithTimingSink(ctx, timings)
	embedder := QueryEmbedder{CodeProvider: code, NoteProvider: note}
	_, err := embedder.Embed(ctx, "query")
	require.ErrorIs(t, err, failure)
	require.NotErrorIs(t, err, context.Canceled)
	require.NoError(t, ctx.Err())
	require.Len(t, timings.events, 2)
	require.Equal(t, "error", timings.events["semantic.embed.note"].Status)
	require.Equal(t, failure.Error(), timings.events["semantic.embed.note"].Err)
	require.Equal(t, "canceled", timings.events["semantic.embed.code"].Status)
}

func TestQueryEmbeddingMemoDiscardsLateSuccessAfterLeaderCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	ctx, memo := WithQueryEmbeddingMemo(ctx, nil)
	started, release := make(chan struct{}), make(chan struct{})
	provider := &controlledQueryProvider{embed: func(_ context.Context, _ string, call int32) (embeddings.Embedding, error) {
		if call == 1 {
			close(started)
			<-release // Model a transport that returns success after cancellation.
		}
		return embeddings.Embedding{float32(call), 0, 0, 0}, nil
	}}
	embedder := QueryEmbedder{NoteProvider: provider}
	leaderContext, cancelLeader := context.WithCancel(ctx)
	leader := startQueryEmbedding(&embedder, leaderContext, "query")
	<-started
	followerContext := &observedQueryContext{Context: ctx, waiting: make(chan struct{})}
	follower := startQueryEmbedding(&embedder, followerContext, "query")
	waitForQueryFollower(t, followerContext)
	cancelLeader()
	close(release)
	require.ErrorIs(t, receiveQueryEmbedding(t, leader).err, context.Canceled)
	result := receiveQueryEmbedding(t, follower)
	require.NoError(t, result.err)
	require.Equal(t, embeddings.Embedding{2, 0, 0, 0}, result.vectors.Note)
	require.Equal(t, result.vectors.Note, memo.Snapshot()[0].Note)
}

func TestQueryEmbeddingMemoRequestCancellationDrainsWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ctx, memo := WithQueryEmbeddingMemo(ctx, nil)
	started := make(chan struct{})
	var active atomic.Int32
	provider := &controlledQueryProvider{embed: func(ctx context.Context, _ string, call int32) (embeddings.Embedding, error) {
		active.Add(1)
		defer active.Add(-1)
		if call == 1 {
			close(started)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	embedder := QueryEmbedder{NoteProvider: provider}
	leader := startQueryEmbedding(&embedder, ctx, "query")
	<-started
	followerContext := &observedQueryContext{Context: ctx, waiting: make(chan struct{})}
	follower := startQueryEmbedding(&embedder, followerContext, "query")
	waitForQueryFollower(t, followerContext)
	cancel()
	require.ErrorIs(t, receiveQueryEmbedding(t, leader).err, context.Canceled)
	require.ErrorIs(t, receiveQueryEmbedding(t, follower).err, context.Canceled)
	require.Zero(t, active.Load())
	require.Empty(t, memo.Snapshot())
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	expired, _ = WithQueryEmbeddingMemo(expired, nil)
	_, err := embedder.Embed(expired, "query")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, int32(1), provider.calls.Load())
}
