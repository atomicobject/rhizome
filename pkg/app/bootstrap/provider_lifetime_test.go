package bootstrap

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeemb "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	embsqlite "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{NoteEmbeddings: &cfg, CodeEmbeddings: &cfg}))
	return root, cfg
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
			// Prepare current note and code metadata so query-only startup can
			// validate the existing index without requesting remote embeddings.
			info := cfg.ProviderCfg("synthetic-only")
			seed := embeddings.NewDeterministicProvider(info)
			notes, err := embsqlite.OpenWithMetadata(t.Context(), cfg.IndexPath, seed, embeddings.MetadataForProvider(seed, info))
			require.NoError(t, err)
			require.NoError(t, notes.Close())
			meta := embeddings.MetadataForProvider(seed, info)
			meta.FingerprintVersion = 2
			code, err := codeemb.OpenWithMetadata(t.Context(), cfg.IndexPath, seed, meta)
			require.NoError(t, err)
			require.NoError(t, code.Close())
			intel, err := semdb.Open(cfg.IndexPath)
			require.NoError(t, err)
			require.NoError(t, intel.Close())
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
	root, _ := cachedProviderVault(t)
	before := providerDatabaseOwners()
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
