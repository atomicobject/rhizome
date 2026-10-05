package indexwriter

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/search/graphdb"
	"github.com/atomicobject/rhizome/pkg/search/intentstore"
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
	indexQueueSnapshotName       = "queue.depth"
	indexQueueCodeDepthName      = "queue.code_depth"
	indexQueueOtherDepthName     = "queue.other_depth"
	indexQueueCodeWaitName       = "queue.code_wait"
	indexQueueOtherWaitName      = "queue.other_wait"
	indexQueueDefaultPhaseLabel  = "_"
	noteMetadataQueueDepthName   = "validation.writeback.metadata.queue_depth"
	noteMetadataFlushCountName   = "validation.writeback.metadata.flush.count"
	noteMetadataBatchRowsName    = "validation.writeback.metadata.batch_rows"
	noteMetadataBatchBytesName   = "validation.writeback.metadata.batch_bytes"
	noteMetadataFlushLatencyName = "validation.writeback.metadata.flush"
)

type FlushPolicy struct {
	Rows  int
	Bytes int
	Idle  time.Duration
}

type Config struct {
	DefaultPolicy FlushPolicy
	CodeIndex     FlushPolicy
	ItemEmbed     FlushPolicy
	IntelEmbed    FlushPolicy
	PollInterval  time.Duration
	CodePriority  time.Duration
	DeferScale    int
}

func DefaultConfig() Config {
	return Config{
		DefaultPolicy: FlushPolicy{
			Rows:  indexQueueFlushRows,
			Bytes: indexQueueFlushBytes,
			Idle:  indexQueueFlushIdle,
		},
		CodeIndex: FlushPolicy{
			Rows:  indexQueueCodeFlushRows,
			Bytes: indexQueueCodeFlushBytes,
			Idle:  indexQueueCodeFlushIdle,
		},
		ItemEmbed: FlushPolicy{
			Rows:  indexQueueItemEmbedRows,
			Bytes: indexQueueItemEmbedBytes,
			Idle:  indexQueueItemEmbedIdle,
		},
		IntelEmbed: FlushPolicy{
			Rows:  indexQueueIntelEmbedRows,
			Bytes: indexQueueIntelEmbedBytes,
			Idle:  indexQueueIntelEmbedIdle,
		},
		PollInterval: indexQueuePollInterval,
		CodePriority: indexQueueCodePriorityWindow,
		DeferScale:   indexQueueSemanticDeferScale,
	}
}

func (c Config) codePriorityActive(now, lastCodeActivity time.Time, pendingCode int) bool {
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

func (c Config) semanticUrgent(policy FlushPolicy, rows, bytes int) bool {
	// Deferred semantic writes still need an escape hatch. Without this, writer
	// priority can turn into unbounded backlog during large code scans.
	scale := c.DeferScale
	if scale < 2 {
		scale = 2
	}
	return rows >= policy.Rows*scale || bytes >= policy.Bytes*scale
}

type Handlers struct {
	ApplyGraphScores                   func(context.Context, graphdb.DerivedScores) error
	AckDerivedWork                     func(context.Context, []codeanchor.DerivedWork) error
	ApplyStructuralFinalize            func(context.Context, StructuralFinalize) error
	MarkDerivedDirty                   func(context.Context, []codeanchor.DerivedScope) ([]codeanchor.DerivedWork, error)
	ActivateDerivedWork                func(context.Context, []codeanchor.DerivedWork) error
	ApplyIntentEmbeddingSnapshot       func(context.Context, intentstore.Snapshot) error
	ApplyCodeIndexBatch                func(context.Context, []codeanchor.CodeIndexWork) error
	ApplyNoteIndexBatch                func(context.Context, []codeanchor.NoteIndexWork) error
	ApplyNoteMetaBatch                 func(context.Context, []embeddings.NoteFileInfo) error
	ApplyCodeItemEmbeddingBatch        func(context.Context, []codeindex.ItemEmbeddingUpsert) error
	ApplyCodeItemChunkBatch            func(context.Context, []codeindex.ItemChunksUpsert) error
	ApplyCodeItemChunks                func(context.Context, codeindex.AnchorID, []codeindex.ChunkInput, []string, []embeddings.Embedding) error
	ApplyNoteChunkSyncBatch            func(context.Context, []embeddings.NoteChunkSync) error
	ApplyNoteChunkSync                 func(context.Context, embeddings.NoteChunkSync) error
	ApplyNoteChunkBatch                func(context.Context, []embeddings.NoteChunksUpsert) error
	ApplyNoteChunks                    func(context.Context, embeddings.NoteID, []embeddings.ChunkInput, []embeddings.Embedding) error
	ApplyIntelChunks                   func(context.Context, []string, []codeanchor.IntelChunk) error
	ApplyIntelChunksByFamily           func(context.Context, []string, string, []codeanchor.IntelChunk) error
	ApplyIntelEmbeddings               func(context.Context, map[string]embeddings.Embedding) error
	ApplyOntologyNodeReadModel         func(context.Context, codeanchor.IntelOntologyNodeReadModel) error
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
	cfg      Config

	afterCodeIndexBatch func(context.Context, []codeanchor.CodeIndexWork) error
	afterNoteIndexBatch func(context.Context, []codeanchor.NoteIndexWork) error

	ctrlCh     chan any
	codeCmdCh  chan any
	otherCmdCh chan any
	doneCh     chan struct{}

	mu  sync.Mutex
	err error

	// Control ops wait for all payload submits that started before them to
	// settle, but they must not serialize unrelated producer sends.
	submitSeq      atomic.Uint64
	settleMu       sync.Mutex
	settledThrough uint64
	settledPending map[uint64]struct{}
}

type queueFlush struct {
	barrier uint64
	ack     chan error
}

type queueClose struct {
	barrier uint64
	ack     chan error
}

type queueOwnershipTransitions struct {
	barrier     uint64
	transitions []semdb.OwnershipTransition
	ack         chan ownershipTransitionAck
}

type ownershipTransitionAck struct {
	result semdb.OwnershipTransitionResult
	err    error
}

type queueAcknowledgeOwnershipReconciliation struct {
	barrier    uint64
	generation int64
	ack        chan error
}

type noteChunkWrite struct {
	noteID embeddings.NoteID
	chunks []embeddings.ChunkInput
	vecs   []embeddings.Embedding
}

type noteChunkSyncWrite struct {
	item embeddings.NoteChunkSync
}

func New(ctx context.Context, handlers Handlers) *Writer {
	return NewWithConfig(ctx, handlers, DefaultConfig())
}

// WHY: SQLite write concurrency is the wrong scaling axis for indexing. This
// lane preserves one durable writer while letting file workers and semantic
// provider lanes run ahead under bounded, observable backpressure.
// Docs: [[indexing-pipeline-architecture#^spec-0012-us1-ac1]]
func NewWithConfig(ctx context.Context, handlers Handlers, cfg Config) *Writer {
	ctx, cancel := context.WithCancel(ctx)
	if cfg.DefaultPolicy.Rows <= 0 {
		cfg = DefaultConfig()
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
