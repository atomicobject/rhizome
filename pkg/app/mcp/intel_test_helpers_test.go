package mcp

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/stretchr/testify/require"
)

type docSectionSpec struct {
	SectionID  string
	Title      string
	Content    string
	ChunkID    string
	Breadcrumb string
	Heading    string
}

func newIntelStore(t testing.TB) *semdb.Store {
	t.Helper()
	intelPath := filepath.Join(t.TempDir(), "intel.db")
	intelStore, err := sqlitefixture.Open(intelPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })
	return intelStore
}

func addDocSections(t testing.TB, store *semdb.Store, prov embeddings.Provider, path string, specs []docSectionSpec) {
	t.Helper()
	require.NotNil(t, store)
	require.NotNil(t, prov)
	require.NotEmpty(t, specs)

	ctx := context.Background()
	sections := make([]codeanchor.IntelDocSection, 0, len(specs))
	chunks := make([]codeanchor.IntelChunk, 0, len(specs))
	ownerIDs := make([]string, 0, len(specs))
	texts := make([]string, 0, len(specs))

	for i, spec := range specs {
		require.NotEmpty(t, spec.SectionID)
		require.NotEmpty(t, spec.ChunkID)

		sections = append(sections, codeanchor.IntelDocSection{
			SectionID:   spec.SectionID,
			Path:        path,
			Title:       spec.Title,
			Level:       1,
			Content:     spec.Content,
			Fingerprint: "fp-" + spec.SectionID,
		})

		heading := spec.Heading
		if heading == "" {
			heading = spec.Title
		}
		breadcrumb := spec.Breadcrumb
		if breadcrumb == "" {
			breadcrumb = spec.Title
		}

		chunks = append(chunks, codeanchor.IntelChunk{
			ChunkID:     spec.ChunkID,
			OwnerID:     spec.SectionID,
			OwnerType:   "doc_section",
			Ord:         i,
			Granularity: "section",
			Breadcrumb:  breadcrumb,
			Heading:     heading,
			ContentHash: "h-" + spec.ChunkID,
		})

		ownerIDs = append(ownerIDs, spec.SectionID)
		texts = append(texts, spec.Content)
	}

	require.NoError(t, store.ReplaceIntelDocSections(ctx, path, sections, nil, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, ownerIDs, chunks))

	vecs, err := prov.EmbedTexts(ctx, texts)
	require.NoError(t, err)
	require.Len(t, vecs, len(specs))

	embeds := make(map[string]embeddings.Embedding, len(specs))
	for i, spec := range specs {
		embeds[spec.ChunkID] = vecs[i]
	}
	require.NoError(t, store.UpsertEmbeddings(ctx, embeds))
}

func addDocSection(t testing.TB, store *semdb.Store, prov embeddings.Provider, path, title, content, sectionID, chunkID string) {
	t.Helper()
	addDocSections(t, store, prov, path, []docSectionSpec{
		{
			SectionID: sectionID,
			Title:     title,
			Content:   content,
			ChunkID:   chunkID,
		},
	})
}

func addCurrentNoteOwnership(t testing.TB, store *semdb.Store, notePaths ...string) {
	t.Helper()
	ctx := context.Background()
	indexMeta := make(map[string]codeanchor.NoteIndexMeta, len(notePaths))
	rows := make([]semdb.NoteMetadataRow, 0, len(notePaths))
	for _, notePath := range notePaths {
		hash := "hash-" + notePath
		indexMeta[notePath] = codeanchor.NoteIndexMeta{ContentHash: hash, IndexerVersion: codeanchor.NoteIndexerVersion, Mtime: 1}
		rows = append(rows, semdb.NoteMetadataRow{
			Path: notePath, ContentHash: hash, IndexerVersion: codeanchor.NoteIndexerVersion, Mtime: 1, Size: 1, FormatID: "markdown",
			Projection: semdb.NoteProjectionState{ProviderVersion: "markdown-provider-v1", ProjectionVersion: "markdown-projection-v3", SourceContentHash: hash, Status: semdb.NoteProjectionStatusCurrent, UpdatedAt: 1},
		})
	}
	require.NoError(t, store.UpsertNoteMetadataBatch(ctx, indexMeta))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", RawNotesHash: "raw", LoadedAt: 1, Ready: true},
		Notes: rows,
	}))
}

func addAnchor(t testing.TB, store *semdb.Store, prov embeddings.Provider, path, symbol, fqn, anchorID, chunkID string) {
	t.Helper()
	require.NotNil(t, store)
	require.NotNil(t, prov)

	ctx := context.Background()
	anchors := []codeanchor.IntelAnchor{
		{
			AnchorID:    anchorID,
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        path,
			Symbol:      symbol,
			FQN:         fqn,
			Signature:   "func " + symbol + "()",
			StartLine:   7,
			EndLine:     9,
			Fingerprint: "fp-code",
		},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, anchors, nil, nil))
	chunks := []codeanchor.IntelChunk{
		{ChunkID: chunkID, OwnerID: anchorID, OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h-code"},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchorID}, chunks))
	vec := embedSingle(t, prov, symbol)
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{chunkID: vec}))
}

func embedSingle(t testing.TB, prov embeddings.Provider, text string) embeddings.Embedding {
	t.Helper()
	vecs, err := prov.EmbedTexts(context.Background(), []string{text})
	require.NoError(t, err)
	require.Len(t, vecs, 1)
	return vecs[0]
}
