package indexing

import (
	"context"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

type ontologyBodyQueuedStore struct {
	readStore interface {
		OntologyNodeEmbeddingStatesByChunkIDs(context.Context, []string) (map[string]codeanchor.IntelOntologyNodeEmbeddingState, error)
		OntologyNodesByPaths(context.Context, []string) ([]codeanchor.IntelOntologyNode, error)
		OntologyNodeFieldValuesByNodeIDs(context.Context, []string, []string) ([]codeanchor.IntelOntologyNodeFieldValue, error)
		IntelChunksByOwners(context.Context, []string) ([]codeanchor.IntelChunk, error)
	}
	queue indexwriter.SemanticWriteQueue
}

func (s ontologyBodyQueuedStore) ReplaceOntologyNodes(ctx context.Context, notePaths []string, nodes []codeanchor.IntelOntologyNode) error {
	return s.queue.SubmitOntologyNodes(ctx, notePaths, nodes)
}

func (s ontologyBodyQueuedStore) ReplaceOntologyNodeReadModel(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel) error {
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

func (s ontologyBodyQueuedStore) OntologyNodesByPaths(ctx context.Context, paths []string) ([]codeanchor.IntelOntologyNode, error) {
	return s.readStore.OntologyNodesByPaths(ctx, paths)
}

func (s ontologyBodyQueuedStore) OntologyNodeFieldValuesByNodeIDs(ctx context.Context, nodeIDs []string, fieldNames []string) ([]codeanchor.IntelOntologyNodeFieldValue, error) {
	return s.readStore.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, fieldNames)
}

func (s ontologyBodyQueuedStore) IntelChunksByOwners(ctx context.Context, ownerIDs []string) ([]codeanchor.IntelChunk, error) {
	return s.readStore.IntelChunksByOwners(ctx, ownerIDs)
}
