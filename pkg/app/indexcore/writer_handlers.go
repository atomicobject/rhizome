package indexcore

import (
	"context"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	graphindex "github.com/atomicobject/rhizome/pkg/graph/index"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// BindWriterHandlers composes identical structural controls for both drivers.
// Existing semantic handlers remain owned by the caller's provider executor.
func BindWriterHandlers(handlers indexwriter.Handlers, service *anchors.Service, store *semdb.Store) indexwriter.Handlers {
	handlers.ResetReverseIndex = func(ctx context.Context) error {
		if err := store.SetReverseIndexBackfillComplete(ctx, false); err != nil {
			return err
		}
		return store.SetReverseIndexVersion(ctx, anchors.ReverseIndexVersion)
	}
	handlers.ApplyCodeIndexBatch = service.ApplyCodeIndexBatch
	handlers.ApplyNoteIndexBatch = service.ApplyNoteIndexBatch
	handlers.ApplyOwnershipTransitions = func(ctx context.Context, transitions []semdb.OwnershipTransition) (result semdb.OwnershipTransitionResult, err error) {
		err = indexingperf.RunPhase(ctx, "ownership_transition", func(ctx context.Context) error {
			var applyErr error
			result, applyErr = store.ApplyOwnershipTransitions(ctx, transitions)
			return applyErr
		})
		return result, err
	}
	handlers.AcknowledgeOwnershipReconciliation = store.AcknowledgeOwnershipReconciliation
	handlers.ApplyNoteMetadataDelta = store.ApplyNoteMetadataDelta
	handlers.ApplyOntologyDelta = store.ApplyOntologyDelta
	handlers.ApplyOntologyNodeReadModel = store.ReplaceOntologyNodeReadModel
	handlers.MarkDerivedDirty = store.MarkDerivedDirty
	handlers.ActivateDerivedWork = store.ActivateDerivedWork
	handlers.AckDerivedWork = store.AckDerivedWork
	handlers.ApplyGraphScores = func(ctx context.Context, scores semdb.DerivedGraphScores) error {
		return graphindex.PublishDerivedScores(ctx, store, scores)
	}
	handlers.ApplyStructuralFinalize = func(ctx context.Context, work indexwriter.StructuralFinalize) error {
		if len(work.ChangedCodePaths) > 0 || work.DefDeltas.HasChanges() {
			indexingperf.AddCount(ctx, indexingperf.CallEdgesRecomputeStructuralChange, 1)
			end := indexingperf.StartSpan(ctx, "rebuild_call_edges")
			plan, err := service.PlanCallEdgeRebuildForDefDeltas(ctx, work.ChangedCallerPaths, work.DefDeltas)
			if err == nil {
				err = service.RebuildParserCallEdgesForPaths(ctx, plan.Paths)
			}
			end(err)
			if err != nil {
				return err
			}
			if err := service.RebuildGoPackageRelationshipsForPaths(ctx, mergePaths(work.ChangedCodePaths, plan.Paths)); err != nil {
				return err
			}
		}
		if len(work.AffectedAnchorIDs) > 0 {
			indexingperf.AddCount(ctx, indexingperf.AnchorScopesAffectedIds, int64(len(work.AffectedAnchorIDs)))
			if err := indexingperf.RunPhase(ctx, "rebuild_affected_anchor_scopes", func(ctx context.Context) error {
				return service.RebuildAnchorScopesForIDs(ctx, work.AffectedAnchorIDs)
			}); err != nil {
				return err
			}
		}
		if work.RecomputeScopes {
			indexingperf.AddCount(ctx, indexingperf.AnchorScopesRecomputeStructuralChangeOrRecovery, 1)
			if err := indexingperf.RunPhase(ctx, "recompute_anchor_scopes", service.RecomputeAnchorScopes); err != nil {
				return err
			}
		}
		if err := store.SetIndexerVersion(ctx, anchors.IndexerVersion); err != nil {
			return err
		}
		if work.Complete {
			if err := store.SetReverseIndexVersion(ctx, anchors.ReverseIndexVersion); err != nil {
				return err
			}
			if err := store.SetReverseIndexBackfillComplete(ctx, true); err != nil {
				return err
			}
		}
		return nil
	}
	return handlers
}
