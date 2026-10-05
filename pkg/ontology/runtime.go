package ontology

import (
	"context"
	"errors"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type Runtime struct {
	Schema *Schema
	Store  *semdb.Store
	Issues []ValidationIssue
	Ready  bool
}

// PublishedRuntimeWithStore returns a runtime only when the caller's already
// published note metadata and ontology projection form one current, complete
// read model. It is deliberately read-only: indexing coordinators call it
// after their writer barrier and it never discovers notes, loads note sources,
// projects, or repairs persisted state.
func PublishedRuntimeWithStore(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, store *semdb.Store) (*Runtime, error) {
	if store == nil {
		return &Runtime{}, nil
	}
	runtime := &Runtime{Store: store}
	if vaultDef.BasePath() == "" {
		return runtime, nil
	}

	state, err := store.GetOntologySchemaState(ctx)
	if err != nil {
		return runtime, err
	}
	runtime.Issues = ValidationIssuesFromJSON(state.ErrorJSON)
	if err := validateOntologyMaterializationVersion(state.MaterializationVersion); err != nil {
		return runtime, err
	}

	schema, err := LoadSchema(vaultDef.BasePath())
	if err != nil {
		if errors.Is(err, ErrNoOntologyFiles) {
			return runtime, nil
		}
		return runtime, err
	}
	noteState, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return runtime, err
	}
	if !noteState.Ready || noteState.LoadedAt == 0 ||
		!state.Ready || state.LoadedAt == 0 ||
		state.SchemaHash != schema.Hash ||
		state.NotesHash != noteState.NotesHash ||
		state.LoadedAt != noteState.LoadedAt ||
		state.MaterializationVersion != OntologyMaterializationVersion {
		return runtime, nil
	}
	materialized, err := ontologyStateMaterialized(ctx, noteMetadata, store, schema, state.ErrorJSON)
	if err != nil {
		return runtime, err
	}
	if !materialized {
		return runtime, nil
	}
	runtime.Schema = schema
	runtime.Ready = true
	return runtime, nil
}

func OpenStoreForWrite(vaultPath string) (*semdb.Store, func(), error) {
	cfg, err := obsidian.LoadCodeConfig(vaultPath)
	if err != nil {
		return nil, nil, err
	}
	return obsidian.OpenIntelStoreForWriteFromConfig(vaultPath, cfg)
}

func EnsureRuntime(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader) (*Runtime, func(), error) {
	if vaultDef.BasePath() == "" {
		return &Runtime{}, nil, nil
	}
	if _, err := LoadSchema(vaultDef.BasePath()); errors.Is(err, ErrNoOntologyFiles) {
		return &Runtime{}, nil, nil
	}
	if err := noteMetadata.Validate(); err != nil {
		return nil, nil, err
	}

	store, cleanup, err := OpenStoreForWrite(vaultDef.BasePath())
	if err != nil {
		return nil, nil, err
	}
	rt, ensureErr := EnsureRuntimeWithStore(ctx, noteMetadata, vaultDef, noteMgr, store)
	return rt, cleanup, ensureErr
}

func EnsureFreshRuntime(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader) (*Runtime, func(), error) {
	if vaultDef.BasePath() == "" {
		return &Runtime{}, nil, nil
	}
	if _, err := LoadSchema(vaultDef.BasePath()); errors.Is(err, ErrNoOntologyFiles) {
		return &Runtime{}, nil, nil
	}
	if err := noteMetadata.Validate(); err != nil {
		return nil, nil, err
	}

	store, cleanup, err := OpenStoreForWrite(vaultDef.BasePath())
	if err != nil {
		return nil, nil, err
	}
	rt, ensureErr := EnsureFreshRuntimeWithStore(ctx, noteMetadata, vaultDef, noteMgr, store)
	return rt, cleanup, ensureErr
}

func EnsureRuntimeWithStore(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store) (*Runtime, error) {
	if store == nil {
		return &Runtime{}, nil
	}
	if err := noteMetadata.Validate(); err != nil {
		return nil, err
	}
	result, err := EnsureIndexed(ctx, noteMetadata, vaultDef, noteMgr, store)
	if err != nil {
		state, stateErr := store.GetOntologySchemaState(ctx)
		if stateErr == nil {
			return &Runtime{
				Store:  store,
				Issues: ValidationIssuesFromJSON(state.ErrorJSON),
			}, err
		}
		return &Runtime{Store: store}, err
	}
	return &Runtime{
		Schema: result.Schema,
		Store:  store,
		Issues: result.Issues,
		Ready:  result.Schema != nil,
	}, nil
}

func EnsureFreshRuntimeWithStore(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store) (*Runtime, error) {
	if store == nil {
		return &Runtime{}, nil
	}
	if err := noteMetadata.Validate(); err != nil {
		return nil, err
	}
	if _, err := noteMetadata.EnsureIndexed(ctx, vaultDef, noteMgr, store); err != nil {
		return &Runtime{Store: store}, err
	}
	return EnsureRuntimeWithStore(ctx, noteMetadata, vaultDef, noteMgr, store)
}
