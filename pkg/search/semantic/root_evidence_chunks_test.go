package semantic

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

type capturingRootProvider struct {
	inner  embeddings.Provider
	mu     sync.Mutex
	inputs []string
}

func (p *capturingRootProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *capturingRootProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.mu.Lock()
	p.inputs = append(p.inputs, texts...)
	p.mu.Unlock()
	return p.inner.EmbedTexts(ctx, texts)
}

func TestBuildRootEvidenceChunksUsesProviderRegionsWithoutRawSource(t *testing.T) {
	root := &ontology.RootDocumentSnapshot{
		NotePath:  paths.NotePath("prototype.html"),
		RawSource: []byte(`<script>secretRawSource()</script>`),
		SearchRegions: []noteformat.SearchRegionFact{
			{Kind: noteformat.SearchRegionVisible, Origin: noteformat.SearchRegionDerived, Text: "Prototype", MediaType: "text/plain"},
			{Kind: noteformat.SearchRegionVisible, Origin: noteformat.SearchRegionAuthored, Text: "Quarterly results", MediaType: "text/plain", Range: sourceRange(10, 29)},
			{Kind: noteformat.SearchRegionSupplemental, Origin: noteformat.SearchRegionAuthored, Text: `{"export":"forecast.csv"}`, MediaType: "application/json", Range: sourceRange(40, 65)},
		},
	}
	owner := codeanchor.IntelOntologyNode{NodeID: "node:prototype", NotePath: "prototype.html", NodeKind: "NOTE", TypeName: "_FallbackNote", Title: "Prototype"}

	plan := BuildRootEvidenceChunks(root, owner, embeddings.ProviderConfig{Provider: "test", Model: "fixed"}, 1024, 7)

	require.Len(t, plan.Chunks, 2, "an exact title-only region should not duplicate title evidence")
	require.Equal(t, GranularityOntologyNodeVisible, plan.Chunks[0].Granularity)
	require.Equal(t, int64(10), plan.Chunks[0].StartByte)
	require.Equal(t, int64(29), plan.Chunks[0].EndByte)
	require.Equal(t, GranularityOntologyNodeSupplemental, plan.Chunks[1].Granularity)
	require.Equal(t, root.ContentHash, plan.States[1].SourceContentHash)
	require.Contains(t, plan.Texts[plan.Chunks[1].ChunkID], "MediaType: application/json")
	for _, text := range plan.Texts {
		require.NotContains(t, text, "secretRawSource")
	}
}

func TestBuildRootEvidenceChunksRetainsTitleOnlyEvidence(t *testing.T) {
	root := &ontology.RootDocumentSnapshot{
		NotePath: paths.NotePath("annual.html"),
		Format:   "html",
		SearchRegions: []noteformat.SearchRegionFact{{
			Kind: noteformat.SearchRegionVisible, Origin: noteformat.SearchRegionAuthored,
			Text: "Annual Revenue", MediaType: "text/plain", Range: sourceRange(7, 21),
		}},
	}
	owner := codeanchor.IntelOntologyNode{
		NodeID: "node:annual", NotePath: "annual.html", NodeKind: "NOTE", Title: "Annual Revenue",
	}

	plan := BuildRootEvidenceChunks(root, owner, embeddings.ProviderConfig{}, 1024, 7)

	require.Len(t, plan.Chunks, 1)
	require.Equal(t, int64(7), plan.Chunks[0].StartByte)
	require.Equal(t, int64(21), plan.Chunks[0].EndByte)
	require.Contains(t, plan.Texts[plan.Chunks[0].ChunkID], "Annual Revenue")
}

func TestBuildRootEvidenceChunksSplitsUTF8Deterministically(t *testing.T) {
	root := &ontology.RootDocumentSnapshot{
		NotePath: paths.NotePath("unicode.html"),
		SearchRegions: []noteformat.SearchRegionFact{{
			Kind: noteformat.SearchRegionSupplemental, Origin: noteformat.SearchRegionDerived,
			Text: "alpha🙂bravo🙂charlie", MediaType: "application/json",
		}},
	}
	owner := codeanchor.IntelOntologyNode{NodeID: "node:unicode", NotePath: "unicode.html", NodeKind: "NOTE", Title: "Unicode"}

	first := BuildRootEvidenceChunks(root, owner, embeddings.ProviderConfig{}, 7, 1)
	second := BuildRootEvidenceChunks(root, owner, embeddings.ProviderConfig{}, 7, 1)

	require.Equal(t, first, second)
	require.Greater(t, len(first.Chunks), 1)
	var parts []string
	for _, chunk := range first.Chunks {
		text := first.Texts[chunk.ChunkID]
		require.True(t, utf8.ValidString(text))
		require.NotContains(t, text, "�")
		require.Contains(t, text, "\n\n")
		parts = append(parts, text[strings.LastIndex(text, "\n\n")+2:])
	}
	require.Equal(t, "alpha🙂bravo🙂charlie", strings.Join(parts, ""))
}

func TestOntologyNodeSyncerUsesProviderRootEvidenceWithoutMarkdownStructure(t *testing.T) {
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	providerInfo := embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}
	root := &ontology.RootDocumentSnapshot{
		NotePath: paths.NotePath("prototype.html"), Format: "html", ContentHash: "source-hash",
		RawSource: []byte(`<script>rawOnlySecret()</script>`),
		SearchRegions: []noteformat.SearchRegionFact{{
			Kind: noteformat.SearchRegionVisible, Origin: noteformat.SearchRegionDerived,
			Text: "Projected prototype evidence", MediaType: "text/plain",
		}},
	}
	projection := &ontology.NodeProjection{
		Ref:          ontology.NodeRef{NotePath: "prototype.html", Kind: ontology.NodeKindNote, TypeName: "Project"},
		RootSnapshot: root, ResolvedType: "Project",
	}
	provider := &capturingRootProvider{inner: embeddings.NewDeterministicProvider(providerInfo)}
	syncer := OntologyNodeSyncer{
		Store: store, Provider: provider,
		ProviderInfo: providerInfo, Schema: &ontology.Schema{Types: map[string]*ontology.NoteType{}},
	}

	require.NoError(t, syncer.SyncProjections(context.Background(), projection))
	ownerID := ontology.OntologyNodeID(projection.Ref)
	chunks, err := store.IntelChunksByOwners(context.Background(), []string{ownerID})
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, GranularityOntologyNodeVisible, chunks[0].Granularity)
	vectors, err := store.EmbeddingsByChunkIDs(context.Background(), []string{chunks[0].ChunkID})
	require.NoError(t, err)
	require.Len(t, vectors[chunks[0].ChunkID], providerInfo.Dimensions)
	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.NotEmpty(t, provider.inputs)
	for _, input := range provider.inputs {
		require.Contains(t, input, "Projected prototype evidence")
		require.NotContains(t, input, "rawOnlySecret()")
	}
}

func sourceRange(start, end int) noteformat.OptionalSourceRange {
	return noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: start, EndByte: end}}
}
