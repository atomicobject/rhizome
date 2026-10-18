// Package ontologytest prepares published ontology fixtures through the writer queue.
package ontologytest

import (
	"context"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func Publish(ctx context.Context, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader, store *semdb.Store) (runtime *ontology.Runtime, err error) {
	lease, acquired, err := indexlock.TryAcquireLease(vault.BasePath(), indexlock.AcquireOptions{Role: "test/ontology-publication"})
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("fixture vault writer is already active")
	}
	defer func() { err = errors.Join(err, lease.Release()) }()
	return PublishHeld(ctx, lease, metadata, vault, reader, store)
}

func PublishHeld(ctx context.Context, lease *indexlock.Lease, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader, store *semdb.Store) (runtime *ontology.Runtime, err error) {
	if _, err := SyncHeld(ctx, lease, metadata, vault, reader, store, nil, nil); err != nil {
		return nil, err
	}
	return ontology.PublishedRuntimeWithStore(ctx, metadata, vault, store)
}

func Sync(ctx context.Context, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader, store *semdb.Store, changed, deleted []string) (result *ontology.SyncResult, err error) {
	lease, acquired, err := indexlock.TryAcquireLease(vault.BasePath(), indexlock.AcquireOptions{Role: "test/ontology-publication"})
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, fmt.Errorf("fixture vault writer is already active")
	}
	defer func() { err = errors.Join(err, lease.Release()) }()
	return SyncHeld(ctx, lease, metadata, vault, reader, store, changed, deleted)
}

func SyncHeld(ctx context.Context, lease *indexlock.Lease, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader, store *semdb.Store, changed, deleted []string) (result *ontology.SyncResult, err error) {
	if err := lease.Validate(vault.BasePath()); err != nil {
		return nil, err
	}
	if _, err := metadata.EnsureIndexed(ctx, vault, reader, store); err != nil {
		return nil, err
	}
	queue := indexwriter.New(ctx, indexwriter.Handlers{ApplyOntologyDelta: store.ApplyOntologyDeltaPreservingSemantic})
	defer func() { err = errors.Join(err, queue.Close()) }()
	result, err = ontology.SyncPublishedPaths(ctx, metadata, vault, reader, store, queue, changed, deleted)
	if err != nil {
		return nil, err
	}
	if err := queue.FlushAndWait(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func OpenHeld(ctx context.Context, lease *indexlock.Lease, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader) (*ontology.Runtime, func(), error) {
	if err := lease.Validate(vault.BasePath()); err != nil {
		return nil, nil, err
	}
	store, cleanup, err := indexstore.Open(vault.BasePath(), indexstore.Options{Access: indexstore.ReadWrite, Create: true})
	if err != nil {
		return nil, nil, err
	}
	runtime, err := PublishHeld(ctx, lease, metadata, vault, reader, store)
	return runtime, cleanup, err
}

func Open(ctx context.Context, metadata notemeta.Indexer, vault obsidian.VaultDefinition, reader obsidian.NoteReader) (runtime *ontology.Runtime, cleanup func(), err error) {
	lease, acquired, err := indexlock.TryAcquireLease(vault.BasePath(), indexlock.AcquireOptions{Role: "test/ontology-publication"})
	if err != nil {
		return nil, nil, err
	}
	if !acquired {
		return nil, nil, fmt.Errorf("fixture vault writer is already active")
	}
	defer func() { err = errors.Join(err, lease.Release()) }()
	return OpenHeld(ctx, lease, metadata, vault, reader)
}
