package indexwriter

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"testing"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestWriterMissingHandlersReturnErrorsWithoutPanicking(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*Writer) error{
		"code": func(q *Writer) error {
			return q.SubmitCodeIndexWork(context.Background(), anchors.CodeIndexWork{Path: "file.go"})
		},
		"note": func(q *Writer) error {
			return q.SubmitNoteIndexWork(context.Background(), anchors.NoteIndexWork{Path: "note.md"})
		},
		"note metadata": func(q *Writer) error {
			return q.SubmitNoteMeta(context.Background(), embeddings.NoteFileInfo{Path: "note.md"})
		},
		"code vectors": func(q *Writer) error {
			return q.SubmitCodeItemEmbedding(context.Background(), codeindex.ItemEmbeddingUpsert{AnchorID: "a", Embedding: embeddings.Embedding{1}})
		},
		"code chunks": func(q *Writer) error { return q.SubmitCodeItemChunks(context.Background(), "a", nil, nil, nil) },
		"note chunk sync": func(q *Writer) error {
			return q.SubmitNoteChunkSync(context.Background(), embeddings.NoteChunkSync{NoteID: "note"})
		},
		"intel chunks": func(q *Writer) error { return q.SubmitIntelChunks(context.Background(), []string{"owner"}, nil) },
		"intel vectors": func(q *Writer) error {
			return q.SubmitIntelEmbeddings(context.Background(), map[string]embeddings.Embedding{"chunk": {1}})
		},
		"ontology nodes": func(q *Writer) error { return q.SubmitOntologyNodes(context.Background(), []string{"note.md"}, nil) },
		"ontology states": func(q *Writer) error {
			return q.SubmitOntologyNodeEmbeddingStates(context.Background(), []anchors.IntelOntologyNodeEmbeddingState{{NotePath: "note.md"}})
		},
		"ontology delta": func(q *Writer) error {
			return q.SubmitOntologyDelta(context.Background(), semdb.OntologyDelta{FullRebuild: true})
		},
		"metadata delta": func(q *Writer) error {
			return q.SubmitNoteMetadataDelta(context.Background(), semdb.NoteMetadataDelta{Notes: []semdb.NoteMetadataRow{{Path: "note.md"}}})
		},
		"validation": func(q *Writer) error {
			return q.SubmitValidationSnapshot(context.Background(), semdb.ValidationSnapshot{}, "", 0)
		},
	}
	for name, submit := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			q := New(context.Background(), Handlers{})
			t.Cleanup(func() { _ = q.StopAndWait() })
			require.NoError(t, submit(q))
			require.ErrorContains(t, q.FlushAndWait(context.Background()), "queue not initialized")
		})
	}
}

func TestControlReturnsCommittedResultWhenCancellationAndExitAreReady(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	close(done)
	q := &Writer{doneCh: done}
	ack := make(chan controlResult[int], 1)
	ack <- controlResult[int]{value: 42}
	result, err := awaitControl(ctx, q, ack)
	require.NoError(t, err)
	require.Equal(t, 42, result)
}

func TestWriterRejectsUnscopedChunksWithOnlyFamilyHandler(t *testing.T) {
	t.Parallel()
	q := New(context.Background(), Handlers{ApplyIntelChunksByFamily: func(context.Context, []string, string, []anchors.IntelChunk) error { return nil }})
	t.Cleanup(func() { _ = q.StopAndWait() })
	require.NoError(t, q.SubmitIntelChunks(context.Background(), []string{"owner"}, []anchors.IntelChunk{{}}))
	require.ErrorContains(t, q.FlushAndWait(context.Background()), "intel chunk queue not initialized")
}

func TestWriterRejectsPayloadAfterSuccessfulClose(t *testing.T) {
	t.Parallel()
	calls := 0
	q := New(context.Background(), Handlers{ApplyNoteIndexBatch: func(context.Context, []anchors.NoteIndexWork) error { calls++; return nil }})
	require.NoError(t, q.Close())
	require.NoError(t, q.Close(), "close is idempotent")
	require.Error(t, q.SubmitNoteIndexWork(context.Background(), anchors.NoteIndexWork{Path: "late.md"}))
	require.Zero(t, calls)
}

func TestWriterCancellationStopsBetweenBufferedKinds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	nodeCalls := 0
	cfg := defaultConfig()
	cfg.DefaultPolicy.Idle, cfg.PollInterval = time.Hour, time.Hour
	q := newWithConfig(ctx, Handlers{
		ApplyNoteMetadataDelta: func(context.Context, semdb.NoteMetadataDelta) error {
			cancel()
			return nil
		},
		ApplyOntologyNodeReadModel: func(context.Context, readmodel.NodeReadModel) error {
			nodeCalls++
			return nil
		},
	}, cfg)
	// Both kinds stay buffered until the close control starts its ordered drain.
	require.NoError(t, q.SubmitNoteMetadataDelta(ctx, semdb.NoteMetadataDelta{Notes: []semdb.NoteMetadataRow{{Path: "note.md"}}}))
	require.NoError(t, q.SubmitOntologyNodes(ctx, []string{"note.md"}, nil))
	require.ErrorIs(t, q.Close(), context.Canceled)
	require.Zero(t, nodeCalls, "a completed handler must not start another kind after cancellation")
}
