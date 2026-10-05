package indexwriter

import (
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
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
	model codeanchor.IntelOntologyNodeReadModel
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

type pendingControl struct {
	graph                    *queueGraphScores
	structural               *queueStructuralFinalize
	derived                  *queueDerived
	barrier                  uint64
	close                    bool
	ack                      chan error
	transitions              []semdb.OwnershipTransition
	transitionAck            chan ownershipTransitionAck
	reconciliationGeneration int64
	reconciliationAck        chan error
}

type queueBatch[T any] struct {
	phase     string
	items     []T
	rows      int
	bytes     int
	updatedAt time.Time
}

func (b *queueBatch[T]) add(item T, rows, bytes int, now time.Time) {
	b.items = append(b.items, item)
	b.rows += rows
	b.bytes += bytes
	b.updatedAt = now
}

func (b *queueBatch[T]) addAll(items []T, rows, bytes int, now time.Time) {
	b.items = append(b.items, items...)
	b.rows += rows
	b.bytes += bytes
	b.updatedAt = now
}

type intelChunkBatch struct {
	phase     string
	family    string
	ownerIDs  []string
	chunks    []codeanchor.IntelChunk
	rows      int
	bytes     int
	updatedAt time.Time
}

func (b *intelChunkBatch) add(ownerIDs []string, chunks []codeanchor.IntelChunk, bytes int, now time.Time) {
	b.ownerIDs = append(b.ownerIDs, ownerIDs...)
	b.chunks = append(b.chunks, chunks...)
	b.rows += len(chunks)
	b.bytes += bytes
	b.updatedAt = now
}

type intelEmbeddingBatch struct {
	phase     string
	rowsMap   map[string]embeddings.Embedding
	rows      int
	bytes     int
	updatedAt time.Time
}

func (b *intelEmbeddingBatch) add(rows map[string]embeddings.Embedding, bytes int, now time.Time) {
	if b.rowsMap == nil {
		b.rowsMap = make(map[string]embeddings.Embedding, len(rows))
	}
	for key, row := range rows {
		b.rowsMap[key] = row
	}
	b.rows += len(rows)
	b.bytes += bytes
	b.updatedAt = now
}

type ontologyDeltaBatch struct {
	phase     string
	delta     semdb.OntologyDelta
	rows      int
	bytes     int
	updatedAt time.Time
}

func (b *ontologyDeltaBatch) add(delta semdb.OntologyDelta, bytes int, now time.Time) {
	semdb.MergeOntologyDelta(&b.delta, delta)
	b.rows += delta.RowCount()
	b.bytes += bytes
	b.updatedAt = now
}
