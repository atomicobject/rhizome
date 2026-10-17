package embedindex

import (
	"context"

	"path/filepath"

	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	"github.com/stretchr/testify/require"
)

func TestNoteSyncerSyncPathsBatchesAcrossPaths(t *testing.T) {
	tempDir := t.TempDir()

	sectionsByPath := map[string][]codeanchor.IntelDocSection{
		"notes/a.md": {{
			SectionID:   "a-1",
			Path:        "notes/a.md",
			Title:       "A",
			Level:       2,
			Content:     "hello a",
			Fingerprint: "fp-a",
			UpdatedAt:   time.Now().Unix(),
		}},
		"notes/b.md": {{
			SectionID:   "b-1",
			Path:        "notes/b.md",
			Title:       "B",
			Level:       2,
			Content:     "hello b",
			Fingerprint: "fp-b",
			UpdatedAt:   time.Now().Unix(),
		}},
	}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(tempDir, "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := NoteSyncer{
		Index:         store,
		Provider:      prov,
		ProviderInfo:  embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:         stubDocIntelSourceByPath{byPath: sectionsByPath},
		BatchSize:     16,
		MaxConcurrent: 8,
		NoteEmbedPacker: &EmbedPackerOptions{
			MinTexts: 2,
			MaxTexts: 16,
			MaxBytes: 32 << 10,
			MaxWait:  10 * time.Millisecond,
		},
	}

	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"notes/a.md", "notes/b.md"}))
	require.EqualValues(t, 1, prov.calls.Load())
	for path, sections := range sectionsByPath {
		chunks, err := store.NoteChunks(context.Background(), embeddings.NoteID(path))
		require.NoError(t, err)
		require.Len(t, chunks, 1, path)
		require.Equal(t, sections[0].Title, chunks[0].Heading)
		require.Len(t, chunks[0].Embedding, prov.Dimensions())
	}
}

func TestNoteSyncerSyncPathsDeletesMissingNote(t *testing.T) {
	tempDir := t.TempDir()

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(tempDir, "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	require.NoError(t, store.EnsureSchema(ctx))
	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
		Provider:   "test",
		Model:      "deterministic",
		Dimensions: prov.Dimensions(),
	}))
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{
		ID:    "notes/missing.md",
		Path:  "notes/missing.md",
		Title: "Missing",
		Mtime: time.Now(),
	}))

	syncer := NoteSyncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{}},
	}

	require.NoError(t, syncer.SyncPaths(ctx, []string{"notes/missing.md"}))
	notes, err := store.ListNotes(ctx)
	require.NoError(t, err)
	require.Empty(t, notes)
}
