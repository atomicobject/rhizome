package indexwriter

import (
	"context"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestQueuedWriterPublishesNoteMetadataBeforeOntology(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, name)
	}
	q := New(context.Background(), Handlers{
		ApplyNoteMetadataDelta: func(context.Context, semdb.NoteMetadataDelta) error {
			record("metadata")
			return nil
		},
		ApplyOntologyDelta: func(context.Context, semdb.OntologyDelta) error {
			record("ontology")
			return nil
		},
	})
	defer func() { _ = q.Close() }()

	require.NoError(t, q.SubmitNoteMetadataDelta(context.Background(), semdb.NoteMetadataDelta{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{{Path: "note.md"}},
	}))
	require.NoError(t, q.SubmitOntologyDelta(context.Background(), semdb.OntologyDelta{FullRebuild: true, DeletePaths: []string{"old.md"}}))
	require.NoError(t, q.FlushAndWait(context.Background()))
	require.Equal(t, []string{"metadata", "ontology"}, order)
}

func TestQueuedWriterBatchesOntologyDeltasByPhase(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls int
		last  semdb.OntologyDelta
	)
	q := newWithConfig(context.Background(), Handlers{
		ApplyOntologyDelta: func(ctx context.Context, delta semdb.OntologyDelta) error {
			mu.Lock()
			defer mu.Unlock()
			calls++
			last = delta
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	phaseCtx := indexingperf.WithPhase(context.Background(), "write_ontology")
	require.NoError(t, q.SubmitOntologyDelta(phaseCtx, semdb.OntologyDelta{
		ReplacePaths: []string{"notes/a.md"},
		EdgeSources:  []string{"notes/a.md"},
		Assessments: []semdb.OntologyNoteAssessmentRow{
			{NotePath: "notes/a.md", AssessmentJSON: "{}", SchemaHash: "abc"},
		},
	}))
	require.NoError(t, q.SubmitOntologyDelta(phaseCtx, semdb.OntologyDelta{
		ReplacePaths: []string{"notes/b.md"},
		EdgeSources:  []string{"notes/b.md"},
		Assessments: []semdb.OntologyNoteAssessmentRow{
			{NotePath: "notes/b.md", AssessmentJSON: "{}", SchemaHash: "abc"},
		},
	}))
	require.NoError(t, q.Close())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, calls)
	require.ElementsMatch(t, []string{"notes/a.md", "notes/b.md"}, last.ReplacePaths)
	require.Len(t, last.Assessments, 2)
}

func TestQueuedWriterClosePublishesAllOntologyBodyKinds(t *testing.T) {
	t.Parallel()
	received := map[string]int{}
	cfg := defaultConfig()
	cfg.DefaultPolicy.Idle = time.Hour
	cfg.PollInterval = time.Hour
	q := newWithConfig(context.Background(), Handlers{
		ApplyOntologyNodeReadModel: func(context.Context, readmodel.NodeReadModel) error { received["nodes"]++; return nil },
		ApplyOntologyNodeStates: func(context.Context, []codeanchor.IntelOntologyNodeEmbeddingState) error {
			received["states"]++
			return nil
		},
		ApplyIntelEmbeddings: func(context.Context, map[string]embeddings.Embedding) error { received["vectors"]++; return nil },
	}, cfg)
	require.NoError(t, q.SubmitOntologyNodes(context.Background(), []string{"note.md"}, []readmodel.NodeRow{{NodeID: "node:a"}}))
	require.NoError(t, q.SubmitOntologyNodeEmbeddingStates(context.Background(), []codeanchor.IntelOntologyNodeEmbeddingState{{ChunkID: "chunk:a"}}))
	require.NoError(t, q.SubmitIntelEmbeddings(context.Background(), map[string]embeddings.Embedding{"chunk:a": {1}}))
	require.NoError(t, q.Close())
	require.Equal(t, map[string]int{"nodes": 1, "states": 1, "vectors": 1}, received)
}

func TestQueuedWriterFlushesOntologyDeltaBeforeReadModel(t *testing.T) {
	// Regression: a FullRebuild ApplyOntologyDelta truncates ontology_nodes /
	// ontology_node_field_values. If the read-model batch flushed first, those
	// inserts would be wiped by the trailing delta. flushAll must drain
	// ontologyOps before ontologyNodes; this test pins that order so a
	// future reorder of flushAll cannot silently regress catalog writes.
	t.Parallel()

	var (
		mu    sync.Mutex
		order []string
	)
	q := newWithConfig(context.Background(), Handlers{
		ApplyOntologyDelta: func(ctx context.Context, delta semdb.OntologyDelta) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "delta")
			return nil
		},
		ApplyOntologyNodeReadModel: func(ctx context.Context, model readmodel.NodeReadModel) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, "read-model")
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	phaseCtx := indexingperf.WithPhase(context.Background(), "sync_ontology")
	// Submit read-model FIRST to make the order assertion meaningful: the
	// queue must still drain the delta before the read-model.
	require.NoError(t, q.SubmitOntologyNodeReadModel(phaseCtx, readmodel.NodeReadModel{
		NotePaths: []string{"notes/a.md"},
		Nodes: []readmodel.NodeRow{{
			NodeID:      "node:a",
			NotePath:    "notes/a.md",
			NodeRefJSON: "{}",
		}},
	}))
	require.NoError(t, q.SubmitOntologyDelta(phaseCtx, semdb.OntologyDelta{
		FullRebuild: true,
		Assessments: []semdb.OntologyNoteAssessmentRow{{NotePath: "notes/a.md"}},
	}))
	require.NoError(t, q.Close())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"delta", "read-model"}, order, "ontology delta must drain before read-model so FullRebuild truncate cannot wipe freshly-inserted catalog rows")
}

func TestQueuedWriterDoesNotThresholdFlushOntologyReadModelBeforeFullRebuild(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		order []string
	)
	flushed := make(chan string, 2)
	q := newWithConfig(context.Background(), Handlers{
		ApplyOntologyDelta: func(ctx context.Context, delta semdb.OntologyDelta) error {
			mu.Lock()
			order = append(order, "delta")
			mu.Unlock()
			flushed <- "delta"
			return nil
		},
		ApplyOntologyNodeReadModel: func(ctx context.Context, model readmodel.NodeReadModel) error {
			mu.Lock()
			order = append(order, "read-model")
			mu.Unlock()
			flushed <- "read-model"
			return nil
		},
	}, config{
		DefaultPolicy: flushPolicy{Rows: 1, Bytes: 1, Idle: time.Hour},
		ItemEmbed:     flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		IntelEmbed:    flushPolicy{Rows: 100, Bytes: 1 << 20, Idle: time.Hour},
		PollInterval:  time.Hour,
	})

	phaseCtx := indexingperf.WithPhase(context.Background(), "sync_ontology")
	require.NoError(t, q.SubmitOntologyNodeReadModel(phaseCtx, readmodel.NodeReadModel{
		NotePaths: []string{"notes/a.md"},
		Nodes:     []readmodel.NodeRow{{NodeID: "node:a", NotePath: "notes/a.md", NodeRefJSON: "{}"}},
	}))
	select {
	case got := <-flushed:
		t.Fatalf("ontology read-model flushed before FullRebuild delta was enqueued: %s", got)
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, q.SubmitOntologyDelta(phaseCtx, semdb.OntologyDelta{
		FullRebuild: true,
		Assessments: []semdb.OntologyNoteAssessmentRow{{NotePath: "notes/a.md"}},
	}))
	require.NoError(t, q.Close())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"delta", "read-model"}, order)
}
