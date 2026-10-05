package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type observingDiagnosticRanker struct {
	collector  *indexingperf.Collector
	err        error
	panicValue any
	forceSlow  bool
}

type synchronizedDiagnosticRanker struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (r synchronizedDiagnosticRanker) Rank(ctx context.Context, spec QuerySpec, candidates []Candidate) ([]RankedResult, error) {
	makeDiagnosticSearchSlow(ctx)
	r.started <- struct{}{}
	select {
	case <-r.release:
		return stubRanker{}.Rank(ctx, spec, candidates)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestConcurrentSearchReportsIsolateCollectorsWithSharedTimingDisplay(t *testing.T) {
	ctx, root, _, _ := diagnosticSearchContext(t)
	timings := &Timings{}
	ctx = WithTimings(ctx, timings)
	started, release := make(chan struct{}, 2), make(chan struct{})
	svc := Service{
		Retrievers: []Retriever{staticRetriever{name: "lexical"}},
		Ranker:     synchronizedDiagnosticRanker{started: started, release: release},
	}
	var workers sync.WaitGroup
	searchErrors := make(chan error, 2)
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := svc.Search(ctx, QuerySpec{Text: "PRIVATE_QUERY"})
			searchErrors <- err
		}()
	}
	// Both facets must bind their collectors before either records rank/total.
	// This exposes shared mutable rebinding regardless of goroutine order.
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			workers.Wait()
			t.Fatal("search facets did not reach the rank barrier")
		}
	}
	close(release)
	workers.Wait()
	for range 2 {
		require.NoError(t, <-searchErrors)
	}
	reports, err := diagnostics.ReadReports(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 2)
	for _, report := range reports.Reports {
		var snapshot indexingperf.Snapshot
		require.NoError(t, json.Unmarshal(report.Metrics, &snapshot))
		require.Equal(t, "search_operation", report.Attributes["metrics_scope"])
		require.Equal(t, int64(1), snapshotDiagnosticCount(snapshot, "search.stage.rank.outcome.ok"))
		require.Equal(t, int64(1), snapshotDiagnosticCount(snapshot, "search.stage.search.outcome.ok"))
		require.Equal(t, int64(1), snapshotDiagnosticCount(snapshot, "search.outcome.ok"))
	}
	var displayedSearches int
	for _, event := range timings.Snapshot() {
		if event.Name == "search" {
			displayedSearches++
		}
	}
	require.Equal(t, 2, displayedSearches)
	require.Contains(t, timings.Render(), "Timings:")
}

func (r *observingDiagnosticRanker) Rank(ctx context.Context, spec QuerySpec, candidates []Candidate) ([]RankedResult, error) {
	r.collector = indexingperf.FromContext(ctx)
	if r.forceSlow {
		makeDiagnosticSearchSlow(ctx)
	}
	if r.panicValue != nil {
		panic(r.panicValue)
	}
	if r.err != nil {
		return nil, r.err
	}
	return stubRanker{}.Rank(ctx, spec, candidates)
}

func makeDiagnosticSearchSlow(ctx context.Context) {
	operation := ctx.Value(searchOperationKey{}).(*searchOperation)
	operation.started = operation.started.Add(-slowSearchReportThreshold)
	operation.operation.StartedAt = operation.operation.StartedAt.Add(-slowSearchReportThreshold)
}

func diagnosticSearchContext(t *testing.T) (context.Context, string, *diagnostics.Recorder, diagnostics.Operation) {
	t.Helper()
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: io.Discard})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	parent := diagnostics.NewOperation("tool", "semantic_query")
	ctx := diagnostics.WithOperation(diagnostics.WithRecorder(context.Background(), recorder), parent)
	return ctx, root, recorder, parent
}

func readSearchReport(t *testing.T, root string) (diagnostics.Report, indexingperf.Snapshot) {
	t.Helper()
	reports, err := diagnostics.ReadReports(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
	report := reports.Reports[0]
	require.Equal(t, "search", report.Kind)
	var snapshot indexingperf.Snapshot
	require.NoError(t, json.Unmarshal(report.Metrics, &snapshot))
	return report, snapshot
}

func snapshotDiagnosticCount(snapshot indexingperf.Snapshot, name string) int64 {
	for _, counter := range snapshot.Counters {
		if counter.Name == name {
			return counter.Total
		}
	}
	return 0
}

func TestSearchRecorderCollectsFallbackWithoutTimingFlag(t *testing.T) {
	ctx, root, recorder, parent := diagnosticSearchContext(t)
	candidate := Candidate{Handle: knowledge.NoteHandle("Notes/PRIVATE_RESULT.md"), Owner: knowledge.NoteHandle("Notes/PRIVATE_RESULT.md"), Type: "note", Path: "Notes/PRIVATE_RESULT.md", Title: "PRIVATE_TITLE", Evidence: []Evidence{{Type: "note_title_exact", RawScore: 1}}}
	ranker := &observingDiagnosticRanker{err: context.DeadlineExceeded}
	svc := Service{Retrievers: []Retriever{staticRetriever{name: "lexical", results: []Candidate{candidate}}}, Ranker: ranker}
	response, err := svc.Search(ctx, QuerySpec{Text: "PRIVATE_QUERY", Seeds: []knowledge.Handle{knowledge.NoteHandle("Notes/PRIVATE_SEED.md")}})
	require.NoError(t, err)
	require.Len(t, response.Results, 1)
	require.Nil(t, TimingsFromContext(ctx))
	require.NotNil(t, ranker.collector)
	report, snapshot := readSearchReport(t, root)
	require.NotEqual(t, parent.ID, report.OperationID)
	require.Equal(t, parent.ID, report.ParentOperationID)
	require.Equal(t, parent.TraceID, report.TraceID)
	require.Equal(t, "success", report.Status)
	require.Equal(t, "fallback_used", report.ReasonCode)
	require.Equal(t, "partial", report.Attributes["outcome"])
	require.Equal(t, "search_operation", report.Attributes["metrics_scope"])
	require.Greater(t, snapshot.Limits.Series, 0)
	require.Equal(t, int64(1), snapshotDiagnosticCount(snapshot, indexingperf.DiagnosticFallbackPrefix+"rank.timeout"))
	require.Equal(t, int64(1), snapshotDiagnosticCount(snapshot, "search.outcome.partial"))
	require.Equal(t, int64(1), snapshotDiagnosticCount(snapshot, "search.results"))
	require.NotEmpty(t, snapshot.Spans)
	require.NoError(t, recorder.Close())
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 2)
	require.Equal(t, "search.started", events.Events[0].Name)
	require.Equal(t, "search.finished", events.Events[1].Name)
	require.Equal(t, report.OperationID, events.Events[0].OperationID)
	encoded, err := json.Marshal([]any{report, events})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE")
}

func TestSearchRecorderReusesContextCollectorAndLateTimingBridge(t *testing.T) {
	for _, existingCollector := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existingCollector], func(t *testing.T) {
			ctx, root, _, _ := diagnosticSearchContext(t)
			timings := &Timings{}
			ctx = WithTimings(ctx, timings)
			collector := indexingperf.NewBounded()
			if existingCollector {
				ctx = indexingperf.WithCollector(ctx, collector)
				indexingperf.AddCount(ctx, "parent.metric", 7)
			}
			ranker := &observingDiagnosticRanker{forceSlow: true}
			svc := Service{Retrievers: []Retriever{staticRetriever{name: "lexical"}}, Ranker: ranker}
			_, err := svc.Search(ctx, QuerySpec{Text: "PRIVATE_QUERY"})
			require.NoError(t, err)
			report, snapshot := readSearchReport(t, root)
			if existingCollector {
				require.Same(t, collector, ranker.collector)
				require.Equal(t, "context", report.Attributes["metrics_scope"])
				require.Equal(t, int64(7), snapshotDiagnosticCount(snapshot, "parent.metric"))
			} else {
				require.Equal(t, "search_operation", report.Attributes["metrics_scope"])
			}
			require.Equal(t, int64(1), snapshotDiagnosticCount(snapshot, "search.stage.search.outcome.ok"))
			require.Equal(t, "success", report.Status)
			require.NotEmpty(t, timings.Snapshot())
			require.Contains(t, timings.Render(), "Timings:")
			require.Contains(t, timings.Render(), "search")
		})
	}
}

func TestSearchReportSelectionRetainsEventsAndMetrics(t *testing.T) {
	for _, test := range []struct {
		name     string
		duration time.Duration
		warning  Warning
		report   bool
	}{
		{name: "fast", duration: time.Millisecond},
		{name: "below_threshold", duration: slowSearchReportThreshold - time.Nanosecond},
		{name: "threshold", duration: slowSearchReportThreshold, report: true},
		{name: "slow", duration: time.Second, report: true},
		{name: "retriever_degraded", warning: Warning{Code: "retriever_degraded"}, report: true},
		{name: "rerank_degraded", warning: Warning{Code: "rerank_unavailable", Kind: "degraded"}, report: true},
		{name: "graph_unavailable", warning: Warning{Code: "indexed_graph_unavailable", Kind: "index_unavailable"}, report: true},
		{name: "typo_fallback", warning: Warning{Code: "typo_fallback", Kind: "query_interpretation"}, report: true},
		{name: "intent_fallback", warning: Warning{Code: "missing_definition"}, report: true},
		{name: "ordinary_warning", warning: Warning{Code: "other"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, root, recorder, parent := diagnosticSearchContext(t)
			var stderr bytes.Buffer
			restore := recorder.SetStderr(&stderr)
			defer restore()
			timings := &Timings{}
			ctx = WithTimings(ctx, timings)
			ctx, operation := beginSearchOperation(ctx)
			response := Response{Results: []RankedResult{{}}}
			if test.warning.Code != "" {
				response.Warnings = []Warning{test.warning}
			}
			operation.finishAt(ctx, response, nil, false, operation.started.Add(test.duration))
			reports, err := diagnostics.ReadReports(root, diagnostics.Filter{})
			require.NoError(t, err)
			if test.report {
				require.Len(t, reports.Reports, 1)
				require.Equal(t, test.duration.Milliseconds(), reports.Reports[0].DurationMS)
			} else {
				require.Empty(t, reports.Reports)
			}
			outcome := "ok"
			if test.warning.Code != "" && test.report {
				outcome = "partial"
			}
			require.Equal(t, int64(1), snapshotDiagnosticCount(indexingperf.FromContext(ctx).Snapshot(), "search.outcome."+outcome))
			require.Len(t, timings.Snapshot(), 1)
			require.Equal(t, test.duration, timings.Snapshot()[0].Duration)
			require.NoError(t, recorder.Close())
			events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
			require.NoError(t, err)
			require.Len(t, events.Events, 2)
			for _, event := range events.Events {
				require.Equal(t, operation.operation.ID, event.OperationID)
				require.Equal(t, parent.ID, event.ParentOperationID)
				require.Equal(t, parent.TraceID, event.TraceID)
			}
			terminal := events.Events[1]
			require.Equal(t, "search.finished", terminal.Name)
			require.Equal(t, outcome, terminal.Attributes["outcome"])
			require.EqualValues(t, 1, terminal.Attributes["result_count"])
			require.Contains(t, terminal.Attributes, "duration_ms")
			require.Empty(t, stderr.String())
		})
	}
}

func TestSearchRecorderReportsErrorsCancellationAndPanicsSafely(t *testing.T) {
	for _, test := range []struct {
		name, status, outcome, reason string
		err                           error
		cancel, panic                 bool
	}{
		{name: "error", err: errors.New("PRIVATE_ERROR"), status: "error", outcome: "error", reason: "operation_failed"},
		{name: "cancellation", cancel: true, status: "canceled", outcome: "partial", reason: "canceled"},
		{name: "panic", panic: true, status: "error", outcome: "error", reason: "handler_panicked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, root, recorder, _ := diagnosticSearchContext(t)
			ranker := &observingDiagnosticRanker{err: test.err}
			if test.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			panicValue := &struct{ private string }{private: "PRIVATE_PANIC"}
			if test.panic {
				ranker.panicValue = panicValue
			}
			svc := Service{Retrievers: []Retriever{staticRetriever{name: "lexical"}}, Ranker: ranker}
			if test.panic {
				require.PanicsWithValue(t, panicValue, func() { _, _ = svc.Search(ctx, QuerySpec{Text: "PRIVATE_QUERY"}) })
			} else {
				_, err := svc.Search(ctx, QuerySpec{Text: "PRIVATE_QUERY"})
				require.ErrorIs(t, err, test.err)
			}
			report, snapshot := readSearchReport(t, root)
			require.Equal(t, test.status, report.Status)
			require.Equal(t, test.outcome, report.Attributes["outcome"])
			require.Equal(t, test.reason, report.ReasonCode)
			require.Equal(t, int64(1), snapshotDiagnosticCount(snapshot, "search.outcome."+test.outcome))
			require.Zero(t, snapshotDiagnosticCount(snapshot, "search.outcome.ok"))
			var total indexingperf.SpanSnapshot
			for _, span := range snapshot.Spans {
				if span.Name == "search.search" {
					total = span
				}
			}
			require.Equal(t, int64(1), total.Count)
			require.Equal(t, "error", total.Status)
			require.NoError(t, recorder.Close())
			events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
			require.NoError(t, err)
			encoded, err := json.Marshal([]any{report, events})
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "PRIVATE")
		})
	}
}
