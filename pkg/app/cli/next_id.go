package actions

import (
	"context"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AllocateNextID refreshes the ontology projection before allocating from the
// current identifier pool.
func AllocateNextID(ctx context.Context, vaultDef obsidian.VaultDefinition, indexer notemeta.Indexer, store *semdb.Store, request idalloc.Request) (*idalloc.Result, error) {
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, indexer, vaultDef, &obsidian.Note{}, store)
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
