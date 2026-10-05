package web

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func fallbackDatabaseOwners() int {
	stack := make([]byte, 2<<20)
	n := runtime.Stack(stack, true)
	return strings.Count(string(stack[:n]), "database/sql.(*DB).connectionOpener(")
}

func TestFallbackRuntimeClosesCachedProviders(t *testing.T) {
	for _, failure := range []string{"", "embedding stores", "file catalog", "agent service"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("RHIZOME_EMBEDDING_CACHE", filepath.Join(root, "cache.sqlite"))
			t.Setenv("RHIZOME_OPENAI_API_KEY", "synthetic-only")
			embCfg := embeddings.Config{Enabled: true, Provider: "openai", Model: "synthetic", Dimensions: 4, Endpoint: "http://127.0.0.1:1"}
			require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0o755))
			require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{NoteEmbeddings: &embCfg, CodeEmbeddings: &embCfg}))
			cfg := Config{Vault: &obsidian.Vault{Name: "test"}, VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testNoteMetadataIndexer(t)}
			if failure == "embedding stores" {
				require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome", "db.sqlite"), 0o755))
			}
			if failure == "file catalog" {
				cfg.VaultDef = obsidian.VaultDefinition{}
			}
			if failure == "agent service" {
				require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "agent"), nil, 0o600))
			}
			before := fallbackDatabaseOwners()
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
	cfg := embeddings.ProviderConfig{Provider: "test", Dimensions: 4}
	provider, err := embeddings.NewCachedProvider(embeddings.NewDeterministicProvider(cfg), cfg, filepath.Join(t.TempDir(), "cache.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, provider.(io.Closer).Close()) }()
	root := t.TempDir()
	server, err := NewServer(t.Context(), Config{Vault: &obsidian.Vault{Name: "test"}, VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, NoteMetadata: testNoteMetadataIndexer(t), Runtime: &Runtime{NoteProvider: provider, CodeProvider: provider}}, nil)
	require.NoError(t, err)
	require.NoError(t, server.Close())
	_, err = provider.EmbedTexts(t.Context(), []string{"still borrowed"})
	require.NoError(t, err)
}
