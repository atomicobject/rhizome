package indexing

import (
	"context"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/stretchr/testify/require"
)

func TestUnifiedBatchFinalizerDrainsAsyncSemanticBeforeWriterClose(t *testing.T) {
	t.Parallel()

	var (
		mu            sync.Mutex
		embeddedRows  int
		noteRows      int
		insideSubmit  = make(chan struct{})
		releaseSubmit = make(chan struct{})
	)
	q := indexwriter.NewWithConfig(context.Background(), indexwriter.Handlers{
		ApplyCodeItemEmbeddingBatch: func(ctx context.Context, batch []codeindex.ItemEmbeddingUpsert) error {
			mu.Lock()
			defer mu.Unlock()
			embeddedRows += len(batch)
			return nil
		},
		ApplyNoteIndexBatch: func(ctx context.Context, batch []codeanchor.NoteIndexWork) error {
			mu.Lock()
			defer mu.Unlock()
			noteRows += len(batch)
			return nil
		},
	}, indexwriter.Config{
		DefaultPolicy: indexwriter.FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     indexwriter.FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    indexwriter.FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	submitter := codeintel.NewAsyncCodeSemanticSubmitter(context.Background(), semanticQueueWriterFunc(func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
		close(insideSubmit)
		<-releaseSubmit
		items := make([]codeindex.ItemEmbeddingUpsert, 0, len(batch))
		for _, work := range batch {
			items = append(items, codeindex.ItemEmbeddingUpsert{
				AnchorID:  codeindex.AnchorID(work.Path),
				Hash:      work.Path,
				Embedding: embeddings.Embedding{1},
			})
		}
		return q.SubmitCodeItemEmbeddingBatch(ctx, items)
	}), nil)
	require.NoError(t, submitter.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "pkg/a.go"}))

	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	cancelCleanup()
	done := make(chan error, 1)
	go func() {
		done <- (&unifiedBatchFinalizer{
			writeQueue:             q,
			asyncSemanticSubmitter: submitter,
		}).Shutdown(cleanupCtx)
	}()

	select {
	case <-insideSubmit:
	case <-time.After(time.Second):
		t.Fatal("expected async semantic submitter to start draining")
	}
	require.NoError(t, q.SubmitNoteIndexWork(context.Background(), codeanchor.NoteIndexWork{Path: "still-open.md"}))
	close(releaseSubmit)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("expected shutdown to complete")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, embeddedRows)
	require.Equal(t, 1, noteRows)
}

func TestUnifiedBatchFinalizerDrainsPostCodeWorkBeforeWriterClose(t *testing.T) {
	t.Parallel()

	var (
		mu           sync.Mutex
		embeddedRows int
		events       []string
	)
	q := indexwriter.NewWithConfig(context.Background(), indexwriter.Handlers{
		ApplyCodeIndexBatch: func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
			mu.Lock()
			events = append(events, "code")
			mu.Unlock()
			return nil
		},
		ApplyCodeItemEmbeddingBatch: func(ctx context.Context, batch []codeindex.ItemEmbeddingUpsert) error {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, "embedding")
			embeddedRows += len(batch)
			return nil
		},
	}, indexwriter.Config{
		DefaultPolicy: indexwriter.FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		CodeIndex:     indexwriter.FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     indexwriter.FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    indexwriter.FlushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})
	postCodeSubmitter := newAsyncCodeBatchSubmitter(context.Background(), func(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
		mu.Lock()
		events = append(events, "post")
		mu.Unlock()
		items := make([]codeindex.ItemEmbeddingUpsert, 0, len(batch))
		for _, work := range batch {
			items = append(items, codeindex.ItemEmbeddingUpsert{
				AnchorID:  codeindex.AnchorID(work.Path),
				Hash:      work.Path,
				Embedding: embeddings.Embedding{1},
			})
		}
		return q.SubmitCodeItemEmbeddingBatch(ctx, items)
	}, nil)
	postWriteDispatcher := newPostWriteDispatcher(context.Background(), postCodeSubmitter, nil, nil)
	q.SetAfterCodeIndexBatch(postWriteDispatcher.EnqueueCodeBatch)

	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "pkg/a.go"}))
	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	cancelCleanup()

	finalizer := &unifiedBatchFinalizer{
		writeQueue:          q,
		postCodeSubmitter:   postCodeSubmitter,
		postWriteDispatcher: postWriteDispatcher,
	}
	require.NoError(t, finalizer.Shutdown(cleanupCtx))
	require.NoError(t, finalizer.Shutdown(context.Background()))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, embeddedRows)
	require.Equal(t, []string{"code", "post", "embedding"}, events)
}

func TestPostWriteDispatcherDecouplesWriterFromFullPostCodeQueue(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	submitter := &asyncCodeBatchSubmitter{
		ctx: ctx,
		ch:  make(chan []codeanchor.CodeIndexWork, 1),
	}
	submitter.ch <- []codeanchor.CodeIndexWork{{Path: "queued.go"}}

	dispatcher := newPostWriteDispatcher(ctx, submitter, nil, nil)
	err := dispatcher.EnqueueCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{Path: "writer.go"}})
	require.NoError(t, err)

	require.ErrorIs(t, dispatcher.Close(), errPostCodeSubmitterFull)
	cancel()
}

func TestPostWriteDispatcherReturnsClosedInsteadOfPanickingOnLateEnqueue(t *testing.T) {
	t.Parallel()

	dispatcher := newPostWriteDispatcher(context.Background(), &asyncCodeBatchSubmitter{
		ctx: context.Background(),
		ch:  make(chan []codeanchor.CodeIndexWork, 1),
	}, nil, nil)
	require.NoError(t, dispatcher.Close())

	err := dispatcher.EnqueueCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{Path: "late.go"}})
	require.ErrorIs(t, err, errPostWriteDispatcherClosed)
}

func TestAsyncCodeBatchSubmitterReturnsBackpressureWhenQueueFull(t *testing.T) {
	t.Parallel()

	submitter := &asyncCodeBatchSubmitter{
		ctx: context.Background(),
		ch:  make(chan []codeanchor.CodeIndexWork, 1),
	}
	submitter.ch <- []codeanchor.CodeIndexWork{{Path: "queued.go"}}

	err := submitter.Enqueue(context.Background(), []codeanchor.CodeIndexWork{{Path: "blocked.go"}})
	require.ErrorIs(t, err, errPostCodeSubmitterFull)
}

func TestUnifiedStreamingTrySubmitScopeNotePathsReturnsBackpressureWhenCodeQueueFull(t *testing.T) {
	t.Parallel()

	coordinator := &unifiedStreamingCoordinator{
		ctx:    context.Background(),
		codeCh: make(chan codeStreamingBatch, 1),
	}
	coordinator.codeCh <- codeStreamingBatch{indexedPaths: []string{"queued.go"}}

	err := coordinator.TrySubmitScopeNotePaths(context.Background(), []string{"note.md"})
	require.ErrorIs(t, err, errUnifiedStreamingCodeQueueFull)
}

type semanticQueueWriterFunc func(context.Context, []codeanchor.CodeIndexWork) error

func (f semanticQueueWriterFunc) SubmitPreparedCodeBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	return f(ctx, batch)
}
