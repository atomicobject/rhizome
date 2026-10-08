package search

import (
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestMergeEvidenceIdempotent(t *testing.T) {
	ev := Evidence{Type: "intel_fts_match", RawScore: 0.6, Source: "lexical", Details: map[string]string{"path": "docs/search.md"}}
	candidate := Candidate{Handle: knowledge.NoteHandle("docs/search.md"), Owner: knowledge.NoteHandle("docs/search.md"), Evidence: []Evidence{ev}}

	merged := MergeCandidate(candidate, Candidate{Handle: candidate.Handle, Owner: candidate.Owner, Evidence: []Evidence{ev}})

	require.Len(t, merged.Evidence, 1)
	require.Equal(t, 0.6, AggregateEvidenceScoresForRanking(merged.Evidence)[EvidenceChannelLexical])
}

func TestMergeEvidenceRetainsDistinctFacetFacts(t *testing.T) {
	base := Evidence{Type: "intel_fts_match", RawScore: 0.6, Source: "lexical", Details: map[string]string{"facet": "definition"}}
	other := Evidence{Type: "intel_fts_match", RawScore: 0.5, Source: "lexical", Details: map[string]string{"facet": "tests"}}
	merged := MergeCandidate(Candidate{Evidence: []Evidence{base}}, Candidate{Evidence: []Evidence{other}})
	require.Len(t, merged.Evidence, 2)
}

func TestMergeEvidencePermutationProducesIdenticalRetainedEvidence(t *testing.T) {
	evidence := []Evidence{
		{Type: "intel_fts_match", RawScore: 0.7, Source: "lexical", Details: map[string]string{"facet": "definition"}},
		{Type: "note_vector_similarity", RawScore: 0.6, Source: "vector", Details: map[string]string{"facet": "definition"}},
		{Type: "graph_proximity", RawScore: 0.5, Source: "graph", Details: map[string]string{"edge": "docs"}},
		{Type: "symbol_exact", RawScore: 1, Source: "definition", Details: map[string]string{"symbol": "Run"}},
	}
	permutations := [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {1, 3, 0, 2}, {2, 0, 3, 1}}
	var want []Evidence
	for i, order := range permutations {
		merged := Candidate{}
		for _, index := range order {
			merged = MergeCandidate(merged, Candidate{Evidence: []Evidence{evidence[index]}})
		}
		if i == 0 {
			want = merged.Evidence
			continue
		}
		require.Equal(t, want, merged.Evidence)
	}
}

func TestMergeEvidenceGroupingProducesIdenticalBoundedSummary(t *testing.T) {
	all := make([]Evidence, 0, 30)
	for i := 0; i < 30; i++ {
		all = append(all, Evidence{Type: fmt.Sprintf("signal_%02d", i%10), Score: float64(30-i) / 30, Source: fmt.Sprintf("lane-%d", i%3), Details: map[string]string{"fact": fmt.Sprintf("%02d", i)}})
	}
	sequential := Candidate{}
	for _, evidence := range all {
		sequential = MergeCandidate(sequential, Candidate{Evidence: []Evidence{evidence}})
	}
	left := Candidate{Evidence: append([]Evidence(nil), all[:10]...)}
	middle := Candidate{Evidence: append([]Evidence(nil), all[10:20]...)}
	right := Candidate{Evidence: append([]Evidence(nil), all[20:]...)}
	grouped := MergeCandidate(MergeCandidate(left, middle), right)
	require.Equal(t, sequential.Evidence, grouped.Evidence)
}

func TestMergeEvidenceSameFactRanksAndProvenanceAreDeterministic(t *testing.T) {
	a := Evidence{Type: "intel_fts_match", RawScore: 0.6, Source: "lane-b", Details: map[string]string{"path": "docs/search.md", "rank": "2"}}
	b := Evidence{Type: "intel_fts_match", RawScore: 0.6, Source: "lane-a", Details: map[string]string{"path": "docs/search.md", "rank": "5"}}
	c := Evidence{Type: "intel_fts_match", RawScore: 0.6, Source: "lane-c", Details: map[string]string{"path": "docs/search.md", "rank": "1"}}
	orders := [][]Evidence{{a, b, c}, {c, b, a}, {b, a, c}, {b, c, a}}
	var want []Evidence
	for i, order := range orders {
		merged := Candidate{}
		for _, evidence := range order {
			merged = MergeCandidate(merged, Candidate{Evidence: []Evidence{evidence}})
		}
		if i == 0 {
			want = merged.Evidence
			continue
		}
		require.Equal(t, want, merged.Evidence)
	}
	require.Len(t, want, 1)
	require.Equal(t, "lane-a,lane-b,lane-c", want[0].Source)
	require.Equal(t, "lane-a=5,lane-b=2,lane-c=1", want[0].Details["lane_ranks"])
	require.NotContains(t, want[0].Details, "rank")
}

func TestMergeCandidate_RetrieverRankMarkersDoNotEvictRankableEvidence(t *testing.T) {
	rankableTypes := []string{
		"note_vector_similarity",
		"code_vector_similarity",
		"anchor_vector_similarity",
		"intel_fts_match",
		"intel_doc_match",
		"note_title_match",
		"graph_proximity",
		"same_community",
		"doc_link",
		"code_ref",
		"recently_modified",
		"query_specificity",
	}
	existing := Candidate{}
	for i, typ := range rankableTypes {
		existing.Evidence = append(existing.Evidence, Evidence{
			Type:     typ,
			RawScore: 0.5 - float64(i)*0.01,
			Source:   fmt.Sprintf("real-%d", i),
		})
	}

	merged := MergeCandidate(existing, Candidate{Evidence: []Evidence{
		{Type: "retriever_rank:vector", Score: 1, Details: map[string]string{"rank": "0"}},
		{Type: "retriever_rank:graph", Score: 1, Details: map[string]string{"rank": "1"}},
	}})

	require.Len(t, merged.Evidence, maxEvidenceTotal)
	for _, ev := range merged.Evidence {
		require.NotContains(t, ev.Type, "retriever_rank:")
	}
}

func TestMergeCandidate_FillsOntologyNodeMetadata(t *testing.T) {
	ref := &ontology.NodeRef{
		NotePath:   "docs/spec.md",
		Fragment:   "^story-1",
		NodeID:     "story-1",
		TypeName:   "UserStory",
		Kind:       ontology.NodeKindEmbedded,
		Structural: "structure-1",
	}
	merged := MergeCandidate(Candidate{Type: "note"}, Candidate{
		NodeRef:       ref,
		NodeID:        "node:story",
		NodeRefJSON:   `{"nodeId":"story"}`,
		SourceLocator: "docs/spec.md#^story",
		NodeKind:      "EMBEDDED",
		NodeType:      "UserStory",
		ParentNodeID:  "node:stories",
	})

	require.Equal(t, &ontology.NodeRef{
		NotePath:   "docs/spec.md",
		Fragment:   "^story-1",
		NodeID:     "story-1",
		TypeName:   "UserStory",
		Kind:       ontology.NodeKindEmbedded,
		Structural: "structure-1",
	}, merged.NodeRef)
	require.Equal(t, "node:story", merged.NodeID)
	require.Equal(t, `{"nodeId":"story"}`, merged.NodeRefJSON)
	require.Equal(t, "docs/spec.md#^story", merged.SourceLocator)
	require.Equal(t, "EMBEDDED", merged.NodeKind)
	require.Equal(t, "UserStory", merged.NodeType)
	require.Equal(t, "node:stories", merged.ParentNodeID)
}

func TestMergeEvidenceCapRetainsDeterministicTiedFact(t *testing.T) {
	all := make([]Evidence, 0, 14)
	for i := 0; i < 11; i++ {
		all = append(all, Evidence{Type: fmt.Sprintf("signal_%02d", i), Score: 0.8})
	}
	for _, fact := range []string{"a", "b", "c"} {
		all = append(all, Evidence{Type: "tied_signal", Score: 0.5, Details: map[string]string{"fact": fact}})
	}
	for i := 0; i < 100; i++ {
		merged := MergeCandidate(Candidate{Evidence: all}, Candidate{})
		require.Len(t, merged.Evidence, maxEvidenceTotal)
		require.Equal(t, "a", merged.Evidence[maxEvidenceTotal-1].Details["fact"])
	}
}

func TestCoalesceNoteCandidatesMergesOnlyWholeNoteIdentity(t *testing.T) {
	path := "notes/plan.md"
	owner := knowledge.NoteHandle(path)
	rootChunk := Candidate{
		Handle: knowledge.NodeChunkHandle("root", path, "node_body", 0), Owner: owner, Type: "note", Path: path,
		NodeRef:  &ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote},
		Evidence: []Evidence{{Type: "note_vector_similarity", RawScore: 0.6}, {Type: "query_specificity", RawScore: 0.9, Details: map[string]string{"matched": "plan"}}},
	}
	titleMatch := Candidate{
		Handle: owner, Owner: owner, Type: "note", Path: path, ChunkIndex: -1,
		Evidence: []Evidence{{Type: "note_title_match", RawScore: 1}, {Type: "query_specificity", RawScore: 0.5, Details: map[string]string{"matched": "plan,review"}}},
	}
	// An embedded node keeps its own canonical identity, so facets that rank
	// it see the same source whichever chunk is strongest.
	embedded := Candidate{
		Handle: knowledge.NodeChunkHandle("decision", path, "node_body", 0), Owner: owner, Type: "note", Path: path,
		NodeRef:  &ontology.NodeRef{NotePath: path, NodeID: "decision", Kind: "EMBEDDED"},
		Evidence: []Evidence{{Type: "note_vector_similarity", RawScore: 0.8}},
	}
	section := Candidate{Handle: knowledge.FileHandle(path), Owner: owner, Type: "doc_section", Path: path, Evidence: []Evidence{{Type: "intel_doc_match", RawScore: 0.9}}}

	orders := [][]Candidate{
		{rootChunk, titleMatch, embedded, section},
		{section, embedded, titleMatch, rootChunk},
		{titleMatch, section, rootChunk, embedded},
	}
	var first []Candidate
	for i, order := range orders {
		got := coalesceNoteCandidates(order)
		require.Len(t, got, 3, "order %d", i)
		byHandle := map[string]Candidate{}
		for _, c := range got {
			byHandle[c.Handle.String()] = c
		}
		merged, ok := byHandle[rootChunk.Handle.String()]
		require.True(t, ok, "the strongest whole-note member is the base (order %d)", i)
		types := map[string]int{}
		for _, ev := range merged.Evidence {
			types[ev.Type]++
		}
		require.Equal(t, 1, types["note_title_match"])
		require.Equal(t, 1, types["query_specificity"], "only the base member's specificity survives (order %d)", i)
		require.Contains(t, byHandle, embedded.Handle.String())
		require.Contains(t, byHandle, section.Handle.String())
		if first == nil {
			first = []Candidate{merged}
			continue
		}
		require.Equal(t, first[0].Evidence, merged.Evidence, "order %d", i)
	}
}
