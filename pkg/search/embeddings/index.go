package embeddings

import (
	"context"
	"time"
)

// SimilarNote captures similarity results prior to graph re-ranking.
type SimilarNote struct {
	ID         NoteID
	Title      string
	Score      float64
	GraphScore float64
	FinalScore float64
	ChunkIndex int
	Breadcrumb string
	Heading    string
}

// NoteChunksUpsert batches chunk writes for a single note.
type NoteChunksUpsert struct {
	NoteID     NoteID
	Chunks     []ChunkInput
	Embeddings []Embedding
}

// NoteChunkSync batches chunk writes plus stale-chunk cleanup for a single note.
type NoteChunkSync struct {
	NoteID      NoteID
	Chunks      []ChunkInput
	Embeddings  []Embedding
	KeepIndices []int
}

// Index abstracts persistence and vector search for note embeddings.
type Index interface {
	EnsureSchema(ctx context.Context) error
	UpsertNoteMeta(ctx context.Context, info NoteFileInfo) error
	DeleteNotesNotIn(ctx context.Context, existingIDs []NoteID) error
	ListNotes(ctx context.Context) ([]NoteFileInfo, error)
	EmbeddingByHash(ctx context.Context, hash string) (Embedding, bool, error)
	CacheEmbedding(ctx context.Context, hash string, emb Embedding) error
	DeleteNote(ctx context.Context, id NoteID) error
	ChunkHashes(ctx context.Context, id NoteID) (map[int]string, error)
	UpsertNoteChunks(ctx context.Context, id NoteID, chunks []ChunkInput, embeddings []Embedding) error
	DeleteChunksNotIn(ctx context.Context, id NoteID, indices []int) error
	NoteChunks(ctx context.Context, id NoteID) ([]StoredChunk, error)
	SearchChunksByVector(ctx context.Context, query Embedding, k int) ([]SimilarChunk, int, error)
	SearchNotesByText(ctx context.Context, query string, k int) ([]SimilarNote, error)
	Metadata(ctx context.Context) (IndexMetadata, bool, error)
	ValidateOrInitMetadata(ctx context.Context, meta IndexMetadata) error
	UpdateLastSync(ctx context.Context, ts time.Time) error
	UpdateSourceHighWater(ctx context.Context, ts time.Time) error
	Stats(ctx context.Context) (notes int, chunks int, err error)
	Close() error
}

// LazyPruningNoteIndex extends Index with generation-based lazy pruning support.
// This allows notes to be preserved across branch switches and only pruned
// when stale notes exceed a configurable threshold.
type LazyPruningNoteIndex interface {
	Index
	// IncrementSyncGeneration bumps the sync generation. Call at start of sync.
	IncrementSyncGeneration(ctx context.Context) (int64, error)
	// CommitSyncGeneration makes the current sync generation query-visible after a successful sync.
	CommitSyncGeneration(ctx context.Context) error
	// PruneStaleNotes removes notes not seen in current generation if stale fraction > threshold.
	// Returns count of notes pruned.
	PruneStaleNotes(ctx context.Context, threshold float64) (int, error)
}

// NoteChunkSyncIndex extends Index with batched chunk sync operations that
// combine chunk upserts with stale-chunk cleanup in one write transaction.
type NoteChunkSyncIndex interface {
	Index
	SyncNoteChunks(ctx context.Context, item NoteChunkSync) error
	SyncNoteChunksBatch(ctx context.Context, items []NoteChunkSync) error
}
