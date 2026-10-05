package semantic

import (
	"context"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

type notePreparedTask struct {
	task                 noteTask
	useLegacy            bool
	readyAt              time.Time
	missingProviderCalls int
}

func (t *notePreparedTask) pipelineProviderCalls() int {
	if t == nil {
		return 0
	}
	return t.missingProviderCalls
}

type notePipelineJob struct {
	noteID     embeddings.NoteID
	missingIdx int
	text       string
	bytes      int
}

func (j notePipelineJob) pipelineOwnerKey() embeddings.NoteID { return j.noteID }
func (j notePipelineJob) pipelineText() string                { return j.text }
func (j notePipelineJob) pipelineBytes() int                  { return j.bytes }

type notePipelineOwner struct {
	prepared    *notePreparedTask
	missingVecs []embeddings.Embedding
	pending     int
}

func (o *notePipelineOwner) pipelineReadyAt() time.Time { return o.prepared.readyAt }
func (o *notePipelineOwner) pipelinePending() int       { return o.pending }
func (o *notePipelineOwner) pipelineSetPending(n int)   { o.pending = n }
func (o *notePipelineOwner) pipelineDoneCalls() int {
	if o == nil || o.prepared == nil {
		return 0
	}
	return o.prepared.missingProviderCalls
}

type noteEmbeddingPipeline struct {
	preparedCh chan *notePreparedTask
	stage      *preparedEmbedStage[embeddings.NoteID, *notePreparedTask, *notePipelineOwner, notePipelineJob]
}

func newNoteEmbeddingPipelineWithNode(ctx context.Context, provider embeddings.Provider, sharedNode *SharedEmbeddingNode, maxConcurrent int, globalSem chan struct{}, opts EmbedPackerOptions) *noteEmbeddingPipeline {
	stage := newPreparedEmbedStage(
		ctx,
		provider,
		sharedNode,
		maxConcurrent,
		globalSem,
		opts,
		func(prepared *notePreparedTask) (embeddings.NoteID, *notePipelineOwner, []notePipelineJob) {
			prepared.readyAt = time.Now()
			owner := &notePipelineOwner{
				prepared:    prepared,
				missingVecs: make([]embeddings.Embedding, len(prepared.task.payload.embedChunks)),
			}
			jobs := make([]notePipelineJob, 0, len(prepared.task.payload.embedTexts))
			for idx, text := range prepared.task.payload.embedTexts {
				jobs = append(jobs, notePipelineJob{
					noteID:     prepared.task.id,
					missingIdx: idx,
					text:       text,
					bytes:      len(text),
				})
			}
			return prepared.task.id, owner, jobs
		},
		func(owner *notePipelineOwner, job notePipelineJob, vec embeddings.Embedding) error {
			if job.missingIdx >= 0 && job.missingIdx < len(owner.missingVecs) {
				owner.missingVecs[job.missingIdx] = vec
			}
			return nil
		},
	)
	return &noteEmbeddingPipeline{preparedCh: stage.preparedCh, stage: stage}
}

func (p *noteEmbeddingPipeline) run(finalize func(*notePipelineOwner) error) error {
	if p == nil || p.stage == nil {
		return nil
	}
	return p.stage.run(finalize)
}

func (s *NoteSyncer) processTasksPipelined(ctx context.Context, plan NotePlan, intelWriter intelEmbeddingSubmitter) error {
	return s.ComputeDerived(ctx, plan, func(ctx context.Context, result NoteDerivedResult) error {
		return applySemanticWriteIntents(ctx, noteWriteSink{syncer: s, useLegacy: result.prepared.useLegacy, intelWriter: intelWriter}, buildNoteWriteIntents(result.prepared, result.vectors, s.EmbeddingWriter != nil)...)
	})
}
