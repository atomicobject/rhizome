package diagnostics

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (f *perfFixture) webBatch(surface, session, metrics string) {
	f.recorder.EventMetrics(context.Background(), slog.LevelInfo, "web", webClientEvent, "", json.RawMessage(metrics),
		slog.String("surface", surface), slog.String("session", session))
}

func TestPerfSummarizesWebBatchesAndFlagsChurnStallsAndReconnects(t *testing.T) {
	root := t.TempDir()
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	f := newPerfFixture(t, root, start)
	f.at(10*time.Second).webBatch("desktop", "aaaaaaaaaaaaaaaa",
		`{"regions":{"type-view-table":{"update":4,"actual_total_ms":30,"actual_max_ms":12,"commits_after_refresh":3}},"frames":{"over_50ms":2,"over_200ms":1,"stalled_ms":400,"longest_ms":310},"refresh":{"cycles":2,"refetched":2,"unchanged":2,"churned":2},"stability":{"reconnects":0}}`)
	// Commits followed refreshes, but the browser judged none of them churn.
	f.at(10*time.Second).webBatch("desktop", "aaaaaaaaaaaaaaaa",
		`{"regions":{"type-view-table":{"update":1,"actual_total_ms":5,"actual_max_ms":5,"commits_after_refresh":1}},"frames":{},"refresh":{"cycles":1,"refetched":2,"unchanged":1},"stability":{"reconnects":0}}`)
	f.at(10*time.Second).webBatch("browser", "bbbbbbbbbbbbbbbb",
		`{"frames":{},"refresh":{"cycles":3},"stability":{"reconnects":3,"errors":{"TypeError":2}}}`)
	// One reconnect, such as a runtime restart, is not instability.
	f.at(10*time.Second).webBatch("browser", "cccccccccccccccc", `{"frames":{},"refresh":{"cycles":1},"stability":{"reconnects":1}}`)
	require.NoError(t, f.recorder.Close())

	summary := *readPerf(t, root, f.now, "1h").Perf
	require.Len(t, summary.Web, 2)
	browser, desktop := summary.Web[0], summary.Web[1]
	require.Equal(t, "desktop", desktop.Surface)
	require.Equal(t, 2, desktop.Windows)
	require.Equal(t, 1, desktop.Sessions)
	require.Equal(t, WebRefresh{Cycles: 3, Refetched: 4, Unchanged: 3, Churned: 2}, desktop.Refresh)
	require.Equal(t, []WebRegion{{Name: "type-view-table", Update: 5, ActualTotalMs: 35, ActualMaxMs: 12, CommitsAfterRefresh: 4}}, desktop.Regions)
	require.Equal(t, map[string]uint64{"TypeError": 2}, browser.Errors)

	churn := flagsOfKind(summary, "web_refresh_render_churn")
	require.Len(t, churn, 1)
	require.Equal(t, 2, churn[0].Count, "counts churned refreshes, not windows with commits")
	require.Equal(t, []string{"session:aaaaaaaaaaaaaaaa"}, churn[0].OperationIDs)
	require.Equal(t, 1, flagsOfKind(summary, "web_frame_stalls")[0].Count)
	require.Equal(t, 1, flagsOfKind(summary, "web_stream_instability")[0].Count)
}

func TestPerfMergesExperienceFramesPerHash(t *testing.T) {
	root := t.TempDir()
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	f := newPerfFixture(t, root, start)
	f.at(10*time.Second).webBatch("desktop", "aaaaaaaaaaaaaaaa",
		`{"frames":{},"refresh":{},"stability":{},"experiences":{"0a1b2c3d4e5f60":{"frames_created":1,"pages_retained":2,"phases":{"first-render":{"count":3,"p50_ms":40,"p95_ms":90},"shell-start":{"count":1,"p50_ms":12,"p95_ms":12}}}}}`)
	f.at(10*time.Second).webBatch("desktop", "aaaaaaaaaaaaaaaa",
		`{"frames":{},"refresh":{},"stability":{},"experiences":{"0a1b2c3d4e5f60":{"locations_reused":4,"pages_retained":1,"resumes":1,"evictions":1,"phases":{"first-render":{"count":1,"p50_ms":20,"p95_ms":20}}}}}`)
	require.NoError(t, f.recorder.Close())

	desktop := readPerf(t, root, f.now, "1h").Perf.Web[0]
	require.Equal(t, []WebExperience{{
		Hash: "0a1b2c3d4e5f60", Windows: 2, FramesCreated: 1, LocationsReused: 4, PagesRetained: 2, Resumes: 1, Evictions: 1,
		Phases: []WebExperiencePhase{
			{Phase: "shell-start", Samples: 1, P50Ms: 12, P95MaxMs: 12},
			{Phase: "first-render", Samples: 4, P50Ms: 35, P95MaxMs: 90},
		},
	}}, desktop.Experiences)

	var text strings.Builder
	writeWebText(&text, []WebSummary{desktop})
	require.Contains(t, text.String(), "experience 0a1b2c3d4e5f60: windows=2 frames created=1 locations reused=4 pages retained<=2 resumes=1 evictions=1")
	require.Contains(t, text.String(), "first-render: samples=4 p50=35ms p95<=90ms")
}

func TestPerfTotalsReadCacheWindowsPerQueryHash(t *testing.T) {
	root := t.TempDir()
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	f := newPerfFixture(t, root, start)
	window := func(metrics string) {
		f.recorder.EventMetrics(context.Background(), slog.LevelInfo, "web", readCacheEvent, "", json.RawMessage(metrics))
	}
	f.at(10 * time.Second)
	window(`{"entries":3,"bytes":4000,"queries":{"aaaa":{"hits":5,"misses":1,"executions":1,"exec_total_ms":40,"exec_max_ms":40,"stored_bytes":900},"bbbb":{"bypasses":2,"executions":2,"exec_total_ms":10,"exec_max_ms":6}}}`)
	f.at(70 * time.Second)
	window(`{"entries":4,"bytes":5000,"queries":{"aaaa":{"hits":2,"misses":1,"joins":3,"executions":1,"exec_total_ms":60,"exec_max_ms":60,"stored_bytes":950}}}`)
	require.NoError(t, f.recorder.Close())

	summary := readPerf(t, root, f.now, "1h").Perf.ReadCache
	require.NotNil(t, summary)
	require.Equal(t, 2, summary.Windows)
	require.Equal(t, 4, summary.Entries)
	require.EqualValues(t, 5000, summary.Bytes)
	require.Equal(t, []ReadCacheQuery{
		{Hash: "aaaa", Hits: 7, Misses: 2, Joins: 3, Executions: 2, ExecTotalMs: 100, ExecMaxMs: 60, StoredBytes: 1850},
		{Hash: "bbbb", Bypasses: 2, Executions: 2, ExecTotalMs: 10, ExecMaxMs: 6},
	}, summary.Queries)

	var text strings.Builder
	writeReadCacheText(&text, summary)
	require.Contains(t, text.String(), "aaaa: hits=7 misses=2 joined=3 bypassed=0 executions=2 exec total=100ms max=60ms stored=1850B")
}
