package notemeta

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func touchPublishedNote(t *testing.T, root, notePath string, delta time.Duration) int64 {
	t.Helper()
	absPath := filepath.Join(root, filepath.FromSlash(notePath))
	info, err := os.Stat(absPath)
	require.NoError(t, err)
	next := info.ModTime().Add(delta)
	require.NoError(t, os.Chtimes(absPath, next, next))
	return next.Unix()
}

func TestBuildPublishedMetadataDeltaTouchOnlyChangeUpdatesMtimeWithoutRematerializing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writePublishedTestFile(t, root, "notes/source.md", "See [[target]].\n")
	writePublishedTestFile(t, root, "notes/target.md", "# Target\n#topic\n")
	store := openPublishedMetadataStore(t, root)
	indexer, _ := publishedMetadataIndexer(t)
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}, Links: obsidian.LinkTypeBoth}
	pathsList := []paths.NotePath{"notes/source.md", "notes/target.md"}

	initial, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, pathsList, nil)
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *initial))
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, edges)
	fingerprint, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)

	sourceMtime := touchPublishedNote(t, root, "notes/source.md", 2*time.Second)
	targetMtime := touchPublishedNote(t, root, "notes/target.md", 2*time.Second)

	touchDelta, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, pathsList, nil)
	require.NoError(t, err)
	require.NotNil(t, touchDelta)
	require.Empty(t, touchDelta.Notes, "content-identical notes must not be re-materialized")
	require.Empty(t, touchDelta.DeletedPaths)
	require.Empty(t, touchDelta.WikilinkEdges)
	require.Equal(t, state, touchDelta.State, "a touch-only delta retains the published generation")
	require.ElementsMatch(t, []string{"notes/source.md", "notes/target.md"}, touchedPaths(touchDelta.SourceTouches))

	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *touchDelta))
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/source.md", "notes/target.md"})
	require.NoError(t, err)
	require.Equal(t, sourceMtime, rows["notes/source.md"].Mtime)
	require.Equal(t, targetMtime, rows["notes/target.md"].Mtime)
	updatedState, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, state, updatedState)
	updatedEdges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Equal(t, edges, updatedEdges)
	updatedFingerprint, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, fingerprint, updatedFingerprint)

	settled, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, pathsList, nil)
	require.NoError(t, err)
	require.Nil(t, settled, "a settled published state still yields no delta")
}

func TestBuildPublishedMetadataDeltaProviderEnvelopeChangeStillRewrites(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writePublishedTestFile(t, root, "notes/source.md", "See [[target]].\n")
	writePublishedTestFile(t, root, "notes/target.md", "# Target\n")
	store := openPublishedMetadataStore(t, root)
	indexer, _ := publishedMetadataIndexer(t)
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}, Links: obsidian.LinkTypeBoth}
	pathsList := []paths.NotePath{"notes/source.md", "notes/target.md"}

	initial, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, pathsList, nil)
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *initial))
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/source.md"})
	require.NoError(t, err)
	drifted := rows["notes/source.md"]
	drifted.Projection.ProviderVersion = "markdown-provider-v0"
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, semdb.NoteMetadataDelta{State: state, Notes: []semdb.NoteMetadataRow{drifted}}))
	touchPublishedNote(t, root, "notes/source.md", 2*time.Second)

	rebuilt, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, pathsList, nil)
	require.NoError(t, err)
	require.NotNil(t, rebuilt)
	require.Len(t, rebuilt.Notes, 2, "a drifted provider envelope must fully rewrite, not touch")
	require.Empty(t, rebuilt.SourceTouches)
	require.NotEqual(t, state.LoadedAt, rebuilt.State.LoadedAt)
}

func touchedPaths(touches []semdb.NoteSourceTouch) []string {
	out := make([]string, 0, len(touches))
	for _, touch := range touches {
		out = append(out, touch.Path)
	}
	return out
}
