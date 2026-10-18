package indexing

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	noteemb "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRunUnifiedCore_PendingOwnershipPrunesUndiscoverableNoteEmbeddings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "notes", "Source.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
	require.NoError(t, os.WriteFile(sourcePath, []byte("# Source\n\nSemantic body.\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Keep.md"), []byte("# Keep\n\nUnchanged semantic body.\n"), 0o600))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}}
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{
		Notes:          definitionToLocalNotes(definition),
		NoteEmbeddings: &embeddings.Config{Enabled: true, Provider: "test", Model: "test", Dimensions: 8, MaxConcurrency: 1},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	indexer := testkit.NoteMetadataIndexer(t)
	require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	require.ElementsMatch(t, []string{"notes/Keep.md", "notes/Source.md"}, pendingSemanticNoteEmbeddingPaths(t, ctx, root))

	// Model an interrupted deletion after ownership committed. The source is no
	// longer discoverable, so the normal deleted-path stream has no input.
	require.NoError(t, os.Rename(sourcePath, sourcePath+".removed"))
	store, cleanup, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	result, err := store.ApplyOwnershipTransitions(ctx, []semdb.OwnershipTransition{{
		Path: "notes/Source.md", Target: semdb.OwnershipTargetUnowned,
	}})
	require.NoError(t, err)
	require.Positive(t, result.ReconciliationGeneration)
	cleanup()

	require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	require.ElementsMatch(t, []string{"notes/Keep.md"}, pendingSemanticNoteEmbeddingPaths(t, ctx, root))
	store, cleanup, err = indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	t.Cleanup(cleanup)
	generation, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.Equal(t, result.ReconciliationGeneration, generation)
	require.False(t, pending)
}

func pendingSemanticNoteEmbeddingPaths(t *testing.T, ctx context.Context, root string) []string {
	t.Helper()
	store, err := noteemb.OpenNotes(context.Background(), noteemb.File(repoconfig.UnifiedIndexPath(root, "")), noteemb.Options{Access: noteemb.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	notes, err := store.ListNotes(ctx)
	require.NoError(t, err)
	paths := make([]string, 0, len(notes))
	for _, note := range notes {
		paths = append(paths, note.Path)
	}
	return paths
}

func definitionToLocalNotes(definition obsidian.VaultDefinition) repoconfig.LocalVaultConfig {
	return repoconfig.LocalVaultConfig{Includes: append([]string(nil), definition.Includes...)}
}
