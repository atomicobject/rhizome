package operations

import (
	"context"

	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func loadPersistedGraphSnapshotWithStore(vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, noteMetadata notemeta.Indexer, store *semdb.Store, fallback MetadataStoreFallbackPolicy) (*vaultgraph.GraphSnapshot, func(), error) {
	if err := noteMetadata.Validate(); err != nil {
		return nil, func() {}, err
	}
	cleanup := func() {}
	var err error
	if store == nil && fallback.allowsStoreOpen() {
		store, cleanup, err = openMetadataStore(vaultDef)
	}
	if err != nil || store == nil {
		return nil, cleanup, err
	}
	if !fallback.allowsStoreOpen() {
		// The caller supplied this managed reader after accepting index readiness.
		// Do not rediscover live notes: this path cannot repair or open a store.
		graphSnapshot, err := notemeta.LoadReadyPersistedGraphSnapshot(context.Background(), store)
		if err != nil {
			return nil, cleanup, err
		}
		return graphSnapshot, cleanup, nil
	}
	graphSnapshot, err := noteMetadata.LoadPersistedGraphSnapshot(context.Background(), vaultDef, note, store)
	if err != nil {
		return nil, cleanup, err
	}
	return graphSnapshot, cleanup, nil
}
