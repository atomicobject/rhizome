package relevance

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestSpecificityRankerPrefersSpecificCodeSurface(t *testing.T) {
	base := &WeightedRanker{
		Weights: Weights{
			Semantic:    1.0,
			Specificity: 1.2,
		},
		MaxPerOwner: 2,
	}
	ranker := &SpecificityRanker{Base: base}

	results, err := ranker.Rank(context.Background(), search.QuerySpec{
		Text:   "how are notes embedded?",
		Intent: search.IntentSearch,
		Limits: search.Limits{Total: 2},
	}, []search.Candidate{
		{
			Handle: knowledge.FileHandle("pkg/generic/store.go"),
			Owner:  knowledge.FileHandle("pkg/generic/store.go"),
			Type:   "code",
			Path:   "pkg/generic/store.go",
			Evidence: []search.Evidence{
				{Type: "code_vector_similarity", RawScore: 0.95},
			},
		},
		{
			Handle: knowledge.FileHandle("pkg/search/semantic/note_syncer.go"),
			Owner:  knowledge.FileHandle("pkg/search/semantic/note_syncer.go"),
			Type:   "code",
			Path:   "pkg/search/semantic/note_syncer.go",
			Symbol: "PlanNoteEmbeddings",
			Evidence: []search.Evidence{
				{Type: "code_vector_similarity", RawScore: 0.55},
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "pkg/search/semantic/note_syncer.go", results[0].Path)
	require.Greater(t, querySpecificity(results[0].Evidence), 0.5)
}

func TestSpecificityRankerTagsSemanticEvidenceWithoutChangingRole(t *testing.T) {
	base := &WeightedRanker{
		Weights:     Weights{Semantic: 1, Specificity: 1},
		MaxPerOwner: 1,
	}
	ranker := &SpecificityRanker{Base: base}

	results, err := ranker.Rank(context.Background(), search.QuerySpec{
		Text: "how are notes embedded?",
	}, []search.Candidate{{
		Handle:      knowledge.CodeChunkHandle("a1", "signature_body", 0),
		Owner:       knowledge.AnchorHandle("a1"),
		Type:        "code",
		Path:        "pkg/search/semantic/note_syncer.go",
		Granularity: "signature_body",
		Evidence: []search.Evidence{
			{Type: "code_vector_similarity", RawScore: 0.7},
		},
	}})

	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "code", results[0].Type)
	require.Equal(t, "signature_body", results[0].Granularity)
	require.Greater(t, querySpecificity(results[0].Evidence), 0.0)
}

func querySpecificity(evidence []search.Evidence) float64 {
	best := 0.0
	for _, ev := range evidence {
		if ev.Type != "query_specificity" {
			continue
		}
		if score := search.EvidenceScore(ev); score > best {
			best = score
		}
	}
	return best
}
