package indexing

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	codeemb "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

func TestSummarizeCodeStreamingBatch(t *testing.T) {
	t.Parallel()

	batch := summarizeCodeStreamingBatch([]codeanchor.CodeIndexWork{
		{
			Path:          "pkg/foo.go",
			ReplaceIndex:  true,
			HasRefSignals: true,
			RefFootprint: codeanchor.DurableRefFootprint{
				Path:       "pkg/foo.go",
				SymbolKeys: []string{"go||Run"},
			},
			DefDeltas: codeanchor.DefDeltas{
				AddedModules: []string{"foo"},
			},
		},
		{
			Path:         "pkg/foo.go",
			ReplaceIndex: true,
		},
		{
			Path:          "pkg/bar.go",
			ReplaceIndex:  true,
			HasRefSignals: true,
			RefFootprint: codeanchor.DurableRefFootprint{
				Path:    "pkg/bar.go",
				Modules: []string{"foo"},
			},
			DefDeltas: codeanchor.DefDeltas{
				RemovedModules: []string{"bar"},
			},
		},
	})

	require.Equal(t, []string{"pkg/bar.go", "pkg/foo.go"}, batch.indexedPaths)
	require.Equal(t, []string{"pkg/bar.go", "pkg/foo.go"}, batch.changedCallerPaths)
	require.Equal(t, []string{"foo"}, batch.defDeltas.AddedModules)
	require.Equal(t, []string{"bar"}, batch.defDeltas.RemovedModules)
	require.Equal(t, []codeanchor.DurableRefFootprint{
		{Path: "pkg/foo.go", SymbolKeys: []string{"go||Run"}},
		{Path: "pkg/bar.go", Modules: []string{"foo"}},
	}, batch.footprints)
}

func TestMergeCodeStreamingBatches(t *testing.T) {
	t.Parallel()

	merged := mergeCodeStreamingBatches([]codeStreamingBatch{
		{
			indexedPaths:       []string{"pkg/foo.go"},
			changedCallerPaths: []string{"pkg/foo.go"},
			footprints: []codeanchor.DurableRefFootprint{{
				Path:       "pkg/foo.go",
				SymbolKeys: []string{"go||Run"},
			}},
			defDeltas: codeanchor.DefDeltas{
				AddedModules: []string{"foo"},
			},
		},
		{
			indexedPaths: []string{"pkg/bar.go", "pkg/foo.go"},
			footprints: []codeanchor.DurableRefFootprint{
				{Path: "pkg/bar.go", Modules: []string{"foo"}},
				{Path: "pkg/foo.go", Modules: []string{"bar"}},
			},
			defDeltas: codeanchor.DefDeltas{
				RemovedModules: []string{"bar"},
			},
		},
	})

	require.Equal(t, []string{"pkg/bar.go", "pkg/foo.go"}, merged.indexedPaths)
	require.Equal(t, []string{"pkg/foo.go"}, merged.changedCallerPaths)
	require.Equal(t, []string{"foo"}, merged.defDeltas.AddedModules)
	require.Equal(t, []string{"bar"}, merged.defDeltas.RemovedModules)
	require.Equal(t, []codeanchor.DurableRefFootprint{
		{Path: "pkg/foo.go", SymbolKeys: []string{"go||Run"}, Modules: []string{"bar"}},
		{Path: "pkg/bar.go", Modules: []string{"foo"}},
	}, merged.footprints)
}

func TestMergeCodeStreamingBatchesPreservesFullRebuild(t *testing.T) {
	t.Parallel()

	merged := mergeCodeStreamingBatches([]codeStreamingBatch{
		{indexedPaths: []string{"pkg/foo.go"}},
		{indexedPaths: []string{"pkg/bar.go"}, forceFullRebuild: true},
	})

	require.True(t, merged.forceFullRebuild)
	require.Equal(t, []string{"pkg/bar.go", "pkg/foo.go"}, merged.indexedPaths)
}

func TestStaleCodeEmbeddingPaths(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	store, err := codeemb.Open(filepath.Join(tempDir, "code.db"), 8)
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

func TestSubmitCodeBatchQueuesDuringFallbackForEarlyPrep(t *testing.T) {
	t.Parallel()

	c := &unifiedStreamingCoordinator{
		ctx:            context.Background(),
		fallbackReason: "reverse_index_backfill_incomplete",
		seenCodePath:   make(map[string]struct{}),
		codeCh:         make(chan codeStreamingBatch, 1),
	}

	err := c.SubmitCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{
		Path:          "pkg/foo.go",
		ReplaceIndex:  true,
		HasRefSignals: true,
	}})
	require.NoError(t, err)
	require.Equal(t, []string{"pkg/foo.go"}, c.snapshotSeenCodePaths())

	select {
	case batch := <-c.codeCh:
		require.Equal(t, []string{"pkg/foo.go"}, batch.indexedPaths)
	case <-time.After(time.Second):
		t.Fatal("expected queued code batch")
	}
}

func TestSubmitPreparedCodeBatchQueuesPrePersistWork(t *testing.T) {
	t.Parallel()

	c := &unifiedStreamingCoordinator{
		ctx:          context.Background(),
		seenCodePath: make(map[string]struct{}),
		codeCh:       make(chan codeStreamingBatch, 1),
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

func TestFilterRuntimeRebuildPathsSkipsOnlyUnchangedFreshPaths(t *testing.T) {
	t.Parallel()

	rebuilt := map[string]uint64{
		"pkg/already.go": 3,
	}
	current := func(paths []string) map[string]uint64 {
		return map[string]uint64{
			"pkg/already.go": 3,
			"pkg/new.go":     1,
			"pkg/updated.go": 4,
		}
	}

	filtered := filterRuntimeRebuildPaths([]string{
		"pkg/already.go",
		"pkg/new.go",
		"pkg/updated.go",
		"pkg/from-db.go",
	}, current, rebuilt)

	require.Equal(t, []string{"pkg/new.go", "pkg/updated.go", "pkg/from-db.go"}, filtered)
	require.Equal(t, uint64(3), rebuilt["pkg/already.go"])
	require.Equal(t, uint64(1), rebuilt["pkg/new.go"])
	require.Equal(t, uint64(4), rebuilt["pkg/updated.go"])
	require.Zero(t, rebuilt["pkg/from-db.go"])
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

func TestRunCodeReadiesResidualBacklogFromDurableFootprints(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var planCalls int
	rebuilds := make(chan []string, 2)
	c := &unifiedStreamingCoordinator{
		ctx:          ctx,
		cancel:       cancel,
		codeCh:       make(chan codeStreamingBatch, 2),
		seenCodePath: make(map[string]struct{}),
		reverseIndexReady: func(context.Context) (bool, error) {
			return false, nil
		},
		planRebuild: func(_ context.Context, _ []string, deltas codeanchor.DefDeltas, residual codeanchor.CallEdgeResidual, backfillComplete bool) (codeanchor.CallEdgeRebuildPlan, error) {
			planCalls++
			require.False(t, residual.HasPending())
			require.True(t, deltas.HasChanges())
			return codeanchor.CallEdgeRebuildPlan{
				Residual: codeanchor.CallEdgeResidual{
					SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
				},
			}, nil
		},
		rebuildCallEdges: func(_ context.Context, paths []string) error {
			rebuilds <- append([]string(nil), paths...)
			return nil
		},
		markCodeLastSync: func(context.Context) error { return nil },
	}
	c.wg.Add(1)
	go c.runCode()

	require.NoError(t, c.SubmitCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{
		Path:         "pkg/callee.go",
		ReplaceIndex: true,
		DefDeltas: codeanchor.DefDeltas{
			AddedSymbols: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
		},
	}}))
	require.NoError(t, c.SubmitCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{
		Path:          "pkg/caller2.go",
		ReplaceIndex:  true,
		HasRefSignals: true,
		RefFootprint: codeanchor.DurableRefFootprint{
			Path:       "pkg/caller2.go",
			SymbolKeys: []string{"go||Run"},
		},
	}}))

	require.NoError(t, c.CloseAndWait())
	select {
	case got := <-rebuilds:
		require.Equal(t, []string{"pkg/caller2.go"}, got)
	case <-time.After(time.Second):
		t.Fatal("expected residual ready-queue rebuild")
	}
	require.Equal(t, 1, planCalls)
}

func TestRunCodeFinalDrainResolvesResidualAfterBackfillCompletes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ready atomic.Bool
	rebuilds := make(chan []string, 1)
	var planCalls int
	c := &unifiedStreamingCoordinator{
		ctx:          ctx,
		cancel:       cancel,
		codeCh:       make(chan codeStreamingBatch, 2),
		seenCodePath: make(map[string]struct{}),
		reverseIndexReady: func(context.Context) (bool, error) {
			return ready.Load(), nil
		},
		planRebuild: func(_ context.Context, _ []string, _ codeanchor.DefDeltas, residual codeanchor.CallEdgeResidual, _ bool) (codeanchor.CallEdgeRebuildPlan, error) {
			planCalls++
			if !ready.Load() {
				return codeanchor.CallEdgeRebuildPlan{
					Residual: codeanchor.CallEdgeResidual{
						SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangPy, Pkg: "app.callee", Name: "run"}},
					},
				}, nil
			}
			t.Fatalf("unexpected planner call during final residual drain: residual=%v", residual)
			return codeanchor.CallEdgeRebuildPlan{}, nil
		},
		rebuildCallEdges: func(_ context.Context, paths []string) error {
			rebuilds <- append([]string(nil), paths...)
			return nil
		},
		markCodeLastSync: func(context.Context) error { return nil },
	}
	c.wg.Add(1)
	go c.runCode()

	require.NoError(t, c.SubmitCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{
		Path:         "src/callee.py",
		ReplaceIndex: true,
		DefDeltas: codeanchor.DefDeltas{
			AddedSymbols: []codeanchor.SymbolRef{{Lang: codeanchor.LangPy, Pkg: "app.callee", Name: "run"}},
		},
	}}))
	require.NoError(t, c.SubmitCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{
		Path:          "src/caller.py",
		ReplaceIndex:  true,
		HasRefSignals: true,
		RefFootprint: codeanchor.DurableRefFootprint{
			Path:       "src/caller.py",
			SymbolKeys: []string{"py|app.callee|run"},
		},
	}}))

	require.NoError(t, c.CloseAndWait())
	select {
	case got := <-rebuilds:
		require.Equal(t, []string{"src/caller.py"}, got)
	case <-time.After(time.Second):
		t.Fatal("expected final residual drain rebuild")
	}
	require.Equal(t, 1, planCalls)
}

func TestRunCodeThrottlesReverseIndexReadyChecks(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var readyChecks atomic.Int64
	c := &unifiedStreamingCoordinator{
		ctx:          ctx,
		cancel:       cancel,
		codeCh:       make(chan codeStreamingBatch, 4),
		seenCodePath: make(map[string]struct{}),
		reverseIndexReady: func(context.Context) (bool, error) {
			readyChecks.Add(1)
			return false, nil
		},
		planRebuild: func(_ context.Context, _ []string, _ codeanchor.DefDeltas, _ codeanchor.CallEdgeResidual, _ bool) (codeanchor.CallEdgeRebuildPlan, error) {
			return codeanchor.CallEdgeRebuildPlan{
				Residual: codeanchor.CallEdgeResidual{
					SymbolRefs: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
				},
			}, nil
		},
		markCodeLastSync: func(context.Context) error { return nil },
	}
	c.wg.Add(1)
	go c.runCode()

	for i := 0; i < 3; i++ {
		require.NoError(t, c.SubmitCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{
			Path:         "pkg/callee.go",
			ReplaceIndex: true,
			DefDeltas: codeanchor.DefDeltas{
				AddedSymbols: []codeanchor.SymbolRef{{Lang: codeanchor.LangGo, Name: "Run"}},
			},
		}}))
	}

	require.NoError(t, c.CloseAndWait())
	require.EqualValues(t, 1, readyChecks.Load())
}

func TestRunCodeDefersCallEdgeRebuildUntilFinalDrain(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx, cancel := context.WithCancel(indexingperf.WithCollector(context.Background(), collector))
	defer cancel()

	rebuilds := make(chan []string, 2)
	c := &unifiedStreamingCoordinator{
		ctx:          ctx,
		cancel:       cancel,
		codeCh:       make(chan codeStreamingBatch, 4),
		codeDone:     make(chan struct{}),
		seenCodePath: make(map[string]struct{}),
		reverseIndexReady: func(context.Context) (bool, error) {
			return true, nil
		},
		rebuildCallEdges: func(_ context.Context, paths []string) error {
			rebuilds <- append([]string(nil), paths...)
			return nil
		},
		markCodeLastSync: func(context.Context) error { return nil },
	}
	c.wg.Add(1)
	go c.runCode()

	makeBatch := func(prefix string) []codeanchor.CodeIndexWork {
		batch := make([]codeanchor.CodeIndexWork, 0, 100)
		for i := 0; i < 100; i++ {
			batch = append(batch, codeanchor.CodeIndexWork{
				Path:          fmt.Sprintf("%s/file%03d.go", prefix, i),
				ReplaceIndex:  true,
				HasRefSignals: true,
			})
		}
		return batch
	}

	require.NoError(t, c.SubmitCodeBatch(context.Background(), makeBatch("pkg/one")))
	time.Sleep(unifiedStreamingBatchIdle + 25*time.Millisecond)
	select {
	case got := <-rebuilds:
		t.Fatalf("unexpected early rebuild: %v", got)
	default:
	}

	require.NoError(t, c.SubmitCodeBatch(context.Background(), makeBatch("pkg/two")))
	time.Sleep(unifiedStreamingBatchIdle + 25*time.Millisecond)
	select {
	case got := <-rebuilds:
		t.Fatalf("unexpected rebuild before close: %v", got)
	default:
	}

	require.NoError(t, c.SubmitCodeBatch(context.Background(), makeBatch("pkg/three")))
	require.NoError(t, c.CloseAndWait())
	select {
	case got := <-rebuilds:
		require.Len(t, got, 300)
	case <-time.After(time.Second):
		t.Fatal("expected final rebuild")
	}
	summary := collector.RenderSummary()
	require.Contains(t, summary, "rebuild_call_edges")
	require.False(t, strings.Contains(summary, "rebuild_call_edges") && strings.Contains(summary, "x2"))
}

func TestRunCodeDoesNotReplanPrimaryChunksAfterCallEdgeRebuild(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var prepareCalls atomic.Int32
	c := &unifiedStreamingCoordinator{
		ctx:          ctx,
		cancel:       cancel,
		codeCh:       make(chan codeStreamingBatch, 1),
		seenCodePath: make(map[string]struct{}),
		reverseIndexReady: func(context.Context) (bool, error) {
			return true, nil
		},
		rebuildCallEdges: func(_ context.Context, paths []string) error {
			return nil
		},
		prepareCodePaths: func(_ context.Context, paths []string) (semantic.PreparedCodePaths, error) {
			prepareCalls.Add(1)
			return semantic.PreparedCodePaths{}, nil
		},
		markCodeLastSync: func(context.Context) error { return nil },
	}
	c.wg.Add(1)
	go c.runCode()

	require.NoError(t, c.SubmitCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{
		Path:          "pkg/caller.go",
		ReplaceIndex:  true,
		HasRefSignals: true,
	}}))
	require.NoError(t, c.CloseAndWait())

	require.Zero(t, prepareCalls.Load())
}

func TestRunCodePostPersistBatchDoesNotReplanSemanticChunks(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var prepareCalls atomic.Int32
	c := &unifiedStreamingCoordinator{
		ctx:          ctx,
		cancel:       cancel,
		codeCh:       make(chan codeStreamingBatch, 1),
		seenCodePath: make(map[string]struct{}),
		prepareCodePaths: func(context.Context, []string) (semantic.PreparedCodePaths, error) {
			prepareCalls.Add(1)
			return semantic.PreparedCodePaths{}, nil
		},
		reverseIndexReady: func(context.Context) (bool, error) { return true, nil },
		markCodeLastSync:  func(context.Context) error { return nil },
	}
	c.wg.Add(1)
	go c.runCode()

	require.NoError(t, c.SubmitCodeBatch(context.Background(), []codeanchor.CodeIndexWork{{
		Path:         "pkg/post.go",
		ReplaceIndex: true,
	}}))
	require.NoError(t, c.CloseAndWait())
	require.Zero(t, prepareCalls.Load())
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
