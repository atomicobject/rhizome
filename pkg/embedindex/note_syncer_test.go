package embedindex

import (
	"context"

	"path/filepath"

	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestNoteSyncerSyncPathsUnchangedPrefixShrinkPrunesStaleChunks(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	notePath := "notes/shrinks.md"
	now := time.Now().Unix()
	sections := []codeanchor.IntelDocSection{
		{
			SectionID:   "section-1",
			Path:        notePath,
			Title:       "First",
			Level:       2,
			Content:     "unchanged prefix",
			Fingerprint: "fp-1",
			UpdatedAt:   now,
		},
		{
			SectionID:   "section-2",
			Path:        notePath,
			Title:       "Second",
			Level:       2,
			Content:     "removed suffix",
			Fingerprint: "fp-2",
			UpdatedAt:   now,
		},
	}
	originalSections := append([]codeanchor.IntelDocSection(nil), sections...)

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(tempDir, "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	source := stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{notePath: sections}}
	syncer := NoteSyncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        source,
		WriteQueue:   &recordingNoteWriteQueue{store: store},
	}

	require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
	chunks, err := store.NoteChunks(ctx, embeddings.NoteID(notePath))
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	firstVector := append(embeddings.Embedding(nil), chunks[0].Embedding...)
	secondVector := append(embeddings.Embedding(nil), chunks[1].Embedding...)
	require.NotEmpty(t, firstVector)
	require.NotEmpty(t, secondVector)
	require.NotEqual(t, firstVector, secondVector)
	queue := syncer.WriteQueue.(*recordingNoteWriteQueue)
	require.Len(t, queue.syncs, 1)
	require.Equal(t, embeddings.NoteID(notePath), queue.syncs[0].NoteID)
	require.Equal(t, []int{0, 1}, queue.syncs[0].KeepIndices)
	require.Len(t, queue.syncs[0].Chunks, 2)
	require.Len(t, queue.syncs[0].Embeddings, 2)
	sections[1].Content = "changed suffix"
	sections[1].Fingerprint = "fp-2-changed"
	sections[1].UpdatedAt = now + 1
	require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
	chunks, err = store.NoteChunks(ctx, embeddings.NoteID(notePath))
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	require.Equal(t, firstVector, chunks[0].Embedding)
	require.NotEqual(t, secondVector, chunks[1].Embedding)

	source.byPath[notePath] = sections[:1]
	require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))

	chunks, err = store.NoteChunks(ctx, embeddings.NoteID(notePath))
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, 0, chunks[0].Index)
	require.Equal(t, "First", chunks[0].Heading)
	require.Equal(t, firstVector, chunks[0].Embedding)

	t.Run("unified store", func(t *testing.T) {
		collector := indexingperf.New()
		ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "embed_notes")
		idx, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
		require.NoError(t, err)
		t.Cleanup(func() { _ = idx.Close() })
		reused := embeddings.Embedding{9, 8, 7, 6, 5, 4, 3, 2}
		firstChunk := buildSectionChunks("shrinks", originalSections, 0)[0]
		require.NoError(t, idx.CacheEmbedding(ctx, firstChunk.Hash, reused))
		intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = intelStore.Close() })
		require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, notePath, originalSections, nil, nil))
		source := stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{notePath: append([]codeanchor.IntelDocSection(nil), originalSections...)}}
		syncer := NoteSyncer{Index: idx, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: source, ChunkWriter: intelStore, EmbeddingWriter: intelStore}
		require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
		summary := collector.RenderSummary()
		require.Contains(t, summary, "reuse_hits=1")
		persisted, err := intelStore.IntelChunksByOwners(ctx, []string{"section-1", "section-2"})
		require.NoError(t, err)
		require.Len(t, persisted, 2)
		ids := []string{persisted[0].ChunkID, persisted[1].ChunkID}
		vectors, err := intelStore.EmbeddingsByChunkIDs(ctx, ids)
		require.NoError(t, err)
		require.Equal(t, reused, vectors[ids[0]])
		require.NotEmpty(t, vectors[ids[1]])
		require.NotEqual(t, vectors[ids[0]], vectors[ids[1]])
		first := append(embeddings.Embedding(nil), vectors[ids[0]]...)
		second := append(embeddings.Embedding(nil), vectors[ids[1]]...)
		changed := append([]codeanchor.IntelDocSection(nil), originalSections...)
		changed[1] = sections[1]
		source.byPath[notePath] = changed
		require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, notePath, changed, nil, nil))
		require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
		vectors, err = intelStore.EmbeddingsByChunkIDs(ctx, ids)
		require.NoError(t, err)
		require.Equal(t, first, vectors[ids[0]])
		require.NotEqual(t, second, vectors[ids[1]])
		source.byPath[notePath] = sections[:1]
		require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, notePath, sections[:1], nil, nil))
		require.NoError(t, syncer.SyncPaths(ctx, []string{notePath}))
		persisted, err = intelStore.IntelChunksByOwners(ctx, []string{"section-1", "section-2"})
		require.NoError(t, err)
		require.Len(t, persisted, 1)
		require.Equal(t, "section-1", persisted[0].OwnerID)
		vectors, err = intelStore.EmbeddingsByChunkIDs(ctx, ids)
		require.NoError(t, err)
		require.Equal(t, first, vectors[ids[0]])
		require.NotContains(t, vectors, ids[1])
	})
}

func TestNoteSyncerSyncPathsDoesNotMarkLastSync(t *testing.T) {
	tempDir := t.TempDir()

	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(tempDir, "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := NoteSyncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel: stubDocIntelSourceByPath{byPath: map[string][]codeanchor.IntelDocSection{
			"notes/a.md": {{
				SectionID:   "a-1",
				Path:        "notes/a.md",
				Title:       "A",
				Level:       2,
				Content:     "hello a",
				Fingerprint: "fp-a",
				UpdatedAt:   time.Now().Unix(),
			}},
		}},
	}

	require.NoError(t, syncer.SyncPaths(context.Background(), []string{"notes/a.md"}))

	meta, ok, err := store.Metadata(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, meta.LastSync.IsZero())
}

func TestNoteSyncerFullSyncUsesSourceHighWaterForSkip(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(tempDir, "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sections := []codeanchor.IntelDocSection{{
		SectionID:   "a-1",
		Path:        "notes/a.md",
		Title:       "A",
		Level:       1,
		Content:     "first body",
		Fingerprint: "fp-a",
		UpdatedAt:   100,
	}}
	syncer := NoteSyncer{
		Index:        store,
		Provider:     prov,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        stubDocIntelSource{sections: sections},
	}
	require.NoError(t, syncer.Sync(ctx))
	prov.Reset()

	require.NoError(t, store.UpdateLastSync(ctx, time.Unix(10_000, 0)))
	syncer.Intel = stubDocIntelSource{sections: []codeanchor.IntelDocSection{{
		SectionID:   "a-1",
		Path:        "notes/a.md",
		Title:       "A",
		Level:       1,
		Content:     "changed body after high-water",
		Fingerprint: "fp-a2",
		UpdatedAt:   150,
	}}}

	require.NoError(t, syncer.Sync(ctx))
	require.Greater(t, prov.calls.Load(), int64(0), "future diagnostic last_sync must not hide newer source rows")

	meta, ok, err := store.Metadata(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(150), meta.SourceHighWater.Unix())
}

func TestNoteSyncerFullSyncPrunesStaleExtraWithoutReembeddingCurrent(t *testing.T) {
	ctx := context.Background()
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
	sections := []codeanchor.IntelDocSection{{
		SectionID: "current-1", Path: "notes/current.md", Title: "Current", Level: 1,
		Content: "current body", Fingerprint: "current-fp", UpdatedAt: 100,
	}}
	syncer := NoteSyncer{
		Index: store, Provider: provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:        stubDocIntelSource{sections: sections},
	}
	require.NoError(t, syncer.Sync(ctx))
	require.NoError(t, store.UpsertNoteMeta(ctx, embeddings.NoteFileInfo{ID: "notes/stale.md", Path: "notes/stale.md", Title: "Stale", Mtime: time.Unix(99, 0)}))
	provider.Reset()

	require.NoError(t, syncer.Sync(ctx))
	require.Zero(t, provider.calls.Load(), "unchanged current source must not be re-embedded")
	notes, err := store.ListNotes(ctx)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	require.Equal(t, "notes/current.md", notes[0].Path)
}
