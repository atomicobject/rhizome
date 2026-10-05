package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	noteemb "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunUnifiedCore_PendingOwnershipPrunesUndiscoverableNoteEmbeddings(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "notes", "Source.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o755))
	require.NoError(t, os.WriteFile(sourcePath, []byte("# Source\n\nSemantic body.\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Keep.md"), []byte("# Keep\n\nUnchanged semantic body.\n"), 0o600))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}}
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Notes:          definitionToLocalNotes(definition),
		NoteEmbeddings: &embeddings.Config{Enabled: true, Provider: "test", Model: "test", Dimensions: 8, MaxConcurrency: 1},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	indexer := testNoteMetadataIndexer(t)
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	require.ElementsMatch(t, []string{"notes/Keep.md", "notes/Source.md"}, pendingSemanticNoteEmbeddingPaths(t, ctx, root))

	// Model an interrupted deletion after ownership committed. The source is no
	// longer discoverable, so the normal deleted-path stream has no input.
	require.NoError(t, os.Rename(sourcePath, sourcePath+".removed"))
	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	result, err := store.ApplyOwnershipTransitions(ctx, []semdb.OwnershipTransition{{
		Path: "notes/Source.md", Target: semdb.OwnershipTargetUnowned,
	}})
	require.NoError(t, err)
	require.Positive(t, result.ReconciliationGeneration)
	cleanup()

	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	require.ElementsMatch(t, []string{"notes/Keep.md"}, pendingSemanticNoteEmbeddingPaths(t, ctx, root))
	store, cleanup, err = obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	generation, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.Equal(t, result.ReconciliationGeneration, generation)
	require.False(t, pending)
}

func pendingSemanticNoteEmbeddingPaths(t *testing.T, ctx context.Context, root string) []string {
	t.Helper()
	store, err := noteemb.Open(obsidian.UnifiedIndexPath(root, ""), 8)
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

func definitionToLocalNotes(definition obsidian.VaultDefinition) obsidian.LocalVaultConfig {
	return obsidian.LocalVaultConfig{Includes: append([]string(nil), definition.Includes...)}
}
