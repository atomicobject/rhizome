package notemeta

import (
	"context"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestMarkdownPublicationPreservesResolverRejection(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	for name, body := range map[string]string{
		"Source.md":       "[bad](C%3A/Other/Only.md)",
		"Notes/Source.md": "[bad](C%3A/Other/Only.md)",
		"Notes/Valid.md":  "[good](../Other/Only.md)",
		"Other/Only.md":   "# Target",
	} {
		writePublishedTestFile(t, root, name, body)
	}
	vault := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	reader := &obsidian.Note{}
	store := openPublishedMetadataStore(t, root)
	indexer := testIndexer(t)
	_, err := indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	edges, err := store.GraphDocNoteLinkEdges(ctx)
	require.NoError(t, err)
	require.Equal(t, []semdb.GraphDocEdgeRow{{
		SrcPath: "Notes/Valid.md", DstPath: "Other/Only.md", Kind: "note_link:mdlink:basic",
		Confidence: semdb.EdgeConfidenceExtracted, ConfidenceScore: 0.9,
	}}, edges)
	for _, source := range []string{"Source.md", "Notes/Source.md"} {
		writePublishedTestFile(t, root, source, "[bad](a%3Ab/Only.md)")
	}
	err = indexer.SyncPaths(ctx, vault, reader, store, []string{"Source.md", "Notes/Source.md"}, nil)
	require.NoError(t, err)
	after, err := store.GraphDocNoteLinkEdges(ctx)
	require.NoError(t, err)
	require.Equal(t, edges, after)
	fresh := openPublishedMetadataStore(t, t.TempDir())
	_, err = indexer.EnsureIndexed(ctx, vault, reader, fresh)
	require.NoError(t, err)
	snapshot, err := fresh.GraphDocNoteLinkEdges(ctx)
	require.NoError(t, err)
	require.Equal(t, edges, snapshot)
}
