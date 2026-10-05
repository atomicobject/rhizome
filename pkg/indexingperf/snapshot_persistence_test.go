package indexingperf_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

func TestLargeBoundedSnapshotPersistsAllAggregates(t *testing.T) {
	c := indexingperf.NewBounded()
	ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), c), "bulk")
	at := time.Unix(100, 0)
	for metric := 0; metric < 160; metric++ {
		name := fmt.Sprintf("metric.%03d.%s", metric, strings.Repeat("x", 90))
		for observation := 0; observation < 300; observation++ {
			indexingperf.AddCount(ctx, name, 1)
			indexingperf.SetGauge(ctx, name, int64(observation))
			indexingperf.ObserveLatency(ctx, name, time.Millisecond)
			indexingperf.ObserveSample(ctx, name, 10)
			indexingperf.ObserveInterval(ctx, name, at.Add(time.Duration(observation)*time.Millisecond), time.Millisecond)
			c.RecordSpanWindow(name, at, at.Add(time.Millisecond), nil)
			indexingperf.ObserveDBWrite(indexingperf.WithOp(ctx, name), "store", time.Millisecond, time.Millisecond)
		}
	}
	expected := c.Snapshot()
	data, err := json.Marshal(expected)
	require.NoError(t, err)
	require.Less(t, len(data), 1<<20)
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: io.Discard})
	require.NoError(t, err)
	defer func() { require.NoError(t, recorder.Close()) }()
	operation := diagnostics.NewOperation("index", "snapshot-test")
	ctx = diagnostics.WithOperation(ctx, operation)
	err = recorder.PublishReport(ctx, diagnostics.Report{Status: "success", Metrics: data})
	require.NoError(t, err)
	latest, err := diagnostics.ReadLatest(root, "index")
	require.NoError(t, err)
	require.False(t, latest.Truncated)
	var actual indexingperf.Snapshot
	require.NoError(t, json.Unmarshal(latest.Metrics, &actual))
	require.Equal(t, expected, actual)
	named, err := diagnostics.ReadReport(root, operation.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(latest.Metrics), string(named.Metrics))
	t.Logf("persisted %d series across %d bytes with all aggregates intact", 160*6, len(data))
}
