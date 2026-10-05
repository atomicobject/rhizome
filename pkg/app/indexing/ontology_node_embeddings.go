package indexing

import (
	"context"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type ontologyNodeEmbeddingSyncRequest struct {
	schema       *ontology.Schema
	changedPaths []string
	deletedPaths []string
	rebuilt      bool
}

func ontologyFollowupEmbeddingRequest(result *ontology.SyncResult, changedPaths, deletedPaths []string) (ontologyNodeEmbeddingSyncRequest, bool) {
	if result == nil || result.Schema == nil {
		return ontologyNodeEmbeddingSyncRequest{}, false
	}
	return ontologyNodeEmbeddingSyncRequest{
		schema:       result.Schema,
		changedPaths: append([]string(nil), changedPaths...),
		deletedPaths: append([]string(nil), deletedPaths...),
		rebuilt:      result.Rebuilt,
	}, true
}

func syncOntologyNodeEmbeddings(
	ctx context.Context,
	vaultDef obsidian.VaultDefinition,
	noteMetadata notemeta.Indexer,
	intelStore *semdb.Store,
	provider embeddings.Provider,
	providerCfg embeddings.ProviderConfig,
	schema *ontology.Schema,
	batchSize int,
	maxConcurrent int,
	embedGate chan struct{},
	embeddingNode *semantic.SharedEmbeddingNode,
	writeQueue indexwriter.SemanticWriteQueue,
	changedPaths []string,
	deletedPaths []string,
	rebuilt bool,
) error {
	if intelStore == nil || provider == nil || schema == nil {
		return nil
	}
	projectablePaths, err := ontology.ProjectableMetadataPaths(ctx, noteMetadata, intelStore)
	if err != nil {
		return err
	}
	paths := retainOntologyBodyPaths(changedPaths, projectablePaths)
	if rebuilt {
		// IMPORTANT: ontology rebuild changes structural/schema fingerprints for
		// semantic surface. Use the same projectable path set as ontology sync,
		// not all metadata paths, so descriptor-only formats never reach
		// Markdown ProjectNote.
		paths = projectablePaths
	}
	store := semantic.OntologyNodeStore(intelStore)
	if writeQueue != nil {
		store = ontologyBodyQueuedStore{readStore: intelStore, queue: writeQueue}
	}
	nodeSyncer := semantic.OntologyNodeSyncer{
		Store:         store,
		Provider:      provider,
		ProviderInfo:  providerCfg,
		Schema:        schema,
		BatchSize:     batchSize,
		MaxConcurrent: maxConcurrent,
		EmbedGate:     embedGate,
		EmbeddingNode: embeddingNode,
	}
	if len(paths) > 0 {
		sources, err := noteMetadata.LoadNoteSourceSnapshots(ctx, vaultDef, &obsidian.Note{}, intelStore, paths)
		if err != nil {
			return err
		}
		if err := nodeSyncer.SyncSourceSnapshots(ctx, sources); err != nil {
			return err
		}
	}
	if len(deletedPaths) > 0 {
		return nodeSyncer.SyncNotePaths(ctx, vaultDef, &obsidian.Note{}, nil, deletedPaths)
	}
	return nil
}

func retainOntologyBodyPaths(paths, projectable []string) []string {
	if len(paths) == 0 || len(projectable) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(projectable))
	for _, path := range projectable {
		allowed[path] = struct{}{}
	}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, ok := allowed[path]; ok {
			result = append(result, path)
		}
	}
	return result
}
