package search_test

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/relevance"
	"github.com/stretchr/testify/require"
)

func TestArchetypeRegression_MixedVaultDocsForCodeKeepsDocsAndCodeNearTop(t *testing.T) {
	spec := search.QuerySpec{Intent: search.IntentDocsForCode, Limits: search.Limits{Total: 4}}
	ranker := relevance.WeightedRanker{
		Weights: relevance.Weights{
			Semantic:           0.5,
			Lexical:            0.9,
			Graph:              0.15,
			Refs:               1.2,
			OntologyStructural: 0.45,
			OntologyAmbient:    0.15,
			SeedLocality:       0.55,
		},
		MaxPerOwner: 2,
	}

	results, err := ranker.Rank(context.Background(), spec, []search.Candidate{
		{
			Handle: knowledge.NoteHandle("notes/architecture.md"),
			Owner:  knowledge.NoteHandle("notes/architecture.md"),
			Type:   "note",
			Path:   "notes/architecture.md",
			Evidence: []search.Evidence{
				{Type: "doc_link", RawScore: 1.0},
				{Type: "module_doc", RawScore: 0.9},
			},
		},
		{
			Handle: knowledge.FileHandle("pkg/search/service.go"),
			Owner:  knowledge.FileHandle("pkg/search/service.go"),
			Type:   "code",
			Path:   "pkg/search/service.go",
			Evidence: []search.Evidence{
				{Type: "code_vector_similarity", RawScore: 0.8},
				{Type: "code_ref", RawScore: 0.6},
			},
		},
		{
			Handle: knowledge.NoteHandle("notes/glossary.md"),
			Owner:  knowledge.NoteHandle("notes/glossary.md"),
			Type:   "note",
			Path:   "notes/glossary.md",
			Evidence: []search.Evidence{
				{Type: "intel_fts_match", RawScore: 0.35},
			},
		},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 2)
	require.Equal(t, "note", results[0].Type)
	require.Equal(t, "code", results[1].Type)
}

func TestArchetypeRegression_OntologyHeavyPrefersStructuralOverAmbient(t *testing.T) {
	spec := search.QuerySpec{Intent: search.IntentRelatedToSeed, Limits: search.Limits{Total: 3}}
	ranker := relevance.WeightedRanker{
		Weights: relevance.Weights{
			Graph:              0.8,
			Refs:               0.5,
			OntologyStructural: 1.0,
			OntologyAmbient:    0.6,
		},
		MaxPerOwner: 1,
	}
	results, err := ranker.Rank(context.Background(), spec, []search.Candidate{
		{
			Handle:   knowledge.NoteHandle("notes/decision-a.md"),
			Owner:    knowledge.NoteHandle("notes/decision-a.md"),
			Type:     "note",
			Path:     "notes/decision-a.md",
			Evidence: []search.Evidence{{Type: "ontology_relation_structural", RawScore: 1.25}},
		},
		{
			Handle:   knowledge.NoteHandle("notes/runbook-a.md"),
			Owner:    knowledge.NoteHandle("notes/runbook-a.md"),
			Type:     "note",
			Path:     "notes/runbook-a.md",
			Evidence: []search.Evidence{{Type: "ontology_relation_ambient", RawScore: 1.05}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "notes/decision-a.md", results[0].Path)
}

type fixedRetriever struct {
	name       string
	candidates []search.Candidate
}

func (r fixedRetriever) Name() string { return r.name }

func (r fixedRetriever) Retrieve(context.Context, search.QuerySpec) ([]search.Candidate, error) {
	return r.candidates, nil
}

// A note found by the vector lane and the title lane is one source, so its
// lanes add up. Before whole-note candidates were coalesced before ranking,
// each lane's row was scored alone and a note with a slightly higher cosine
// and no title match led (dogfood query "Innovation teams", 2026-10-08).
func TestArchetypeRegression_WholeNoteLaneAgreementOutranksOneLane(t *testing.T) {
	target := "Projects/Opportunity - Volunteer drivers and rural routes.md"
	rival := "Notes/Drivers.md"
	wholeNoteChunk := func(path string, cosine float64) search.Candidate {
		return search.Candidate{
			Handle:      knowledge.NodeChunkHandle("node-"+path, path, "node_body", 0),
			Owner:       knowledge.NoteHandle(path),
			Type:        "note",
			Path:        path,
			Granularity: "node_body",
			Evidence:    []search.Evidence{{Type: "note_vector_similarity", RawScore: cosine}},
		}
	}
	titleMatch := search.Candidate{
		Handle:     knowledge.NoteHandle(target),
		Owner:      knowledge.NoteHandle(target),
		Type:       "note",
		Path:       target,
		ChunkIndex: -1,
		Evidence:   []search.Evidence{{Type: "note_title_match", RawScore: 1}},
	}
	service := search.Service{
		Retrievers: []search.Retriever{
			fixedRetriever{name: "vector", candidates: []search.Candidate{wholeNoteChunk(rival, 0.66), wholeNoteChunk(target, 0.62)}},
			fixedRetriever{name: "note_lexical", candidates: []search.Candidate{titleMatch}},
		},
		Ranker: &relevance.WeightedRanker{Weights: relevance.DefaultWeights(), MaxPerOwner: 3},
	}

	response, err := service.Search(context.Background(), search.QuerySpec{Text: "volunteer drivers", Limits: search.Limits{Total: 10}})
	require.NoError(t, err)
	require.Len(t, response.Results, 2)
	require.Equal(t, target, response.Results[0].Path)
	require.Equal(t, "node_body", response.Results[0].Granularity, "the best whole-note chunk stays the displayed node")
	var types []string
	for _, ev := range response.Results[0].Evidence {
		types = append(types, ev.Type)
	}
	require.Contains(t, types, "note_vector_similarity")
	require.Contains(t, types, "note_title_match")
}
