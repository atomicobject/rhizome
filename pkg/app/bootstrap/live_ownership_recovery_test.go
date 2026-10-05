package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	anchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLiveOwnership_QuietPendingDeletionPrunesDurableDerivedDebt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	currentPath := "current.md"
	deletedPath := "deleted.md"
	require.NoError(t, os.WriteFile(filepath.Join(root, currentPath), []byte("# Current\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer store.Close()
	defer cacheService.Close()
	cacheService.MarkDirty(currentPath, cache.DirtyModified)
	runOwnershipBatch(t, w)
	sections, err := store.IntelDocSections(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, sections)
	currentUpdatedAt := sections[0].UpdatedAt

	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 2})
	noteIndex, err := embsqlite.Open(filepath.Join(root, "notes.db"), provider.Dimensions())
	require.NoError(t, err)
	defer noteIndex.Close()
	current := embeddings.NoteFileInfo{ID: embeddings.NoteID(currentPath), Path: currentPath, Title: "Current"}
	deleted := embeddings.NoteFileInfo{ID: embeddings.NoteID(deletedPath), Path: deletedPath, Title: "Deleted"}
	require.NoError(t, noteIndex.UpsertNoteMeta(ctx, current))
	require.NoError(t, noteIndex.UpsertNoteChunks(ctx, current.ID, []embeddings.ChunkInput{embeddings.NewChunkInput(0, "current", "", "")}, []embeddings.Embedding{{0, 1}}))
	require.NoError(t, noteIndex.UpsertNoteMeta(ctx, deleted))
	require.NoError(t, noteIndex.UpsertNoteChunks(ctx, deleted.ID, []embeddings.ChunkInput{embeddings.NewChunkInput(0, "stale", "", "")}, []embeddings.Embedding{{1, 0}}))
	// The current source is already reflected in the durable high-water mark.
	// The full recovery must still remove the stale extra rather than fast-skip.
	require.NoError(t, noteIndex.UpdateSourceHighWater(ctx, time.Unix(currentUpdatedAt, 0)))

	// This simulates an interruption after ownership removed the note but before
	// its semantic destination was pruned. No raw path remains to rediscover.
	_, err = store.ApplyOwnershipTransitions(ctx, []anchorsqlite.OwnershipTransition{{
		Path: deletedPath, Target: anchorsqlite.OwnershipTargetNote,
		Note: &anchorsqlite.NoteSourceState{Title: "Deleted", FormatID: "markdown", ContentHash: "before", Mtime: 1, Size: 1,
			ProviderVersion: "builtin-markdown-v1", ProjectionVersion: "builtin-markdown-v1", Status: anchorsqlite.NoteProjectionStatusCurrent, ObservedAt: 1},
	}})
	require.NoError(t, err)
	deletedTransition, err := store.ApplyOwnershipTransitions(ctx, []anchorsqlite.OwnershipTransition{{Path: deletedPath, Target: anchorsqlite.OwnershipTargetUnowned}})
	require.NoError(t, err)
	require.Positive(t, deletedTransition.ReconciliationGeneration)

	w.noteSyncer = &semantic.NoteSyncer{
		Index:        noteIndex,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        store,
	}
	w.noteSemanticStore = true

	// The cache has no dirty input. Reconciliation must preserve the current
	// keep-set entry, prune the stranded entry, then acknowledge this generation.
	require.Eventually(t, func() bool {
		runOwnershipBatch(t, w)
		_, pending, err := store.PendingOwnershipReconciliation(ctx)
		return err == nil && !pending
	}, 3*time.Second, 10*time.Millisecond)
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	work, err := store.PendingDerivedWork(ctx, time.Now(), 100)
	require.NoError(t, err)
	for _, ticket := range work {
		if ticket.Kind == codeanchor.DerivedNotes {
			require.NoError(t, d.execute(ticket))
		}
	}
	currentChunks, err := noteIndex.NoteChunks(ctx, current.ID)
	require.NoError(t, err)
	require.NotEmpty(t, currentChunks, "full note sync must retain the current embedding")
	deletedChunks, err := noteIndex.NoteChunks(ctx, deleted.ID)
	require.NoError(t, err)
	require.Empty(t, deletedChunks, "full note sync must remove the stranded embedding")
	acknowledgedGeneration, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, acknowledgedGeneration, deletedTransition.ReconciliationGeneration)
	require.False(t, pending)
}
