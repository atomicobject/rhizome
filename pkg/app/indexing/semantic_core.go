package indexing

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/embedindex"
)

func syncRawNoteEmbeddings(ctx context.Context, syncer *embedindex.NoteSyncer) error {
	if syncer == nil {
		return nil
	}
	var plan embedindex.NotePlan
	if err := runPhase(ctx, "plan_note_embeddings", func(phaseCtx context.Context) error {
		var planErr error
		plan, planErr = syncer.Plan(phaseCtx)
		return planErr
	}); err != nil {
		return err
	}
	return runPhase(ctx, "embed_notes", func(phaseCtx context.Context) error {
		return syncer.Embed(phaseCtx, plan)
	})
}
