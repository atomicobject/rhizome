package bootstrap

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeemb "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	semdb "github.com/atomicobject/rhizome/pkg/store"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func providerDatabaseOwners() int {
	stack := make([]byte, 2<<20)
	n := runtime.Stack(stack, true)
	return strings.Count(string(stack[:n]), "database/sql.(*DB).connectionOpener(")
}

func cachedProviderVault(t *testing.T) (string, embeddings.Config) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("RHIZOME_EMBEDDING_CACHE", filepath.Join(root, "cache.sqlite"))
	t.Setenv("RHIZOME_OPENAI_API_KEY", "synthetic-only")
	cfg := embeddings.Config{Enabled: true, Provider: "openai", Model: "synthetic", Dimensions: 4, Endpoint: "http://127.0.0.1:1", IndexPath: filepath.Join(root, ".rhizome", "db.sqlite")}
	require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{NoteEmbeddings: &cfg, CodeEmbeddings: &cfg}))
	return root, cfg
}

func prepareCachedProviderIndex(t *testing.T, cfg embeddings.Config) {
	t.Helper()
	// Prepare current note and code metadata so query-only startup can
	// validate the existing index without requesting remote embeddings.
	info := cfg.ProviderCfg("synthetic-only")
	seed := embeddings.NewDeterministicProvider(info)
	embeddingMetadata1 := embeddings.MetadataForProvider(seed, info)
	notes, err := codeemb.OpenNotes(t.Context(), codeemb.File(cfg.IndexPath), codeemb.Options{Access: codeemb.WriteIndex, Dimensions: seed.Dimensions(), Metadata: &embeddingMetadata1})
	require.NoError(t, err)
	require.NoError(t, notes.Close())
	meta := embeddings.MetadataForProvider(seed, info)
	meta.FingerprintVersion = 2
	code, err := codeemb.OpenCode(t.Context(), codeemb.File(cfg.IndexPath), codeemb.Options{Access: codeemb.WriteIndex, Dimensions: seed.Dimensions(), Metadata: &meta})
	require.NoError(t, err)
	require.NoError(t, code.Close())
	intel, err := semdb.Open(cfg.IndexPath)
	require.NoError(t, err)
	require.NoError(t, intel.Close())
}

func TestLiveRuntimeClosesCachedProviders(t *testing.T) {
	for _, queryOnly := range []bool{false, true} {
		name := "serve providers"
		if queryOnly {
			name = "shared query providers"
		}
		t.Run(name, func(t *testing.T) {
			root, cfg := cachedProviderVault(t)
			before := providerDatabaseOwners()
			prepareCachedProviderIndex(t, cfg)
			require.Eventually(t, func() bool { return providerDatabaseOwners() == before }, 3*time.Second, 10*time.Millisecond)
			for range 3 {
				rt, err := NewLiveRuntime(t.Context(), LiveOptions{VaultName: root, DisableLeaderWork: true, DisableWatchHub: true, SkipCacheWarmup: true, DisableSessionStore: true, QueryProvidersOnly: queryOnly, ReadOnlyCodeIndex: queryOnly, Requirements: RequireRuntimeCapabilities(RuntimeCapabilitySemantic, RuntimeCapabilityCodeIndex, RuntimeCapabilityCodeEmbeddings)})
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, rt.Close()) })
				require.NoError(t, rt.WaitForSemantic(t.Context()))
				require.NoError(t, rt.WaitForCodeIndex(t.Context()))
				_, codeProvider := rt.CodeEmbeddings()
				require.NotNil(t, rt.NoteProvider())
				require.NotNil(t, codeProvider)
				if queryOnly {
					require.Same(t, rt.NoteProvider(), codeProvider)
					// Only the shared provider cache and existing Intel reader.
					require.Eventually(t, func() bool { return providerDatabaseOwners() == before+2 }, 3*time.Second, 10*time.Millisecond)
				}
				require.NoError(t, rt.Close())
				require.NoError(t, rt.Close())
				require.Eventually(t, func() bool { return providerDatabaseOwners() == before }, 3*time.Second, 10*time.Millisecond)
			}
		})
	}
}

func TestBootstrapCachedProvidersCloseAfterIndexFailure(t *testing.T) {
	root, cfg := cachedProviderVault(t)
	require.NoError(t, os.Mkdir(cfg.IndexPath, 0o755))
	before := providerDatabaseOwners()
	for range 3 {
		rt, err := NewLiveRuntime(t.Context(), LiveOptions{VaultName: root, DisableLeaderWork: true, DisableWatchHub: true, SkipCacheWarmup: true, DisableSessionStore: true, Requirements: RequireRuntimeCapabilities(RuntimeCapabilitySemantic)})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, rt.Close()) })
		require.Error(t, rt.WaitForSemantic(t.Context()))
		require.NoError(t, rt.Close())
		require.Eventually(t, func() bool { return providerDatabaseOwners() == before }, 3*time.Second, 10*time.Millisecond)

		cleanup, warnings := initSemanticTools(t.Context(), root, &agentapi.Config{})
		require.NotEmpty(t, warnings)
		require.NotNil(t, cleanup)
		cleanup()
		require.Eventually(t, func() bool { return providerDatabaseOwners() == before }, 3*time.Second, 10*time.Millisecond)
	}
}

func TestCodexSemanticToolsCleanupClosesCachedProviders(t *testing.T) {
	root, indexConfig := cachedProviderVault(t)
	before := providerDatabaseOwners()
	prepareCachedProviderIndex(t, indexConfig)
	require.Eventually(t, func() bool { return providerDatabaseOwners() == before }, 3*time.Second, 10*time.Millisecond)
	for range 3 {
		cfg := &agentapi.Config{}
		cleanup, warnings := initSemanticTools(t.Context(), root, cfg)
		require.Empty(t, warnings)
		require.NotNil(t, cfg.EmbedProvider)
		require.NotNil(t, cfg.CodeEmbedProvider)
		require.NotNil(t, cleanup)
		cleanup()
		require.Eventually(t, func() bool { return providerDatabaseOwners() == before }, 3*time.Second, 10*time.Millisecond)
	}
}

func TestCodexSemanticToolsDoesNotCreateMissingIndexes(t *testing.T) {
	root, indexConfig := cachedProviderVault(t)
	cfg := &agentapi.Config{}
	cleanup, warnings := initSemanticTools(t.Context(), root, cfg)
	require.NotNil(t, cleanup)
	defer cleanup()
	require.Len(t, warnings, 2)
	for _, warning := range warnings {
		require.Contains(t, warning, "run `rzm index`")
	}
	require.Nil(t, cfg.Embeddings)
	require.Nil(t, cfg.CodeEmbeddings)
	_, err := os.Stat(indexConfig.IndexPath)
	require.True(t, os.IsNotExist(err))
}
