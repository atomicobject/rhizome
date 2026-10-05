package semantic

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// NoteDerivedResult contains one completed owner, bounded by pipeline capacity.
// It owns immutable chunk inputs and vectors; computing it performs no writes.
type NoteDerivedResult struct {
	task     noteTask
	prepared *notePreparedTask
	vectors  []embeddings.Embedding
}

// ComputeDerived runs the shared provider node and packer without publishing.
// sink supplies backpressure and must finish consuming a result before returning.
func (s *NoteSyncer) ComputeDerived(ctx context.Context, plan NotePlan, sink func(context.Context, NoteDerivedResult) error) error {
	opts := EmbedPackerOptions{}
	if s.NoteEmbedPacker != nil {
		opts = *s.NoteEmbedPacker
	}
	pipeline := newNoteEmbeddingPipelineWithNode(ctx, s.Provider, s.EmbeddingNode, s.maxConcurrent(), s.EmbedGate, opts)
	return runPreparedPipeline(ctx, plan.tasks, plan.TotalWork, s.maxConcurrent(), "noteembed.prepare", "noteembed.ready_enqueue_wait", pipeline.preparedCh, pipeline.run,
		func(ctx context.Context, task noteTask) (*notePreparedTask, error) {
			if reused := len(task.payload.reuseChunks); reused > 0 {
				indexingperf.AddCount(ctx, "noteembed.reuse_hits", int64(reused))
			}
			return &notePreparedTask{task: task, useLegacy: s.EmbeddingWriter == nil && s.Index != nil, missingProviderCalls: len(task.payload.embedChunks)}, nil
		},
		func(owner *notePipelineOwner) error {
			return observeNoteMetricErr(ctx, "noteembed.finalize", func() error {
				return sink(ctx, NoteDerivedResult{task: owner.prepared.task, prepared: owner.prepared, vectors: owner.missingVecs})
			})
		}, s.OnEmbedProgress)
}

// PublishDerived must execute after the coordinator checks its generation while
// holding the writer lease. Chunk replacement and vectors share that boundary.
func (s *NoteSyncer) PublishDerived(ctx context.Context, result NoteDerivedResult) error {
	if s.ChunkWriter != nil {
		if err := s.persistNoteIntelChunks(ctx, []noteTask{result.task}); err != nil {
			return err
		}
	}
	target := s.EmbeddingWriter
	if s.WriteQueue != nil {
		target = queueEmbeddingWriter{queue: s.WriteQueue}
	}
	var writer intelEmbeddingSubmitter
	if target != nil {
		writer = directIntelEmbeddingSubmitter{ctx: ctx, writer: target}
	}
	return applySemanticWriteIntents(ctx, noteWriteSink{syncer: s, useLegacy: result.prepared.useLegacy, intelWriter: writer}, buildNoteWriteIntents(result.prepared, result.vectors, s.EmbeddingWriter != nil)...)
}

func (s *NoteSyncer) FinishDerived(ctx context.Context, plan NotePlan) error {
	lazy, _ := s.Index.(embeddings.LazyPruningNoteIndex)
	if err := s.applyNotePlanCleanup(ctx, lazy, plan.state); err != nil {
		return err
	}
	if s.WriteQueue != nil {
		return s.WriteQueue.FlushAndWait(ctx)
	}
	return nil
}

// CodeDerivedResult is the corresponding immutable code owner result.
type CodeDerivedResult struct {
	task     codeTask
	prepared *codePreparedTask
	vectors  []embeddings.Embedding
}

func (s *Syncer) ComputeDerived(ctx context.Context, plan SyncPlan, sink func(context.Context, CodeDerivedResult) error) error {
	opts := EmbedPackerOptions{}
	if s.CodeEmbedPacker != nil {
		opts = *s.CodeEmbedPacker
	}
	pipeline := newCodeEmbeddingPipelineWithNode(ctx, s.Provider, s.EmbeddingNode, s.maxConcurrent(), s.EmbedGate, opts)
	tasks := make(map[string]codeTask, len(plan.tasks))
	for _, task := range plan.tasks {
		tasks[string(task.id)] = task
	}
	return runPreparedPipeline(ctx, plan.tasks, plan.TotalWork, s.maxConcurrent(), "codeembed.prepare", "codeembed.ready_enqueue_wait", pipeline.preparedCh, pipeline.run,
		func(ctx context.Context, task codeTask) (*codePreparedTask, error) {
			return s.prepareTaskForPipeline(ctx, task, plan.state.states[task.id], plan.state.reuseCache, plan.state.forceReembed)
		},
		func(owner *codePipelineOwner) error {
			s.recordPreparedReuseEmbeddings(plan.state.reuseCache, owner.prepared, owner.missingVecs, owner.prepared.itemVec)
			return sink(ctx, CodeDerivedResult{task: tasks[string(owner.prepared.anchorID)], prepared: owner.prepared, vectors: owner.missingVecs})
		}, s.OnEmbedProgress)
}

func (s *Syncer) PublishDerived(ctx context.Context, result CodeDerivedResult) error {
	if s.ChunkWriter != nil {
		if err := s.persistIntelChunks(ctx, []codeTask{result.task}); err != nil {
			return err
		}
	}
	target := s.EmbeddingWriter
	if s.WriteQueue != nil {
		target = queueEmbeddingWriter{queue: s.WriteQueue}
	}
	var writer intelEmbeddingSubmitter
	if target != nil {
		writer = directIntelEmbeddingSubmitter{ctx: ctx, writer: target}
	}
	return applySemanticWriteIntents(ctx, codeWriteSink{syncer: s, useLegacy: result.prepared.useLegacy, intelWriter: writer}, buildCodeWriteIntents(result.prepared, result.vectors, result.prepared.itemVec, s.EmbeddingWriter != nil)...)
}
