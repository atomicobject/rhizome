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
	first, second := indexingperf.NewBounded(), indexingperf.NewBounded()
	firstContext := indexingperf.WithCollector(base, first)
	secondContext := WithTimings(indexingperf.WithCollector(base, second), TimingsFromContext(firstContext))
	// Reading/binding the second context must not change the first or base.
	TimingsFromContext(firstContext).Add(TimingEvent{Name: "first", Started: time.Now(), Status: "ok"})
	TimingsFromContext(secondContext).Add(TimingEvent{Name: "second", Started: time.Now(), Status: "ok"})
	TimingsFromContext(base).Add(TimingEvent{Name: "display_only", Started: time.Now(), Status: "ok"})
	require.Equal(t, int64(1), diagnosticCount(first, "search.stage.first.outcome.ok"))
	require.Zero(t, diagnosticCount(first, "search.stage.second.outcome.ok"))
	require.Equal(t, int64(1), diagnosticCount(second, "search.stage.second.outcome.ok"))
	require.Zero(t, diagnosticCount(second, "search.stage.first.outcome.ok"))
	require.Zero(t, diagnosticCount(first, "search.stage.display_only.outcome.ok"))
	require.Zero(t, diagnosticCount(second, "search.stage.display_only.outcome.ok"))
	require.Len(t, display.Snapshot(), 3)
	require.Len(t, TimingsFromContext(firstContext).Snapshot(), 3)
	require.Contains(t, display.Render(), "display_only")
}

func TestPackingAfterSearchKeepsLateCollectorAndSharedTimingDisplay(t *testing.T) {
	display := &Timings{}
	base := WithTimings(context.Background(), display)
	svc := Service{Retrievers: []Retriever{staticRetriever{name: "lexical"}}, Ranker: stubRanker{}}
	response, err := svc.Search(base, QuerySpec{Text: "query"})
	require.NoError(t, err)
	collector := indexingperf.NewBounded()
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
