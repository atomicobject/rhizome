package indexing

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	codeemb "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	"github.com/stretchr/testify/require"
)

func TestStaleCodeEmbeddingPaths(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	store, err := codeemb.OpenCode(context.Background(), codeemb.File(filepath.Join(tempDir, "code.db")), codeemb.Options{Access: codeemb.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now()
	require.NoError(t, store.UpsertItemMetaBatch(context.Background(), []codeindex.Item{
		anchor("a1", "pkg/foo.go", now),
		anchor("a2", "pkg/bar.go", now),
	}))

	stale, err := staleCodeEmbeddingPaths(context.Background(), store, []string{"pkg/foo.go"})
	require.NoError(t, err)
	require.Equal(t, []string{"pkg/bar.go"}, stale)
}

func TestSubmitPreparedCodeBatchQueuesPrePersistWork(t *testing.T) {
	t.Parallel()

	c := &unifiedStreamingCoordinator{
		ctx:    context.Background(),
		codeCh: make(chan codeStreamingBatch, 1),
	}

	err := c.SubmitPreparedCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{
		Path:         "pkg/foo.go",
		ReplaceIndex: true,
	}})
	require.NoError(t, err)

	select {
	case batch := <-c.codeCh:
		require.Len(t, batch.prePersistWorks, 1)
		require.Equal(t, "pkg/foo.go", batch.prePersistWorks[0].Path)
		require.Empty(t, batch.indexedPaths)
	case <-time.After(time.Second):
		t.Fatal("expected queued pre-persist code batch")
	}
}

func TestUnifiedStreamingCoordinatorCloseBeforeStartDoesNotLaunchWorkers(t *testing.T) {
	t.Parallel()

	c := &unifiedStreamingCoordinator{
		ctx:    context.Background(),
		codeCh: make(chan codeStreamingBatch),
		noteCh: make(chan noteStreamingBatch),
	}
	require.NoError(t, c.CloseAndWait())
	c.Start()
	require.NoError(t, c.CloseAndWait())
}

func TestRunNotesUsesOntologyBodyCallbackBeforeFinalDrain(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &unifiedStreamingCoordinator{
		ctx:    ctx,
		cancel: cancel,
		noteCh: make(chan noteStreamingBatch, 1),
	}
	called := make(chan struct{})
	c.syncNoteBodyPaths = func(ctx context.Context, changedPaths []string, deletedPaths []string, fullSync bool) error {
		require.Equal(t, []string{"notes/a.md", "notes/b.md"}, changedPaths)
		require.Empty(t, deletedPaths)
		require.True(t, fullSync)
		close(called)
		return nil
	}
	c.wg.Add(1)
	go c.runNotes()

	require.NoError(t, c.SubmitNoteFullRebuild(ctx, []string{"notes/b.md", "notes/a.md"}))
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("expected ontology body callback before final drain")
	}
	require.NoError(t, c.CloseAndWait())
}

func TestShouldFlushEarlyPrepared(t *testing.T) {
	t.Parallel()

	now := time.Now()
	require.False(t, shouldFlushEarlyPrepared(0, time.Time{}, false, now))
	require.False(t, shouldFlushEarlyPrepared(unifiedEarlyCodeTaskFlush-1, now, false, now))
	require.True(t, shouldFlushEarlyPrepared(unifiedEarlyCodeTaskFlush, now, false, now))
	require.True(t, shouldFlushEarlyPrepared(1, now.Add(-unifiedEarlyCodeMaxWait-time.Millisecond), false, now))
	require.True(t, shouldFlushEarlyPrepared(1, now, true, now))
}

func TestRunNotesMarksLastSyncAfterStreamingCompletes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	synced := make(chan struct{}, 1)
	marked := make(chan struct{}, 1)
	c := &unifiedStreamingCoordinator{
		ctx:    ctx,
		cancel: cancel,
		noteCh: make(chan noteStreamingBatch, 1),
		syncNotePaths: func(context.Context, []string) error {
			synced <- struct{}{}
			return nil
		},
		markNoteLastSync: func(context.Context) error {
			marked <- struct{}{}
			return nil
		},
	}
	c.wg.Add(1)
	go c.runNotes()

	require.NoError(t, c.SubmitNotePaths(context.Background(), []string{"notes/a.md"}))

	select {
	case <-synced:
	case <-time.After(time.Second):
		t.Fatal("expected streamed note batch")
	}

	select {
	case <-marked:
		t.Fatal("marked last_sync before streaming finished")
	default:
	}

	require.NoError(t, c.CloseAndWait())

	select {
	case <-marked:
		t.Fatal("marked last_sync before explicit durable barrier")
	default:
	}

	require.NoError(t, c.MarkNoteLastSync(context.Background()))

	select {
	case <-marked:
	case <-time.After(time.Second):
		t.Fatal("expected note last_sync mark after durable barrier")
	}
}

func TestRunNotesStreamsWhileCodeStreamingContinues(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	codeDone := make(chan struct{})
	synced := make(chan struct{}, 1)
	c := &unifiedStreamingCoordinator{
		ctx:      ctx,
		cancel:   cancel,
		noteCh:   make(chan noteStreamingBatch, 1),
		codeDone: codeDone,
		syncNotePaths: func(context.Context, []string) error {
			synced <- struct{}{}
			return nil
		},
		markNoteLastSync: func(context.Context) error { return nil },
	}
	c.wg.Add(1)
	go c.runNotes()

	require.NoError(t, c.SubmitNotePaths(context.Background(), []string{"notes/a.md"}))

	select {
	case <-synced:
	case <-time.After(time.Second):
		t.Fatal("expected notes to stream before code stream completion")
	}

	close(codeDone)
	close(c.noteCh)
	c.wg.Wait()
}

func TestRunNotesPrefersOntologyBodyStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type bodyCall struct {
		changed []string
		deleted []string
		full    bool
	}
	calls := make(chan bodyCall, 1)
	rawCalled := make(chan struct{}, 1)
	c := &unifiedStreamingCoordinator{
		ctx:    ctx,
		cancel: cancel,
		noteCh: make(chan noteStreamingBatch, 2),
		syncNoteBodyPaths: func(_ context.Context, changed []string, deleted []string, full bool) error {
			calls <- bodyCall{changed: changed, deleted: deleted, full: full}
			return nil
		},
		syncNotePaths: func(context.Context, []string) error {
			rawCalled <- struct{}{}
			return nil
		},
		markNoteLastSync: func(context.Context) error { return nil },
	}
	c.wg.Add(1)
	go c.runNotes()

	require.NoError(t, c.SubmitNotePaths(context.Background(), []string{"notes/b.md", "notes/a.md"}))
	require.NoError(t, c.SubmitDeletedNotePaths(context.Background(), []string{"notes/deleted.md"}))

	select {
	case call := <-calls:
		require.Equal(t, []string{"notes/a.md", "notes/b.md"}, call.changed)
		require.Equal(t, []string{"notes/deleted.md"}, call.deleted)
		require.False(t, call.full)
	case <-time.After(time.Second):
		t.Fatal("expected ontology body stream")
	}
	select {
	case <-rawCalled:
		t.Fatal("raw note sync should not run when ontology body stream is configured")
	default:
	}

	close(c.noteCh)
	c.wg.Wait()
}

func anchor(id, path string, now time.Time) codeindex.Item {
	return codeindex.Item{
		AnchorID:    codeindex.AnchorID(id),
		Lang:        "go",
		Kind:        "func",
		Path:        path,
		Symbol:      id,
		FQN:         id,
		Fingerprint: "fp-" + id,
		UpdatedAt:   now,
	}
}
