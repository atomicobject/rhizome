package embedindex

import (
	"context"

	"fmt"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// Embed executes a prepared note sync plan.
func (s *NoteSyncer) Embed(ctx context.Context, plan NotePlan) error {
	return s.embed(ctx, plan, true)
}

func (s *NoteSyncer) EmbedWithoutSyncMark(ctx context.Context, plan NotePlan) error {
	return s.embed(ctx, plan, false)
}

func (s *NoteSyncer) embed(ctx context.Context, plan NotePlan, updateLastSync bool) error {
	if plan.skipEmbed {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Persist intel chunks prior to embedding if a writer is configured.
	if s.ChunkWriter != nil && len(plan.tasks) > 0 {
		// With a queued writer, note intel chunks and later embedding rows stay in
		// order on the same FIFO lane; flushing here only removes overlap.
		if err := s.persistNoteIntelChunks(ctx, plan.tasks); err != nil {
			return fmt.Errorf("persist note intel chunks: %w", err)
		}
	}

	lazyIndex, _ := s.Index.(embeddings.LazyPruningNoteIndex)

	if len(plan.tasks) == 0 {
		s.progressf("No changed notes to embed")
		if err := s.applyNotePlanCleanup(ctx, lazyIndex, plan.state); err != nil {
			return err
		}
		return s.markPlanFreshness(ctx, plan, updateLastSync)
	}

	if plan.TotalWork > 0 {
		s.progressf("Embedding %d changed notes (%d items)", len(plan.tasks), plan.TotalWork)
	} else {
		s.progressf("Applying note chunk updates for %d notes", len(plan.tasks))
	}
	if plan.TotalWork > 0 && s.OnEmbedProgress != nil {
		s.OnEmbedProgress(0, plan.TotalWork)
	}

	// Multi-note sync must batch intel embedding persistence; per-note writes
	// reintroduce the tiny-transaction regression in unified indexing.
	intelTarget := s.EmbeddingWriter
	if s.WriteQueue != nil {
		intelTarget = queueEmbeddingWriter{queue: s.WriteQueue}
	}
	intelWriter := newBatchedIntelEmbeddingWriter(ctx, intelTarget, intelEmbeddingBatchWriterOptions{
		Label:      "notes",
		OnError:    func(error) { cancel() },
		OnProgress: s.progressf,
	})
	if err := s.processTasksPipelined(ctx, plan, intelWriter); err != nil {
		if intelWriter != nil {
			_ = intelWriter.Close()
		}
		return err
	}
	if intelWriter != nil {
		if err := intelWriter.Close(); err != nil {
			return err
		}
	}
	if s.WriteQueue != nil {
		if err := observeNoteMetricErr(ctx, indexingperf.NoteembedWritebackFlushWait, func() error {
			return s.WriteQueue.FlushAndWait(ctx)
		}); err != nil {
			return err
		}
	}

	if err := s.applyNotePlanCleanup(ctx, lazyIndex, plan.state); err != nil {
		return err
	}
	// Provider may have learned its output dimensions during this batch (Voyage
	// reports them with the first response). Re-run the validator so the meta
	// row reflects reality before the next sync cycle.
	_ = s.ensureReady(ctx)
	return s.markPlanFreshness(ctx, plan, updateLastSync)
}
