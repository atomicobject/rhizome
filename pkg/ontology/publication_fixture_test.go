package ontology

import (
	"context"
	"errors"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func ensureIndexedForTest(ctx context.Context, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader, store *semdb.Store) (*SyncResult, error) {
	return syncPathsForTest(ctx, metadata, vault, reader, store, nil, nil, nil)
}

func syncPathsForTest(ctx context.Context, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader, store *semdb.Store, queue DeltaWriteQueue, changed, deleted []string) (*SyncResult, error) {
	return publishOntologyFixture(ctx, metadata, vault, reader, store, queue, changed, deleted, true)
}

func syncPublishedPathsForTest(ctx context.Context, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader, store *semdb.Store, queue DeltaWriteQueue, changed, deleted []string) (*SyncResult, error) {
	return publishOntologyFixture(ctx, metadata, vault, reader, store, queue, changed, deleted, false)
}

// Keep source discovery explicit in fixtures. The production convergence boundary
// accepts only metadata already published by the writer that holds this lease.
func publishOntologyFixture(ctx context.Context, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader, store *semdb.Store, queue DeltaWriteQueue, changed, deleted []string, discover bool) (result *SyncResult, err error) {
	if store == nil {
		return &SyncResult{}, nil
	}
	if err := metadata.Validate(); err != nil {
		return nil, err
	}
	state, err := store.GetOntologySchemaState(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateOntologyMaterializationVersion(state.MaterializationVersion); err != nil {
		return nil, err
	}
	lease, acquired, err := indexlock.TryAcquireLease(vault.BasePath(), indexlock.AcquireOptions{Role: "test/ontology-publication"})
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("fixture vault writer is already active")
	}
	defer func() { err = errors.Join(err, lease.Release()) }()
	var schemaErr error
	if discover {
		_, schemaErr = ontoschema.LoadSchema(vault.BasePath())
		if schemaErr == nil {
			if _, err := metadata.EnsureIndexed(ctx, vault, reader, store); err != nil {
				return nil, err
			}
		}
	}
	if queue == nil {
		writer := indexwriter.New(ctx, indexwriter.Handlers{ApplyOntologyDelta: store.ApplyOntologyDeltaPreservingSemantic})
		defer func() { err = errors.Join(err, writer.Close()) }()
		queue = writer
	}
	result, err = SyncPublishedPaths(ctx, metadata, vault, reader, store, queue, changed, deleted)
	if err != nil {
		return nil, err
	}
	if barrier, ok := queue.(interface{ FlushAndWait(context.Context) error }); ok {
		if err := barrier.FlushAndWait(ctx); err != nil {
			return nil, err
		}
	}
	if schemaErr != nil && schemaErr != ontoschema.ErrNoOntologyFiles {
		return nil, schemaErr
	}
	return result, nil
}
