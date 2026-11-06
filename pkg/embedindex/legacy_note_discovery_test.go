package embedindex

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLegacyNoteDiscoveryExcludesIgnoredPathsWhenOntologyUnavailable(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Ignored.md"), []byte("# Ignored\nprivate body\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Included.md"), []byte("# Included\npublic body\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ignore"), []byte("Ignored.md\n"), 0o600))

	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	snapshot, err := noteownership.Discover(ctx, noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Path: root},
		Registry:        runtime.Registry(),
	})
	require.NoError(t, err)
	require.Equal(t, []paths.NotePath{"Included.md"}, snapshot.NoteKeepPaths())

	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	service := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	for _, candidate := range snapshot.PresentNoteCandidates() {
		content, err := os.ReadFile(candidate.AbsPath.String())
		require.NoError(t, err)
		source, err := indexer.ProjectNoteSource(candidate.Path.String(), string(content), candidate.ModTime)
		require.NoError(t, err)
		_, err = service.IngestNoteSource(ctx, source)
		require.NoError(t, err)
	}

	provider := &legacyNoteRecordingProvider{Provider: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
	index, err := embsqlite.OpenNotes(ctx, embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: provider.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = index.Close() })
	syncer := NoteSyncer{
		Index: index, Provider: provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        store, ChunkWriter: store, EmbeddingWriter: store,
		RawEligibility: NewOntologyRawNoteEligibility(store),
	}
	plan, err := syncer.Plan(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, plan.TotalWork)
	require.NoError(t, syncer.Embed(ctx, plan))

	require.Len(t, provider.texts, 1)
	require.Contains(t, provider.texts[0], "public body")
	require.NotContains(t, provider.texts[0], "private body")
	sections, err := store.IntelDocSections(ctx)
	require.NoError(t, err)
	require.Len(t, sections, 1)
	require.Equal(t, "Included.md", sections[0].Path)
	chunks, err := store.IntelChunksByOwners(ctx, []string{sections[0].SectionID})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	vectors, err := store.EmbeddingsByChunkIDs(ctx, []string{chunks[0].ChunkID})
	require.NoError(t, err)
	require.Len(t, vectors[chunks[0].ChunkID], provider.Dimensions())
	ignored, err := index.NoteChunks(ctx, embeddings.NoteID("Ignored.md"))
	require.NoError(t, err)
	require.Empty(t, ignored)
}

type legacyNoteRecordingProvider struct {
	embeddings.Provider
	mu    sync.Mutex
	texts []string
}

func (p *legacyNoteRecordingProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.mu.Lock()
	p.texts = append(p.texts, texts...)
	p.mu.Unlock()
	return p.Provider.EmbedTexts(ctx, texts)
}
