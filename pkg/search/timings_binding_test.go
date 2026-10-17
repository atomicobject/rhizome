package search

import (
	"context"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

func TestTimingBindingsFollowContextWithoutChangingSharedDisplay(t *testing.T) {
	display := &Timings{}
	base := WithTimings(context.Background(), display)
	first, second := indexingperf.New(), indexingperf.New()
	firstContext := indexingperf.WithCollector(base, first)
	secondContext := WithTimings(indexingperf.WithCollector(base, second), TimingsFromContext(firstContext))
	// Reading/binding the second context must not change the first or base.
	TimingsFromContext(firstContext).Add(TimingEvent{Name: "rank", Started: time.Now(), Status: "ok"})
	TimingsFromContext(secondContext).Add(TimingEvent{Name: "pack", Started: time.Now(), Status: "ok"})
	TimingsFromContext(base).Add(TimingEvent{Name: "retrieve", Started: time.Now(), Status: "ok"})
	require.Equal(t, int64(1), diagnosticCount(first, "search.stage.rank.outcome.ok"))
	require.Zero(t, diagnosticCount(first, "search.stage.pack.outcome.ok"))
	require.Equal(t, int64(1), diagnosticCount(second, "search.stage.pack.outcome.ok"))
	require.Zero(t, diagnosticCount(second, "search.stage.rank.outcome.ok"))
	require.Zero(t, diagnosticCount(first, "search.stage.retrieve.outcome.ok"))
	require.Zero(t, diagnosticCount(second, "search.stage.retrieve.outcome.ok"))
	require.Len(t, display.Snapshot(), 3)
	require.Len(t, TimingsFromContext(firstContext).Snapshot(), 3)
	require.Contains(t, display.Render(), "retrieve")
}

func TestPackingAfterSearchKeepsLateCollectorAndSharedTimingDisplay(t *testing.T) {
	display := &Timings{}
	base := WithTimings(context.Background(), display)
	svc := Service{Retrievers: []Retriever{staticRetriever{name: "note_lexical"}}, Ranker: stubRanker{}}
	response, err := svc.Search(base, QuerySpec{Text: "query"})
	require.NoError(t, err)
	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(base, collector)
	_, err = PackRankedResults(ctx, expiredPacker{}, QuerySpec{Budget: Budget{Chars: 100}}, response.Results)
	require.NoError(t, err)
	require.Equal(t, int64(1), diagnosticCount(collector, indexingperf.DiagnosticFallbackPrefix+"pack.timeout"))
	require.Equal(t, int64(1), diagnosticCount(collector, "search.stage.pack.outcome.timeout"))
	require.Equal(t, int64(1), diagnosticCount(collector, "search.stage.pack_fallback.outcome.partial"))
	require.Zero(t, diagnosticCount(collector, "search.stage.search.outcome.ok"))
	require.Contains(t, display.Render(), "search")
	require.Contains(t, display.Render(), "pack_fallback")
}
