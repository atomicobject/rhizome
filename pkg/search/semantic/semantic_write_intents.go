package semantic

import (
	"context"
	"errors"
	"fmt"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
)

type semanticWriteIntent interface {
	apply(context.Context, semanticWriteSink) error
}

type semanticWriteSink interface {
	writeCodeChunks(context.Context, codeindex.AnchorID, []codeindex.ChunkInput, []string, []embeddings.Embedding) error
	writeNoteChunks(context.Context, embeddings.NoteID, string, []embeddings.ChunkInput, []embeddings.Embedding, []int) error
	writeCodeItemEmbedding(context.Context, codeindex.AnchorID, string, embeddings.Embedding) error
	writeIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error
	deleteCodeChunksNotIn(context.Context, codeindex.AnchorID, []int) error
}

type codeChunksIntent struct {
	anchorID codeindex.AnchorID
	chunks   []codeindex.ChunkInput
	texts    []string
	vecs     []embeddings.Embedding
}

func (i codeChunksIntent) apply(ctx context.Context, sink semanticWriteSink) error {
	return sink.writeCodeChunks(ctx, i.anchorID, i.chunks, i.texts, i.vecs)
}

type noteChunksIntent struct {
	noteID      embeddings.NoteID
	path        string
	chunks      []embeddings.ChunkInput
	vecs        []embeddings.Embedding
	keepIndices []int
}

func (i noteChunksIntent) apply(ctx context.Context, sink semanticWriteSink) error {
	return sink.writeNoteChunks(ctx, i.noteID, i.path, i.chunks, i.vecs, i.keepIndices)
}

type codeItemEmbeddingIntent struct {
	anchorID codeindex.AnchorID
	hash     string
	vec      embeddings.Embedding
}

func (i codeItemEmbeddingIntent) apply(ctx context.Context, sink semanticWriteSink) error {
	return sink.writeCodeItemEmbedding(ctx, i.anchorID, i.hash, i.vec)
}

type intelEmbeddingsIntent struct {
	rows map[string]embeddings.Embedding
}

func (i intelEmbeddingsIntent) apply(ctx context.Context, sink semanticWriteSink) error {
	return sink.writeIntelEmbeddings(ctx, i.rows)
}

type deleteCodeChunksIntent struct {
	anchorID    codeindex.AnchorID
	keepIndices []int
}

func (i deleteCodeChunksIntent) apply(ctx context.Context, sink semanticWriteSink) error {
	return sink.deleteCodeChunksNotIn(ctx, i.anchorID, i.keepIndices)
}

func applySemanticWriteIntents(ctx context.Context, sink semanticWriteSink, intents ...semanticWriteIntent) error {
	for _, intent := range intents {
		if intent == nil {
			continue
		}
		if err := intent.apply(ctx, sink); err != nil {
			return fmt.Errorf("apply %T: %w", intent, err)
		}
	}
	return nil
}

type codeWriteSink struct {
	syncer       *Syncer
	useLegacy    bool
	itemWriter   func(context.Context, codeindex.AnchorID, string, embeddings.Embedding) error
	intelWriter  intelEmbeddingSubmitter
	queueBatcher *codeWritebackBatcher
}

func (s codeWriteSink) writeCodeChunks(ctx context.Context, anchorID codeindex.AnchorID, chunks []codeindex.ChunkInput, texts []string, vecs []embeddings.Embedding) error {
	if !s.useLegacy || len(chunks) == 0 {
		return nil
	}
	if s.queueBatcher != nil {
		return s.queueBatcher.SubmitCodeChunks(ctx, anchorID, chunks, texts, vecs)
	}
	if s.syncer.WriteQueue != nil {
		return s.syncer.WriteQueue.SubmitCodeItemChunks(ctx, anchorID, chunks, texts, vecs)
	}
	return s.syncer.Index.UpsertItemChunks(ctx, anchorID, chunks, texts, vecs)
}

func (s codeWriteSink) writeNoteChunks(context.Context, embeddings.NoteID, string, []embeddings.ChunkInput, []embeddings.Embedding, []int) error {
	return nil
}

func (s codeWriteSink) writeCodeItemEmbedding(ctx context.Context, anchorID codeindex.AnchorID, hash string, vec embeddings.Embedding) error {
	if s.itemWriter != nil {
		return s.itemWriter(ctx, anchorID, hash, vec)
	}
	return s.syncer.Index.UpsertItemEmbedding(ctx, anchorID, hash, vec)
}

func (s codeWriteSink) writeIntelEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	if len(rows) == 0 || s.syncer.EmbeddingWriter == nil {
		return nil
	}
	if s.queueBatcher != nil {
		return s.queueBatcher.SubmitIntelEmbeddings(ctx, rows)
	}
	if s.intelWriter == nil {
		return errors.New("intel embedding writer missing batch adapter")
	}
	if err := s.intelWriter.Submit(rows); err != nil {
		return fmt.Errorf("persist intel embeddings: %w", err)
	}
	return nil
}

func (s codeWriteSink) deleteCodeChunksNotIn(ctx context.Context, anchorID codeindex.AnchorID, keepIndices []int) error {
	if !s.useLegacy {
		return nil
	}
	return s.syncer.Index.DeleteChunksNotIn(ctx, anchorID, keepIndices)
}

type noteWriteSink struct {
	syncer      *NoteSyncer
	useLegacy   bool
	intelWriter intelEmbeddingSubmitter
}

func (s noteWriteSink) writeCodeChunks(context.Context, codeindex.AnchorID, []codeindex.ChunkInput, []string, []embeddings.Embedding) error {
	return nil
}

func (s noteWriteSink) writeNoteChunks(ctx context.Context, noteID embeddings.NoteID, path string, chunks []embeddings.ChunkInput, vecs []embeddings.Embedding, keepIndices []int) error {
	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "noteembed.finalize.write_chunks", time.Since(started))
	}()
	if !s.useLegacy {
		return nil
	}
	syncItem := embeddings.NoteChunkSync{
		NoteID:      noteID,
		Chunks:      chunks,
		Embeddings:  vecs,
		KeepIndices: keepIndices,
	}
	submitStarted := time.Now()
	if s.syncer.WriteQueue != nil {
		err := s.syncer.WriteQueue.SubmitNoteChunkSync(ctx, syncItem)
		indexingperf.ObserveLatency(ctx, "noteembed.finalize.writeback_submit", time.Since(submitStarted))
		indexingperf.ObserveLatency(ctx, "noteembed.writeback.chunk.submit_wait", time.Since(submitStarted))
		if err != nil {
			return fmt.Errorf("upsert chunks %s: %w", path, err)
		}
		return nil
	}
	syncIndex, ok := s.syncer.Index.(embeddings.NoteChunkSyncIndex)
	if ok {
		err := syncIndex.SyncNoteChunks(ctx, syncItem)
		indexingperf.ObserveLatency(ctx, "noteembed.finalize.writeback_submit", time.Since(submitStarted))
		if err != nil {
			return fmt.Errorf("sync chunks %s: %w", path, err)
		}
		return nil
	}
	if err := s.syncer.Index.UpsertNoteChunks(ctx, noteID, chunks, vecs); err != nil {
		indexingperf.ObserveLatency(ctx, "noteembed.finalize.writeback_submit", time.Since(submitStarted))
		return fmt.Errorf("upsert chunks %s: %w", path, err)
	}
	err := s.syncer.Index.DeleteChunksNotIn(ctx, noteID, keepIndices)
	indexingperf.ObserveLatency(ctx, "noteembed.finalize.writeback_submit", time.Since(submitStarted))
	if err != nil {
		return fmt.Errorf("cleanup chunks %s: %w", path, err)
	}
	return nil
}

func (s noteWriteSink) writeCodeItemEmbedding(context.Context, codeindex.AnchorID, string, embeddings.Embedding) error {
	return nil
}

func (s noteWriteSink) writeIntelEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	started := time.Now()
	defer func() {
		indexingperf.ObserveLatency(ctx, "noteembed.finalize.write_intel", time.Since(started))
	}()
	if len(rows) == 0 || s.syncer.EmbeddingWriter == nil {
		return nil
	}
	if s.intelWriter == nil {
		return errors.New("intel embedding writer missing batch adapter")
	}
	if err := s.intelWriter.Submit(rows); err != nil {
		return fmt.Errorf("persist intel embeddings: %w", err)
	}
	return nil
}

func (s noteWriteSink) deleteCodeChunksNotIn(context.Context, codeindex.AnchorID, []int) error {
	return nil
}

func buildCodeWriteIntents(
	prepared *codePreparedTask,
	missingVecs []embeddings.Embedding,
	itemVec embeddings.Embedding,
	includeIntel bool,
) []semanticWriteIntent {
	if prepared == nil {
		return nil
	}
	intents := make([]semanticWriteIntent, 0, 5)
	intelEmbeddings := make(map[string]embeddings.Embedding, len(prepared.reuseInputs)+len(prepared.missingInputs))
	if len(prepared.reuseInputs) > 0 {
		intents = append(intents, codeChunksIntent{
			anchorID: prepared.anchorID,
			chunks:   prepared.reuseInputs,
			texts:    prepared.reuseTexts,
			vecs:     prepared.reuseVecs,
		})
		for i, input := range prepared.reuseInputs {
			chunkID := codeanchor.IntelChunkID(string(prepared.anchorID), input.Index, input.Granularity)
			intelEmbeddings[chunkID] = prepared.reuseVecs[i]
		}
	}
	if len(prepared.missingInputs) > 0 {
		intents = append(intents, codeChunksIntent{
			anchorID: prepared.anchorID,
			chunks:   prepared.missingInputs,
			texts:    prepared.missingTexts,
			vecs:     missingVecs,
		})
		for i, input := range prepared.missingInputs {
			if i >= len(missingVecs) {
				break
			}
			chunkID := codeanchor.IntelChunkID(string(prepared.anchorID), input.Index, input.Granularity)
			intelEmbeddings[chunkID] = missingVecs[i]
		}
	}
	if prepared.itemNeedsWrite && len(itemVec) > 0 {
		intents = append(intents, codeItemEmbeddingIntent{
			anchorID: prepared.anchorID,
			hash:     prepared.itemHash,
			vec:      itemVec,
		})
	}
	if includeIntel && len(intelEmbeddings) > 0 {
		intents = append(intents, intelEmbeddingsIntent{rows: intelEmbeddings})
	}
	if prepared.useLegacy {
		intents = append(intents, deleteCodeChunksIntent{
			anchorID:    prepared.anchorID,
			keepIndices: prepared.keepIndices,
		})
	}
	return intents
}

func buildNoteWriteIntents(
	prepared *notePreparedTask,
	missingVecs []embeddings.Embedding,
	includeIntel bool,
) []semanticWriteIntent {
	if prepared == nil {
		return nil
	}
	task := prepared.task
	intents := make([]semanticWriteIntent, 0, 4)
	intelEmbeddings := make(map[string]embeddings.Embedding, len(task.payload.reuseChunks)+len(task.payload.embedChunks))
	allChunks := make([]embeddings.ChunkInput, 0, len(task.payload.reuseChunks)+len(task.payload.embedChunks))
	allVecs := make([]embeddings.Embedding, 0, len(task.payload.reuseVecs)+len(missingVecs))
	if len(task.payload.reuseChunks) > 0 {
		allChunks = append(allChunks, task.payload.reuseChunks...)
		allVecs = append(allVecs, task.payload.reuseVecs...)
		for i, chunk := range task.payload.reuseChunks {
			if chunk.Index >= 0 && chunk.Index < len(task.payload.sections) {
				secID := task.payload.sections[chunk.Index].SectionID
				chunkID := codeanchor.IntelChunkID(secID, 0, "section")
				intelEmbeddings[chunkID] = task.payload.reuseVecs[i]
			}
		}
	}
	if len(task.payload.embedChunks) > 0 {
		allChunks = append(allChunks, task.payload.embedChunks...)
		allVecs = append(allVecs, missingVecs...)
		for i, chunk := range task.payload.embedChunks {
			if i >= len(missingVecs) {
				break
			}
			if chunk.Index >= 0 && chunk.Index < len(task.payload.sections) {
				secID := task.payload.sections[chunk.Index].SectionID
				chunkID := codeanchor.IntelChunkID(secID, 0, "section")
				intelEmbeddings[chunkID] = missingVecs[i]
			}
		}
	}
	if prepared.useLegacy {
		intents = append(intents, noteChunksIntent{
			noteID:      task.payload.info.ID,
			path:        task.payload.info.Path,
			chunks:      allChunks,
			vecs:        allVecs,
			keepIndices: task.payload.allChunkIdx,
		})
	}
	if includeIntel && len(intelEmbeddings) > 0 {
		intents = append(intents, intelEmbeddingsIntent{rows: intelEmbeddings})
	}
	return intents
}
