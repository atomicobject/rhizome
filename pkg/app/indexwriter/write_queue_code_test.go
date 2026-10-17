package indexwriter

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueuedWriterUsesEmbedFlushPolicyForSingleAndBatchSubmissions(t *testing.T) {
	t.Parallel()
	for _, batchSubmit := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%t", batchSubmit), func(t *testing.T) {
			t.Parallel()
			var flushes []int
			cfg := defaultConfig()
			cfg.DefaultPolicy.Idle = time.Hour
			cfg.ItemEmbed = flushPolicy{Rows: 3, Bytes: 1 << 20, Idle: time.Hour}
			cfg.PollInterval = time.Hour
			q := newWithConfig(context.Background(), Handlers{ApplyCodeItemEmbeddingBatch: func(_ context.Context, batch []codeindex.ItemEmbeddingUpsert) error {
				flushes = append(flushes, len(batch))
				return nil
			}}, cfg)
			items := make([]codeindex.ItemEmbeddingUpsert, 5)
			for i := range items {
				items[i] = codeindex.ItemEmbeddingUpsert{AnchorID: codeindex.AnchorID(fmt.Sprintf("a%d", i)), Hash: "h", Embedding: embeddings.Embedding{1}}
			}
			if batchSubmit {
				require.NoError(t, q.SubmitCodeItemEmbeddingBatch(context.Background(), items[:3]))
				require.NoError(t, q.SubmitCodeItemEmbeddingBatch(context.Background(), items[3:]))
			} else {
				for _, item := range items {
					require.NoError(t, q.SubmitCodeItemEmbedding(context.Background(), item))
				}
			}
			require.NoError(t, q.Close())
			require.Equal(t, []int{3, 2}, flushes)
		})
	}
}

func TestQueuedWriterInvokesPostCodeBatchCallback(t *testing.T) {
	t.Parallel()

	called := make(chan []string, 1)
	q := New(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			return nil
		},
	})
	q.SetAfterCodeIndexBatch(func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
		paths := make([]string, 0, len(batch))
		for _, work := range batch {
			paths = append(paths, work.Path)
		}
		called <- paths
		return nil
	})

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "a.go"}))
	require.NoError(t, q.Close())
	require.Equal(t, []string{"a.go"}, <-called)
}

func TestQueuedWriterUsesCodeIndexFlushPolicyForCodeWork(t *testing.T) {
	t.Parallel()

	flushes := make(chan int, 2)
	q := newWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			flushes <- len(batch)
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 3, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	for i := 0; i < 5; i++ {
		require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{
			Path: "file.go",
			Summary: codeanchor.FileSummary{
				FilePath: "file.go",
			},
		}))
	}
	require.NoError(t, q.Close())

	first := <-flushes
	second := <-flushes
	require.Equal(t, 3, first)
	require.Equal(t, 2, second)
}

func TestQueuedWriterDefersSemanticFlushWhileCodeIndexIsHot(t *testing.T) {
	t.Parallel()

	flushed := make(chan int, 1)
	q := newWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			return nil
		},
		ApplyCodeItemEmbeddingBatch: func(ctx context.Context, batch []codeindex.ItemEmbeddingUpsert) error {
			flushed <- len(batch)
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		ItemEmbed:     flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  10 * time.Millisecond,
		CodePriority:  time.Hour,
		DeferScale:    4,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "a.go"}))
	require.NoError(t, q.SubmitCodeItemEmbedding(context.Background(), codeindex.ItemEmbeddingUpsert{
		AnchorID:  codeindex.AnchorID("a"),
		Hash:      "h",
		Embedding: embeddings.Embedding{1, 2, 3},
	}))

	select {
	case <-flushed:
		t.Fatal("semantic flush should defer while code indexing is hot")
	case <-time.After(50 * time.Millisecond):
	}

	require.NoError(t, q.Close())
	require.Equal(t, 1, <-flushed)
}

func TestQueuedWriterBatchesSingleAndBatchCodeChunkSubmissions(t *testing.T) {
	t.Parallel()
	for _, batchSubmit := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%t", batchSubmit), func(t *testing.T) {
			t.Parallel()
			var batches, rows int
			cfg := defaultConfig()
			cfg.DefaultPolicy = flushPolicy{Rows: 3, Bytes: 1 << 20, Idle: time.Hour}
			cfg.PollInterval = time.Hour
			q := newWithConfig(context.Background(), Handlers{ApplyCodeItemChunkBatch: func(_ context.Context, items []codeindex.ItemChunksUpsert) error {
				batches++
				for _, item := range items {
					rows += len(item.Chunks)
				}
				return nil
			}}, cfg)
			items := []codeindex.ItemChunksUpsert{{AnchorID: "a", Chunks: []codeindex.ChunkInput{{Index: 0}}, Texts: []string{"a"}, Embeddings: []embeddings.Embedding{{1}}}, {AnchorID: "b", Chunks: []codeindex.ChunkInput{{Index: 0}}, Texts: []string{"b"}, Embeddings: []embeddings.Embedding{{2}}}}
			if batchSubmit {
				require.NoError(t, q.SubmitCodeItemChunkBatch(context.Background(), items))
			} else {
				for _, item := range items {
					require.NoError(t, q.SubmitCodeItemChunks(context.Background(), item.AnchorID, item.Chunks, item.Texts, item.Embeddings))
				}
			}
			require.NoError(t, q.Close())
			require.Equal(t, 1, batches)
			require.Equal(t, 2, rows)
		})
	}
}

func TestQueuedWriterAllowsUrgentSemanticFlushWhileCodeIndexIsHot(t *testing.T) {
	t.Parallel()

	flushed := make(chan int, 1)
	q := newWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			return nil
		},
		ApplyCodeItemEmbeddingBatch: func(ctx context.Context, batch []codeindex.ItemEmbeddingUpsert) error {
			flushed <- len(batch)
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		ItemEmbed:     flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: 10 * time.Millisecond},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  10 * time.Millisecond,
		CodePriority:  time.Hour,
		DeferScale:    2,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "a.go"}))
	require.NoError(t, q.SubmitCodeItemEmbedding(context.Background(), codeindex.ItemEmbeddingUpsert{
		AnchorID:  codeindex.AnchorID("a"),
		Hash:      "h1",
		Embedding: embeddings.Embedding{1, 2, 3},
	}))
	require.NoError(t, q.SubmitCodeItemEmbedding(context.Background(), codeindex.ItemEmbeddingUpsert{
		AnchorID:  codeindex.AnchorID("b"),
		Hash:      "h2",
		Embedding: embeddings.Embedding{4, 5, 6},
	}))

	select {
	case n := <-flushed:
		require.Equal(t, 2, n)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("urgent semantic flush should bypass code-hot defer")
	}
}

func TestQueuedWriterAllowsCodeSubmitWhenOtherLaneIsBackedUp(t *testing.T) {
	t.Parallel()

	firstBatchStarted := make(chan struct{})
	releaseFirstBatch := make(chan struct{})
	var firstBatch sync.Once

	q := newWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			return nil
		},
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			firstBatch.Do(func() {
				close(firstBatchStarted)
				<-releaseFirstBatch
			})
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.Close() })

	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "first.md"}))
	select {
	case <-firstBatchStarted:
	case <-time.After(time.Second):
		t.Fatal("writer never entered first note batch")
	}

	for i := 0; i < cap(q.otherCmdCh); i++ {
		q.otherCmdCh <- queuedPayload{
			value: codeanchor.NoteIndexWork{Path: fmt.Sprintf("queued-%03d.md", i)},
			bytes: 1,
			phase: "index_code",
		}
	}

	blockedDone := make(chan error, 1)
	go func() {
		blockedDone <- q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "blocked.md"})
	}()

	select {
	case err := <-blockedDone:
		t.Fatalf("blocked note submit returned early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	submitCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.NoError(t, q.SubmitCodeIndexWork(submitCtx, codeanchor.CodeIndexWork{Path: "file.go"}))

	close(releaseFirstBatch)

	select {
	case err := <-blockedDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("blocked note submit did not complete")
	}
}

func TestQueuedWriterPrefersCodeLaneWhileCodeIsQueued(t *testing.T) {
	t.Parallel()

	order := make(chan string, 2)
	firstBatchStarted := make(chan struct{})
	releaseFirstBatch := make(chan struct{})
	var noteCalls atomic.Int32
	q := newWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			order <- "code"
			return nil
		},
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			if noteCalls.Add(1) == 1 {
				close(firstBatchStarted)
				<-releaseFirstBatch
				return nil
			}
			order <- "other"
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     flushPolicy{Rows: 1, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 1 << 20, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	t.Cleanup(func() { _ = q.Close() })

	q.otherCmdCh <- queuedPayload{
		value: codeanchor.NoteIndexWork{Path: "blocker.md"},
		bytes: 1,
		phase: "index_code",
	}
	select {
	case <-firstBatchStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked note batch did not start")
	}

	q.otherCmdCh <- queuedPayload{
		value: codeanchor.NoteIndexWork{Path: "note.md"},
		bytes: 1,
		phase: "index_code",
	}
	q.codeCmdCh <- queuedPayload{
		value: codeanchor.CodeIndexWork{Path: "file.go"},
		bytes: 1,
		phase: "index_code",
	}
	close(releaseFirstBatch)

	require.Equal(t, "code", <-order)
	require.Equal(t, "other", <-order)
}
