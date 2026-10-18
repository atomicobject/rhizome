package queryengine

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	semdb "github.com/atomicobject/rhizome/pkg/store"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEngineRejectsMismatchedAndChangedSchemas(t *testing.T) {
	schema := &ontoschema.Schema{Hash: "same"}
	exec, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	_, err = New(&ontoschema.Schema{Hash: "same"}, exec, ontologyquery.Deps{})
	require.ErrorContains(t, err, "does not match")
	engine, err := New(schema, exec, ontologyquery.Deps{})
	require.NoError(t, err)
	prepared, errs := ontologyquery.Prepare(exec, `{ __schema { queryType { name } } }`)
	require.Empty(t, errs)
	result, err := engine.Run(context.Background(), prepared)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.NotEmpty(t, result.Data["__schema"])
	schema.Hash = "changed"
	_, err = engine.Run(context.Background(), prepared)
	require.ErrorContains(t, err, "changed after compilation")
}

func TestDependenciesRejectsDifferentStoresAndVaultsAndPreservesExactPorts(t *testing.T) {
	root := t.TempDir()
	vault := obsidian.VaultDefinition{Path: root}
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	base := ontologyquery.Deps{VaultDef: vault, Store: store, PathOwner: func(string) ontologyquery.PathOwner { return ontologyquery.PathOwnerCode }}
	_, err = Dependencies(base, unifiedsearch.Options{VaultDef: vault, IntelStore: &semdb.Store{}})
	require.ErrorContains(t, err, "stores differ")
	_, err = Dependencies(base, unifiedsearch.Options{VaultDef: obsidian.VaultDefinition{Path: t.TempDir()}, IntelStore: store})
	require.ErrorContains(t, err, "vaults differ")
	deps, err := Dependencies(base, unifiedsearch.Options{VaultPath: root, VaultDef: vault, IntelStore: store})
	require.NoError(t, err)
	require.Equal(t, ontologyquery.PathOwnerCode, deps.PathOwner("anything"))
	require.Same(t, store, deps.Store)
	require.NotNil(t, deps.NoteSearcher)
	require.Nil(t, deps.SemanticSearcher)
}

func TestDependenciesSearchUsesConfiguredNoteRuntime(t *testing.T) {
	root := t.TempDir()
	vault := obsidian.VaultDefinition{Path: root}
	store, err := semdb.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	formats, err := testkit.NoteMetadataIndexer(t).FormatRuntime()
	require.NoError(t, err)
	deps, err := Dependencies(ontologyquery.Deps{VaultDef: vault, Store: store, NoteFormats: formats}, unifiedsearch.Options{
		VaultPath: root, VaultDef: vault, IntelStore: store,
		NoteProvider: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
	})
	require.NoError(t, err)
	result, err := deps.NoteSearcher.SearchNotes(context.Background(), ontologyquery.NoteSearchRequest{Queries: []string{"missing evidence"}, First: 1})
	require.NoError(t, err)
	require.Empty(t, result.Hits)
}
