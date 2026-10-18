package ontology

import (
	"context"
	"errors"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type Runtime struct {
	Schema *ontoschema.Schema
	Store  *semdb.Store
	Issues []ValidationIssue
	Ready  bool
}

// PublishedRuntimeWithStore reads the singleton proof published by complete sync.
// It never discovers sources, materializes rows, or repairs a stale index.
func PublishedRuntimeWithStore(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, store *semdb.Store) (*Runtime, error) {
	runtime := &Runtime{Store: store}
	if store == nil || vaultDef.BasePath() == "" {
		return runtime, nil
	}
	formats, err := noteMetadata.FormatRuntime()
	if err != nil {
		return runtime, err
	}
	publication, err := store.GetOntologyReadiness(ctx, OntologyProviderHash(formats))
	if err != nil {
		return runtime, err
	}
	state := publication.SchemaState
	runtime.Issues = ValidationIssuesFromJSON(state.ErrorJSON)
	if err := validateOntologyMaterializationVersion(state.MaterializationVersion); err != nil {
		return runtime, err
	}
	schema, err := ontoschema.LoadSchema(vaultDef.BasePath())
	if errors.Is(err, ontoschema.ErrNoOntologyFiles) {
		return runtime, nil
	}
	if err != nil {
		return runtime, err
	}
	if !publication.Ready || state.SchemaHash != schema.Hash ||
		state.MaterializationVersion != OntologyMaterializationVersion ||
		state.LoadedAt != publication.MetadataState.LoadedAt {
		return runtime, nil
	}
	runtime.Schema = schema
	runtime.Ready = true
	return runtime, nil
}

// OpenPublishedRuntime opens an existing index without schema or data writes.
func OpenPublishedRuntime(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition) (*Runtime, func(), error) {
	root := vaultDef.BasePath()
	if root == "" {
		return &Runtime{}, nil, nil
	}
	if _, err := ontoschema.LoadSchema(root); errors.Is(err, ontoschema.ErrNoOntologyFiles) {
		return &Runtime{}, nil, nil
	} else if err != nil {
		return &Runtime{}, nil, err
	}
	resolvedConfig1, configErr1 := repoconfig.Load(root)
	cfg, err := resolvedConfig1.Code, configErr1
	if err != nil {
		return nil, nil, err
	}
	store, err := semdb.OpenReadOnlyExisting(repoconfig.UnifiedIndexPath(root, cfg.IndexPath), ctx, sqliteutil.Options{})
	if errors.Is(err, os.ErrNotExist) {
		return &Runtime{}, nil, fmt.Errorf("ontology index is not ready; run `rzm index`")
	}
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = store.Close() }
	runtime, err := PublishedRuntimeWithStore(ctx, noteMetadata, vaultDef, store)
	if err == nil && !runtime.Ready {
		err = fmt.Errorf("ontology index is not ready; run `rzm index`")
	}
	return runtime, cleanup, err
}
