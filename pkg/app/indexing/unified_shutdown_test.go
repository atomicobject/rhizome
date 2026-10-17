package indexing

import (
	"context"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
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
	q := indexwriter.New(context.Background(), indexwriter.Handlers{
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
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("expected shutdown to complete")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, embeddedRows)
	require.Equal(t, 1, noteRows)
}

type semanticQueueWriterFunc func(context.Context, []codeanchor.CodeIndexWork) error

func (f semanticQueueWriterFunc) SubmitPreparedCodeBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	return f(ctx, batch)
}
