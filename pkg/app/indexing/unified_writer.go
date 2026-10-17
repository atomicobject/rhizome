package indexing

import (
	"context"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/indexwriter"

	semdb "github.com/atomicobject/rhizome/pkg/store"

	"github.com/atomicobject/rhizome/pkg/embedindex"
)

func (r *unifiedSemanticResources) writerHandlers(store *semdb.Store) indexwriter.Handlers {
	handlers := indexwriter.Handlers{
		ApplyIntelChunks:         store.ReplaceIntelChunks,
		ApplyIntelChunksByFamily: store.ReplaceIntelChunksByFamily,
		ApplyIntelEmbeddings:     store.UpsertEmbeddings,
		ApplyOntologyNodeStates:  store.UpsertOntologyNodeEmbeddingStates,
		ApplyValidationState: func(ctx context.Context, write indexwriter.ValidationStateWrite) error {
			generation, err := store.SetValidationRunning(ctx)
			if err != nil {
				return err
			}
			if strings.TrimSpace(write.ErrorMessage) != "" {
				_, err = store.SetValidationError(ctx, generation, write.ErrorMessage, write.DurationMs)
				return err
			}
			write.Snapshot.Generation = generation
			published, err := store.PublishValidationSnapshot(ctx, write.Snapshot)
			if err == nil && !published {
				return fmt.Errorf("validation generation %d was superseded before queued publication", generation)
			}
			return err
		},
	}
	if r.codeStore != nil {
		handlers.ApplyCodeItemEmbeddingBatch = r.codeStore.UpsertItemEmbeddingBatch
		handlers.ApplyCodeItemChunkBatch = r.codeStore.UpsertItemChunksBatch
	}
	if r.noteStore != nil {
		handlers.ApplyNoteMetaBatch = r.noteStore.UpsertNoteMetaBatch
		handlers.ApplyNoteChunkSyncBatch = r.noteStore.SyncNoteChunksBatch
	}
	return handlers
}
func (r *unifiedSemanticResources) bindWriter(writer *indexwriter.Writer) {
	if r.codeSyncer != nil {
		r.codeSyncer.WriteQueue = writer
	}
	if r.noteSyncer != nil {
		r.noteSyncer.WriteQueue = writer
	}
}

var (
	_ embedindex.SyncWriteQueue     = (*indexwriter.Writer)(nil)
	_ embedindex.NoteMetaWriteQueue = (*indexwriter.Writer)(nil)
)
