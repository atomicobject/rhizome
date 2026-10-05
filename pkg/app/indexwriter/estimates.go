package indexwriter

import (
	"context"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

func ctxWithPhaseOp(ctx context.Context, phase, op string) context.Context {
	if phase != "" && phase != indexQueueDefaultPhaseLabel {
		ctx = indexingperf.WithPhase(ctx, phase)
	}
	if op != "" {
		ctx = indexingperf.WithOp(ctx, op)
	}
	return ctx
}

func normalizeQueuePhase(phase string) string {
	phase = strings.TrimSpace(phase)
	if phase == "" {
		return indexQueueDefaultPhaseLabel
	}
	return phase
}

func ensureQueueBatch[T any](batches map[string]*queueBatch[T], phase string) *queueBatch[T] {
	if batch, ok := batches[phase]; ok {
		return batch
	}
	batch := &queueBatch[T]{phase: phase}
	batches[phase] = batch
	return batch
}

func ensureIntelChunkBatch(batches map[string]*intelChunkBatch, phase string, family string) *intelChunkBatch {
	key := phase + "\x00" + family
	if batch, ok := batches[key]; ok {
		return batch
	}
	batch := &intelChunkBatch{phase: phase, family: family}
	batches[key] = batch
	return batch
}

func ensureIntelEmbeddingBatch(batches map[string]*intelEmbeddingBatch, phase string) *intelEmbeddingBatch {
	if batch, ok := batches[phase]; ok {
		return batch
	}
	batch := &intelEmbeddingBatch{phase: phase}
	batches[phase] = batch
	return batch
}

func ensureOntologyDeltaBatch(batches map[string]*ontologyDeltaBatch, phase string) *ontologyDeltaBatch {
	if batch, ok := batches[phase]; ok {
		return batch
	}
	batch := &ontologyDeltaBatch{phase: phase}
	batches[phase] = batch
	return batch
}

func estimateCodeIndexWork(work codeanchor.CodeIndexWork) int {
	return len(work.Path)*2 + len(work.IntelAnchors)*128 + len(work.IntelEdges)*64 + len(work.IntelFTSRows)*96 +
		len(work.ExternalEvidence.Symbols)*320 + len(work.ExternalEvidence.Imports)*224
}

func estimateNoteIndexWork(work codeanchor.NoteIndexWork) int {
	return len(work.Path)*2 + len(work.IntelSections)*160 + len(work.IntelMentions)*64 + len(work.IntelFTSRows)*96
}

func estimateCodeChunkWrite(chunks []codeindex.ChunkInput, texts []string, vecs []embeddings.Embedding) int {
	n := len(chunks) * 96
	for _, t := range texts {
		n += len(t)
	}
	for _, v := range vecs {
		n += len(v) * 4
	}
	return n
}

func estimateCodeItemEmbeddingBatch(items []codeindex.ItemEmbeddingUpsert) int {
	n := 0
	for _, item := range items {
		n += len(item.Hash) + len(item.Embedding)*4 + 64
	}
	return n
}

func estimateCodeChunkBatch(items []codeindex.ItemChunksUpsert) int {
	n := 0
	for _, item := range items {
		n += estimateCodeChunkWrite(item.Chunks, item.Texts, item.Embeddings)
	}
	return n
}

func CountCodeChunkRows(items []codeindex.ItemChunksUpsert) int {
	rows := 0
	for _, item := range items {
		rows += len(item.Chunks)
	}
	return rows
}

func estimateNoteChunkWrite(chunks []embeddings.ChunkInput, vecs []embeddings.Embedding) int {
	n := len(chunks) * 96
	for _, v := range vecs {
		n += len(v) * 4
	}
	return n
}

func estimateNoteChunkSyncWrite(item embeddings.NoteChunkSync) int {
	return estimateNoteChunkWrite(item.Chunks, item.Embeddings) + len(item.KeepIndices)*8
}

func noteChunkSyncRows(item embeddings.NoteChunkSync) int {
	if len(item.Chunks) > 0 {
		return len(item.Chunks)
	}
	return 1
}

func cloneNoteChunkSync(item embeddings.NoteChunkSync) embeddings.NoteChunkSync {
	cloned := embeddings.NoteChunkSync{
		NoteID:      item.NoteID,
		Chunks:      append([]embeddings.ChunkInput(nil), item.Chunks...),
		Embeddings:  make([]embeddings.Embedding, len(item.Embeddings)),
		KeepIndices: append([]int(nil), item.KeepIndices...),
	}
	for i, vec := range item.Embeddings {
		cloned.Embeddings[i] = append(embeddings.Embedding(nil), vec...)
	}
	return cloned
}

func CloneOwnershipTransitions(transitions []semdb.OwnershipTransition) []semdb.OwnershipTransition {
	if len(transitions) == 0 {
		return nil
	}
	cloned := make([]semdb.OwnershipTransition, len(transitions))
	for i, transition := range transitions {
		cloned[i] = transition
		if transition.Note == nil {
			continue
		}
		note := *transition.Note
		cloned[i].Note = &note
	}
	return cloned
}

func estimateIntelEmbeddings(rows map[string]embeddings.Embedding) int {
	n := 0
	for key, row := range rows {
		n += len(key) + len(row)*4 + 32
	}
	return n
}

func estimateOntologyDelta(delta semdb.OntologyDelta) int {
	delta = semdb.NormalizeOntologyDelta(delta)
	n := len(delta.DeletePaths)*48 + len(delta.ReplacePaths)*48 + len(delta.EdgeSources)*48
	for _, model := range delta.ReadModels {
		n += len(model.NotePaths)*48 + len(model.Nodes)*256 + len(model.FieldValues)*192 + len(model.LinkDependencies)*192
	}
	for _, row := range delta.Assessments {
		n += len(row.NotePath) + len(row.DeclaredType) + len(row.ResolvedType) + len(row.AssessmentJSON) + len(row.SchemaHash) + 64
	}
	for _, row := range delta.NoteStates {
		n += len(row.NotePath) + len(row.InputFingerprint) + len(row.SchemaHash) + len(row.ResolvedType) + 64
	}
	for _, row := range delta.NoteTypes {
		n += len(row.NotePath) + len(row.TypeName) + len(row.SchemaHash) + 48
	}
	for _, row := range delta.Edges {
		n += len(row.SrcPath) + len(row.RelationName) + len(row.DstPath) + len(row.DstType) + len(row.Provenance) + len(row.SchemaHash) + 64
	}
	if delta.SchemaState != nil {
		n += len(delta.SchemaState.SchemaHash) + len(delta.SchemaState.NotesHash) + len(delta.SchemaState.ErrorJSON) + 64
	}
	return n
}

func noteMetadataDeltaRows(delta semdb.NoteMetadataDelta) int {
	return len(delta.Notes) + len(delta.PropertyValues) + len(delta.Tags) + len(delta.FragmentTargets) + len(delta.WikilinkEdges) + len(delta.DeletedPaths) + len(delta.SourceTouches) + 1
}

func estimateNoteMetadataDelta(delta semdb.NoteMetadataDelta) int {
	n := len(delta.DeletedPaths) * 48
	for _, row := range delta.Notes {
		n += len(row.Path) + len(row.Title) + len(row.ContentHash) + 64
	}
	for _, row := range delta.PropertyValues {
		n += len(row.NotePath) + len(row.PropertyName) + len(row.ValueText) + len(row.ValueNorm) + 48
	}
	for _, row := range delta.Tags {
		n += len(row.NotePath) + len(row.TagNorm) + 24
	}
	for _, row := range delta.FragmentTargets {
		n += len(row.NotePath) + len(row.Target) + len(row.TargetNorm) + 32
	}
	for _, touch := range delta.SourceTouches {
		n += len(touch.Path) + 24
	}
	n += len(delta.WikilinkEdges) * 96
	return n
}

type SemanticWriteQueue interface {
	SubmitNoteMeta(context.Context, embeddings.NoteFileInfo) error
	SubmitCodeItemEmbedding(context.Context, codeindex.ItemEmbeddingUpsert) error
	SubmitCodeItemEmbeddingBatch(context.Context, []codeindex.ItemEmbeddingUpsert) error
	SubmitCodeItemChunks(context.Context, codeindex.AnchorID, []codeindex.ChunkInput, []string, []embeddings.Embedding) error
	SubmitCodeItemChunkBatch(context.Context, []codeindex.ItemChunksUpsert) error
	SubmitNoteChunkSync(context.Context, embeddings.NoteChunkSync) error
	SubmitNoteChunks(context.Context, embeddings.NoteID, []embeddings.ChunkInput, []embeddings.Embedding) error
	SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error
	SubmitIntelChunksByFamily(context.Context, []string, string, []codeanchor.IntelChunk) error
	SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error
	SubmitOntologyNodes(context.Context, []string, []codeanchor.IntelOntologyNode) error
	SubmitOntologyNodeReadModel(context.Context, codeanchor.IntelOntologyNodeReadModel) error
	SubmitOntologyNodeEmbeddingStates(context.Context, []codeanchor.IntelOntologyNodeEmbeddingState) error
	SubmitOntologyDelta(context.Context, semdb.OntologyDelta) error
	SubmitNoteMetadataDelta(context.Context, semdb.NoteMetadataDelta) error
	FlushAndWait(context.Context) error
}

var (
	_ semantic.SyncWriteQueue     = (*Writer)(nil)
	_ semantic.NoteMetaWriteQueue = (*Writer)(nil)
)
