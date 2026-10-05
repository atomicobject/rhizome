package diagnostics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestParseTime(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		input   string
		want    time.Time
		invalid bool
	}{
		{"7d", now.Add(-7 * 24 * time.Hour), false}, {"2h", now.Add(-2 * time.Hour), false}, {"2026-10-03T12:00:00Z", now.Add(-24 * time.Hour), false}, {"", time.Time{}, false}, {"-1d", time.Time{}, true}, {"invalid", time.Time{}, true}, {"NaNd", time.Time{}, true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseTime(tc.input, now)
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestExplicitRootBypassesBrokenConfigAndIndex(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("not: [valid"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "index.db"), []byte("broken database"), 0600))
	got, err := ResolveRoot(root)
	require.NoError(t, err)
	canonical, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	require.Equal(t, canonical.Root(), got)
	before, err := os.ReadDir(filepath.Join(root, ".rhizome"))
	require.NoError(t, err)
	result, err := Read(Options{Vault: root, Mode: "index", Last: true})
	require.NoError(t, err)
	require.Empty(t, result.Reports)
	require.NotEmpty(t, result.Coverage.Warnings)
	after, err := os.ReadDir(filepath.Join(root, ".rhizome"))
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestSummaryUsesTypedMeasurementsAndDisclosesCoverage(t *testing.T) {
	snapshot := indexingperf.Snapshot{
		Spans:    []indexingperf.SpanSnapshot{{Name: "short", DurationNs: 2}, {Name: "slow", DurationNs: 9}, {Name: "tied", DurationNs: 9}},
		Counters: []indexingperf.CounterSnapshot{{MetricIdentity: indexingperf.MetricIdentity{Name: indexingperf.DiagnosticFallbackPrefix + "rank.timeout"}, Total: 4}, {MetricIdentity: indexingperf.MetricIdentity{Name: "files"}, Total: 500}},
		Coverage: indexingperf.Coverage{DroppedSamples: 3},
	}
	metrics, err := json.Marshal(snapshot)
	require.NoError(t, err)
	summary := SummarizeIndex(evidence.Report{OperationID: "op-1", Metrics: metrics})
	require.Equal(t, []PhaseDuration{{Name: "slow", DurationNS: 9}, {Name: "tied", DurationNS: 9}, {Name: "short", DurationNS: 2}}, summary.SlowPhases)
	require.Equal(t, []FallbackCount{{Reason: "rank.timeout", Count: 4}}, summary.FallbackCounts)
	require.Len(t, summary.EvidenceGaps, 1)
	require.Contains(t, summary.EvidenceGaps[0], "3 dropped samples")
}

func TestSummaryDoesNotInventMetrics(t *testing.T) {
	for _, metrics := range []json.RawMessage{nil, json.RawMessage(`{"duration":"bad"}`), json.RawMessage(`{"spans":[]}`)} {
		summary := SummarizeIndex(evidence.Report{Metrics: metrics})
		require.Empty(t, summary.SlowPhases)
		require.Empty(t, summary.FallbackCounts)
		require.NotEmpty(t, summary.EvidenceGaps)
	}
}

func TestLogFiltersPartialRecordsAndBounds(t *testing.T) {
	root := t.TempDir()
	eventsDir := filepath.Join(root, ".rhizome", "diagnostics", "events")
	require.NoError(t, os.MkdirAll(eventsDir, 0700))
	now := time.Now().UTC()
	events := []evidence.Event{
		{SchemaVersion: evidence.SchemaVersion, Time: now.Add(-3 * time.Minute), ProcessID: "process", Sequence: 1, Level: "WARN", Name: "first", Subsystem: "search", OperationID: "parent", TraceID: "trace"},
		{SchemaVersion: evidence.SchemaVersion, Time: now.Add(-2 * time.Minute), ProcessID: "process", Sequence: 2, Level: "INFO", Name: "second", Subsystem: "search", OperationID: "child", ParentOperationID: "parent", TraceID: "trace"},
		{SchemaVersion: evidence.SchemaVersion, Time: now.Add(-time.Minute), ProcessID: "process", Sequence: 3, Level: "ERROR", Name: "third", Subsystem: "search", OperationID: "other", TraceID: "other-trace"},
	}
	var lines []byte
	for _, event := range events {
		data, err := json.Marshal(event)
		require.NoError(t, err)
		lines = append(lines, data...)
		lines = append(lines, '\n')
	}
	lines = append(lines, []byte("not json\n{\"partial\":")...)
	require.NoError(t, os.WriteFile(filepath.Join(eventsDir, now.Format("20060102")+"-process-000001-4194304.jsonl"), lines, 0600))
	result, err := Read(Options{Vault: root, Mode: "logs", Since: "2d", Level: "warn", Now: now})
	require.NoError(t, err)
	require.Len(t, result.Events, 2)
	require.NotEmpty(t, result.Coverage.Warnings)
	require.Equal(t, "first", result.Events[0].Name)
	require.Equal(t, "third", result.Events[1].Name)
	result, err = Read(Options{Vault: root, Mode: "logs", Since: "2d", Level: "debug", TraceID: "trace", Now: now})
	require.NoError(t, err)
	require.Len(t, result.Events, 2)
	require.Equal(t, "child", result.Events[1].OperationID)
	result, err = Read(Options{Vault: root, Mode: "logs", Since: "2d", Level: "debug", OperationID: "parent", Now: now})
	require.NoError(t, err)
	require.Len(t, result.Events, 1, "exact operation filter must not implicitly widen to child")
	result, err = Read(Options{Vault: root, Mode: "logs", Since: "2d", Level: "warn", Limit: 1, Now: now})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, "third", result.Events[0].Name)
	require.True(t, result.Coverage.Truncated)
}

func TestReportHistoryKindsStatusesTracesAndRetentionGap(t *testing.T) {
	root := t.TempDir()
	reportsDir := filepath.Join(root, ".rhizome", "diagnostics", "reports")
	require.NoError(t, os.MkdirAll(reportsDir, 0700))
	now := time.Now().UTC()
	reports := []evidence.Report{
		{SchemaVersion: evidence.SchemaVersion, OperationID: "index-success", Kind: "index", Status: "success", TraceID: "trace", StartedAt: now.Add(-time.Hour), FinishedAt: now.Add(-time.Hour + time.Second)},
		{SchemaVersion: evidence.SchemaVersion, OperationID: "search-error", Kind: "search", Status: "error", TraceID: "trace", StartedAt: now.Add(-time.Minute), FinishedAt: now.Add(-time.Minute + time.Second)},
		{SchemaVersion: evidence.SchemaVersion, OperationID: "mutation-canceled", Kind: "mutation", Status: "canceled", TraceID: "other", StartedAt: now.Add(-time.Second), FinishedAt: now},
	}
	for _, report := range reports {
		data, err := json.Marshal(report)
		require.NoError(t, err)
		name := report.FinishedAt.Format("20060102T150405") + fmt.Sprintf("%09dZ-", report.FinishedAt.Nanosecond()) + report.OperationID + ".json"
		require.NoError(t, os.WriteFile(filepath.Join(reportsDir, name), data, 0600))
	}
	result, err := Read(Options{Vault: root, Mode: "reports", Kind: "search", Status: "error", TraceID: "trace", Since: "7d", Now: now})
	require.NoError(t, err)
	require.Len(t, result.Reports, 1)
	require.Equal(t, "search-error", result.Reports[0].OperationID)
	require.NotEmpty(t, result.Coverage.Warnings, "requested window predates retained evidence")
	result, err = Read(Options{Vault: root, Mode: "reports", TraceID: "trace", Since: "7d", Now: now})
	require.NoError(t, err)
	require.Len(t, result.Reports, 2)
	require.Equal(t, "search-error", result.Reports[0].OperationID)
	result, err = Read(Options{Vault: root, Mode: "reports", Since: "7d", Limit: 1, Now: now})
	require.NoError(t, err)
	require.Len(t, result.Reports, 1)
	require.Equal(t, "mutation-canceled", result.Reports[0].OperationID)
	require.True(t, result.Coverage.Truncated)
}

func TestExactReportReadsKeepCollectedDetailBoundsSeparate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "diagnostics")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reports"), 0700))
	now := time.Now().UTC()
	metrics, err := json.Marshal(indexingperf.Snapshot{
		Coverage:  indexingperf.Coverage{DroppedSamples: 44},
		Latencies: []indexingperf.DistributionSnapshot{{MetricIdentity: indexingperf.MetricIdentity{Name: "latency"}, QuantileCoverage: "retained_prefix"}},
	})
	require.NoError(t, err)
	report := evidence.Report{SchemaVersion: evidence.SchemaVersion, OperationID: "bounded-detail", Kind: "index", Status: "success", StartedAt: now.Add(-time.Second), FinishedAt: now, Truncated: true, Metrics: metrics}
	data, err := json.Marshal(report)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "latest-index.json"), data, 0600))
	filename := now.Format("20060102T150405") + fmt.Sprintf("%09dZ-", now.Nanosecond()) + report.OperationID + ".json"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reports", filename), data, 0600))
	for _, opts := range []Options{{Vault: root, Mode: "index", Last: true}, {Vault: root, Mode: "report", OperationID: report.OperationID}} {
		result, err := Read(opts)
		require.NoError(t, err)
		require.True(t, result.Reports[0].Truncated)
		require.False(t, result.Coverage.Truncated, "complete exact read must not imply a truncated file read")
		var rendered bytes.Buffer
		require.NoError(t, WriteText(&rendered, result))
		require.Contains(t, rendered.String(), "Report contains bounded/incomplete detail.")
		require.Contains(t, rendered.String(), "retained sample prefix")
		require.NotContains(t, rendered.String(), "bounded read was truncated")
	}
}

func TestIndexingJobSummaryPreservesActualProducerReasonFamilies(t *testing.T) {
	root := t.TempDir()
	reportsDir := filepath.Join(root, ".rhizome", "diagnostics", "reports")
	require.NoError(t, os.MkdirAll(reportsDir, 0700))
	metrics, err := json.Marshal(indexingperf.Snapshot{
		Spans: []indexingperf.SpanSnapshot{{Name: "watcher.ownership_discovery", DurationNs: int64(time.Second), Status: "success"}},
		Counters: []indexingperf.CounterSnapshot{
			{MetricIdentity: indexingperf.MetricIdentity{Name: "calledge.fallback.reverse_index_backfill_incomplete"}, Total: 1},
			{MetricIdentity: indexingperf.MetricIdentity{Name: "scope.full_recompute.dirty_scope_overflow"}, Total: 2},
			{MetricIdentity: indexingperf.MetricIdentity{Name: "watcher.full_discovery.pending_reconciliation"}, Total: 3},
			{MetricIdentity: indexingperf.MetricIdentity{Name: "search.fallback.rank.timeout"}, Total: 4},
			{MetricIdentity: indexingperf.MetricIdentity{Name: "watcher.scoped_discovery"}, Total: 5},
		},
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	report := evidence.Report{SchemaVersion: evidence.SchemaVersion, OperationID: "watcher-job", Kind: "indexing-job", Status: "success", StartedAt: now.Add(-time.Second), FinishedAt: now, Metrics: metrics}
	data, err := json.Marshal(report)
	require.NoError(t, err)
	filename := now.Format("20060102T150405") + fmt.Sprintf("%09dZ-", now.Nanosecond()) + report.OperationID + ".json"
	require.NoError(t, os.WriteFile(filepath.Join(reportsDir, filename), data, 0600))
	result, err := Read(Options{Vault: root, Mode: "reports", Since: "7d"})
	require.NoError(t, err)
	require.Len(t, result.IndexSummaries, 1)
	summary := result.IndexSummaries[0]
	require.Equal(t, []PhaseDuration{{Name: "watcher.ownership_discovery", DurationNS: int64(time.Second), Status: "success"}}, summary.SlowPhases)
	require.Equal(t, []FallbackCount{
		{Reason: "calledge.fallback.reverse_index_backfill_incomplete", Count: 1},
		{Reason: "rank.timeout", Count: 4},
		{Reason: "scope.full_recompute.dirty_scope_overflow", Count: 2},
		{Reason: "watcher.full_discovery.pending_reconciliation", Count: 3},
	}, summary.FallbackCounts)
	var rendered bytes.Buffer
	require.NoError(t, WriteText(&rendered, result))
	require.Contains(t, rendered.String(), "reason scope.full_recompute.dirty_scope_overflow: 2 observed")
	require.Contains(t, rendered.String(), "reason watcher.full_discovery.pending_reconciliation: 3 observed")
	require.NotContains(t, rendered.String(), "rebuild")
}
