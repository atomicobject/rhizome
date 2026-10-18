package validationproduct

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ValidationProjectionTarget selects the durable live index or an isolated,
// process-local scratch database. Scratch location is intentionally not caller
// configurable so WAL never lands on a shared checkout filesystem.
type ValidationProjectionTarget string

const (
	ValidationProjectionLive    ValidationProjectionTarget = "live"
	ValidationProjectionScratch ValidationProjectionTarget = "scratch"
)

// ValidationProjectionPaths is an exact post-mutation refresh set. A nil
// request discovers dirtiness; a non-nil empty set is an explicit no-op.
type ValidationProjectionPaths struct {
	Changed []paths.NotePath
	Deleted []paths.NotePath
}

type ValidationProjectionRequest struct {
	// Lane owns projection writes inside a live runtime. One-shot callers leave it nil.
	Lane         lane.Lane
	VaultPath    string
	VaultDef     obsidian.VaultDefinition
	NoteMetadata notemeta.Indexer
	NoteReader   obsidian.NoteReader
	Target       ValidationProjectionTarget
	ExactPaths   *ValidationProjectionPaths
	// BeforeMutation runs while the canonical index lock is held and before a
	// live or scratch projection store is opened. Product orchestration uses it
	// for read-only barriers that must be atomic with projection entry.
	BeforeMutation func(context.Context) error
	// refreshNoteAnchors is post-apply-only. Normal live and scratch validation
	// projection deliberately leave independently indexed anchor declarations
	// untouched.
	refreshNoteAnchors     bool
	noteAnchorChangedPaths []paths.NotePath
}

type ProjectionDomain string

const (
	ProjectionDomainMetadata    ProjectionDomain = "metadata"
	ProjectionDomainOntology    ProjectionDomain = "ontology"
	ProjectionDomainCode        ProjectionDomain = "code"
	ProjectionDomainCodeAnchors ProjectionDomain = "code_anchors"
	ProjectionDomainChunks      ProjectionDomain = "chunks"
	ProjectionDomainEmbeddings  ProjectionDomain = "embeddings"
	ProjectionDomainGraphScores ProjectionDomain = "graph_scores"
)

type ProjectionState string

const (
	ProjectionFresh       ProjectionState = "fresh"
	ProjectionUntouched   ProjectionState = "untouched"
	ProjectionUnavailable ProjectionState = "unavailable"
)

type ProjectionFreshness struct {
	State      ProjectionState `json:"state"`
	Hash       string          `json:"hash,omitempty"`
	Generation int64           `json:"generation,omitempty"`
}

type ValidationProjectionCounters struct {
	NoteScans               int `json:"noteScans"`
	ExactPathsRead          int `json:"exactPathsRead"`
	MetadataBatches         int `json:"metadataBatches"`
	OntologyNotesConsidered int `json:"ontologyNotesConsidered"`
	QueueFlushes            int `json:"queueFlushes"`
	ProjectCodeEnumerations int `json:"projectCodeEnumerations"`
	ProviderCalls           int `json:"providerCalls"`
}

type ValidationProjectionResult struct {
	Runtime      *ontology.Runtime                        `json:"-"`
	IndexPath    string                                   `json:"indexPath"`
	ChangedPaths []paths.NotePath                         `json:"changedPaths,omitempty"`
	DeletedPaths []paths.NotePath                         `json:"deletedPaths,omitempty"`
	Freshness    map[ProjectionDomain]ProjectionFreshness `json:"freshness"`
	Counters     ValidationProjectionCounters             `json:"counters"`
	Timings      map[string]time.Duration                 `json:"timings"`

	closeOnce sync.Once
	closeFn   func() error
	closeErr  error
}

func (r *ValidationProjectionResult) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if r.closeFn != nil {
			r.closeErr = r.closeFn()
		}
	})
	return r.closeErr
}

// RefreshValidationProjection owns the normal index-lock lifecycle.
func RefreshValidationProjection(ctx context.Context, request ValidationProjectionRequest) (*ValidationProjectionResult, error) {
	request = normalizeValidationProjectionRequest(request)
	if err := validateValidationProjectionRequest(request); err != nil {
		return nil, err
	}
	if request.Lane != nil {
		return refreshValidationProjectionOnLane(ctx, request)
	}
	lockStarted := time.Now()
	lease, err := indexing.TryAcquireIndexLock(ctx, request.VaultPath, indexing.LockWaitOptions{RequestPriority: true, Out: &bytes.Buffer{}})
	if err != nil {
		return nil, err
	}
	lockWait := time.Since(lockStarted)
	if request.BeforeMutation != nil {
		if barrierErr := request.BeforeMutation(ctx); barrierErr != nil {
			return nil, errors.Join(barrierErr, lease.Release())
		}
	}
	result, refreshErr := refreshValidationProjectionWithHeldIndexLock(ctx, lease, request)
	releaseErr := lease.Release()
	if refreshErr != nil {
		if result != nil {
			_ = result.Close()
		}
		return nil, refreshErr
	}
	if releaseErr != nil {
		_ = result.Close()
		return nil, releaseErr
	}
	result.Timings["lock_wait"] = lockWait
	return result, nil
}

func refreshValidationProjectionWithHeldIndexLock(ctx context.Context, lease *indexlock.Lease, request ValidationProjectionRequest) (*ValidationProjectionResult, error) {
	if err := validateValidationProjectionRequest(request); err != nil {
		return nil, err
	}
	if err := lease.Validate(request.VaultPath); err != nil {
		return nil, fmt.Errorf("validation projection lease: %w", err)
	}
	store, indexPath, cleanup, err := openValidationProjectionStore(request)
	if err != nil {
		return nil, err
	}
	return refreshValidationProjectionWithHeldStore(ctx, lease, request, store, indexPath, cleanup)
}

// refreshValidationProjectionWithHeldStore runs the projection while its
// caller owns both the canonical index lock and the store lifetime. It is used
// by post-mutation coordinators that must open the live writer before editing
// source, then retain that exact writer through metadata and ontology
// convergence.
func refreshValidationProjectionWithHeldStore(
	ctx context.Context,
	lease *indexlock.Lease,
	request ValidationProjectionRequest,
	store *semdb.Store,
	indexPath string,
	cleanup func() error,
) (*ValidationProjectionResult, error) {
	result := &ValidationProjectionResult{
		IndexPath: indexPath,
		Freshness: untouchedProjectionFreshness(),
		Timings:   map[string]time.Duration{},
		closeFn:   cleanup,
	}
	if err := validateValidationProjectionRequest(request); err != nil {
		return nil, errors.Join(err, result.Close())
	}
	if err := lease.Validate(request.VaultPath); err != nil {
		return nil, errors.Join(fmt.Errorf("validation projection lease: %w", err), result.Close())
	}
	reader := request.NoteReader
	if reader == nil {
		reader = &obsidian.Note{}
	}
	store.SetWriteMu(&sync.Mutex{})
	handlers := indexwriter.Handlers{
		ApplyNoteMetadataDelta:     store.ApplyNoteMetadataDelta,
		ApplyOntologyDelta:         store.ApplyOntologyDeltaPreservingSemantic,
		ApplyOntologyNodeReadModel: store.ReplaceOntologyNodeReadModelPreservingSemantic,
	}
	var anchorService *codeanchor.Service
	if request.refreshNoteAnchors {
		anchorService = codeanchor.NewServiceWithOptions(
			store, nil,
			codeanchor.WithBasePath(request.VaultPath),
			codeanchor.WithoutWarmCache(),
		)
		handlers.ApplyNoteIndexBatch = func(ctx context.Context, works []codeanchor.NoteIndexWork) error {
			notes := make([]codeanchor.NoteWithKeepLabels, 0, len(works))
			for _, work := range works {
				notes = append(notes, codeanchor.NoteWithKeepLabels{Note: work.Note, KeepLabels: work.KeepLabels})
			}
			_, err := store.UpsertNotesWithCleanupBatch(ctx, notes)
			return err
		}
		handlers.ApplyNoteMetadataDelta = func(ctx context.Context, delta semdb.NoteMetadataDelta) error {
			if err := store.ApplyNoteMetadataDelta(ctx, delta); err != nil {
				return err
			}
			if len(delta.DeletedPaths) == 0 {
				return nil
			}
			_, err := store.GarbageCollectOrphanedAnchors(ctx)
			return err
		}
	}
	queue := indexwriter.New(ctx, handlers)
	queueClosed := false
	fail := func(cause error) (*ValidationProjectionResult, error) {
		cleanupErr := closeValidationProjectionAfterError(queue, result, cause)
		queueClosed = true
		return nil, cleanupErr
	}
	defer func() {
		if !queueClosed {
			_ = queue.Close()
		}
	}()

	changed, deleted, discoverErr := validationProjectionPaths(ctx, request, reader, store, &result.Counters, result.Timings)
	if discoverErr != nil {
		return fail(discoverErr)
	}
	result.ChangedPaths = stringPathsToNotePaths(changed)
	result.DeletedPaths = stringPathsToNotePaths(deleted)
	if request.ExactPaths != nil && len(changed)+len(deleted) == 0 {
		if err := populateValidationProjectionEvidence(ctx, request.VaultDef, store, result); err != nil {
			return fail(err)
		}
		if request.refreshNoteAnchors {
			if err := markValidationCodeAnchorsFresh(ctx, store, result); err != nil {
				return fail(err)
			}
		}
		if err := queue.Close(); err != nil {
			return fail(err)
		}
		queueClosed = true
		return result, nil
	}

	ontologyChanged := append([]string(nil), changed...)
	ontologyDeleted := append([]string(nil), deleted...)
	sourceChanged, sourceDeleted, anchorChanged, err := validationProjectionSourcePaths(request, changed, deleted)
	if err != nil {
		return fail(err)
	}
	ontologyDeleted = unionOntologyPaths(ontologyDeleted, sourceDeleted)
	if len(changed)+len(deleted) > 0 {
		finishSourceSpan := indexingperf.StartSpan(ctx, "validation_projection_source")
		started := time.Now()
		delta, err := request.NoteMetadata.BuildPathDelta(ctx, request.VaultDef, reader, store, sourceChanged, sourceDeleted)
		if err != nil {
			finishSourceSpan(err)
			return fail(fmt.Errorf("build note metadata delta: %w", err))
		}
		result.Timings["metadata_build"] = time.Since(started)
		if delta != nil {
			// The delta both narrows (content-identical notes need no ontology
			// work) and expands (a bootstrap covers more than was requested).
			// Exact paths are an explicit instruction, so they are a floor the
			// narrowing must not cut below; discovered dirty paths are not.
			deltaChanged := noteMetadataDeltaChangedPaths(*delta)
			deltaDeleted := append([]string(nil), delta.DeletedPaths...)
			if request.ExactPaths == nil {
				ontologyChanged, ontologyDeleted = deltaChanged, deltaDeleted
			} else {
				ontologyChanged = unionOntologyPaths(ontologyChanged, deltaChanged)
				ontologyDeleted = unionOntologyPaths(ontologyDeleted, deltaDeleted)
			}
			phaseCtx := indexingperf.WithPhase(ctx, "validation_projection_source")
			if err := queue.SubmitNoteMetadataDelta(phaseCtx, *delta); err != nil {
				finishSourceSpan(err)
				return fail(err)
			}
			result.Counters.MetadataBatches++
			if err := queue.FlushAndWait(phaseCtx); err != nil {
				finishSourceSpan(err)
				return fail(err)
			}
			result.Counters.QueueFlushes++
		}
		finishSourceSpan(nil)
	}
	if request.refreshNoteAnchors {
		if len(anchorChanged) > 0 {
			started := time.Now()
			anchorCtx := indexingperf.WithPhase(ctx, "validation_projection_code_anchors")
			sources, err := request.NoteMetadata.LoadNoteSourceSnapshots(anchorCtx, request.VaultDef, reader, store, anchorChanged)
			if err != nil {
				return fail(fmt.Errorf("load exact code-anchor sources: %w", err))
			}
			for _, source := range sources {
				declarations, err := anchorService.ExtractAnchorDeclarations(anchorCtx, source)
				if err != nil {
					return fail(fmt.Errorf("extract code-anchor source %s: %w", source.Path, err))
				}
				if err := queue.SubmitNoteIndexWork(anchorCtx, codeanchor.NoteIndexWork{
					Path: source.Path.String(), Note: declarations.Note, KeepLabels: declarations.KeepLabels,
				}); err != nil {
					return fail(err)
				}
			}
			if err := queue.FlushAndWait(anchorCtx); err != nil {
				return fail(err)
			}
			result.Counters.QueueFlushes++
			result.Timings["code_anchors"] = time.Since(started)
		}
	}

	ontologyStarted := time.Now()
	finishOntologySpan := indexingperf.StartSpan(ctx, "validation_projection_ontology")
	ontologyCtx := indexingperf.WithPhase(ctx, "validation_projection_ontology")
	ontologyResult, err := ontology.SyncPublishedPaths(ontologyCtx, request.NoteMetadata, request.VaultDef, reader, store, queue, ontologyChanged, ontologyDeleted)
	if err != nil {
		finishOntologySpan(err)
		return fail(fmt.Errorf("sync validation ontology: %w", err))
	}
	if err := queue.FlushAndWait(ontologyCtx); err != nil {
		finishOntologySpan(err)
		return fail(err)
	}
	finishOntologySpan(nil)
	result.Counters.QueueFlushes++
	result.Timings["ontology"] = time.Since(ontologyStarted)
	if ontologyResult != nil {
		result.Counters.OntologyNotesConsidered = ontologyResult.Considered
		result.Runtime = &ontology.Runtime{
			Schema: ontologyResult.Schema,
			Store:  store,
			Issues: ontologyResult.Issues,
			Ready:  ontologyResult.Schema != nil,
		}
	} else {
		result.Runtime = &ontology.Runtime{Store: store}
	}

	if err := populateValidationProjectionEvidence(ctx, request.VaultDef, store, result); err != nil {
		return fail(err)
	}
	if request.refreshNoteAnchors {
		if err := markValidationCodeAnchorsFresh(ctx, store, result); err != nil {
			return fail(err)
		}
	}
	if err := queue.Close(); err != nil {
		return fail(err)
	}
	queueClosed = true
	return result, nil
}

func closeValidationProjectionAfterError(queue *indexwriter.Writer, result *ValidationProjectionResult, cause error) error {
	// Stop and await the writer before closing SQLite. A later producer error can
	// leave earlier ontology work queued even though the coordinator is failing.
	queueErr := queue.StopAndWait()
	resultErr := result.Close()
	return errors.Join(cause, queueErr, resultErr)
}
