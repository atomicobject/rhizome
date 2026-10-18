package retrieval

import (
	"context"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

// VectorRetriever wraps pkg/search/semantic code+note chunk retrieval.
//
// It is the only retriever that calls embedding-backed semantic search at query
// time. Results retain canonical source and ontology-node identity.
type VectorRetriever struct {
	Semantic *semantic.Searcher
}

func (r *VectorRetriever) Name() string { return "vector" }

// Retrieve runs semantic similarity search against the configured note+code embedding indexes.
// It delegates to pkg/search/semantic.Searcher.Search, then converts results to Candidates
// with appropriate evidence types (note_vector_similarity, code_vector_similarity, etc.).
// Returns nil if spec.Text is empty or no semantic backend is configured.
func (r *VectorRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if strings.TrimSpace(spec.Text) == "" {
		return nil, nil
	}
	if r.Semantic == nil {
		return nil, fmt.Errorf("vector retriever missing semantic backend")
	}

	limit := spec.Limits.Total
	if limit <= 0 {
		limit = 25
	}
	results, err := r.Semantic.Search(ctx, semantic.SearchRequest{
		QueryText: spec.Text,
		Filters: semantic.SearchFilters{
			Types:        spec.Filters.Types,
			PathPrefixes: spec.Filters.PathPrefixes,
			NoteTypes:    spec.Filters.NoteTypes,
			ExactSymbols: spec.Filters.ExactSymbols,
			TestsOnly:    spec.Filters.TestsOnly,
			ExcludeTests: spec.Filters.ExcludeTests,
		},
		K: limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]search.Candidate, 0, len(results))
	for _, res := range results {
		candidate, err := candidateFromSemanticResult(res)
		if err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	return out, nil
}

func candidateFromSemanticResult(res semantic.Result) (search.Candidate, error) {
	h, err := knowledge.ParseHandle(res.Handle)
	if err != nil {
		return search.Candidate{}, fmt.Errorf("parse handle %q: %w", res.Handle, err)
	}
	evType := ""
	switch res.Type {
	case "note":
		evType = "note_vector_similarity"
	case "code":
		evType = "code_vector_similarity"
	case "anchor":
		evType = "anchor_vector_similarity"
	default:
		return search.Candidate{}, fmt.Errorf("unsupported semantic result type %q", res.Type)
	}
	evidence := []search.Evidence{{Type: evType, RawScore: res.Score, Source: "pkg/semantic"}}
	return search.Candidate{
		Handle:    h,
		Owner:     h.Owner(),
		Evidence:  evidence,
		Type:      res.Type,
		SourceRef: res.SourceRef,
	}, nil
}
