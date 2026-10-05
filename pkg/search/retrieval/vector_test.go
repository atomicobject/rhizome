package retrieval

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

func TestVectorRetrieverReturnsPrimaryCodeChunkEvidence(t *testing.T) {
	ctx := context.Background()
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := codeanchorsqlite.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "anchor1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/search/service.go",
		Symbol:      "Search",
		FQN:         "search.Service.Search",
		Fingerprint: "fp1",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, anchor.Path, []codeanchor.IntelAnchor{anchor}, nil, nil))
	chunkText := "func (s Service) Search query planner semantic retrieval"
	chunkID := codeanchor.IntelChunkID(anchor.AnchorID, 0, "signature_doc")
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchor.AnchorID}, []codeanchor.IntelChunk{{
		ChunkID: chunkID, OwnerID: anchor.AnchorID, OwnerType: "anchor", Ord: 0,
		Granularity: "signature_doc", Breadcrumb: anchor.Path, Heading: anchor.Symbol, ContentHash: "primary-hash",
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		chunkID: embedForRetrievalTest(t, provider, chunkText),
	}))

	retriever := &VectorRetriever{Semantic: &semantic.Searcher{CodeProvider: provider, NoteProvider: provider, IntelStore: store}}
	candidates, err := retriever.Retrieve(ctx, search.QuerySpec{Text: chunkText, Limits: search.Limits{Total: 1}})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, anchor.AnchorID, candidates[0].AnchorID)
	require.Equal(t, anchor.Path, candidates[0].Path)
	require.Equal(t, "signature_doc", candidates[0].Granularity)
	requireEvidenceType(t, candidates[0].Evidence, "code_vector_similarity")
}

func TestVectorRetrieverPreservesOntologyNodeRefFromPrimaryChunk(t *testing.T) {
	ctx := context.Background()
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := codeanchorsqlite.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	node := codeanchor.IntelOntologyNode{
		NodeID:        "node:story",
		NotePath:      "docs/spec.md",
		NodeRefJSON:   `{"notePath":"docs/spec.md","fragment":"^story","nodeId":"story","typeName":"UserStory","kind":"EMBEDDED","structuralFingerprint":"structure-1"}`,
		NodeKind:      "EMBEDDED",
		TypeName:      "UserStory",
		Title:         "Faster checkout",
		SourceLocator: "docs/spec.md#^story",
		UpdatedAt:     1,
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []codeanchor.IntelOntologyNode{node}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID: "node-body", OwnerID: node.NodeID, OwnerType: semantic.OntologyNodeOwnerType, Ord: 0,
		Granularity: semantic.GranularityOntologyNodeBody, Breadcrumb: "docs/spec.md > Faster checkout", Heading: node.Title, ContentHash: "body-hash", UpdatedAt: 1,
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"node-body": embedForRetrievalTest(t, provider, "faster checkout user story"),
	}))

	retriever := &VectorRetriever{Semantic: &semantic.Searcher{NoteProvider: provider, IntelStore: store}}
	candidates, err := retriever.Retrieve(ctx, search.QuerySpec{Text: "faster checkout user story", Limits: search.Limits{Total: 5}})
	require.NoError(t, err)
	require.NotEmpty(t, candidates)
	require.Equal(t, node.NodeID, candidates[0].NodeID)
	require.Equal(t, semantic.GranularityOntologyNodeBody, candidates[0].Granularity)
	require.NotNil(t, candidates[0].NodeRef)
	require.Equal(t, node.NotePath, candidates[0].NodeRef.NotePath)
	require.Equal(t, "story", candidates[0].NodeRef.NodeID)
	require.Equal(t, ontology.NodeKindEmbedded, candidates[0].NodeRef.Kind)
	requireEvidenceType(t, candidates[0].Evidence, "note_vector_similarity")

	section := codeanchor.IntelOntologyNode{
		NodeID: "node:fallback-section", NotePath: "notes/research.md",
		NodeRefJSON: `{"notePath":"notes/research.md","fragment":"findings-18","nodeId":"fallback-section","typeName":"_FallbackSection","kind":"SECTION","structuralFingerprint":"structure-1"}`,
		NodeKind:    "SECTION", TypeName: "_FallbackSection", ParentNodeID: "node:fallback-note",
		Title: "Findings", SourceLocator: "notes/research.md#findings-18", UpdatedAt: 1,
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{section.NotePath}, []codeanchor.IntelOntologyNode{section}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{section.NodeID}, []codeanchor.IntelChunk{{
		ChunkID: "section-body", OwnerID: section.NodeID, OwnerType: semantic.OntologyNodeOwnerType, Ord: 0,
		Granularity: semantic.GranularityOntologyNodeBody, Heading: section.Title, ContentHash: "section-hash", UpdatedAt: 1,
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"section-body": embedForRetrievalTest(t, provider, "research findings eighteen"),
	}))
	sections, err := retriever.Retrieve(ctx, search.QuerySpec{Text: "research findings eighteen", Limits: search.Limits{Total: 5}})
	require.NoError(t, err)
	var found *search.Candidate
	for i := range sections {
		if sections[i].NodeID == section.NodeID {
			found = &sections[i]
			break
		}
	}
	require.NotNil(t, found)
	require.Equal(t, knowledge.KindNodeChunk, found.Handle.Kind)
	require.Equal(t, section.SourceLocator, found.SourceLocator)
	require.Equal(t, section.ParentNodeID, found.ParentNodeID)
	require.Equal(t, section.NodeKind, found.NodeKind)
	require.NotNil(t, found.NodeRef)
	require.Equal(t, ontology.NodeRef{NotePath: section.NotePath, Fragment: "findings-18", NodeID: "fallback-section", TypeName: "_FallbackSection", Kind: ontology.NodeKindSection, Structural: "structure-1"}, *found.NodeRef)
	requireEvidenceType(t, found.Evidence, "note_vector_similarity")
}

func embedForRetrievalTest(t *testing.T, provider embeddings.Provider, text string) embeddings.Embedding {
	t.Helper()
	vectors, err := provider.EmbedTexts(context.Background(), []string{text})
	require.NoError(t, err)
	require.Len(t, vectors, 1)
	return vectors[0]
}

func requireEvidenceType(t *testing.T, evidence []search.Evidence, typ string) {
	t.Helper()
	for _, item := range evidence {
		if item.Type == typ {
			return
		}
	}
	t.Fatalf("expected evidence %s in %#v", typ, evidence)
}
