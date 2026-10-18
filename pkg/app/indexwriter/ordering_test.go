package indexwriter

import (
	"context"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestWriterPreservesSameKindSubmissionOrderAcrossPhases(t *testing.T) {
	t.Parallel()
	var titles []string
	cfg := defaultConfig()
	cfg.DefaultPolicy = flushPolicy{Rows: 1 << 20, Bytes: 1 << 30, Idle: time.Hour}
	cfg.PollInterval = time.Hour
	q := newWithConfig(context.Background(), Handlers{ApplyNoteMetadataDelta: func(_ context.Context, delta semdb.NoteMetadataDelta) error {
		for _, note := range delta.Notes {
			titles = append(titles, note.Title)
		}
		return nil
	}}, cfg)
	for i, title := range []string{"first", "second", "last"} {
		phase := []string{"a", "b", "a"}[i]
		ctx := indexingperf.WithPhase(context.Background(), phase)
		require.NoError(t, q.SubmitNoteMetadataDelta(ctx, semdb.NoteMetadataDelta{Notes: []semdb.NoteMetadataRow{{Path: "note.md", Title: title}}}))
	}
	require.NoError(t, q.Close())
	require.Equal(t, []string{"first", "second", "last"}, titles)
}

func TestWriterThresholdCountsAllPendingPhaseSegments(t *testing.T) {
	t.Parallel()
	cfg := defaultConfig()
	cfg.DefaultPolicy = flushPolicy{Rows: 2, Bytes: 1 << 30, Idle: time.Hour}
	cfg.CodePriority = time.Hour
	cfg.PollInterval = time.Hour
	flushed := make(chan struct{}, 1)
	q := newWithConfig(context.Background(), Handlers{
		ApplyCodeIndexBatch: func(context.Context, []codeanchor.CodeIndexWork) error { return nil },
		ApplyNoteMetaBatch: func(context.Context, []embeddings.NoteFileInfo) error {
			select {
			case flushed <- struct{}{}:
			default:
			}
			return nil
		},
	}, cfg)
	t.Cleanup(func() { _ = q.Close() })
	require.NoError(t, q.SubmitCodeIndexWork(context.Background(), codeanchor.CodeIndexWork{Path: "busy.go"}))
	require.NoError(t, q.FlushAndWait(context.Background()))
	for i := range 4 {
		phase := []string{"a", "b"}[i%2]
		require.NoError(t, q.SubmitNoteMeta(indexingperf.WithPhase(context.Background(), phase), embeddings.NoteFileInfo{Path: "note.md"}))
	}
	select {
	case <-flushed:
	case <-time.After(time.Second):
		t.Fatal("alternating phases bypassed the kind's bounded backlog threshold")
	}
}
