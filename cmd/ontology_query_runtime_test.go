package cmd

import (
	"context"
	"errors"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"testing"

	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestOntologyQueryLiveOptionsOmitBackgroundAndNonSemanticProviderWork(t *testing.T) {
	commandOptions := newRuntimeOptionsForTest(t)

	nonSemantic := commandOptions.ontologyQueryLiveOptions(oneshotruntime.OntologyQueryPlan(false, false).RuntimeRequirements())
	require.True(t, nonSemantic.SkipCacheWarmup)
	require.True(t, nonSemantic.DisableWatchHub)
	require.True(t, nonSemantic.DisableLeaderWork)
	require.True(t, nonSemantic.DisableSessionStore)
	require.False(t, nonSemantic.QueryProvidersOnly)
	require.True(t, nonSemantic.Requirements.Includes(bootstrap.RuntimeCapabilityCodeIndex))
	require.False(t, nonSemantic.Requirements.Includes(bootstrap.RuntimeCapabilitySearch))
	require.False(t, nonSemantic.Requirements.Includes(bootstrap.RuntimeCapabilitySemantic))
	require.False(t, nonSemantic.Requirements.Includes(bootstrap.RuntimeCapabilityLeaderSyncers))

	semantic := commandOptions.ontologyQueryLiveOptions(oneshotruntime.OntologyQueryPlan(true, false).RuntimeRequirements())
	require.False(t, semantic.QueryProvidersOnly)
	require.True(t, semantic.Requirements.Includes(bootstrap.RuntimeCapabilitySemantic))
}

func TestOntologyQueryReadinessPreservesExactCapabilityErrors(t *testing.T) {
	codeFailure := errors.New("code index unavailable: exact baseline detail")
	err := awaitOntologyQueryRuntime(context.Background(), &ontologyQueryTestRuntime{codeErr: codeFailure}, oneshotruntime.OntologyQueryPlan(false, false))
	require.ErrorIs(t, err, codeFailure)
	require.EqualError(t, err, "code index unavailable: exact baseline detail")

	semanticFailure := errors.New("semantic unavailable: exact baseline detail")
	err = awaitOntologyQueryRuntime(context.Background(), &ontologyQueryTestRuntime{semanticErr: semanticFailure}, oneshotruntime.OntologyQueryPlan(true, false))
	require.ErrorIs(t, err, semanticFailure)
	require.EqualError(t, err, "semantic unavailable: exact baseline detail")
}

type ontologyQueryTestRuntime struct {
	codeErr     error
	semanticErr error
}

func (r *ontologyQueryTestRuntime) WaitForSearch(context.Context) error    { return nil }
func (r *ontologyQueryTestRuntime) WaitForSemantic(context.Context) error  { return r.semanticErr }
func (r *ontologyQueryTestRuntime) WaitForCodeIndex(context.Context) error { return r.codeErr }
func (r *ontologyQueryTestRuntime) WaitForNoteCache(context.Context) error { return nil }
func (r *ontologyQueryTestRuntime) WaitForSession(context.Context) error   { return nil }
func (r *ontologyQueryTestRuntime) Close()                                 {}

func TestNonSemanticOntologyQueryRuntimeOmitsProviderSessionAndBackgroundOwnership(t *testing.T) {
	commandOptions := newRuntimeOptionsForTest(t)

	vault := setupAgentTestVault(t, nextIDFixtureFiles())
	commandOptions.vaultName = vault.name

	live, err := commandOptions.buildOntologyQueryLiveRuntime(vaultconfig.WithEnv(context.Background(), vault.env), oneshotruntime.OntologyQueryPlan(false, false))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, live.Close()) })

	snapshot := live.Snapshot()
	require.True(t, snapshot.Code.Ready)
	require.Error(t, snapshot.Semantic.Err)
	require.True(t, errors.Is(snapshot.Semantic.Err, bootstrap.ErrCapabilityNotRequested{Capability: bootstrap.RuntimeCapabilitySemantic}))
	require.Nil(t, snapshot.NoteProvider, "non-semantic queries must not construct a provider")
	require.Nil(t, snapshot.SessionStore, "one-shot ontology queries do not own session writes")
	require.Nil(t, snapshot.WatchHub, "one-shot ontology queries do not start watchhub work")
	require.False(t, snapshot.Leader, "one-shot ontology queries do not become leaders")

	runtime, err := ontologytest.Publish(vaultconfig.WithEnv(context.Background(), vault.env), live.NoteMetadataIndexer(), live.VaultDef, &obsidian.Note{}, live.IntelStore())
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.Same(t, live.IntelStore(), runtime.Store, "current projection must reuse the caller-owned Intel store")
}

func TestSemanticOntologyQueryRuntimePreservesMetadataInitializationOnMismatch(t *testing.T) {
	commandOptions := newRuntimeOptionsForTest(t)

	vault := setupAgentTestVault(t, nextIDFixtureFiles())
	commandOptions.vaultName = vault.name
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "config.yml"), []byte(`
noteEmbeddings:
  enabled: true
  provider: test
  dimensions: 8
code:
  enabled: true
`), 0o644))
	resolvedConfig1, configErr1 := repoconfig.Load(vault.path)
	embCfg, err := resolvedConfig1.NoteEmbeddings, configErr1
	require.NoError(t, err)
	provider, providerCfg, err := embeddings.NewProviderForConfig(vault.env, embCfg, "")
	require.NoError(t, err)
	expected := embeddings.MetadataForProvider(provider, providerCfg)
	mismatch := expected
	mismatch.Model = "old-model"
	store, err := embsqlite.OpenNotes(vaultconfig.WithEnv(context.Background(), vault.env), embsqlite.File(embCfg.IndexPath), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: provider.Dimensions(), Metadata: &mismatch})
	require.NoError(t, err)
	require.NoError(t, store.Close())

	live, err := commandOptions.buildOntologyQueryLiveRuntime(vaultconfig.WithEnv(context.Background(), vault.env), oneshotruntime.OntologyQueryPlan(true, false))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, live.Close()) })

	snapshot := live.Snapshot()
	require.True(t, snapshot.Semantic.Ready)
	require.True(t, snapshot.Code.Ready)
	require.NotNil(t, snapshot.NoteProvider)
	require.NotNil(t, live.NoteIndex())
	actual, ok, err := live.NoteIndex().Metadata(vaultconfig.WithEnv(context.Background(), vault.env))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, expected.Provider, actual.Provider)
	require.Equal(t, expected.Model, actual.Model)
	require.Equal(t, expected.Dimensions, actual.Dimensions)
	require.NotEqual(t, mismatch.Model, actual.Model, "semantic one-shot runtime must retain baseline metadata reset behavior")
}

func TestSemanticOntologyQueryExecutesWithCurrentProjectionRuntime(t *testing.T) {
	commandOptions := newRuntimeOptionsForTest(t)

	vault := setupAgentTestVault(t, ontologyCommandFiles())
	commandOptions.vaultName = vault.name
	require.NoError(t, os.WriteFile(filepath.Join(vault.path, ".rhizome", "config.yml"), []byte(`
noteEmbeddings:
  enabled: true
  provider: test
  dimensions: 8
code:
  enabled: true
`), 0o644))

	runtime, prepared, cleanup, err := commandOptions.prepareOntologyQuery(vaultconfig.WithEnv(context.Background(), vault.env), `{ project(semantic: ["roadmap"], first: 1) { name } }`, nil)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.True(t, prepared.UsesSemantic)
	result, err := runtime.run(vaultconfig.WithEnv(context.Background(), vault.env), prepared)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
}
