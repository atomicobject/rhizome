package search

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type diagnosticRanker struct{ err error }

func (r diagnosticRanker) Rank(context.Context, QuerySpec, []Candidate) ([]RankedResult, error) {
	return nil, r.err
}

func diagnosticCount(c *indexingperf.Collector, name string) int64 {
	for _, metric := range c.Snapshot().Counters {
		if metric.Name == name {
			return metric.Total
		}
	}
	return 0
}

func TestSearchMetricsRecordActualFailureWithoutPersistingErrorText(t *testing.T) {
	c := indexingperf.NewBounded()
	ctx := indexingperf.WithCollector(context.Background(), c)
	svc := Service{Retrievers: []Retriever{staticRetriever{name: "lexical"}}, Ranker: diagnosticRanker{err: errors.New("private failure body")}}
	_, err := svc.Search(ctx, QuerySpec{Text: "private query"})
	require.EqualError(t, err, "private failure body")
	require.Equal(t, int64(1), diagnosticCount(c, "search.outcome.error"))
	var total indexingperf.SpanSnapshot
	for _, span := range c.Snapshot().Spans {
		if span.Name == "search.search" {
			total = span
		}
	}
	require.Equal(t, "error", total.Status)
	require.Equal(t, int64(1), total.Count)
	encoded, err := json.Marshal(c.Snapshot())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private failure body")
	require.NotContains(t, string(encoded), "private query")
}

func TestSearchMetricsObserveRankFallbackAndRetainResults(t *testing.T) {
	c := indexingperf.NewBounded()
	ctx := indexingperf.WithCollector(context.Background(), c)
	candidate := Candidate{Handle: knowledge.NoteHandle("Notes/alpha.md"), Owner: knowledge.NoteHandle("Notes/alpha.md"), Type: "note", Path: "Notes/alpha.md", Title: "Alpha", Evidence: []Evidence{{Type: "note_title_exact", RawScore: 1}}}
	svc := Service{Retrievers: []Retriever{staticRetriever{name: "lexical", results: []Candidate{candidate}}}, Ranker: diagnosticRanker{err: context.DeadlineExceeded}}
	response, err := svc.Search(ctx, QuerySpec{Text: "alpha"})
	require.NoError(t, err)
	require.Len(t, response.Results, 1)
	require.Equal(t, candidate.Path, response.Results[0].Path)
	require.Equal(t, int64(1), diagnosticCount(c, indexingperf.DiagnosticFallbackPrefix+"rank.timeout"))
	require.Equal(t, int64(1), diagnosticCount(c, "search.retriever.lexical.outcome.ran"))
	require.Equal(t, int64(1), diagnosticCount(c, "search.retriever.lexical.results"))
	require.Equal(t, int64(1), diagnosticCount(c, "search.results"))
}

func TestSearchMetricsAreSeparateFromTimingEventText(t *testing.T) {
	c := indexingperf.NewBounded()
	timings := &Timings{}
	ctx := WithTimings(indexingperf.WithCollector(context.Background(), c), timings)
	svc := Service{Retrievers: []Retriever{staticRetriever{name: "lexical"}}, Ranker: stubRanker{}}
	_, err := svc.Search(ctx, QuerySpec{Text: "alpha"})
	require.NoError(t, err)
	before := c.Snapshot()
	require.NotEmpty(t, timings.Snapshot())
	require.Contains(t, timings.Render(), "Timings:")
	require.Equal(t, before, c.Snapshot())
	require.Equal(t, int64(1), diagnosticCount(c, "search.stage.search.outcome.ok"))
}
