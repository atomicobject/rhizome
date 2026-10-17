package embedindex

// Docs:
// - [Indexing pipeline - Live updating (watcher runtime)](docs/reference/analysis/Indexing pipeline - Live updating (watcher runtime).md)
// - [Rhizome semantic code index spine](docs/specs/technical/semantic-code-index-spine.md)

import (
	"context"

	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
)

// IntelSource provides read access to intel rows for semantic chunking.
type IntelSource interface {
	IntelAnchors(ctx context.Context) ([]codeanchor.IntelAnchor, error)
	IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error)
	IntelEdges(ctx context.Context) ([]codeanchor.IntelEdge, error)
}

// RationaleSource optionally exposes the inline rationale comments extracted for a
// file at index time. Chunk synthesis embeds them so prose "why" questions can
// reach code whose explanation lives inside function bodies.
type RationaleSource interface {
	RationaleForPath(ctx context.Context, path string) ([]codeanchor.Rationale, error)
}

// pathRationale fetches a file's rationale comments at most once, and only when a
// chunk actually needs them, so files whose chunks carry no rationale cost no query.
// Not safe for concurrent use; each plan worker owns one per file.
type pathRationale struct {
	load   func() []codeanchor.Rationale
	rows   []codeanchor.Rationale
	loaded bool
}

// inSpan returns the rationale explaining a single anchor, loading the file's
// rationale on first use.
func (c *pathRationale) inSpan(anchor codeanchor.IntelAnchor) []codeanchor.Rationale {
	if !c.loaded {
		c.loaded = true
		if c.load != nil {
			c.rows = c.load()
		}
	}
	return RationaleInSpan(anchor, c.rows)
}

// rationaleFor returns a lazy per-file rationale accessor. A missing or failing
// lookup yields no rationale rather than failing indexing.
func (s *Syncer) rationaleFor(ctx context.Context, path string) pathRationale {
	return pathRationale{load: func() []codeanchor.Rationale {
		src, ok := s.Intel.(RationaleSource)
		if !ok {
			return nil
		}
		rows, err := src.RationaleForPath(ctx, path)
		if err != nil {
			return nil
		}
		return rows
	}}
}

// IntelAnchorMetaSource provides lightweight intel anchor metadata for fast planning.
type IntelAnchorMetaSource interface {
	IntelAnchorMetas(ctx context.Context) ([]codeanchor.IntelAnchorMeta, error)
}

// IntelEmbeddingStateSource provides intel chunk hashes for fast embed skips.
type IntelEmbeddingStateSource interface {
	IntelChunkEmbeddingStates(ctx context.Context, ownerIDs []string) (map[string]map[int]string, error)
}

type codeEmbeddingBatchSource interface {
	EmbeddingByHashes(ctx context.Context, hashes []string) (map[string]embeddings.Embedding, error)
}

type codeFingerprintVersionStore interface {
	SetFingerprintVersion(ctx context.Context, version int) error
}

// ChunkWriter persists intel chunk records for deterministic ordering and provenance.
// When set on the Syncer, chunks are recorded prior to embedding.
type ChunkWriter interface {
	ReplaceIntelChunks(ctx context.Context, ownerIDs []string, chunks []codeanchor.IntelChunk) error
}

type ChunkFamilyWriter interface {
	ReplaceIntelChunksByFamily(ctx context.Context, ownerIDs []string, family string, chunks []codeanchor.IntelChunk) error
}

// EmbeddingWriter persists embeddings keyed by chunk_id.
// When set on the Syncer, embeddings are written after chunk records.
type EmbeddingWriter interface {
	UpsertEmbeddings(ctx context.Context, embeddings map[string]embeddings.Embedding) error
}

// SyncWriteQueue centralizes high-volume semantic write operations behind a
// single writer lane during batch indexing.
type SyncWriteQueue interface {
	CodeWritebackBatchConfig() embeddings.CodeWritebackBatchConfig
	SubmitCodeItemEmbedding(context.Context, codeindex.ItemEmbeddingUpsert) error
	SubmitCodeItemEmbeddingBatch(context.Context, []codeindex.ItemEmbeddingUpsert) error
	SubmitCodeItemChunks(context.Context, codeindex.AnchorID, []codeindex.ChunkInput, []string, []embeddings.Embedding) error
	SubmitCodeItemChunkBatch(context.Context, []codeindex.ItemChunksUpsert) error
	SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error
	SubmitIntelChunksByFamily(context.Context, []string, string, []codeanchor.IntelChunk) error
	SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error
	FlushAndWait(context.Context) error
}

type intelEmbeddingQueue interface {
	SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error
}

type intelEmbeddingSubmitter interface {
	Submit(map[string]embeddings.Embedding) error
	Close() error
}

type queueEmbeddingWriter struct {
	queue intelEmbeddingQueue
}

func (w queueEmbeddingWriter) UpsertEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	if w.queue == nil {
		return nil
	}
	return w.queue.SubmitIntelEmbeddings(ctx, rows)
}

type directIntelEmbeddingSubmitter struct {
	ctx    context.Context
	writer EmbeddingWriter
}

func (w directIntelEmbeddingSubmitter) Submit(rows map[string]embeddings.Embedding) error {
	if w.writer == nil {
		return nil
	}
	return w.writer.UpsertEmbeddings(w.ctx, rows)
}

func (directIntelEmbeddingSubmitter) Close() error { return nil }

// FileLoader reads file content given a repository-relative path.
type FileLoader func(relPath string) ([]byte, error)

// Syncer keeps the code embeddings index in sync with intel anchors.
type Syncer struct {
	Index          codeindex.Index
	Provider       embeddings.Provider
	ProviderInfo   embeddings.ProviderConfig
	Intel          IntelSource
	Calls          CallLookup
	Root           string
	Loader         FileLoader
	Budget         ChunkBudget
	BatchSize      int
	MaxConcurrent  int
	PlanConcurrent int
	// CodeEmbedPacker configures staged code request packing.
	CodeEmbedPacker *EmbedPackerOptions
	// EmbedGate optionally limits aggregate EmbedTexts concurrency across syncers.
	EmbedGate     chan struct{}
	EmbeddingNode *SharedEmbeddingNode
	Policy        SynthesisPolicy
	OnProgress    func(format string, args ...any)
	// OnEmbedProgress reports embedding progress as completed/total items.
	// "Items" are module-level chunks + per-anchor chunks.
	OnEmbedProgress func(done, total int)

	// StaleItemThreshold controls lazy pruning of items not seen in current sync.
	// If the fraction of stale items is below this threshold, they're preserved
	// to avoid re-embedding costs when switching branches. Default is 0.3 (30%).
	// Set to 0 for immediate pruning (old behavior).
	StaleItemThreshold float64

	// ChunkWriter optionally persists intel chunk records prior to embedding.
	// When set, chunk ordering is recorded in the intel spine for determinism.
	ChunkWriter ChunkWriter

	// EmbeddingWriter optionally persists embeddings to the intel spine.
	// When set, embeddings are written keyed by chunk_id after embedding.
	EmbeddingWriter EmbeddingWriter

	// WriteQueue optionally routes hot write paths through a single writer lane.
	WriteQueue SyncWriteQueue

	setupMu                sync.Mutex
	schemaReady            bool
	metaDimsKnown          bool
	setupErr               error
	freshnessMu            sync.Mutex
	pendingSourceHighWater time.Time
}

type itemEmbedWrite struct {
	id   codeindex.AnchorID
	hash string
	vec  embeddings.Embedding
}

const itemEmbeddingBatchSize = 50

const codeFingerprintVersion = 3

// Sync performs a single synchronization pass.
// Sync plans and embeds code anchors. It observes ctx between the two phases
// so a cancelled indexing job stops before the expensive one (SPEC-0104 US3).
func (s *Syncer) Sync(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	plan, err := s.Plan(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Embed(ctx, plan)
}
