package diagnostics

import (
	"context"
	"encoding/json"
	"log/slog"
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
		`{"regions":{"type-view-table":{"update":4,"actual_total_ms":30,"actual_max_ms":12,"commits_after_refresh":3}},"frames":{"over_50ms":2,"over_200ms":1,"stalled_ms":400,"longest_ms":310},"refresh":{"cycles":1,"refetched":2,"unchanged":2},"stability":{"reconnects":0}}`)
	// Some refetched data changed, so renders after this refresh are expected.
	f.at(10*time.Second).webBatch("desktop", "aaaaaaaaaaaaaaaa",
		`{"regions":{"type-view-table":{"update":1,"actual_total_ms":5,"actual_max_ms":5,"commits_after_refresh":1}},"frames":{},"refresh":{"cycles":1,"refetched":2,"unchanged":1},"stability":{"reconnects":0}}`)
	f.at(10*time.Second).webBatch("browser", "bbbbbbbbbbbbbbbb",
		`{"frames":{},"refresh":{"cycles":1},"stability":{"reconnects":3,"errors":{"TypeError":2}}}`)
	require.NoError(t, f.recorder.Close())

	summary := *readPerf(t, root, f.now, "1h").Perf
	require.Len(t, summary.Web, 2)
	browser, desktop := summary.Web[0], summary.Web[1]
	require.Equal(t, "desktop", desktop.Surface)
	require.Equal(t, 2, desktop.Windows)
	require.Equal(t, 1, desktop.Sessions)
	require.Equal(t, WebRefresh{Cycles: 2, Refetched: 4, Unchanged: 3}, desktop.Refresh)
	require.Equal(t, []WebRegion{{Name: "type-view-table", Update: 5, ActualTotalMs: 35, ActualMaxMs: 12, CommitsAfterRefresh: 4}}, desktop.Regions)
	require.Equal(t, map[string]uint64{"TypeError": 2}, browser.Errors)

	churn := flagsOfKind(summary, "web_refresh_render_churn")
	require.Len(t, churn, 1)
	require.Equal(t, 1, churn[0].Count, "only the window whose refetches were all unchanged")
	require.Equal(t, []string{"session:aaaaaaaaaaaaaaaa"}, churn[0].OperationIDs)
	require.Equal(t, 1, flagsOfKind(summary, "web_frame_stalls")[0].Count)
	require.Equal(t, 1, flagsOfKind(summary, "web_stream_instability")[0].Count)
}
