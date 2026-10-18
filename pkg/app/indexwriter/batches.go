package indexwriter

import (
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

type queuedPayload struct {
	value any
	bytes int
	phase string
}

type codeItemEmbeddingBatchWrite struct {
	items []codeindex.ItemEmbeddingUpsert
}

type codeChunkBatchWrite struct {
	items []codeindex.ItemChunksUpsert
}

type intelChunkWrite struct {
	ownerIDs []string
	family   string
	chunks   []codeanchor.IntelChunk
}

type intelEmbeddingWrite struct {
	rows map[string]embeddings.Embedding
}

type ontologyNodeWrite struct {
	model readmodel.NodeReadModel
}

type ontologyNodeStateWrite struct {
	states []codeanchor.IntelOntologyNodeEmbeddingState
}

type ontologyDeltaWrite struct {
	delta semdb.OntologyDelta
}

type noteMetadataDeltaWrite struct {
	delta semdb.NoteMetadataDelta
}

type ValidationStateWrite struct {
	Snapshot     semdb.ValidationSnapshot
	ErrorMessage string
	DurationMs   int64
}

type queueBatch[T any] struct {
	phase     string
	items     []T
	rows      int
	bytes     int
	updatedAt time.Time
}

func (b *queueBatch[T]) addAll(items []T, rows, bytes int, now time.Time) {
	b.items = append(b.items, items...)
	b.rows += rows
	b.bytes += bytes
	b.updatedAt = now
}

type intelChunkBatch struct {
	family   string
	ownerIDs []string
	chunks   []codeanchor.IntelChunk
	rows     int
	bytes    int
}

func (b *intelChunkBatch) add(ownerIDs []string, chunks []codeanchor.IntelChunk, bytes int) {
	b.ownerIDs = append(b.ownerIDs, ownerIDs...)
	b.chunks = append(b.chunks, chunks...)
	b.rows += len(chunks)
	b.bytes += bytes
}
