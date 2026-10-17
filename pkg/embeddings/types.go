// Package embeddings provides vector embedding types, providers, and indexing for semantic search.
//
// Docs: [Embeddings (Hub)](docs/hubs/Embeddings (Hub).md)
package embeddings

import "time"

// NoteID uniquely identifies a note, typically by vault-relative path.
type NoteID string

// NoteFileInfo captures filesystem metadata used to detect changes.
type NoteFileInfo struct {
	ID    NoteID
	Path  string
	Title string
	Size  int64
	Mtime time.Time
}

// Embedding is a dense vector representation of text.
type Embedding []float32

// ChunkInput is the text plus metadata to embed.
//
// IMPORTANT: When creating a ChunkInput, the Hash field MUST be computed from
// the Text field using HashText(text). This ensures cache coherency - the hash
// must match what's actually embedded so that cache lookups work correctly.
// Prefer using NewChunkInput() which handles this automatically.
type ChunkInput struct {
	Index      int
	Text       string
	Breadcrumb string
	Heading    string
	Hash       string
}

// NewChunkInput creates a ChunkInput with the hash computed from the text.
// This is the preferred way to create ChunkInputs to ensure cache coherency.
func NewChunkInput(index int, text, breadcrumb, heading string) ChunkInput {
	return ChunkInput{
		Index:      index,
		Text:       text,
		Breadcrumb: breadcrumb,
		Heading:    heading,
		Hash:       HashText(text),
	}
}

// StoredChunk represents a chunk that has already been embedded and stored.
type StoredChunk struct {
	Index      int
	Breadcrumb string
	Heading    string
	Embedding  Embedding
}

// SimilarChunk captures chunk-level similarity results.
type SimilarChunk struct {
	NoteID     NoteID
	Title      string
	ChunkIndex int
	Breadcrumb string
	Heading    string
	Score      float64
	GraphScore float64
	FinalScore float64
}

// IndexMetadata tracks persisted metadata about an embeddings index.
type IndexMetadata struct {
	Provider           string
	Model              string
	Dimensions         int
	SchemaVersion      int
	CreatedAt          time.Time
	LastSync           time.Time
	SourceHighWater    time.Time
	FingerprintVersion int
}
