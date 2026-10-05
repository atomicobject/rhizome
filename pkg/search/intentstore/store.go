package intentstore

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

// EmbeddingRecord represents a stored intent exemplar embedding.
type EmbeddingRecord struct {
	Intent     string
	Exemplar   string
	Embedding  embeddings.Embedding
	Dimensions int
}

// Snapshot is the complete persisted intent embedding corpus and the provider
// configuration that produced it.
type Snapshot struct {
	ProviderFingerprint string
	Rows                []EmbeddingRecord
}

// Reader loads stored intent embeddings.
type Reader interface {
	IntentEmbeddings(ctx context.Context) ([]EmbeddingRecord, error)
}

// SnapshotReader loads the complete intent embedding snapshot, including its
// provider provenance.
type SnapshotReader interface {
	IntentEmbeddingSnapshot(ctx context.Context) (Snapshot, error)
}

// Writer persists intent embeddings.
type Writer interface {
	ReplaceIntentEmbeddingSnapshot(ctx context.Context, snapshot Snapshot) error
}

// Store combines read/write access for intent embeddings.
type Store interface {
	Reader
	SnapshotReader
	Writer
}
