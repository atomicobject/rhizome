package operations

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AllocateNextID allocates from the current published identifier pool.
func AllocateNextID(ctx context.Context, vaultDef obsidian.VaultDefinition, indexer notemeta.Indexer, store *semdb.Store, request idalloc.Request) (*idalloc.Result, error) {
	runtime, err := ontology.PublishedRuntimeWithStore(ctx, indexer, vaultDef, store)
	if err != nil {
		return nil, err
	}
	if runtime == nil || runtime.Schema == nil || runtime.Store == nil {
		return nil, idalloc.ErrSchemaMissing
	}
	if request.Count > idalloc.MaxBatchCount {
		request.Count = idalloc.MaxBatchCount
	}
	return idalloc.AllocateRequest(ctx, runtime.Schema, runtime.Store, request)
}
