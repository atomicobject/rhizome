package notemeta

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestScopedPublishedMetadataSurvivesOwnershipRemovalWithoutReadingUntouchedSources(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writePublishedTestFile(t, root, "notes/source.md", "See [[target]].\n")
	writePublishedTestFile(t, root, "notes/target.md", "# Target\nSee [[source]].\n")
	store := openPublishedMetadataStore(t, root)
	indexer, runtime := publishedMetadataIndexer(t)
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}, Links: obsidian.LinkTypeBoth}
	all := []paths.NotePath{"notes/source.md", "notes/target.md"}
	initial, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, all, nil)
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *initial))
	baseline, err := indexer.CapturePublishedBaseline(ctx, vault, store)
	require.NoError(t, err)
	provider, ok := runtime.Provider("markdown")
	require.True(t, ok)
	source, err := noteformat.NewAuthoredSource("notes/source.md", provider.Descriptor(), []byte("Changed. See [[target]].\n"), 42)
	require.NoError(t, err)
	preparation, err := indexer.PreparePublishedMetadata(ctx, all, map[paths.NotePath]noteformat.AuthoredSource{"notes/source.md": source})
	require.NoError(t, err)
	transitions, err := preparation.OwnershipTransitions(42)
	require.NoError(t, err)
	_, err = store.ApplyOwnershipTransitions(ctx, transitions)
	require.NoError(t, err)
	pending, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.False(t, pending.Ready)
	delta, full, err := indexer.BuildPublishedPathMetadataDeltaFromPreparation(ctx, vault, store, baseline, preparation, nil)
	require.NoError(t, err)
	require.False(t, full)
	require.Len(t, delta.Notes, 1)
	require.Equal(t, "notes/source.md", delta.Notes[0].Path)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *delta))
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/source.md", "notes/target.md"})
	require.NoError(t, err)
	require.Equal(t, source.ContentHash(), rows["notes/source.md"].ContentHash)
	require.Equal(t, baseline.rows["notes/target.md"], rows["notes/target.md"])
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	found := false
	for _, edge := range edges {
		if edge.SrcPath == "notes/source.md" && edge.DstPath == "notes/target.md" {
			found = true
		}
	}
	require.True(t, found)
	detailed, err := store.AllGraphDocEdgesWithConfidence(ctx)
	require.NoError(t, err)
	require.Len(t, detailed, 4, "both detailed and coarse incoming and outgoing links survive")
	// Comparing a full sealed rebuild proves incremental XOR/state identity.
	target, err := noteformat.NewAuthoredSource("notes/target.md", provider.Descriptor(), []byte("# Target\nSee [[source]].\n"), baseline.rows["notes/target.md"].Mtime)
	require.NoError(t, err)
	fullPreparation, err := indexer.PreparePublishedMetadata(ctx, all, map[paths.NotePath]noteformat.AuthoredSource{"notes/source.md": source, "notes/target.md": target})
	require.NoError(t, err)
	fresh, err := indexer.BuildPublishedMetadataDeltaFromPreparation(ctx, vault, &publishedFailingReader{}, store, fullPreparation)
	require.NoError(t, err)
	require.Nil(t, fresh)
}

func TestScopedPublishedMetadataRequestsFullPublicationForAliasTopologyChange(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writePublishedTestFile(t, root, "notes/source.md", "See [[NEW]].\n")
	writePublishedTestFile(t, root, "notes/target.md", "# Target\n")
	store := openPublishedMetadataStore(t, root)
	indexer, runtime := publishedMetadataIndexer(t)
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}, Links: obsidian.LinkTypeBoth}
	all := []paths.NotePath{"notes/source.md", "notes/target.md"}
	initial, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, all, nil)
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *initial))
	baseline, err := indexer.CapturePublishedBaseline(ctx, vault, store)
	require.NoError(t, err)
	provider, _ := runtime.Provider("markdown")
	source, err := noteformat.NewAuthoredSource("notes/target.md", provider.Descriptor(), []byte("---\naliases: [NEW]\n---\n# Target\n"), 42)
	require.NoError(t, err)
	preparation, err := indexer.PreparePublishedMetadata(ctx, all, map[paths.NotePath]noteformat.AuthoredSource{"notes/target.md": source})
	require.NoError(t, err)
	transitions, err := preparation.OwnershipTransitions(42)
	require.NoError(t, err)
	_, err = store.ApplyOwnershipTransitions(ctx, transitions)
	require.NoError(t, err)
	delta, full, err := indexer.BuildPublishedPathMetadataDeltaFromPreparation(ctx, vault, store, baseline, preparation, nil)
	require.NoError(t, err)
	require.True(t, full)
	require.Nil(t, delta)
	complete, err := indexer.BuildPublishedMetadataDeltaFromPreparation(ctx, vault, &obsidian.Note{}, store, preparation)
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *complete))
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	found := false
	for _, edge := range edges {
		if edge.SrcPath == "notes/source.md" && edge.DstPath == "notes/target.md" {
			found = true
		}
	}
	require.True(t, found)
}
