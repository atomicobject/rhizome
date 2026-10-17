package indexing

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

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

	"sync"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"

	// ProgressSink is the minimal user-visible output surface used by indexing
	// phases that only need to print status lines.
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

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
	Config       *repoconfig.Resolved
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

type noopProgress struct{}

func (noopProgress) Println(string)  {}
func (noopProgress) Update(int, int) {}

func withProgressBar(p ProgressBar) ProgressBar {
	if p == nil {
		return noopProgress{}
	}
	return p
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
func RunUnifiedCore(ctx context.Context, lease *indexlock.Lease, opts UnifiedOptions) (err error) {
	done := indexingperf.StartSpan(ctx, "unified_core")
	defer func() { done(err) }()
	if err := lease.Validate(opts.VaultPath); err != nil {
		return fmt.Errorf("unified indexing lease: %w", err)
	}
	if err := opts.NoteMetadata.Validate(); err != nil {
		return fmt.Errorf("note metadata indexer: %w", err)
	}
	if err := lease.Validate(opts.VaultDef.BasePath()); err != nil {
		return fmt.Errorf("unified vault definition lease: %w", err)
	}
	rawProgress := withProgressBar(opts.ProgressBar)
	progress := newIntegratedProgress(rawProgress, opts.Verbose)
	txLockMode := opts.TxLockMode
	if txLockMode == "" {
		txLockMode = sqliteutil.TxLockImmediate
	}

	var resolved repoconfig.Resolved
	if err := runPhase(ctx, "open_config", func(context.Context) error {
		if opts.Config != nil {
			resolved = *opts.Config
			return resolved.ValidateFor(opts.VaultPath)
		}
		var err error
		resolved, err = repoconfig.Load(opts.VaultPath)
		return err
	}); err != nil {
		return err
	}
	codeCfg, embCfg := resolved.Code, resolved.NoteEmbeddings
	codeEmbCfg, codeEmbExplicit := resolved.CodeEmbeddingsFor(embCfg)
	var intelStore *semdb.Store
	var cleanup func()
	if err := runPhase(ctx, "open_stores", func(ctx context.Context) error {
		var err error
		intelStore, cleanup, err = indexstore.Open(
			opts.VaultPath, indexstore.Options{Access: indexstore.ReadWrite, Config: &codeCfg, Create: true, Repair: true, Context: ctx, TxLockMode: txLockMode},
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
	localCfg := resolved.Local
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
	service, _ := codeintel.NewCodeAnchorService(codeintel.CodeAnchorServiceConfig{
		VaultPath:       opts.VaultPath,
		CodeCfg:         codeCfg,
		Store:           intelStore,
		TailIndex:       tailIdx,
		Linker:          linker,
		IncludeIndexers: true,
		WriteAccess:     true,
	})
	semanticResources, err := prepareUnifiedSemanticResources(ctx, unifiedSemanticSetupOptions{
		VaultPath:  opts.VaultPath,
		APIKey:     opts.APIKey,
		TxLockMode: txLockMode,
	}, progress, sharedWriteMu, intelStore, codeCfg, embCfg, codeEmbCfg)
	if err != nil {
		return err
	}
	writeQueue := indexwriter.New(ctx, indexcore.BindWriterHandlers(semanticResources.writerHandlers(intelStore), service, intelStore))
	semanticResources.bindWriter(writeQueue)
	finalizer := &unifiedBatchFinalizer{writeQueue: writeQueue, semanticResources: semanticResources}
	defer func() {
		_ = finalizer.Shutdown(ctx)
	}()
	if reset := reverseIndexNeedsRebuild(ctx, intelStore, func(format string, args ...any) {
		progress.Println(fmt.Sprintf("[index] Warning: "+format, args...))
	}); reset {
		if err := writeQueue.SubmitReverseIndexReset(ctx); err != nil {
			return err
		}
		progress.Println("[index] Reverse index marked stale; def-delta rebuilds will fall back until full scan completes.")
		ctx = codeanchor.WithForceReindex(ctx)
	}

	semanticCtx, semanticCancel := context.WithCancel(ctx)
	finalizer.semanticCancel = semanticCancel
	streaming := newUnifiedStreamingCoordinator(semanticCtx, semanticCancel,
		semanticResources.codeSyncer, semanticResources.noteSyncer)
	finalizer.streaming = streaming
	var ontologyBodySchema *ontoschema.Schema
	if semanticResources.noteSyncer != nil {
		schema, err := ontoschema.LoadSchema(opts.VaultPath)
		if err != nil && !errors.Is(err, ontoschema.ErrNoOntologyFiles) {
			return err
		}
		if err == nil && schema != nil {
			ontologyBodySchema = schema
			streaming.syncNoteBodyPaths = func(stageCtx context.Context, changedPaths []string, deletedPaths []string, fullSync bool) error {
				return syncOntologyNodeEmbeddings(stageCtx, opts.VaultDef, opts.NoteMetadata, intelStore, semanticResources.noteProv, semanticResources.noteSyncer.ProviderInfo, ontologyBodySchema, semanticResources.noteSyncer.BatchSize, semanticResources.noteSyncer.MaxConcurrent, semanticResources.noteSyncer.EmbedGate, semanticResources.noteNode, writeQueue, changedPaths, deletedPaths, fullSync)
			}
		}
	}
	var asyncSemanticSubmitter *codeintel.AsyncCodeSemanticSubmitter
	if semanticResources.codeSyncer != nil {
		asyncSemanticSubmitter = codeintel.NewAsyncCodeSemanticSubmitter(semanticCtx, streaming, streaming.fail)
	}
	finalizer.asyncSemanticSubmitter = asyncSemanticSubmitter
	// Structural finalization uses the same committed source snapshots as the
	// writer, without an asynchronous post-persistence hand-off.
	writeQueue.SetAfterCodeIndexBatch(func(_ context.Context, batch []codeanchor.CodeIndexWork) error {
		service.NoteRuntimeCodeIndexWorks(batch)
		return nil
	})
	if asyncSemanticSubmitter != nil {
		ctx = codeintel.WithCodeSemanticBatchSubmitter(ctx, asyncSemanticSubmitter)
	}

	progress.SetSegment(0, mainPhaseEnd)
	structuralResult, err := indexcore.Publish(ctx, lease, structuralRequest, ownershipDiscovery, service, intelStore, writeQueue, indexcore.PublishOptions{
		BeforeIngest: func(codeFiles, noteFiles int) *codeintel.ProgressCallbacks {
			fileProgress := newFixedTotalFileProgress(progress, codeFiles+noteFiles)
			streaming.Start()
			progress.SetStage("Indexing code and notes")
			return fileProgress.callbacks()
		}, AfterPreparedOwnershipCommit: opts.afterPreparedOwnershipCommit,
	})
	if err != nil {
		return err
	}

	total, noteResult, initialOntologyResult := structuralResult.Code, structuralResult.Notes, structuralResult.Ontology
	progress.Println(fmt.Sprintf("[index] Indexed %d code files (%d unchanged)", total.Indexed, total.Unchanged))
	progress.Println(fmt.Sprintf("[index] Ingested %d notes (%d unchanged)", noteResult.Count, noteResult.Unchanged))
	printNoteBuildWarnings(progress, noteResult.BuildErrors)
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
	progress.AdvanceTo("Finalizing metadata", metadataSyncEnd)
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
	validationSnapshot, validationDurationMs, validationErr := validationrun.IndexValidationSnapshot(ctx, opts.NoteMetadata, opts.VaultDef, runtime, runtimeErr, 500)
	validationSpan(validationErr)
	if err := ctx.Err(); err != nil {
		return err
	}
	if validationErr != nil {
		progress.Println(fmt.Sprintf("[index] Validation: %v", validationErr))
	}
	if err := writeQueue.SubmitValidationSnapshot(ctx, validationSnapshot, validationErrorMessage(validationErr), validationDurationMs); err != nil {
		return fmt.Errorf("publish validation state: %w", err)
	}
	if err := finalizer.FlushQueue(ctx); err != nil {
		return fmt.Errorf("flush validation state: %w", err)
	}

	progress.Finish("Index complete")
	return nil
}
