package semantic

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func TestSearcher_SurveyNodesByType_UnionsTermsAndDedupesByNode(t *testing.T) {
	ctx := context.Background()
	prov := fixedEmbeddingProvider{
		"alpha": {1, 0, 0, 0},
		"beta":  {0, 1, 0, 0},
	}
	intelStore, err := semdb.Open(filepath.Join(t.TempDir(), "survey.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = intelStore.Close() })

	specA := readmodel.NodeRow{
		NodeID:                "node:spec-a",
		NotePath:              "docs/spec-a.md",
		NodeRefJSON:           `{"notePath":"docs/spec-a.md","nodeId":"spec-a","typeName":"TechnicalSpec","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "TechnicalSpec",
		Title:                 "Spec A",
		SourceLocator:         "docs/spec-a.md",
		StructuralFingerprint: "spec-a-fp",
		SchemaHash:            "schema",
		UpdatedAt:             1,
	}
	specB := readmodel.NodeRow{
		NodeID:                "node:spec-b",
		NotePath:              "docs/spec-b.md",
		NodeRefJSON:           `{"notePath":"docs/spec-b.md","nodeId":"spec-b","typeName":"TechnicalSpec","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "TechnicalSpec",
		Title:                 "Spec B",
		SourceLocator:         "docs/spec-b.md",
		StructuralFingerprint: "spec-b-fp",
		SchemaHash:            "schema",
		UpdatedAt:             1,
	}
	ref := readmodel.NodeRow{
		NodeID:                "node:reference",
		NotePath:              "docs/reference.md",
		NodeRefJSON:           `{"notePath":"docs/reference.md","nodeId":"reference","typeName":"ReferenceDoc","kind":"NOTE"}`,
		NodeKind:              "NOTE",
		TypeName:              "ReferenceDoc",
		Title:                 "Reference",
		SourceLocator:         "docs/reference.md",
		StructuralFingerprint: "ref-fp",
		SchemaHash:            "schema",
		UpdatedAt:             1,
	}
	require.NoError(t, intelStore.ReplaceOntologyNodes(ctx, []string{specA.NotePath, specB.NotePath, ref.NotePath}, []readmodel.NodeRow{specA, specB, ref}))
	require.NoError(t, intelStore.ReplaceIntelChunks(ctx, []string{specA.NodeID, specB.NodeID, ref.NodeID}, []codeanchor.IntelChunk{
		{ChunkID: "spec-a-body", OwnerID: specA.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: embeddings.GranularityOntologyNodeBody, Heading: "Spec A body", ContentHash: "spec-a-body"},
		{ChunkID: "spec-a-continuation", OwnerID: specA.NodeID, OwnerType: "ontology_node", Ord: 1, Granularity: embeddings.GranularityOntologyNodeBody, Heading: "Spec A continuation", ContentHash: "spec-a-continuation"},
		{ChunkID: "spec-b-body", OwnerID: specB.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: embeddings.GranularityOntologyNodeBody, Heading: "Spec B body", ContentHash: "spec-b-body"},
		{ChunkID: "reference-body", OwnerID: ref.NodeID, OwnerType: "ontology_node", Ord: 0, Granularity: embeddings.GranularityOntologyNodeBody, Heading: "Reference body", ContentHash: "reference-body"},
	}))
	require.NoError(t, intelStore.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"spec-a-body":         {0.7, 0.3, 0, 0},
		"spec-a-continuation": {1, 0, 0, 0},
		"spec-b-body":         {0, 1, 0, 0},
		"reference-body":      {1, 0, 0, 0},
	}))

	searcher := Searcher{NoteProvider: prov, IntelStore: intelStore}
	results, err := searcher.SurveyNodesByType(ctx, SurveyNodesByTypeRequest{
		QueryTerms: []string{"alpha", "beta"},
		TypeNames:  []string{"TechnicalSpec"},
		K:          10,
	})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.ElementsMatch(t, []string{"node:spec-a", "node:spec-b"}, []string{results[0].NodeID, results[1].NodeID})
	for _, result := range results {
		require.Equal(t, "note", result.Type)
		require.Equal(t, "TechnicalSpec", result.NodeRef.TypeName)
		require.InDelta(t, 1.0, result.Score, 0.001)
	}

	results, err = searcher.SurveyNodesByType(ctx, SurveyNodesByTypeRequest{
		QueryTerms: []string{"alpha", "beta"},
		TypeNames:  []string{"TechnicalSpec"},
		K:          1,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "node:spec-a", results[0].NodeID)
	require.Equal(t, "Spec A", results[0].Title)
}

func TestInterleaveScoredChunksByOwner_DiversifiesOntologyNodesByNote(t *testing.T) {
	scored := []semdb.ScoredChunk{
		{ChunkID: "a1", OwnerID: "node:a1", OwnerType: "ontology_node", Path: "docs/a.md", Score: 0.99},
		{ChunkID: "a2", OwnerID: "node:a2", OwnerType: "ontology_node", Path: "docs/a.md", Score: 0.98},
		{ChunkID: "a3", OwnerID: "node:a3", OwnerType: "ontology_node", Path: "docs/a.md", Score: 0.97},
		{ChunkID: "b1", OwnerID: "node:b1", OwnerType: "ontology_node", Path: "docs/b.md", Score: 0.96},
	}

	out := interleaveScoredChunksByOwner(scored, 2)

	require.Equal(t, []string{"a1", "b1"}, []string{out[0].ChunkID, out[1].ChunkID})
}
