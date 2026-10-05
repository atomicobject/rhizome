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
