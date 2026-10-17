package indexwriter

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"sync"
	"sync/atomic"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

const (
	indexQueueCapacity   = 2048
	indexQueueFlushRows  = 500
	indexQueueFlushBytes = 4 << 20
	indexQueueFlushIdle  = 75 * time.Millisecond
	// Code index batches use a separate policy because they are the main source
	// of sqlite writer pressure during `rzm index`. Too small: fragmented file
	// summary/intel writes and high writer_busy. Too large: we stop overlapping
	// code persistence with ingest and bunch work at the tail of index_code.
	indexQueueCodeFlushRows      = 512
	indexQueueCodeFlushBytes     = 8 << 20
	indexQueueCodeFlushIdle      = 100 * time.Millisecond
	indexQueueItemEmbedRows      = 512
	indexQueueItemEmbedBytes     = 4 << 20
	indexQueueItemEmbedIdle      = 150 * time.Millisecond
	indexQueueIntelEmbedRows     = 2048
	indexQueueIntelEmbedBytes    = 12 << 20
	indexQueueIntelEmbedIdle     = 150 * time.Millisecond
	indexQueuePollInterval       = 25 * time.Millisecond
	indexQueueCodePriorityWindow = 100 * time.Millisecond
	indexQueueSemanticDeferScale = 2
	indexQueueSnapshotName       = indexingperf.QueueDepth
	indexQueueCodeDepthName      = indexingperf.QueueCodeDepth
	indexQueueOtherDepthName     = indexingperf.QueueOtherDepth
	indexQueueCodeWaitName       = indexingperf.QueueCodeWait
	indexQueueOtherWaitName      = indexingperf.QueueOtherWait
	indexQueueDefaultPhaseLabel  = "_"
	noteMetadataQueueDepthName   = "validation.writeback.metadata.queue_depth"
	noteMetadataFlushCountName   = "validation.writeback.metadata.flush.count"
	noteMetadataBatchRowsName    = "validation.writeback.metadata.batch_rows"
	noteMetadataBatchBytesName   = "validation.writeback.metadata.batch_bytes"
	noteMetadataFlushLatencyName = "validation.writeback.metadata.flush"
)

type flushPolicy struct {
	Rows  int
	Bytes int
	Idle  time.Duration
}

type config struct {
	DefaultPolicy flushPolicy
	CodeIndex     flushPolicy
	ItemEmbed     flushPolicy
	IntelEmbed    flushPolicy
	PollInterval  time.Duration
	CodePriority  time.Duration
	DeferScale    int
}

func defaultConfig() config {
	return config{
		DefaultPolicy: flushPolicy{
			Rows:  indexQueueFlushRows,
			Bytes: indexQueueFlushBytes,
			Idle:  indexQueueFlushIdle,
		},
		CodeIndex: flushPolicy{
			Rows:  indexQueueCodeFlushRows,
			Bytes: indexQueueCodeFlushBytes,
			Idle:  indexQueueCodeFlushIdle,
		},
		ItemEmbed: flushPolicy{
			Rows:  indexQueueItemEmbedRows,
			Bytes: indexQueueItemEmbedBytes,
			Idle:  indexQueueItemEmbedIdle,
		},
		IntelEmbed: flushPolicy{
			Rows:  indexQueueIntelEmbedRows,
			Bytes: indexQueueIntelEmbedBytes,
			Idle:  indexQueueIntelEmbedIdle,
		},
		PollInterval: indexQueuePollInterval,
		CodePriority: indexQueueCodePriorityWindow,
		DeferScale:   indexQueueSemanticDeferScale,
	}
}

func (c config) codePriorityActive(now, lastCodeActivity time.Time, pendingCode int) bool {
	// “Code hot” is a writer scheduling hint, not a correctness barrier. We use
	// it to keep code-index persistence from competing with semantic writeback
	// while the ingest lane is still active.
	if pendingCode > 0 {
		return true
	}
	if c.CodePriority <= 0 || lastCodeActivity.IsZero() {
		return false
	}
	return now.Sub(lastCodeActivity) < c.CodePriority
}

func (c config) semanticUrgent(policy flushPolicy, rows, bytes int) bool {
	// Deferred semantic writes still need an escape hatch. Without this, writer
	// priority can turn into unbounded backlog during large code scans.
	scale := c.DeferScale
	if scale < 2 {
		scale = 2
	}
	return rows >= policy.Rows*scale || bytes >= policy.Bytes*scale
}

type Handlers struct {
	ResetReverseIndex                  func(context.Context) error
	ApplyGraphScores                   func(context.Context, semdb.DerivedGraphScores) error
	AckDerivedWork                     func(context.Context, []codeanchor.DerivedWork) error
	ApplyStructuralFinalize            func(context.Context, StructuralFinalize) error
	MarkDerivedDirty                   func(context.Context, []codeanchor.DerivedScope) ([]codeanchor.DerivedWork, error)
	ActivateDerivedWork                func(context.Context, []codeanchor.DerivedWork) error
	ApplyCodeIndexBatch                func(context.Context, []codeanchor.CodeIndexWork) error
	ApplyNoteIndexBatch                func(context.Context, []codeanchor.NoteIndexWork) error
	ApplyNoteMetaBatch                 func(context.Context, []embeddings.NoteFileInfo) error
	ApplyCodeItemEmbeddingBatch        func(context.Context, []codeindex.ItemEmbeddingUpsert) error
	ApplyCodeItemChunkBatch            func(context.Context, []codeindex.ItemChunksUpsert) error
	ApplyNoteChunkSyncBatch            func(context.Context, []embeddings.NoteChunkSync) error
	ApplyIntelChunks                   func(context.Context, []string, []codeanchor.IntelChunk) error
	ApplyIntelChunksByFamily           func(context.Context, []string, string, []codeanchor.IntelChunk) error
	ApplyIntelEmbeddings               func(context.Context, map[string]embeddings.Embedding) error
	ApplyOntologyNodeReadModel         func(context.Context, readmodel.NodeReadModel) error
	ApplyOntologyNodeStates            func(context.Context, []codeanchor.IntelOntologyNodeEmbeddingState) error
	ApplyOntologyDelta                 func(context.Context, semdb.OntologyDelta) error
	ApplyNoteMetadataDelta             func(context.Context, semdb.NoteMetadataDelta) error
	ApplyValidationState               func(context.Context, ValidationStateWrite) error
	ApplyOwnershipTransitions          func(context.Context, []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error)
	AcknowledgeOwnershipReconciliation func(context.Context, int64) (bool, error)
}

// Writer is the single durable write lane for indexing. Producers may run
// in parallel, but every SQLite-backed write arrives here so batching, phase
// attribution, and barrier flushes stay observable.
type Writer struct {
	ctx      context.Context
	cancel   context.CancelFunc
	handlers Handlers
	cfg      config

	afterCodeIndexBatch func(context.Context, []codeanchor.CodeIndexWork) error

	ctrlCh     chan any
	codeCmdCh  chan any
	otherCmdCh chan any
	doneCh     chan struct{}

	mu        sync.Mutex
	err       error
	closeOnce sync.Once
	closeErr  error

	// Control ops wait for all payload submits that started before them to
	// settle, but they must not serialize unrelated producer sends.
	submitSeq      atomic.Uint64
	settleMu       sync.Mutex
	settledThrough uint64
	settledPending map[uint64]struct{}
}

type noteChunkSyncWrite struct {
	item embeddings.NoteChunkSync
}

func New(ctx context.Context, handlers Handlers) *Writer {
	return newWithConfig(ctx, handlers, defaultConfig())
}

// WHY: SQLite write concurrency is the wrong scaling axis for indexing. This
// lane preserves one durable writer while letting file workers and semantic
// provider lanes run ahead under bounded, observable backpressure.
// Docs: [[indexing-pipeline-architecture#^spec-0012-us1-ac1]]
func newWithConfig(ctx context.Context, handlers Handlers, cfg config) *Writer {
	ctx, cancel := context.WithCancel(ctx)
	if cfg.DefaultPolicy.Rows <= 0 {
		cfg = defaultConfig()
	}
	if cfg.CodeIndex.Rows <= 0 {
		cfg.CodeIndex = cfg.DefaultPolicy
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = indexQueuePollInterval
	}
	q := &Writer{
		ctx:        ctx,
		cancel:     cancel,
		handlers:   handlers,
		cfg:        cfg,
		ctrlCh:     make(chan any, 16),
		codeCmdCh:  make(chan any, indexQueueCapacity),
		otherCmdCh: make(chan any, indexQueueCapacity),
		doneCh:     make(chan struct{}),
	}
	go q.run()
	return q
}
