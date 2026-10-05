package codeindex

import (
	"context"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// AnchorID identifies a code anchor or symbol.
type AnchorID string

// Item represents metadata for a code anchor stored in the code embeddings index.
type Item struct {
	AnchorID    AnchorID
	Lang        string
	Kind        string
	Path        string
	Symbol      string
	FQN         string
	Fingerprint string
	UpdatedAt   time.Time
}

// ItemEmbedding stores a single embedding for a code item (usually module/symbol chunk zero).
type ItemEmbedding struct {
	AnchorID  AnchorID
	Hash      string
	Embedding embeddings.Embedding
}

// ItemEmbeddingUpsert batches item-level embedding writes.
type ItemEmbeddingUpsert struct {
	AnchorID  AnchorID
	Hash      string
	Embedding embeddings.Embedding
}

// ItemChunksUpsert batches chunk writes for a single code anchor.
type ItemChunksUpsert struct {
	AnchorID   AnchorID
	Chunks     []ChunkInput
	Texts      []string
	Embeddings []embeddings.Embedding
}

// ItemEmbeddingState captures stored hashes for fast "needs embedding" decisions.
type ItemEmbeddingState struct {
	Fingerprint    string
	HasFingerprint bool
	ItemHash       string
	HasItemHash    bool
	ChunkHashes    map[int]string
}

// ChunkInput describes a chunk to embed for a code item.
type ChunkInput struct {
	Index       int
	Granularity string
	Breadcrumb  string
	Heading     string
	Hash        string
	StartByte   int
	EndByte     int
	StartLine   int
	EndLine     int
}

// StoredChunk is a persisted chunk embedding for a code item.
type StoredChunk struct {
	Index       int
	Granularity string
	Breadcrumb  string
	Heading     string
	Embedding   embeddings.Embedding
}

// SimilarChunk captures similarity results for chunk-level vector search.
type SimilarChunk struct {
	AnchorID    AnchorID
	Path        string
	Symbol      string
	FQN         string
	Kind        string
	Granularity string
	ChunkIndex  int
	Breadcrumb  string
	Heading     string
	Score       float64
}

// SimilarItem is the result of an item-level search.
type SimilarItem struct {
	AnchorID AnchorID
	Path     string
	Symbol   string
	FQN      string
	Kind     string
	Score    float64
}

// VectorSearchFilters narrows vector search to relevant paths/kinds before scoring.
type VectorSearchFilters struct {
	PathPrefixes []string
	Kinds        []string
	Granularity  []string
}

// FilteredChunkSearcher supports vector search with early filtering.
type FilteredChunkSearcher interface {
	SearchChunksByVectorFiltered(ctx context.Context, query embeddings.Embedding, k int, filters VectorSearchFilters) ([]SimilarChunk, int, error)
}

// Index abstracts persistence and vector search for code embeddings.
type Index interface {
	EnsureSchema(ctx context.Context) error
	ValidateOrInitMetadata(ctx context.Context, meta embeddings.IndexMetadata) error
	Metadata(ctx context.Context) (embeddings.IndexMetadata, bool, error)

	EmbeddingByHash(ctx context.Context, hash string) (embeddings.Embedding, bool, error)
	EmbeddingByHashes(ctx context.Context, hashes []string) (map[string]embeddings.Embedding, error)
	CacheEmbedding(ctx context.Context, hash string, emb embeddings.Embedding) error
	ClearEmbeddingCache(ctx context.Context) error
	UpsertItemMeta(ctx context.Context, item Item) error
	DeleteItemsNotIn(ctx context.Context, anchorIDs []AnchorID) error
	ListItems(ctx context.Context) ([]Item, error)

	GetItemEmbedding(ctx context.Context, anchorID AnchorID) (embeddings.Embedding, string, bool, error)
	UpsertItemEmbedding(ctx context.Context, anchorID AnchorID, contentHash string, emb embeddings.Embedding) error

	ItemFingerprint(ctx context.Context, anchorID AnchorID) (string, bool, error)
	ItemEmbeddingStates(ctx context.Context, anchorIDs []AnchorID) (map[AnchorID]ItemEmbeddingState, error)
	ChunkHashes(ctx context.Context, anchorID AnchorID) (map[int]string, error)
	UpsertItemChunks(ctx context.Context, anchorID AnchorID, chunks []ChunkInput, texts []string, embeddings []embeddings.Embedding) error
	DeleteChunksNotIn(ctx context.Context, anchorID AnchorID, indices []int) error
	ItemChunks(ctx context.Context, anchorID AnchorID) ([]StoredChunk, error)

	SearchChunksByVector(ctx context.Context, query embeddings.Embedding, k int) ([]SimilarChunk, int, error)
	SearchItemsByVector(ctx context.Context, query embeddings.Embedding, k int) ([]SimilarItem, int, error)
	SearchChunksByText(ctx context.Context, query string, k int) ([]SimilarChunk, error)

	// GetChunkBody retrieves the chunk body text from FTS for a given anchor and chunk index.
	// Returns empty string if not found. This avoids file I/O and includes doc comments.
	GetChunkBody(ctx context.Context, anchorID AnchorID, chunkIndex int) (string, error)

	UpdateLastSync(ctx context.Context, ts time.Time) error
	UpdateSourceHighWater(ctx context.Context, ts time.Time) error
	Stats(ctx context.Context) (items int, chunks int, err error)
	Close() error
}

// LazyPruningIndex extends Index with generation-based lazy pruning support.
// This allows items to be preserved across branch switches and only pruned
// when stale items exceed a configurable threshold.
type LazyPruningIndex interface {
	Index
	// IncrementSyncGeneration bumps the sync generation. Call at start of sync.
	IncrementSyncGeneration(ctx context.Context) (int64, error)
	// CommitSyncGeneration makes the current sync generation query-visible after a successful sync.
	CommitSyncGeneration(ctx context.Context) error
	// PruneStaleItems removes items not seen in current generation if stale fraction > threshold.
	// Returns count of items pruned.
	PruneStaleItems(ctx context.Context, threshold float64) (int, error)
}
