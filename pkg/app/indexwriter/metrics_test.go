package indexwriter

import (
	"context"
	"errors"
	"testing"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

func TestWriterMetricsDistinguishAttemptedBatchesFromCommittedRows(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "failed"}[failed], func(t *testing.T) {
			collector := indexingperf.NewBounded()
			ctx := indexingperf.WithCollector(context.Background(), collector)
			var applied int
			wantErr := errors.New("synthetic handler failure")
			q := NewWithConfig(ctx, Handlers{ApplyNoteIndexBatch: func(_ context.Context, batch []anchors.NoteIndexWork) error {
				applied += len(batch)
				if failed {
					return wantErr
				}
				return nil
			}}, Config{DefaultPolicy: FlushPolicy{Rows: 1000, Bytes: 1 << 20, Idle: time.Hour}, PollInterval: time.Hour})
			t.Cleanup(func() { _ = q.StopAndWait() })
			require.NoError(t, q.SubmitNoteIndexWork(ctx, anchors.NoteIndexWork{Path: "a.md"}))
			require.NoError(t, q.SubmitNoteIndexWork(ctx, anchors.NoteIndexWork{Path: "b.md"}))
			err := q.FlushAndWait(ctx)
			if failed {
				require.ErrorIs(t, err, wantErr)
				require.ErrorIs(t, q.StopAndWait(), wantErr)
			} else {
				require.NoError(t, err)
				require.NoError(t, q.Close())
			}
			require.Equal(t, 2, applied)
			snapshot := collector.Snapshot()
			count := func(name string) int64 {
				var total int64
				for _, counter := range snapshot.Counters {
					if counter.Name == name {
						total += counter.Total
					}
				}
				return total
			}
			require.Equal(t, int64(1), count("queue.flush"))
			require.Equal(t, int64(2), count("queue.rows"))
			require.Equal(t, int64(1), count("writer.flush_barriers"))
			outcome, rows := "success", int64(2)
			if failed {
				outcome, rows = "error", 0
			}
			require.Equal(t, int64(1), count("queue.flush.outcome."+outcome))
			require.Equal(t, rows, count("queue.rows.committed"))
			require.Equal(t, rows, count("node.write.out"))
			for _, span := range snapshot.Spans {
				if span.Name == "writer_flush_barrier" {
					require.Equal(t, map[bool]string{false: "ok", true: "error"}[failed], span.Status)
				}
			}
		})
	}
}
