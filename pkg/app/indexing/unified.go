package indexing

// Docs:
// - [Indexing pipeline (Hub)](docs/hubs/Indexing pipeline (Hub).md)
// - [[indexing-workflow]]
// - [[indexing-pipeline-architecture]]
// - [Indexing pipeline - rzm index orchestration](docs/reference/analysis/Indexing pipeline - rzm index orchestration.md)
// - [[Indexing pipeline - Phase ownership matrix]]
// - [Code Index - Unified SQLite DB](docs/reference/analysis/Code Index - Unified SQLite DB.md)

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ProgressSink is the minimal user-visible output surface used by indexing
// phases that only need to print status lines.
type ProgressSink interface {
	Println(string)
}

// ProgressBar receives both status lines and absolute progress updates for the
// integrated rzm index progress display.
type ProgressBar interface {
	ProgressSink
	Update(done, total int)
}

// UnifiedOptions configures the full one-pass indexing pipeline.
type UnifiedOptions struct {
	VaultPath    string
	VaultDef     obsidian.VaultDefinition
	NoteMetadata notemeta.Indexer
	ProgressBar  ProgressBar
	Verbose      bool
	Vacuum       bool
	// SkipConfigPersistence leaves .rhizome/config.yml untouched after the run.
	// Explicit `rzm index` persists resolved code and embeddings settings as
	// always; the vault runtime's automatic boot catch-up sets this because
	// background work must not rewrite user configuration, and a rewrite
	// invalidates code-mode connections opened moments earlier (SPEC-0104).
	SkipConfigPersistence bool
	TxLockMode            string
	APIKey                string

	// afterPreparedOwnershipCommit is a package-test seam. Production callers
	// leave it nil; it runs only after prepared source ownership commits and
	// before destination work starts.
	afterPreparedOwnershipCommit func(context.Context) error
}

type unifiedBatchFinalizer struct {
	writeQueue             *indexwriter.Writer
	asyncSemanticSubmitter *codeintel.AsyncCodeSemanticSubmitter
	postCodeSubmitter      *asyncCodeBatchSubmitter
	postWriteDispatcher    *postWriteDispatcher
	streaming              *unifiedStreamingCoordinator
	semanticCancel         context.CancelFunc
	semanticResources      *unifiedSemanticResources
	waitFinalOntology      func() error

	closeAsyncOnce     sync.Once
	closeAsyncErr      error
	closeDispatchOnce  sync.Once
	closeDispatchErr   error
	closePostOnce      sync.Once
	closePostErr       error
	closeStreamingOnce sync.Once
	closeStreamingErr  error
	waitIntentOnce     sync.Once
	waitIntentErr      error
	waitOntologyOnce   sync.Once
	waitOntologyErr    error
	detachWriterOnce   sync.Once
	closeWriterOnce    sync.Once
	closeWriterErr     error
	cancelSemanticOnce sync.Once
	closeSemanticOnce  sync.Once
	writerClosed       bool
}

func nonCanceledQueueContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func (f *unifiedBatchFinalizer) CloseAsyncSemanticSubmitter() error {
	if f == nil {
		return nil
	}
	f.closeAsyncOnce.Do(func() {
		if f.asyncSemanticSubmitter != nil {
			f.closeAsyncErr = f.asyncSemanticSubmitter.Close()
		}
	})
	return f.closeAsyncErr
}

func (f *unifiedBatchFinalizer) FlushQueue(ctx context.Context) error {
	if f == nil || f.writeQueue == nil || f.writerClosed {
		return nil
	}
	return f.writeQueue.FlushAndWait(nonCanceledQueueContext(ctx))
}

func (f *unifiedBatchFinalizer) ClosePostCodeSubmitter() error {
	if f == nil {
		return nil
	}
	f.closePostOnce.Do(func() {
		if f.postCodeSubmitter != nil {
			f.closePostErr = f.postCodeSubmitter.Close()
		}
	})
	return f.closePostErr
}

func (f *unifiedBatchFinalizer) ClosePostWriteDispatcher() error {
	if f == nil {
		return nil
	}
	f.closeDispatchOnce.Do(func() {
		if f.postWriteDispatcher != nil {
			f.closeDispatchErr = f.postWriteDispatcher.Close()
		}
	})
	return f.closeDispatchErr
}

func (f *unifiedBatchFinalizer) CloseStreaming() error {
	if f == nil {
		return nil
	}
	f.closeStreamingOnce.Do(func() {
		if f.streaming != nil {
			f.closeStreamingErr = f.streaming.CloseAndWait()
		}
	})
	return f.closeStreamingErr
}

// DrainStreamingWrites finishes producers and makes their queued body writes
// durable before a later catalog replacement can delete their chunk parents.
func (f *unifiedBatchFinalizer) DrainStreamingWrites(ctx context.Context) (err error) {
	done := indexingperf.StartSpan(ctx, "streaming_durability_barrier")
	defer func() { done(err) }()
	if err := f.CloseStreaming(); err != nil {
		return err
	}
	if ctx == nil {
		return f.FlushQueue(ctx)
	}
	return errors.Join(f.FlushQueue(ctx), ctx.Err())
}

func (f *unifiedBatchFinalizer) WaitIntent() error {
	if f == nil {
		return nil
	}
	f.waitIntentOnce.Do(func() {
		if f.semanticResources != nil {
			f.waitIntentErr = f.semanticResources.waitIntent()
		}
	})
	return f.waitIntentErr
}

func (f *unifiedBatchFinalizer) WaitFinalOntology() error {
	if f == nil {
		return nil
	}
	f.waitOntologyOnce.Do(func() {
		if f.waitFinalOntology != nil {
			f.waitOntologyErr = f.waitFinalOntology()
		}
	})
	return f.waitOntologyErr
}

func (f *unifiedBatchFinalizer) DetachWriterCallbacks() {
	if f == nil {
		return
	}
	f.detachWriterOnce.Do(func() {
		if f.writeQueue != nil {
			f.writeQueue.SetAfterCodeIndexBatch(nil)
			f.writeQueue.SetAfterNoteIndexBatch(nil)
		}
	})
}

func (f *unifiedBatchFinalizer) CloseWriter() error {
	if f == nil {
		return nil
	}
	f.closeWriterOnce.Do(func() {
		f.DetachWriterCallbacks()
		if f.writeQueue != nil {
			f.closeWriterErr = f.writeQueue.Close()
		}
		f.writerClosed = true
	})
	return f.closeWriterErr
}

func (f *unifiedBatchFinalizer) CancelSemanticContext() {
	if f == nil {
		return
	}
	f.cancelSemanticOnce.Do(func() {
		if f.semanticCancel != nil {
			f.semanticCancel()
		}
	})
}

func (f *unifiedBatchFinalizer) CloseSemanticResources() {
	if f == nil {
		return
	}
	f.closeSemanticOnce.Do(func() {
		if f.semanticResources != nil {
			f.semanticResources.close()
		}
	})
}

func (f *unifiedBatchFinalizer) Shutdown(ctx context.Context) error {
	if f == nil {
		return nil
	}
	var err error
	capture := func(next error) {
		if next != nil {
			err = errors.Join(err, next)
		}
	}

	capture(f.CloseAsyncSemanticSubmitter())
	capture(f.FlushQueue(ctx))
	capture(f.ClosePostWriteDispatcher())
	capture(f.ClosePostCodeSubmitter())
	capture(f.CloseStreaming())
	capture(f.WaitIntent())
	capture(f.WaitFinalOntology())
	capture(f.FlushQueue(ctx))
	f.DetachWriterCallbacks()
	capture(f.CloseWriter())
	f.CancelSemanticContext()
	f.CloseSemanticResources()
	return err
}

type noopProgress struct{}

func (noopProgress) Println(string)  {}
func (noopProgress) Update(int, int) {}

type progressSinkFunc func(string)

func (f progressSinkFunc) Println(line string) { f(line) }

func withProgressBar(p ProgressBar) ProgressBar {
	if p == nil {
		return noopProgress{}
	}
	return p
}

func withProgressSink(p ProgressSink) ProgressSink {
	if p == nil {
		return noopProgress{}
	}
	return p
}

func ontologyNodeEmbeddingsRequireRebuild(results ...*ontology.SyncResult) bool {
	for _, result := range results {
		if result != nil && result.Rebuilt {
			return true
		}
	}
	return false
}

func ontologyFollowupNeeded(result *ontology.SyncResult, changedPaths, deletedPaths []string) bool {
	if result == nil {
		return false
	}
	if result.Dirty || result.Rebuilt {
		return true
	}
	return len(changedPaths) > 0 || len(deletedPaths) > 0
}

// RunUnifiedCore performs the core unified indexing work without CLI dependencies.
// Docs:
// - [[indexing-workflow#^spec-0036-us1-ac1]]
// - [[indexing-workflow#^spec-0036-us2-ac2]]
// - [[indexing-pipeline-architecture#^spec-0012-us1]]
// WHY: CLI batch indexing and live refresh need the same staged barriers, writer
// lane, semantic runtime policy, and maintenance behavior; keep orchestration here.
func RunUnifiedCore(ctx context.Context, opts UnifiedOptions) (err error) {
	done := indexingperf.StartSpan(ctx, "unified_core")
	defer func() { done(err) }()
	if err := opts.NoteMetadata.Validate(); err != nil {
		return fmt.Errorf("note metadata indexer: %w", err)
	}
	rawProgress := withProgressBar(opts.ProgressBar)
	progress := newIntegratedProgress(rawProgress, opts.Verbose)
	txLockMode := opts.TxLockMode
	if txLockMode == "" {
		txLockMode = sqliteutil.TxLockImmediate
	}

	var codeCfg codeanchor.Config
	var embCfg embeddings.Config
	if err := runPhase(ctx, "open_config", func(ctx context.Context) error {
		var err error
		codeCfg, err = obsidian.LoadCodeConfig(opts.VaultPath)
		if err != nil {
			return err
		}
		embCfg, err = obsidian.LoadEmbeddingsConfig(opts.VaultPath)
		return err
	}); err != nil {
		return err
	}
	embCfg.IndexPath = obsidian.UnifiedIndexPath(opts.VaultPath, embCfg.IndexPath)
	// Force embeddings to share the unified DB with code intel.
	embCfg.IndexPath = codeCfg.IndexPath

	codeEmbCfg, codeEmbExplicit, err := obsidian.EffectiveCodeEmbeddingsConfig(opts.VaultPath, embCfg)
	if err != nil {
		return err
	}
	// Force code embeddings to share the unified DB with code intel.
	codeEmbCfg.IndexPath = codeCfg.IndexPath
	var intelStore *semdb.Store
	var cleanup func()
	if err := runPhase(ctx, "open_stores", func(ctx context.Context) error {
		var err error
		intelStore, cleanup, err = obsidian.OpenIntelStoreWithRecoveryWithOptions(
			opts.VaultPath,
			codeCfg,
			obsidian.IndexLockPath(opts.VaultPath),
			obsidian.IntelStoreOpenOptions{TxLockMode: txLockMode},
		)
		return err
	}); err != nil {
		return err
	}
	defer cleanup()
	sharedWriteMu := &sync.Mutex{}
	intelStore.SetWriteMu(sharedWriteMu)

	// Full indexing discovers and classifies the complete current ownership set
	// before lane work. Do not use the legacy scope-change invalidation here: it
	// clears durable note source identity, including descriptor-only formats.
	localCfg, _ := obsidian.LoadLocalConfig(opts.VaultPath)
	var currentScopeHash string
	if localCfg != nil {
		currentScopeHash = localCfg.ScopeConfigHash()
	}
	structuralRequest := indexcore.Request{VaultDefinition: opts.VaultDef, CodeConfig: codeCfg, NoteMetadata: opts.NoteMetadata}
	var ownershipDiscovery indexcore.Discovery
	ownershipDiscovery, err = indexcore.Discover(ctx, structuralRequest, intelStore)
	if err != nil {
		return fmt.Errorf("discover full-index ownership: %w", err)
	}
	linker, err := codeintel.NewDocLinkerFromNotePaths(ownershipDiscovery.MarkdownPaths(), intelStore)
	if err != nil {
		return fmt.Errorf("build code-to-note linker: %w", err)
	}

	tailIdx := codeanchor.NewPathTailIndex(5)
	service, _ := bootstrap.NewCodeAnchorService(bootstrap.CodeAnchorServiceConfig{
		VaultPath:       opts.VaultPath,
		CodeCfg:         codeCfg,
		Store:           intelStore,
		TailIndex:       tailIdx,
		Linker:          linker,
		IncludeIndexers: true,
		WriteAccess:     true,
	})
	var (
		codeItemEmbBatch    func(context.Context, []codeindex.ItemEmbeddingUpsert) error
		codeChunkBatch      func(context.Context, []codeindex.ItemChunksUpsert) error
		codeChunkWriter     func(context.Context, codeindex.AnchorID, []codeindex.ChunkInput, []string, []embeddings.Embedding) error
		noteMetaBatch       func(context.Context, []embeddings.NoteFileInfo) error
		noteChunkSyncBatch  func(context.Context, []embeddings.NoteChunkSync) error
		noteChunkSyncWriter func(context.Context, embeddings.NoteChunkSync) error
		intelChunkWriter    = intelStore.ReplaceIntelChunks
		intelEmbWriter      = intelStore.UpsertEmbeddings
	)
	writeQueue := indexwriter.New(ctx, indexcore.BindWriterHandlers(indexwriter.Handlers{
		MarkDerivedDirty:       intelStore.MarkDerivedDirty,
		ActivateDerivedWork:    intelStore.ActivateDerivedWork,
		ApplyCodeIndexBatch:    service.ApplyCodeIndexBatch,
		ApplyNoteIndexBatch:    service.ApplyNoteIndexBatch,
		ApplyNoteMetadataDelta: intelStore.ApplyNoteMetadataDelta,
		ApplyNoteMetaBatch: func(ctx context.Context, infos []embeddings.NoteFileInfo) error {
			if noteMetaBatch == nil {
				return fmt.Errorf("note meta queue not initialized")
			}
			return noteMetaBatch(ctx, infos)
		},
		ApplyCodeItemEmbeddingBatch: func(ctx context.Context, items []codeindex.ItemEmbeddingUpsert) error {
			if codeItemEmbBatch == nil {
				return fmt.Errorf("code item embedding queue not initialized")
			}
			if err := codeItemEmbBatch(ctx, items); err != nil {
				return fmt.Errorf("apply code item embedding batch items=%d: %w", len(items), err)
			}
			return nil
		},
		ApplyCodeItemChunkBatch: func(ctx context.Context, items []codeindex.ItemChunksUpsert) error {
			if codeChunkBatch == nil {
				return fmt.Errorf("code chunk batch queue not initialized")
			}
			if err := codeChunkBatch(ctx, items); err != nil {
				return fmt.Errorf("apply code chunk batch items=%d rows=%d: %w", len(items), indexwriter.CountCodeChunkRows(items), err)
			}
			return nil
		},
		ApplyCodeItemChunks: func(ctx context.Context, anchorID codeindex.AnchorID, chunks []codeindex.ChunkInput, texts []string, vecs []embeddings.Embedding) error {
			if codeChunkWriter == nil {
				return fmt.Errorf("code chunk queue not initialized")
			}
			return codeChunkWriter(ctx, anchorID, chunks, texts, vecs)
		},
		ApplyNoteChunkSyncBatch: func(ctx context.Context, items []embeddings.NoteChunkSync) error {
			if noteChunkSyncBatch == nil {
				return fmt.Errorf("note chunk sync batch queue not initialized")
			}
			return noteChunkSyncBatch(ctx, items)
		},
		ApplyNoteChunkSync: func(ctx context.Context, item embeddings.NoteChunkSync) error {
			if noteChunkSyncWriter == nil {
				return fmt.Errorf("note chunk sync queue not initialized")
			}
			return noteChunkSyncWriter(ctx, item)
		},
		ApplyIntelChunks:         intelChunkWriter,
		ApplyIntelChunksByFamily: intelStore.ReplaceIntelChunksByFamily,
		ApplyIntelEmbeddings: func(ctx context.Context, rows map[string]embeddings.Embedding) error {
			if err := intelEmbWriter(ctx, rows); err != nil {
				return fmt.Errorf("apply intel embeddings rows=%d: %w", len(rows), err)
			}
			return nil
		},
		ApplyIntentEmbeddingSnapshot: intelStore.ReplaceIntentEmbeddingSnapshot,
		ApplyOntologyNodeReadModel:   intelStore.ReplaceOntologyNodeReadModel,
		ApplyOntologyNodeStates:      intelStore.UpsertOntologyNodeEmbeddingStates,
		ApplyOntologyDelta:           intelStore.ApplyOntologyDelta,
		ApplyValidationState: func(ctx context.Context, write indexwriter.ValidationStateWrite) error {
			generation, err := intelStore.SetValidationRunning(ctx)
			if err != nil {
				return err
			}
			if strings.TrimSpace(write.ErrorMessage) != "" {
				_, err = intelStore.SetValidationError(ctx, generation, write.ErrorMessage, write.DurationMs)
				return err
			}
			write.Snapshot.Generation = generation
			published, err := intelStore.PublishValidationSnapshot(ctx, write.Snapshot)
			if err == nil && !published {
				return fmt.Errorf("validation generation %d was superseded before queued publication", generation)
			}
			return err
		},
	}, service, intelStore))
	ctx = codeintel.WithWriteQueue(ctx, writeQueue)
	finalizer := &unifiedBatchFinalizer{writeQueue: writeQueue}
	defer func() {
		_ = finalizer.Shutdown(ctx)
	}()
	if reset := ensureReverseIndexReadiness(ctx, intelStore, func(format string, args ...any) {
		progress.Println(fmt.Sprintf("[index] Warning: "+format, args...))
	}); reset {
		progress.Println("[index] Reverse index marked stale; def-delta rebuilds will fall back until full scan completes.")
		ctx = codeanchor.WithForceReindex(ctx)
	}

	semanticResources, err := prepareUnifiedSemanticResources(ctx, unifiedPostIndexOptions{
		VaultPath:        opts.VaultPath,
		APIKey:           opts.APIKey,
		TxLockMode:       txLockMode,
		Vacuum:           opts.Vacuum,
		CurrentScopeHash: currentScopeHash,
		CodeEmbExplicit:  codeEmbExplicit,
	}, progress, sharedWriteMu, intelStore, writeQueue, &codeItemEmbBatch, &codeChunkBatch, &codeChunkWriter, &noteMetaBatch, &noteChunkSyncBatch, &noteChunkSyncWriter, codeCfg, embCfg, codeEmbCfg)
	if err != nil {
		return err
	}
	finalizer.semanticResources = semanticResources
	semanticCtx, semanticCancel := context.WithCancel(ctx)
	finalizer.semanticCancel = semanticCancel
	var fallbackReason string
	if ready, ok, err := intelStore.ReverseIndexBackfillComplete(ctx); err == nil {
		if !ok || !ready {
			fallbackReason = "reverse_index_backfill_incomplete"
		}
	} else {
		fallbackReason = "reverse_index_backfill_unknown"
	}
	streaming := newUnifiedStreamingCoordinator(
		semanticCtx,
		semanticCancel,
		progress,
		service,
		semanticResources.codeSyncer,
		semanticResources.noteSyncer,
		semanticResources.codeStore,
		fallbackReason,
	)
	streaming.rebuildCallEdges = nil
	streaming.rebuildGoPackages = nil
	streaming.rebuildScopeIDs = nil
	streaming.recomputeScopes = nil
	finalizer.streaming = streaming
	if notePaths, err := service.NotePaths(ctx); err == nil && len(notePaths) > 0 {
		if anchorsByPath, err := service.AnchorsByNotePaths(ctx, notePaths); err == nil {
			streaming.initialScopeAnchors = anchorsByPath
		}
	}
	streaming.reverseIndexReady = func(stageCtx context.Context) (bool, error) {
		ready, ok, err := intelStore.ReverseIndexBackfillComplete(stageCtx)
		if err != nil {
			return false, err
		}
		return ok && ready, nil
	}
	var ontologyBodySchema *ontology.Schema
	if semanticResources.noteSyncer != nil {
		schema, err := ontology.LoadSchema(opts.VaultPath)
		if err != nil && !errors.Is(err, ontology.ErrNoOntologyFiles) {
			return err
		}
		if err == nil && schema != nil {
			ontologyBodySchema = schema
			streaming.syncNoteBodyPaths = func(stageCtx context.Context, changedPaths []string, deletedPaths []string, fullSync bool) error {
				return syncOntologyNodeEmbeddings(stageCtx, opts.VaultDef, opts.NoteMetadata, intelStore, semanticResources.noteProv, semanticResources.noteSyncer.ProviderInfo, ontologyBodySchema, semanticResources.noteSyncer.BatchSize, semanticResources.noteSyncer.MaxConcurrent, semanticResources.noteSyncer.EmbedGate, semanticResources.noteNode, writeQueue, changedPaths, deletedPaths, fullSync)
			}
		}
	}
	postCodeSubmitter := newAsyncCodeBatchSubmitter(semanticCtx, streaming.SubmitCodeBatch, streaming.fail)
	postWriteDispatcher := newPostWriteDispatcher(semanticCtx, postCodeSubmitter, streaming, streaming.fail)
	asyncSemanticSubmitter := codeintel.NewAsyncCodeSemanticSubmitter(semanticCtx, streaming, streaming.fail)
	finalizer.postCodeSubmitter = postCodeSubmitter
	finalizer.postWriteDispatcher = postWriteDispatcher
	finalizer.asyncSemanticSubmitter = asyncSemanticSubmitter
	if writeQueue != nil {
		writeQueue.SetAfterCodeIndexBatch(postWriteDispatcher.EnqueueCodeBatch)
		writeQueue.SetAfterNoteIndexBatch(postWriteDispatcher.EnqueueNoteIndexBatch)
	}
	ctx = codeintel.WithCodeSemanticBatchSubmitter(ctx, asyncSemanticSubmitter)

	streaming.finalDrain = newFinalDrainProgress(progress)
	progress.SetSegment(0, mainPhaseEnd)
	structuralResult, err := indexcore.Publish(ctx, structuralRequest, ownershipDiscovery, service, intelStore, writeQueue, indexcore.PublishOptions{
		BeforeIngest: func(codeFiles, noteFiles int) *indexingpipe.ProgressCallbacks {
			if semanticResources.noteSyncer != nil || semanticResources.codeSyncer != nil {
				semanticResources.startIntentSync(semanticCtx, queuedIntentStore{Store: intelStore, queue: writeQueue})
			}
			fileProgress := newFixedTotalFileProgress(progress, codeFiles+noteFiles)
			streaming.workProgress = fileProgress
			streaming.Start()
			progress.SetStage("Indexing code and notes")
			return fileProgress.callbacks()
		}, AfterPreparedOwnershipCommit: opts.afterPreparedOwnershipCommit,
	})
	if err != nil {
		return err
	}
	if structuralResult.Reconciliation.Required() {
		if err := writeQueue.AcknowledgeOwnershipReconciliation(ctx, structuralResult.StructuralGeneration); err != nil {
			return fmt.Errorf("acknowledge structural ownership: %w", err)
		}
	}
	total, noteResult, initialOntologyResult := structuralResult.Code, structuralResult.Notes, structuralResult.Ontology
	progress.Println(fmt.Sprintf("[index] Indexed %d code files (%d unchanged)", total.Indexed, total.Unchanged))
	progress.Println(fmt.Sprintf("[index] Ingested %d notes (%d unchanged)", noteResult.Count, noteResult.Unchanged))
	printNoteBuildWarnings(progress, noteResult.BuildErrors)
	if err := streaming.SubmitDeletedCodePaths(ctx, structuralResult.CodeRetirements); err != nil {
		return err
	}
	if initialOntologyResult != nil && initialOntologyResult.Schema != nil {
		ontologyBodySchema = initialOntologyResult.Schema
	}
	if (initialOntologyResult != nil && initialOntologyResult.Rebuilt) || ownershipDiscovery.NotesRecovery || ownershipDiscovery.OntologyRecovery {
		allPaths, err := ontology.ProjectableMetadataPaths(ctx, opts.NoteMetadata, intelStore)
		if err != nil {
			return err
		}
		progress.SetStage("Embedding ontology bodies")
		// IMPORTANT: typed-note body evidence is the primary semantic surface in
		// ontology-ready vaults; see [[indexing-workflow#^spec-0036-us2-ac2]].
		if err := streaming.SubmitNoteFullRebuild(ctx, allPaths); err != nil {
			return err
		}
	} else {
		progress.SetStage("Embedding ontology bodies")
		if err := streaming.SubmitNotePaths(ctx, noteResult.ChangedPaths); err != nil {
			return err
		}
	}
	// The ownership run is the complete code-owner authority for this full
	// index. Remove embedding/search rows for every path outside that set,
	// including when the current configuration has no code roots. Submit the
	// cleanup before stream finalization so failed cleanup retains derived debt.
	if err := submitStaleCodeEmbeddingCleanup(ctx, streaming, semanticResources.codeStore, structuralResult.Snapshot.CodeKeepPaths()); err != nil {
		return err
	}
	progress.SetStage("Finalizing stream")
	if err := finalizer.CloseAsyncSemanticSubmitter(); err != nil {
		return err
	}
	if err := streaming.SubmitDeletedNotePaths(ctx, noteResult.DeletedPaths); err != nil {
		return err
	}
	if err := finalizer.FlushQueue(ctx); err != nil {
		return err
	}
	// NOTE: deleted note paths are submitted during the finalization phase so
	// earlier changed-path batches can coalesce with code streaming work. This
	// keeps scoped updates cheap while still cleaning stale chunks before sync
	// marks are persisted.
	if err := finalizer.ClosePostWriteDispatcher(); err != nil {
		return err
	}
	if err := finalizer.ClosePostCodeSubmitter(); err != nil {
		return err
	}
	if err := finalizer.DrainStreamingWrites(ctx); err != nil {
		return err
	}
	var finalOntologyWG sync.WaitGroup
	var finalOntologyErrs errCollector
	var finalOntologyOnce sync.Once
	var finalOntologyErr error
	waitForFinalOntology := func() error {
		finalOntologyOnce.Do(func() {
			finalOntologyWG.Wait()
			finalOntologyErr = finalOntologyErrs.Err()
		})
		return finalOntologyErr
	}
	finalOntologyWG.Add(1)
	go func() {
		defer finalOntologyWG.Done()
		if !ontologyFollowupNeeded(initialOntologyResult, noteResult.ChangedPaths, noteResult.DeletedPaths) {
			return
		}
		ontologyResult, err := ontology.SyncPublishedPaths(ctx, opts.NoteMetadata, opts.VaultDef, &obsidian.Note{}, intelStore, writeQueue, noteResult.ChangedPaths, noteResult.DeletedPaths)
		if err != nil {
			finalOntologyErrs.capture(fmt.Errorf("sync ontology: %w", err))
			return
		}
		if err := finalizer.FlushQueue(ctx); err != nil {
			finalOntologyErrs.capture(err)
			return
		}
		request, ok := ontologyFollowupEmbeddingRequest(ontologyResult, noteResult.ChangedPaths, noteResult.DeletedPaths)
		if semanticResources == nil || semanticResources.noteSyncer == nil || !ok {
			return
		}
		progress.SetStage("Refreshing ontology body embeddings")
		if err := runPhase(ctx, "sync_ontology_body_final", func(phaseCtx context.Context) error {
			return syncOntologyNodeEmbeddings(
				phaseCtx,
				opts.VaultDef,
				opts.NoteMetadata,
				intelStore,
				semanticResources.noteProv,
				semanticResources.noteSyncer.ProviderInfo,
				request.schema,
				semanticResources.noteSyncer.BatchSize,
				semanticResources.noteSyncer.MaxConcurrent,
				semanticResources.noteSyncer.EmbedGate,
				semanticResources.noteNode,
				writeQueue,
				request.changedPaths,
				request.deletedPaths,
				request.rebuilt,
			)
		}); err != nil {
			finalOntologyErrs.capture(fmt.Errorf("refresh ontology body embeddings: %w", err))
			return
		}
		if err := finalizer.FlushQueue(ctx); err != nil {
			finalOntologyErrs.capture(err)
		}
	}()
	finalizer.waitFinalOntology = waitForFinalOntology
	progress.SetStage("Finalizing stream")
	progress.SetSegment(mainPhaseEnd, finalDrainEnd)
	if err := finalizer.CloseStreaming(); err != nil {
		return err
	}
	progress.AdvanceTo("Finalizing stream", finalDrainEnd)
	progress.SetStage("Syncing intent exemplars")
	if err := finalizer.WaitIntent(); err != nil {
		return err
	}
	progress.AdvanceTo("Syncing intent exemplars", metadataSyncEnd)
	if err := finalizer.WaitFinalOntology(); err != nil {
		return err
	}
	// Streaming overlaps cold ingest, but its prepared tasks may precede final
	// cross-file calls and related-document labels. A global code obligation
	// requires a fresh complete-owner plan after those inputs and writes settle.
	// Existing chunk hashes suppress provider calls for already-current work.
	if semanticResources.codeSyncer != nil && (ownershipDiscovery.CodeRecovery || total.DefDeltas.HasChanges() || len(noteResult.ChangedPaths) > 0 || len(noteResult.DeletedPaths) > 0) {
		progress.SetStage("Refreshing code context")
		if err := runPhase(ctx, "sync_code_context_final", func(phaseCtx context.Context) error {
			keep := structuralResult.Snapshot.CodeKeepPaths()
			paths := make([]string, 0, len(keep))
			for _, path := range keep {
				paths = append(paths, path.String())
			}
			plan, err := semanticResources.codeSyncer.PlanPaths(phaseCtx, paths)
			if err != nil {
				return err
			}
			return semanticResources.codeSyncer.EmbedWithoutSyncMark(phaseCtx, plan)
		}); err != nil {
			return fmt.Errorf("refresh final code context: %w", err)
		}
	}
	progress.AdvanceTo("Flushing semantic writes", finalDrainEnd)
	if err := finalizer.FlushQueue(ctx); err != nil {
		return err
	}
	if err := streaming.MarkNoteLastSync(ctx); err != nil {
		return err
	}

	if structuralResult.Reconciliation.Required() || ownershipDiscovery.NotesRecovery || ownershipDiscovery.OntologyRecovery {
		if err := reconcilePendingNoteSemanticKeepSet(ctx, semanticResources, finalizer.FlushQueue); err != nil {
			return fmt.Errorf("reconcile pending note semantic keep set: %w", err)
		}
	}

	if err := runUnifiedPostIndex(ctx, unifiedPostIndexOptions{
		ForceGraph:            ownershipDiscovery.Recovery || ownershipDiscovery.GraphRecovery || (initialOntologyResult != nil && (initialOntologyResult.Dirty || initialOntologyResult.Rebuilt)),
		VaultPath:             opts.VaultPath,
		APIKey:                opts.APIKey,
		TxLockMode:            txLockMode,
		Vacuum:                opts.Vacuum,
		CurrentScopeHash:      currentScopeHash,
		CodeEmbExplicit:       codeEmbExplicit,
		SkipConfigPersistence: opts.SkipConfigPersistence,
	}, progress, intelStore, writeQueue, semanticResources, codeCfg, embCfg, codeEmbCfg, total, noteResult); err != nil {
		return err
	}
	// Each semantic lane uses the shared computation/publication executor. The
	// complete batch keeps its outer lease, and this exact control drains all
	// destination writes before clearing debt. Enabled but unavailable lanes
	// retain their durable obligations for the runtime to recover later.
	completedDerived := make([]codeanchor.DerivedWork, 0, len(structuralResult.DerivedWork))
	for _, work := range structuralResult.DerivedWork {
		switch work.Kind {
		case codeanchor.DerivedGraph:
			completedDerived = append(completedDerived, work)
		case codeanchor.DerivedNotes, codeanchor.DerivedOntology:
			if !embCfg.Enabled || semanticResources.noteSyncer != nil {
				completedDerived = append(completedDerived, work)
			}
		case codeanchor.DerivedCode:
			if !codeEmbCfg.Enabled || semanticResources.codeSyncer != nil {
				completedDerived = append(completedDerived, work)
			}
		}
	}
	if err := writeQueue.SubmitDerivedAcknowledgement(ctx, completedDerived); err != nil {
		return fmt.Errorf("acknowledge completed derived work: %w", err)
	}
	// Run the configured default validation suite from the completed index state
	// and cache the result through the writer lane for instant API serving.
	runtime, runtimeErr := ontology.PublishedRuntimeWithStore(ctx, opts.NoteMetadata, opts.VaultDef, intelStore)
	if runtime != nil && runtime.Schema != nil {
		runtimeErr = nil
	}
	progress.SetStage("Validating index")
	validationSpan := indexingperf.StartSpan(ctx, "validation_snapshot")
	validationSnapshot, validationDurationMs, validationErr := validate.IndexValidationSnapshot(ctx, opts.NoteMetadata, opts.VaultDef, runtime, runtimeErr, 500)
	validationSpan(validationErr)
	if err := ctx.Err(); err != nil {
		return err
	}
	if validationErr != nil {
		log.Printf("validation index: %v", validationErr)
	}
	writeValidation := func(stageCtx context.Context) error {
		return writeQueue.SubmitValidationSnapshot(stageCtx, validationSnapshot, validationErrorMessage(validationErr), validationDurationMs)
	}
	if err := writeValidation(ctx); err != nil {
		log.Printf("validation state queue: %v", err)
	} else if err := finalizer.FlushQueue(ctx); err != nil {
		log.Printf("validation state flush: %v", err)
	}

	progress.Finish("Index complete")
	return nil
}

// reconcilePendingNoteSemanticKeepSet provides the crash-recovery barrier for
// raw note embeddings. Ownership can have removed a source before a process
// stopped, leaving no rediscoverable deleted path for the streaming lane. A
// blocking full note sync prunes every embedding outside the current keep set
// before exact derived obligations are acknowledged.
func reconcilePendingNoteSemanticKeepSet(
	ctx context.Context,
	resources *unifiedSemanticResources,
	flush func(context.Context) error,
) error {
	if resources == nil || resources.noteStore == nil {
		return nil
	}
	if resources.noteSyncer == nil {
		return fmt.Errorf("note semantic store is available but note syncer is unavailable")
	}
	if flush == nil {
		return fmt.Errorf("note semantic keep-set flush is required")
	}
	if err := syncRawNoteEmbeddings(ctx, resources.noteSyncer, 0); err != nil {
		return err
	}
	return flush(ctx)
}

func printNoteBuildWarnings(progress ProgressSink, buildErrors []codeintel.NoteIngestBuildError) {
	if progress == nil || len(buildErrors) == 0 {
		return
	}
	const maxShown = 5
	limit := len(buildErrors)
	if limit > maxShown {
		limit = maxShown
	}
	for i := 0; i < limit; i++ {
		warning := buildErrors[i]
		progress.Println(fmt.Sprintf("[index] Warning: skipped note indexing for %s: %s", warning.Path, warning.Error))
	}
	if len(buildErrors) > limit {
		progress.Println(fmt.Sprintf("[index] Warning: skipped note indexing for %d additional notes", len(buildErrors)-limit))
	}
}

func validationErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
