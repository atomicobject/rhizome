package actions

import (
	"context"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func loadPersistedGraphSnapshotWithStore(vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, noteMetadata notemeta.Indexer, store *semdb.Store, fallback MetadataStoreFallbackPolicy) (*obsidian.GraphSnapshot, func(), error) {
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
