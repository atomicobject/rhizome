package search_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/stretchr/testify/require"
)

type errRetriever struct {
	name string
	err  error
}

func (e errRetriever) Name() string { return e.name }
func (e errRetriever) Retrieve(_ context.Context, _ search.QuerySpec) ([]search.Candidate, error) {
	return nil, e.err
}

type okRetriever struct {
	name   string
	result []search.Candidate
}

func (o okRetriever) Name() string { return o.name }
func (o okRetriever) Retrieve(_ context.Context, _ search.QuerySpec) ([]search.Candidate, error) {
	return o.result, nil
}

type stubRanker struct{}

func (s stubRanker) Rank(_ context.Context, _ search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	out := make([]search.RankedResult, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, search.RankedResult{Candidate: c, FinalScore: 1})
	}
	return out, nil
}

func TestSearch_BroadIntentSoftFailsCompositeRetriever(t *testing.T) {
	svc := search.Service{
		Retrievers: []search.Retriever{
			&retrieval.CompositeRetriever{
				Label: "refs",
				Retrievers: []search.Retriever{
					errRetriever{name: "bad_refs", err: errors.New("store unavailable")},
					okRetriever{name: "good_refs", result: []search.Candidate{{
						Handle: knowledge.NoteHandle("notes/a.md"),
						Owner:  knowledge.NoteHandle("notes/a.md"),
						Type:   "note",
						Path:   "notes/a.md",
						Evidence: []search.Evidence{
							{Type: "doc_link", RawScore: 1.0},
						},
					}}},
				},
			},
		},
		Ranker: stubRanker{},
	}

	resp, err := svc.Search(context.Background(), search.QuerySpec{Text: "docs", Intent: search.IntentDocsForCode, Limits: search.Limits{Total: 5}})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.NotEmpty(t, resp.Warnings)
	require.Equal(t, "retriever_degraded", resp.Warnings[0].Code)
}

func TestSearch_PrecisionIntentStillFailsCompositeRetriever(t *testing.T) {
	svc := search.Service{
		Retrievers: []search.Retriever{
			&retrieval.CompositeRetriever{
				Label: "refs",
				Retrievers: []search.Retriever{
					errRetriever{name: "bad_refs", err: errors.New("store unavailable")},
				},
			},
		},
		Ranker: stubRanker{},
	}

	_, err := svc.Search(context.Background(), search.QuerySpec{Text: "symbol", Intent: search.IntentGoToDef, Limits: search.Limits{Total: 5}})
	require.Error(t, err)
}
