package semantic

import (
	"context"

	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

type fixedEmbeddingProvider map[string]embeddings.Embedding

func (p fixedEmbeddingProvider) EmbedTexts(_ context.Context, texts []string) ([]embeddings.Embedding, error) {
	out := make([]embeddings.Embedding, 0, len(texts))
	for _, text := range texts {
		out = append(out, append(embeddings.Embedding(nil), p[text]...))
	}
	return out, nil
}

func (p fixedEmbeddingProvider) Dimensions() int { return 4 }

func embedSingle(t *testing.T, prov embeddings.Provider, text string) embeddings.Embedding {
	t.Helper()
	vecs, err := prov.EmbedTexts(context.Background(), []string{text})
	require.NoError(t, err)
	require.Len(t, vecs, 1)
	return vecs[0]
}

func setupIntelStore(t *testing.T, prov embeddings.Provider) *semdb.Store {
	t.Helper()
	ctx := context.Background()
	intelPath := filepath.Join(t.TempDir(), "intel.db")
	intelStore, err := semdb.Open(intelPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	anchors := []codeanchor.IntelAnchor{
		{
			AnchorID:    "anchor1",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/utils.go",
			Symbol:      "Helper",
			FQN:         "pkg.utils.Helper",
			Fingerprint: "fp1",
		},
	}
	require.NoError(t, intelStore.ReplaceIntelCodeFile(ctx, "pkg/utils.go", anchors, nil, nil))

	sections := []codeanchor.IntelDocSection{
		{
			SectionID:   "section1",
			Path:        "docs/README.md",
			Title:       "Overview",
			Level:       1,
			Content:     "This is the overview of the project",
			Fingerprint: "fp2",
		},
	}
	require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, "docs/README.md", sections, nil, nil))

	chunks := []codeanchor.IntelChunk{
		{ChunkID: "chunk1", OwnerID: "anchor1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h1"},
		{ChunkID: "chunk2", OwnerID: "section1", OwnerType: "doc_section", Ord: 0, Granularity: "section", ContentHash: "h2"},
	}
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{"anchor1", "section1"}, chunks))

	codeVec := embedSingle(t, prov, "helper function utility")
	noteVec := embedSingle(t, prov, "overview documentation")
	embeddingsMap := map[string]embeddings.Embedding{
		"chunk1": codeVec,
		"chunk2": noteVec,
	}
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, embeddingsMap))

	return intelStore
}

func setupIntelStoreWithEmbeddings(t *testing.T, codeVec, noteVec embeddings.Embedding) *semdb.Store {
	t.Helper()
	ctx := context.Background()
	intelPath := filepath.Join(t.TempDir(), "intel.db")
	intelStore, err := semdb.Open(intelPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	anchors := []codeanchor.IntelAnchor{
		{
			AnchorID:    "anchor1",
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        "pkg/utils.go",
			Symbol:      "Helper",
			FQN:         "pkg.utils.Helper",
			Fingerprint: "fp1",
		},
	}
	require.NoError(t, intelStore.ReplaceIntelCodeFile(ctx, "pkg/utils.go", anchors, nil, nil))

	sections := []codeanchor.IntelDocSection{
		{
			SectionID:   "section1",
			Path:        "docs/README.md",
			Title:       "Overview",
			Level:       1,
			Content:     "This is the overview of the project",
			Fingerprint: "fp2",
		},
	}
	require.NoError(t, intelStore.ReplaceIntelDocSections(ctx, "docs/README.md", sections, nil, nil))

	chunks := []codeanchor.IntelChunk{
		{ChunkID: "chunk1", OwnerID: "anchor1", OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "h1"},
		{ChunkID: "chunk2", OwnerID: "section1", OwnerType: "doc_section", Ord: 0, Granularity: "section", ContentHash: "h2"},
	}
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{"anchor1", "section1"}, chunks))

	embeddingsMap := map[string]embeddings.Embedding{
		"chunk1": codeVec,
		"chunk2": noteVec,
	}
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, embeddingsMap))

	return intelStore
}
