package unifiedsearch

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestIndexGenerationChangesWhenPublishedNoteStateChanges(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.SetScopeConfigHash(ctx, "scope"))
	require.NoError(t, store.SetIndexerVersion(ctx, "indexer"))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{State: semdb.NoteMetadataState{NotesHash: "notes-1", RawNotesHash: "raw-1", LoadedAt: 1, Ready: true}}))
	first, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)
	request := EffectiveRequest{Queries: []QueryInput{{Text: "search", Mode: "search"}}, Profile: ProfileInteractive, Policy: EffectivePolicy{Profile: ProfileInteractive, CandidateWindow: 100, MaxPerOwner: 2}}
	identity, err := RequestIdentity(request, "vault")
	require.NoError(t, err)
	token, err := EncodeContinuation(ContinuationCursor{RequestIdentity: identity, IndexGeneration: first, WindowDigest: "window", CandidateWindow: 100, Offset: 10})
	require.NoError(t, err)
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{State: semdb.NoteMetadataState{NotesHash: "notes-2", RawNotesHash: "raw-2", LoadedAt: 2, Ready: true}}))
	second, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	decoded, err := DecodeContinuation(token)
	require.NoError(t, err)
	require.ErrorIs(t, ValidateContinuation(decoded, identity, second, "window", 100), ErrCursorStale)
}

func TestIndexGenerationChangesWhenCodeCorpusChangesWithoutEmbeddings(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{Path: "pkg/search.go", Lang: codeanchor.LangGo, Hash: "hash-1", ParseStatus: codeanchor.ParseOK}))
	first, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{Path: "pkg/search.go", Lang: codeanchor.LangGo, Hash: "hash-2", ParseStatus: codeanchor.ParseOK}))
	second, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestIndexGenerationRejectsEmptyIndexState(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = IndexGeneration(ctx, store, nil, nil)
	require.ErrorIs(t, err, ErrIndexGenerationUnavailable)
}

func TestIndexGenerationChangesWhenUnifiedEmbeddingChangesWithoutSourceChange(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"section"}, []codeanchor.IntelChunk{{
		ChunkID: "chunk", OwnerID: "section", OwnerType: "doc_section", Granularity: "section", ContentHash: "same-source",
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"chunk": {1, 0, 0, 0}}))
	first, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)

	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"chunk": {0, 1, 0, 0}}))
	second, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestIndexGenerationTracksUnifiedEmbeddingDeletionRebuildAndProviderConfig(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{Path: "pkg/search.go", Lang: codeanchor.LangGo, Hash: "same-source", ParseStatus: codeanchor.ParseOK}))
	chunk := codeanchor.IntelChunk{ChunkID: "chunk", OwnerID: "section", OwnerType: "doc_section", Granularity: "section", ContentHash: "same-source"}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"section"}, []codeanchor.IntelChunk{chunk}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"chunk": {1, 0, 0, 0}}))
	initial, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)

	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"section"}, nil))
	afterDelete, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)
	require.NotEqual(t, initial, afterDelete)

	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"section"}, []codeanchor.IntelChunk{chunk}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"chunk": {1, 0, 0, 0}}))
	afterRebuild, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)
	require.NotEqual(t, afterDelete, afterRebuild)

	require.NoError(t, store.SetPackMetadata(ctx, codeanchor.PackMetadata{ConfigHash: "provider-region-b", ModelHash: "model-b"}))
	afterProviderChange, err := IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)
	require.NotEqual(t, afterRebuild, afterProviderChange)
}
