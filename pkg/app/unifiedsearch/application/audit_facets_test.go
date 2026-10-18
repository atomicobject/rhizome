package application

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestMergeRankedResultsUsesSharedFacetAggregation(t *testing.T) {
	shared := knowledge.FileHandle("pkg/search/semantic/embedder.go")
	merged := AggregateFacets([]search.Response{
		{Query: search.QuerySpec{Text: "embedding", Intent: search.IntentSearch}, Results: []search.RankedResult{
			{Candidate: search.Candidate{SourceRef: search.SourceRef{Path: "pkg/other.go"}, Handle: knowledge.FileHandle("pkg/other.go"), Type: "code"}, FinalScore: 0.91},
			{Candidate: search.Candidate{SourceRef: search.SourceRef{Path: "pkg/search/semantic/embedder.go"}, Handle: shared, Type: "code"}, FinalScore: 0.89},
		}},
		{Query: search.QuerySpec{Text: "memo", Intent: search.IntentSearch}, Results: []search.RankedResult{
			{Candidate: search.Candidate{SourceRef: search.SourceRef{Path: "pkg/search/semantic/embedder.go", Symbol: "QueryEmbeddings"}, Handle: shared, Type: "code"}, FinalScore: 0.88},
		}},
	}, 10)

	require.Len(t, merged, 2)
	require.Equal(t, "pkg/search/semantic/embedder.go", merged[0].Path)
	require.Equal(t, "pkg/other.go", merged[1].Path)
	require.Equal(t, "QueryEmbeddings", merged[0].Symbol)
	require.Equal(t, 0.89, merged[0].FinalScore)
}

func TestMergeRankedResultsPreservesEvidenceAndBestEngineScore(t *testing.T) {
	shared := knowledge.FileHandle("pkg/search/service.go")
	merged := AggregateFacets([]search.Response{
		{Results: []search.RankedResult{{
			Candidate: search.Candidate{
				SourceRef: search.SourceRef{Path: "pkg/search/service.go"},
				Handle:    shared,
				Type:      "code",
				Evidence: []search.Evidence{
					{Type: "intel_fts_match", RawScore: 0.8, Source: "first"},
				},
			},
			FinalScore: 0.6,
		}}},
		{Results: []search.RankedResult{{
			Candidate: search.Candidate{
				SourceRef: search.SourceRef{Path: "pkg/search/service.go"},
				Handle:    shared,
				Type:      "code",
				Evidence: []search.Evidence{
					{Type: "code_vector_similarity", RawScore: 0.9, Source: "second"},
				},
			},
			FinalScore: 0.9,
		}}},
	}, 10)

	require.Len(t, merged, 1)
	require.Equal(t, 0.9, merged[0].FinalScore)
	requireAuditEvidenceSource(t, merged[0].Evidence, "first")
	requireAuditEvidenceSource(t, merged[0].Evidence, "second")
}

func TestMergeRankedResultsMergesNodeBodyAndSectionHitsByPath(t *testing.T) {
	merged := AggregateFacets([]search.Response{
		{Results: []search.RankedResult{{
			Candidate: search.Candidate{
				SourceRef: search.SourceRef{
					Path:        "docs/spec.md",
					Granularity: "node_body",
					NodeRef:     &ontology.NodeRef{NotePath: "docs/spec.md", TypeName: "TechnicalSpec", Kind: ontology.NodeKindNote},
				},
				Handle: knowledge.NodeChunkHandle("spec-node", "docs/spec.md", "node_body", 0),
				Type:   "note",
			},
			FinalScore: 0.7,
		}}},
		{Results: []search.RankedResult{{
			Candidate: search.Candidate{
				SourceRef: search.SourceRef{
					Path:  "docs/spec.md",
					Title: "Spec",
				},
				Handle: knowledge.NoteChunkHandle("docs/spec.md", 0),
				Type:   "note",
			},
			FinalScore: 0.9,
		}}},
	}, 10)

	require.Len(t, merged, 1)
	require.Equal(t, "docs/spec.md", merged[0].Path)
	require.NotNil(t, merged[0].NodeRef)
}

func requireAuditEvidenceSource(t *testing.T, evidence []search.Evidence, source string) {
	t.Helper()
	for _, ev := range evidence {
		if ev.Source == source {
			return
		}
	}
	require.Failf(t, "missing evidence source", "source %q not found in %#v", source, evidence)
}
