package indexing

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
)

type ontologyBodyWriteQueue interface {
	SubmitOntologyNodes(context.Context, []string, []readmodel.NodeRow) error
	SubmitOntologyNodeReadModel(context.Context, readmodel.NodeReadModel) error
	SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error
	SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error
	SubmitOntologyNodeEmbeddingStates(context.Context, []codeanchor.IntelOntologyNodeEmbeddingState) error
}

type ontologyBodyQueuedStore struct {
	readStore interface {
		OntologyNodeEmbeddingStatesByChunkIDs(context.Context, []string) (map[string]codeanchor.IntelOntologyNodeEmbeddingState, error)
		OntologyNodesByPaths(context.Context, []string) ([]readmodel.NodeRow, error)
		OntologyNodeFieldValuesByNodeIDs(context.Context, []string, []string) ([]readmodel.FieldValueRow, error)
		IntelChunksByOwners(context.Context, []string) ([]codeanchor.IntelChunk, error)
	}
	queue ontologyBodyWriteQueue
}

func (s ontologyBodyQueuedStore) ReplaceOntologyNodes(ctx context.Context, notePaths []string, nodes []readmodel.NodeRow) error {
	return s.queue.SubmitOntologyNodes(ctx, notePaths, nodes)
}

func (s ontologyBodyQueuedStore) ReplaceOntologyNodeReadModel(ctx context.Context, model readmodel.NodeReadModel) error {
	return s.queue.SubmitOntologyNodeReadModel(ctx, model)
}

func (s ontologyBodyQueuedStore) ReplaceIntelChunks(ctx context.Context, ownerIDs []string, chunks []codeanchor.IntelChunk) error {
	return s.queue.SubmitIntelChunks(ctx, ownerIDs, chunks)
}

func (s ontologyBodyQueuedStore) UpsertEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	return s.queue.SubmitIntelEmbeddings(ctx, rows)
}

func (s ontologyBodyQueuedStore) UpsertOntologyNodeEmbeddingStates(ctx context.Context, states []codeanchor.IntelOntologyNodeEmbeddingState) error {
	return s.queue.SubmitOntologyNodeEmbeddingStates(ctx, states)
}

func (s ontologyBodyQueuedStore) OntologyNodeEmbeddingStatesByChunkIDs(ctx context.Context, chunkIDs []string) (map[string]codeanchor.IntelOntologyNodeEmbeddingState, error) {
	return s.readStore.OntologyNodeEmbeddingStatesByChunkIDs(ctx, chunkIDs)
}

func (s ontologyBodyQueuedStore) OntologyNodesByPaths(ctx context.Context, paths []string) ([]readmodel.NodeRow, error) {
	return s.readStore.OntologyNodesByPaths(ctx, paths)
}

func (s ontologyBodyQueuedStore) OntologyNodeFieldValuesByNodeIDs(ctx context.Context, nodeIDs []string, fieldNames []string) ([]readmodel.FieldValueRow, error) {
	return s.readStore.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, fieldNames)
}

func (s ontologyBodyQueuedStore) IntelChunksByOwners(ctx context.Context, ownerIDs []string) ([]codeanchor.IntelChunk, error) {
	return s.readStore.IntelChunksByOwners(ctx, ownerIDs)
}
