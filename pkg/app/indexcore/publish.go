package indexcore

import (
	"context"
	"fmt"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	sqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Publish owns the actual structural operations shared by both adapters. It
// preserves the existing code semantic submitter carried in ctx, including
// bounded pre-persistence preparation overlap. It invokes no embedding provider.
func Publish(ctx context.Context, request Request, d Discovery, service *anchors.Service, store *sqlite.Store, writer Writer, options PublishOptions) (result Result, resultErr error) {
	done := indexingperf.StartSpan(ctx, "structural_publication")
	defer func() { done(resultErr) }()
	result = Result{FullDiscovery: d.FullDiscovery}
	if d.noop {
		return result, nil
	}
	if service == nil || store == nil || writer == nil {
		return result, fmt.Errorf("structural service, store, and writer are required")
	}
	ctx = codeintel.WithWriteQueue(ctx, writer)
	runtime, err := request.NoteMetadata.FormatRuntime()
	if err != nil {
		return result, err
	}
	vp, err := paths.NewVaultPaths(request.VaultDefinition.BasePath())
	if err != nil {
		return result, err
	}
	var plan noteownership.TransitionPlan
	var preparation notemeta.PublishedMetadataPreparation
	marked := map[string]struct{}{}
	tickets := map[anchors.DerivedScope]anchors.DerivedWork{}
	mark := func() error {
		var newPaths []string
		for _, p := range sortedSet(d.affected) {
			if _, ok := marked[p]; !ok {
				newPaths = append(newPaths, p)
				marked[p] = struct{}{}
			}
		}
		if len(newPaths) == 0 && len(tickets) > 0 {
			return nil
		}
		scopes := dirtyScopes(newPaths, d.FullDiscovery || request.ForceOntology)
		work, err := writer.SubmitDerivedDirty(ctx, scopes)
		if err != nil {
			return fmt.Errorf("mark structural derived obligations: %w", err)
		}
		for _, w := range work {
			tickets[w.DerivedScope] = w
		}

		return nil
	}
	for {
		indexingperf.AddCount(ctx, "ownership.transition_passes", 1)
		if err := mark(); err != nil {
			return result, err
		}
		plan, err = noteownership.BuildTransitionPlan(ctx, noteownership.TransitionInput{VaultPaths: vp, Snapshot: d.Snapshot, Runtime: runtime, ObservedAt: time.Now().Unix(), AffectedPaths: d.observationPaths()})
		if err != nil {
			return result, fmt.Errorf("build structural ownership transitions: %w", err)
		}
		committed, err := writer.SubmitOwnershipTransitions(ctx, plan.Transitions())
		if err != nil {
			return result, err
		}
		expanded, err := d.observe(committed)
		if err != nil {
			return result, err
		}
		if expanded {
			d.Snapshot, err = d.rediscover(ctx)
			if err != nil {
				return result, err
			}
			continue
		}
		sources := sealedSources(d.Snapshot, plan)
		err = runPhase(ctx, "prepare_note_metadata", func(ctx context.Context) error {
			var err error
			preparation, err = request.NoteMetadata.PreparePublishedMetadata(ctx, d.CompleteNotePaths(), sources)
			return err
		})
		if err != nil {
			return result, fmt.Errorf("prepare sealed structural metadata: %w", err)
		}
		transitions, err := preparation.OwnershipTransitions(time.Now().Unix())
		if err != nil {
			return result, err
		}
		committed, err = writer.SubmitOwnershipTransitions(ctx, transitions)
		if err != nil {
			return result, err
		}
		expanded, err = d.observe(committed)
		if err != nil {
			return result, err
		}
		if options.AfterPreparedOwnershipCommit != nil {
			if err := options.AfterPreparedOwnershipCommit(ctx); err != nil {
				return result, err
			}
		}
		if !expanded {
			break
		}
		d.Snapshot, err = d.rediscover(ctx)
		if err != nil {
			return result, err
		}
	}
	deletedNotes, retiredCode := retiredPaths(d.Snapshot)
	fatal := fatalPaths(plan, preparation)
	var metadata *notemeta.MetadataDelta
	fullMetadata := d.FullDiscovery || request.ForceMetadata
	for path := range fatal {
		if _, ok := plan.Source(paths.NotePath(path)); !ok {
			fullMetadata = true
		}
	}
	if !fullMetadata {
		var needsFull bool
		metadata, needsFull, err = request.NoteMetadata.BuildPublishedPathMetadataDeltaFromPreparation(ctx, request.VaultDefinition, store, d.baseline, preparation, deletedNotes)
		if err != nil {
			return result, err
		}
		fullMetadata = needsFull
	}
	if fullMetadata && !d.FullDiscovery {
		work, err := writer.SubmitDerivedDirty(ctx, dirtyScopes(nil, true))
		if err != nil {
			return result, err
		}
		for _, w := range work {
			tickets[w.DerivedScope] = w
		}
	}
	if fullMetadata {
		indexingperf.AddCount(ctx, "metadata.publication.complete", 1)
		metadata, err = request.NoteMetadata.BuildPublishedMetadataDeltaFromPreparation(ctx, request.VaultDefinition, &obsidian.Note{}, store, preparation)
		if err != nil {
			return result, fmt.Errorf("build complete structural metadata: %w", err)
		}
	}
	if !fullMetadata {
		indexingperf.AddCount(ctx, "metadata.publication.scoped", 1)
	}
	codeCandidates, noteCandidates, sources := ingestCandidates(d.Snapshot, plan, d.affected, fatal)
	indexingperf.AddCount(ctx, "structural.code_candidates", int64(len(codeCandidates)))
	indexingperf.AddCount(ctx, "structural.note_candidates", int64(len(noteCandidates)))
	var progressCallbacks = options.BeforeIngest
	var progress *indexingpipe.ProgressCallbacks
	if progressCallbacks != nil {
		progress = progressCallbacks(len(codeCandidates), len(noteCandidates))
	}
	err = runPhase(ctx, "index_code", func(ctx context.Context) error {
		var err error
		result.Code, err = codeintel.IndexCandidates(ctx, service, vp.Root(), codeCandidates, progress)
		return err
	})
	if err != nil {
		return result, err
	}
	err = runPhase(ctx, "ingest_notes", func(ctx context.Context) error {
		var err error
		result.Notes, err = codeintel.IngestMarkdownCandidatesWithSources(ctx, service, vp.Root(), noteCandidates, sources, progress)
		return err
	})
	if err != nil {
		return result, err
	}
	if len(result.Notes.BuildErrors) > 0 {
		first := result.Notes.BuildErrors[0]
		return result, fmt.Errorf("note indexing failed for %d source(s); first failure: %s: %s", len(result.Notes.BuildErrors), first.Path, first.Error)
	}
	if err := writer.FlushAndWait(ctx); err != nil {
		return result, err
	}
	var metadataChanges, metadataDeletes []string
	if metadata != nil {
		for _, row := range metadata.Notes {
			metadataChanges = append(metadataChanges, row.Path)
		}
		metadataDeletes = metadata.DeletedPaths
		if err := runPhase(ctx, "sync_note_metadata", func(ctx context.Context) error {
			if err := writer.SubmitNoteMetadataDelta(ctx, *metadata); err != nil {
				return err
			}
			return writer.FlushAndWait(ctx)
		}); err != nil {
			return result, err
		}
	}
	result.Notes.ChangedPaths = mergePaths(result.Notes.ChangedPaths, metadataChanges)
	result.Notes.DeletedPaths = mergePaths(result.Notes.DeletedPaths, metadataDeletes, deletedNotes, sortedSet(fatal))
	result.Notes.Deleted = len(result.Notes.DeletedPaths)
	ontologyChanged := result.Notes.ChangedPaths
	if request.ForceOntology {
		ontologyChanged = noteStrings(d.CompleteNotePaths())
	}
	if result.Code.DefDeltas.HasChanges() || len(result.Notes.ChangedPaths) > 0 || len(result.Notes.DeletedPaths) > 0 {
		work, err := writer.SubmitDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedCode}})
		if err != nil {
			return result, err
		}
		for _, w := range work {
			tickets[w.DerivedScope] = w
		}
	}
	if err := writer.SubmitStructuralFinalize(ctx, indexwriter.StructuralFinalize{ChangedCodePaths: mergePaths(result.Code.IndexedPaths, retiredCode), ChangedCallerPaths: result.Code.ChangedCallerPaths, DefDeltas: result.Code.DefDeltas, AffectedAnchorIDs: d.Reconciliation.AffectedAnchorIDs(), Complete: d.FullDiscovery, RecomputeScopes: result.Code.Indexed > 0 || len(result.Notes.ChangedPaths) > 0 || len(result.Notes.DeletedPaths) > 0 || d.Recovery}); err != nil {
		return result, err
	}
	err = runPhase(ctx, "sync_ontology", func(ctx context.Context) error {
		var err error
		result.Ontology, err = ontology.SyncPublishedPaths(ctx, request.NoteMetadata, request.VaultDefinition, &obsidian.Note{}, store, writer, ontologyChanged, result.Notes.DeletedPaths)
		return err
	})
	if err != nil {
		return result, fmt.Errorf("sync structural ontology: %w", err)
	}
	if err := writer.FlushAndWait(ctx); err != nil {
		return result, err
	}
	for _, w := range tickets {
		if w.Path != "" {
			if _, global := tickets[anchors.DerivedScope{Kind: w.Kind}]; global {
				continue
			}
		}
		result.DerivedWork = append(result.DerivedWork, w)
	}
	sortDerived(result.DerivedWork)
	if err := writer.SubmitActivateDerived(ctx, result.DerivedWork); err != nil {
		return result, err
	}
	result.Snapshot = d.Snapshot
	result.Reconciliation = d.Reconciliation
	result.CodeRetirements = retiredCode
	result.StructuralGeneration = d.Reconciliation.AcknowledgementGeneration()
	return result, nil
}

func (d *Discovery) observe(result sqlite.OwnershipTransitionResult) (bool, error) {
	if err := d.Reconciliation.Observe(result); err != nil {
		return false, err
	}
	expanded := false
	for _, p := range append(result.TransitionedPaths, d.Reconciliation.AffectedSourcePaths()...) {
		if _, ok := d.affected[p]; !ok {
			d.affected[p] = struct{}{}
			expanded = true
		}
	}
	return expanded, nil
}
func sealedSources(snapshot noteownership.Snapshot, plan noteownership.TransitionPlan) map[paths.NotePath]noteformat.AuthoredSource {
	sources := map[paths.NotePath]noteformat.AuthoredSource{}
	for _, c := range snapshot.PresentNoteCandidates() {
		p := paths.NotePath(c.Path)
		if source, ok := plan.Source(p); ok {
			sources[p] = source
		}
	}
	return sources
}

func (d Discovery) observationPaths() map[string]struct{} {
	observed := map[string]struct{}{}
	for p := range d.affected {
		observed[p] = struct{}{}
	}
	if d.FullDiscovery {
		for _, c := range d.Snapshot.PresentNoteCandidates() {
			observed[c.Path.String()] = struct{}{}
		}
	}
	return observed
}
