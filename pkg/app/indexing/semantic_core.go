package indexing

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

func syncRawNoteEmbeddings(ctx context.Context, syncer *semantic.NoteSyncer, changedPathCount int) error {
	if syncer == nil {
		return nil
	}
	var plan semantic.NotePlan
	if err := runPhase(ctx, "plan_note_embeddings", func(phaseCtx context.Context) error {
		var planErr error
		plan, planErr = syncer.Plan(phaseCtx)
		indexingperf.AddCount(phaseCtx, "note_raw.compat_paths", int64(changedPathCount))
		return planErr
	}); err != nil {
		return err
	}
	return runPhase(ctx, "embed_notes", func(phaseCtx context.Context) error {
		return syncer.Embed(phaseCtx, plan)
	})
}
