package embedindex

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	"github.com/stretchr/testify/require"
)

func TestNoteSyncerBatchesIntelEmbeddingWritesAcrossNotes(t *testing.T) {
	tempDir := t.TempDir()

	const noteCount = 48
	sections := make([]codeanchor.IntelDocSection, 0, noteCount)
	now := time.Now().Unix()
	for i := 0; i < noteCount; i++ {
		path := filepath.ToSlash(filepath.Join("notes", "n"+strconv.Itoa(i)+".md"))
		sections = append(sections, codeanchor.IntelDocSection{
			SectionID:   "s" + strconv.Itoa(i),
			Path:        path,
			Title:       "Section",
			Level:       2,
			Content:     "Hello " + strconv.Itoa(i),
			Fingerprint: "fp-" + strconv.Itoa(i),
			UpdatedAt:   now,
		})
	}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(tempDir, "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	writer := &recordingEmbeddingWriter{}
	syncer := NoteSyncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           stubDocIntelSource{sections: sections},
		BatchSize:       100,
		MaxConcurrent:   32,
		EmbeddingWriter: writer,
	}

	require.NoError(t, syncer.Sync(context.Background()))
	require.Less(t, writer.CallCount(), noteCount, "intel writes should batch across notes")
	require.Equal(t, noteCount, writer.StoredCount())
}

func TestNoteSyncerNoteEmbedPackerBatchesAcrossNotes(t *testing.T) {
	for _, tc := range []struct {
		name                                               string
		noteCount, sectionsPerNote, batchSize, concurrency int
		packer                                             *EmbedPackerOptions
	}{
		{"default packer", 50, 1, 100, 250, nil},
		{"explicit packer", 40, 3, 8, 12, &EmbedPackerOptions{MinTexts: 12, MaxTexts: 64, MaxBytes: 96 << 10, MaxWait: 50 * time.Millisecond}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			sections := make([]codeanchor.IntelDocSection, 0, tc.noteCount*tc.sectionsPerNote)
			now := time.Now().Unix()
			for i := 0; i < tc.noteCount; i++ {
				path := filepath.ToSlash(filepath.Join("notes", "n"+strconv.Itoa(i)+".md"))
				for j := 0; j < tc.sectionsPerNote; j++ {
					sections = append(sections, codeanchor.IntelDocSection{SectionID: "s" + strconv.Itoa(i) + "-" + strconv.Itoa(j), Path: path, Title: "Section " + strconv.Itoa(j), Level: 2, Content: "Hello " + strconv.Itoa(i) + " " + strconv.Itoa(j), Fingerprint: "fp-" + strconv.Itoa(i) + "-" + strconv.Itoa(j), UpdatedAt: now})
				}
			}
			prov := &countingProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})}
			store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(t.TempDir(), "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			syncer := NoteSyncer{Index: store, Provider: prov, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, Intel: stubDocIntelSource{sections: sections}, BatchSize: tc.batchSize, MaxConcurrent: tc.concurrency, NoteEmbedPacker: tc.packer}
			require.NoError(t, syncer.Sync(ctx))
			require.Positive(t, prov.calls.Load())
			require.Less(t, prov.calls.Load(), int64(tc.noteCount), "provider should batch across notes")
			for i := 0; i < tc.noteCount; i++ {
				path := filepath.ToSlash(filepath.Join("notes", "n"+strconv.Itoa(i)+".md"))
				chunks, err := store.NoteChunks(ctx, embeddings.NoteID(path))
				require.NoError(t, err)
				require.Len(t, chunks, tc.sectionsPerNote, path)
				for j, chunk := range chunks {
					require.Equal(t, j, chunk.Index, path)
					require.Equal(t, "Section "+strconv.Itoa(j), chunk.Heading, path)
					text := "# n" + strconv.Itoa(i) + "\n\n## Section " + strconv.Itoa(j) + "\n\nHello " + strconv.Itoa(i) + " " + strconv.Itoa(j)
					want, err := prov.inner.EmbedTexts(ctx, []string{text})
					require.NoError(t, err)
					require.Equal(t, want[0], chunk.Embedding, path)
				}
			}
		})
	}
}

func TestNoteSyncerReturnsIntelEmbeddingFlushError(t *testing.T) {
	tempDir := t.TempDir()

	sections := []codeanchor.IntelDocSection{{
		SectionID:   "s1",
		Path:        "notes/n1.md",
		Title:       "Section",
		Level:       2,
		Content:     "Hello",
		Fingerprint: "fp-1",
		UpdatedAt:   time.Now().Unix(),
	}}

	prov := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	}
	store, err := embsqlite.OpenNotes(context.Background(), embsqlite.File(filepath.Join(tempDir, "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	syncer := NoteSyncer{
		Index:           store,
		Provider:        prov,
		ProviderInfo:    embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Intel:           stubDocIntelSource{sections: sections},
		BatchSize:       100,
		MaxConcurrent:   4,
		EmbeddingWriter: &recordingEmbeddingWriter{failCall: 1, failErr: errors.New("boom")},
	}

	err = syncer.Sync(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
}
