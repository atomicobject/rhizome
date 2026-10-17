package web

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fallbackDatabaseOwners() int {
	stack := make([]byte, 2<<20)
	n := runtime.Stack(stack, true)
	return strings.Count(string(stack[:n]), "database/sql.(*DB).connectionOpener(")
}

func TestFallbackRuntimeClosesCachedProviders(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	for _, failure := range []string{"", "embedding stores", "file catalog", "agent service"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("RHIZOME_EMBEDDING_CACHE", filepath.Join(root, "cache.sqlite"))
			t.Setenv("RHIZOME_OPENAI_API_KEY", "synthetic-only")
			embCfg := embeddings.Config{Enabled: true, Provider: "openai", Model: "synthetic", Dimensions: 4, Endpoint: "http://127.0.0.1:1", IndexPath: filepath.Join(root, ".rhizome", "db.sqlite")}
			require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0o755))
			require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{NoteEmbeddings: &embCfg, CodeEmbeddings: &embCfg}))
			cfg := Config{Vault: &repoconfig.Vault{Env: env, Name: "test"}, VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testkit.NoteMetadataIndexer(t)}
			before := fallbackDatabaseOwners()
			lease, acquired, err := indexlock.TryAcquireLease(root, indexlock.AcquireOptions{Role: "test/provider-lifetime"})
			require.NoError(t, err)
			require.True(t, acquired)
			t.Cleanup(func() { require.NoError(t, lease.Release()) })
			dimensions := 4
			if failure == "embedding stores" {
				dimensions = 8
			}
			metadata := embeddings.IndexMetadata{Provider: "openai", Model: "synthetic", Dimensions: dimensions}
			notes, err := embsqlite.OpenNotes(t.Context(), embsqlite.File(embCfg.IndexPath), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: dimensions, Metadata: &metadata})
			require.NoError(t, err)
			require.NoError(t, notes.Close())
			code, err := embsqlite.OpenCode(t.Context(), embsqlite.File(embCfg.IndexPath), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: dimensions, Metadata: &metadata})
			require.NoError(t, err)
			require.NoError(t, code.Close())
			store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			require.NoError(t, store.Close())
			require.NoError(t, lease.Release())
			if failure == "file catalog" {
				cfg.VaultDef = obsidian.VaultDefinition{}
			}
			if failure == "agent service" {
				require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "agent"), nil, 0o600))
			}
			for range 3 {
				server, err := NewServer(t.Context(), cfg, nil)
				if failure == "file catalog" || failure == "agent service" {
					require.Error(t, err)
					require.Nil(t, server)
				} else {
					require.NoError(t, err)
					t.Cleanup(func() { require.NoError(t, server.Close()) })
					if failure == "" {
						require.NotNil(t, server.runtime.NoteProvider)
						require.NotNil(t, server.runtime.CodeProvider)
					} else {
						require.Nil(t, server.runtime.NoteProvider)
						require.Nil(t, server.runtime.CodeProvider)
					}
					require.NoError(t, server.Close())
				}
				require.Eventually(t, func() bool { return fallbackDatabaseOwners() == before }, 3*time.Second, 10*time.Millisecond)
			}
		})
	}
}

func TestFallbackRuntimeBorrowsInjectedCachedProvider(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	cfg := embeddings.ProviderConfig{Provider: "test", Dimensions: 4}
	provider, err := embeddings.NewCachedProvider(embeddings.NewDeterministicProvider(cfg), cfg, filepath.Join(t.TempDir(), "cache.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, provider.(io.Closer).Close()) }()
	root := t.TempDir()
	server, err := NewServer(t.Context(), Config{Vault: &repoconfig.Vault{Env: env, Name: "test"}, VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testkit.NoteMetadataIndexer(t), Runtime: &Runtime{NoteProvider: provider, CodeProvider: provider}}, nil)
	require.NoError(t, err)
	require.NoError(t, server.Close())
	_, err = provider.EmbedTexts(t.Context(), []string{"still borrowed"})
	require.NoError(t, err)
}
