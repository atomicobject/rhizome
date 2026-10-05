package indexing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
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
	ProjectionDomainMetadata        ProjectionDomain = "metadata"
	ProjectionDomainLinks           ProjectionDomain = "links"
	ProjectionDomainMarkdownTargets ProjectionDomain = "markdown_targets"
	ProjectionDomainOntology        ProjectionDomain = "ontology"
	ProjectionDomainCode            ProjectionDomain = "code"
	ProjectionDomainCodeAnchors     ProjectionDomain = "code_anchors"
	ProjectionDomainChunks          ProjectionDomain = "chunks"
	ProjectionDomainEmbeddings      ProjectionDomain = "embeddings"
	ProjectionDomainGraphScores     ProjectionDomain = "graph_scores"
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
	release, err := TryAcquireIndexLock(ctx, obsidian.IndexLockPath(request.VaultPath), true, false, &bytes.Buffer{})
	if err != nil {
		return nil, err
	}
	lockWait := time.Since(lockStarted)
	if request.BeforeMutation != nil {
		if barrierErr := request.BeforeMutation(ctx); barrierErr != nil {
			return nil, errors.Join(barrierErr, release())
		}
	}
	result, refreshErr := refreshValidationProjectionWithHeldIndexLock(ctx, request)
	releaseErr := release()
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

func refreshValidationProjectionWithHeldIndexLock(ctx context.Context, request ValidationProjectionRequest) (*ValidationProjectionResult, error) {
	if err := validateValidationProjectionRequest(request); err != nil {
		return nil, err
	}
	store, indexPath, cleanup, err := openValidationProjectionStore(request)
	if err != nil {
		return nil, err
	}
	return refreshValidationProjectionWithHeldStore(ctx, request, store, indexPath, cleanup)
}

// refreshValidationProjectionWithHeldStore runs the projection while its
// caller owns both the canonical index lock and the store lifetime. It is used
// by post-mutation coordinators that must open the live writer before editing
// source, then retain that exact writer through metadata and ontology
// convergence.
func refreshValidationProjectionWithHeldStore(
	ctx context.Context,
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

func markValidationCodeAnchorsFresh(ctx context.Context, store *semdb.Store, result *ValidationProjectionResult) error {
	summary, err := store.UntouchedIndexDomainsSummary(ctx)
	if err != nil {
		return err
	}
	selectors, err := store.CodeAnchorSelectorSummary(ctx)
	if err != nil {
		return err
	}
	result.Freshness[ProjectionDomainCodeAnchors] = ProjectionFreshness{
		State:      ProjectionFresh,
		Hash:       codeAnchorFreshnessHash(summary.CodeAnchors, selectors),
		Generation: summary.CodeAnchors.Generation,
	}
	return nil
}

func codeAnchorFreshnessHash(matches semdb.PersistedDomainSummary, selectors semdb.PersistedContentSummary) string {
	return fmt.Sprintf(
		"matches:%d;generation:%d;selectors:%d:%s",
		matches.Count, matches.Generation, selectors.Count, selectors.Hash,
	)
}

func closeValidationProjectionAfterError(queue *indexwriter.Writer, result *ValidationProjectionResult, cause error) error {
	// Stop and await the writer before closing SQLite. A later producer error can
	// leave earlier ontology work queued even though the coordinator is failing.
	queueErr := queue.StopAndWait()
	resultErr := result.Close()
	return errors.Join(cause, queueErr, resultErr)
}

func noteMetadataDeltaChangedPaths(delta semdb.NoteMetadataDelta) []string {
	paths := make([]string, 0, len(delta.Notes))
	for _, row := range delta.Notes {
		if path := strings.TrimSpace(row.Path); path != "" {
			paths = append(paths, path)
		}
	}
	return dedupeSortedStrings(paths)
}

func unionOntologyPaths(left, right []string) []string {
	return dedupeSortedStrings(append(append([]string(nil), left...), right...))
}

func dedupeSortedStrings(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, value := range input {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeValidationProjectionRequest(request ValidationProjectionRequest) ValidationProjectionRequest {
	if request.VaultPath == "" {
		request.VaultPath = request.VaultDef.BasePath()
	}
	if request.VaultDef.BasePath() == "" {
		request.VaultDef.Path = request.VaultPath
	}
	if request.Target == "" {
		request.Target = ValidationProjectionLive
	}
	return request
}

func validateValidationProjectionRequest(request ValidationProjectionRequest) error {
	if err := request.NoteMetadata.Validate(); err != nil {
		return fmt.Errorf("note metadata indexer: %w", err)
	}
	if strings.TrimSpace(request.VaultPath) == "" {
		return fmt.Errorf("vault path is required")
	}
	vaultPaths, err := paths.NewVaultPaths(request.VaultPath)
	if err != nil {
		return fmt.Errorf("resolve vault path: %w", err)
	}
	definitionPaths, err := paths.NewVaultPaths(request.VaultDef.BasePath())
	if err != nil {
		return fmt.Errorf("resolve vault definition path: %w", err)
	}
	if vaultPaths.Root() == "" || definitionPaths.Root() == "" {
		return fmt.Errorf("vault path is required")
	}
	if !paths.CaseEqual(vaultPaths.Root(), definitionPaths.Root()) {
		return fmt.Errorf("vault path %q does not match vault definition root %q", vaultPaths.Root(), definitionPaths.Root())
	}
	return nil
}

func openValidationProjectionStore(request ValidationProjectionRequest) (*semdb.Store, string, func() error, error) {
	if request.Target == ValidationProjectionScratch {
		dir, err := os.MkdirTemp("", "rzm-validation-")
		if err != nil {
			return nil, "", nil, err
		}
		indexPath := filepath.Join(dir, obsidian.RhizomeDBFile)
		store, err := semdb.Open(indexPath)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, "", nil, err
		}
		return store, indexPath, func() error {
			closeErr := store.Close()
			removeErr := os.RemoveAll(dir)
			return errors.Join(closeErr, removeErr)
		}, nil
	}
	if request.Target != ValidationProjectionLive {
		return nil, "", nil, fmt.Errorf("unknown validation projection target %q", request.Target)
	}
	config, err := obsidian.LoadCodeConfig(request.VaultPath)
	if err != nil {
		return nil, "", nil, err
	}
	indexPath := obsidian.UnifiedIndexPath(request.VaultPath, config.IndexPath)
	store, err := semdb.Open(indexPath)
	if err != nil {
		return nil, "", nil, err
	}
	return store, indexPath, store.Close, nil
}

func validationProjectionPaths(ctx context.Context, request ValidationProjectionRequest, reader obsidian.NoteReader, store notemeta.Store, counters *ValidationProjectionCounters, timings map[string]time.Duration) ([]string, []string, error) {
	if request.ExactPaths != nil {
		vaultPaths, err := paths.NewVaultPaths(request.VaultPath)
		if err != nil {
			return nil, nil, err
		}
		changed, err := normalizedNotePathStrings(vaultPaths, request.ExactPaths.Changed)
		if err != nil {
			return nil, nil, fmt.Errorf("normalize changed validation paths: %w", err)
		}
		deleted, err := normalizedNotePathStrings(vaultPaths, request.ExactPaths.Deleted)
		if err != nil {
			return nil, nil, fmt.Errorf("normalize deleted validation paths: %w", err)
		}
		counters.ExactPathsRead = len(changed) + len(deleted)
		return changed, deleted, nil
	}
	started := time.Now()
	dirty, err := request.NoteMetadata.DiscoverDirtyPaths(ctx, request.VaultDef, reader, store)
	timings["discover_notes"] = time.Since(started)
	if err != nil {
		return nil, nil, err
	}
	counters.NoteScans = 1
	return dirty.Changed, dirty.Deleted, nil
}

func normalizedNotePathStrings(vaultPaths paths.VaultPaths, input []paths.NotePath) ([]string, error) {
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, raw := range input {
		if strings.TrimSpace(string(raw)) == "" {
			continue
		}
		path, err := vaultPaths.RelNotePathStrict(string(raw))
		if err != nil {
			return nil, err
		}
		if path == "" {
			continue
		}
		pathString := path.String()
		if _, ok := seen[pathString]; ok {
			continue
		}
		seen[pathString] = struct{}{}
		out = append(out, pathString)
	}
	sort.Strings(out)
	return out, nil
}

func populateValidationProjectionEvidence(ctx context.Context, vaultDef obsidian.VaultDefinition, store *semdb.Store, result *ValidationProjectionResult) error {
	metadataState, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return err
	}
	if metadataState.Ready {
		evidence := ProjectionFreshness{State: ProjectionFresh, Hash: metadataState.NotesHash, Generation: metadataState.LoadedAt}
		result.Freshness[ProjectionDomainMetadata] = evidence
		result.Freshness[ProjectionDomainLinks] = evidence
		result.Freshness[ProjectionDomainMarkdownTargets] = evidence
	}
	ontologyState, err := store.GetOntologySchemaState(ctx)
	if err != nil {
		return err
	}
	if ontologyState.Ready && ontologyState.MaterializationVersion == ontology.OntologyMaterializationVersion {
		result.Freshness[ProjectionDomainOntology] = ProjectionFreshness{
			State:      ProjectionFresh,
			Hash:       fmt.Sprintf("%s;materialization:%d", ontologyState.SchemaHash, ontologyState.MaterializationVersion),
			Generation: ontologyState.LoadedAt,
		}
	}
	if err := populateUntouchedDomainEvidence(ctx, store, result.Freshness); err != nil {
		return err
	}
	if result.Runtime == nil {
		result.Runtime = &ontology.Runtime{Store: store, Issues: ontology.ValidationIssuesFromJSON(ontologyState.ErrorJSON)}
		schema, schemaErr := ontology.LoadSchema(vaultDef.BasePath())
		if schemaErr == nil && ontologyState.Ready && schema.Hash == ontologyState.SchemaHash && ontologyState.MaterializationVersion == ontology.OntologyMaterializationVersion {
			result.Runtime.Schema = schema
			result.Runtime.Ready = true
		} else if schemaErr != nil && !errors.Is(schemaErr, ontology.ErrNoOntologyFiles) {
			return schemaErr
		}
	}
	return nil
}

func populateUntouchedDomainEvidence(ctx context.Context, store *semdb.Store, freshness map[ProjectionDomain]ProjectionFreshness) error {
	summary, err := store.UntouchedIndexDomainsSummary(ctx)
	if err != nil {
		return err
	}
	packMetadata, err := store.GetPackMetadata(ctx)
	if err != nil {
		return err
	}
	codeHash := fmt.Sprintf("count:%d;indexer:%s", summary.Code.Count, packMetadata.IndexerVersion)
	freshness[ProjectionDomainCode] = persistedDomainEvidence(summary.Code.Count > 0 || packMetadata.IndexerVersion != "", summary.Code.Generation, codeHash)
	selectors, err := store.CodeAnchorSelectorSummary(ctx)
	if err != nil {
		return err
	}
	freshness[ProjectionDomainCodeAnchors] = persistedDomainEvidence(
		summary.CodeAnchors.Count > 0 || selectors.Count > 0,
		summary.CodeAnchors.Generation,
		codeAnchorFreshnessHash(summary.CodeAnchors, selectors),
	)

	chunks, err := store.IntelChunksSummary(ctx)
	if err != nil {
		return err
	}
	freshness[ProjectionDomainChunks] = persistedDomainEvidence(chunks.Count > 0, chunks.Generation, fmt.Sprintf("count:%d", chunks.Count))
	embeddings, err := store.EmbeddingsSummary(ctx)
	if err != nil {
		return err
	}
	embeddingHash := fmt.Sprintf("count:%d;model:%s", embeddings.Count, packMetadata.ModelHash)
	freshness[ProjectionDomainEmbeddings] = persistedDomainEvidence(embeddings.Count > 0, embeddings.Generation, embeddingHash)

	graphCount := summary.GraphDocScores.Count + summary.GraphAnchorScores.Count
	graphGeneration := summary.GraphDocScores.Generation
	if summary.GraphAnchorScores.Generation > graphGeneration {
		graphGeneration = summary.GraphAnchorScores.Generation
	}
	freshness[ProjectionDomainGraphScores] = persistedDomainEvidence(graphCount > 0, graphGeneration, fmt.Sprintf("docs:%d;anchors:%d", summary.GraphDocScores.Count, summary.GraphAnchorScores.Count))
	return nil
}

func persistedDomainEvidence(available bool, generation int64, hash string) ProjectionFreshness {
	if !available {
		return ProjectionFreshness{State: ProjectionUnavailable}
	}
	return ProjectionFreshness{State: ProjectionUntouched, Hash: hash, Generation: generation}
}

func stringPathsToNotePaths(input []string) []paths.NotePath {
	out := make([]paths.NotePath, 0, len(input))
	for _, path := range input {
		if normalized := paths.NormalizeNotePath(path); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func notePathsToStrings(input []paths.NotePath) []string {
	out := make([]string, 0, len(input))
	for _, path := range input {
		if normalized := path.String(); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func untouchedProjectionFreshness() map[ProjectionDomain]ProjectionFreshness {
	out := make(map[ProjectionDomain]ProjectionFreshness)
	for _, domain := range []ProjectionDomain{
		ProjectionDomainMetadata,
		ProjectionDomainLinks,
		ProjectionDomainMarkdownTargets,
		ProjectionDomainOntology,
		ProjectionDomainCode,
		ProjectionDomainCodeAnchors,
		ProjectionDomainChunks,
		ProjectionDomainEmbeddings,
		ProjectionDomainGraphScores,
	} {
		out[domain] = ProjectionFreshness{State: ProjectionUntouched}
	}
	return out
}
