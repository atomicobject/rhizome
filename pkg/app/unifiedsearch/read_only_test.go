package unifiedsearch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeembsql "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	semdb "github.com/atomicobject/rhizome/pkg/store"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestRunDoesNotMutateEmbeddingStoresOnMetadataMismatch(t *testing.T) {
	for _, domain := range []string{"note", "code", "matched"} {
		t.Run(domain, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".rhizome", "db.sqlite")
			store, err := semdb.Open(path)
			require.NoError(t, err)
			defer store.Close()
			cfg := embeddings.Config{Enabled: true, Provider: "test", Model: "indexed-model", Dimensions: 4, IndexPath: path}
			providerCfg := cfg.ProviderCfg("")
			provider := embeddings.NewDeterministicProvider(providerCfg)
			meta := embeddings.MetadataForProvider(provider, providerCfg)
			noteStore, err := codeembsql.OpenNotes(t.Context(), codeembsql.File(path), codeembsql.Options{Access: codeembsql.WriteIndex, Dimensions: provider.Dimensions(), Metadata: &meta})
			require.NoError(t, err)
			require.NoError(t, noteStore.CacheEmbedding(t.Context(), "paid-note-vector", embeddings.Embedding{1, 0, 0, 0}))
			defer noteStore.Close()
			codeStore, err := codeembsql.OpenCode(t.Context(), codeembsql.File(path), codeembsql.Options{Access: codeembsql.WriteIndex, Dimensions: provider.Dimensions(), Metadata: &meta})
			require.NoError(t, err)
			defer codeStore.Close()
			validated, err := semdb.Open(path)
			require.NoError(t, err)
			require.NoError(t, validated.Close())
			// Keep metadata and cache checks on an independent reader so even a same-value
			// metadata UPDATE would be observed as a search write.
			observer, err := store.DB().Conn(t.Context())
			require.NoError(t, err)
			defer observer.Close()
			var before int
			require.NoError(t, observer.QueryRowContext(t.Context(), "PRAGMA data_version").Scan(&before))
			codeCfg := cfg
			if domain == "note" {
				cfg.Model = "requested-model"
			}
			if domain == "code" {
				codeCfg.Model = "requested-model"
			}
			require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{NoteEmbeddings: &cfg, CodeEmbeddings: &codeCfg}))
			result, err := Run(t.Context(), Options{NoteFormats: testNoteFormats(t), Query: "synthetic", UseVector: true, VaultPath: root, EmbCfg: cfg})
			if result.Cleanup != nil {
				result.Cleanup()
			}
			if domain == "matched" {
				require.NoError(t, err)
			} else {
				var mismatch embeddings.MetadataError
				require.ErrorAs(t, err, &mismatch)
			}
			var after, count int
			require.NoError(t, observer.QueryRowContext(t.Context(), "PRAGMA data_version").Scan(&after))
			require.Equal(t, before, after, "query must not write any index domain")
			require.NoError(t, store.DB().QueryRow("SELECT COUNT(*) FROM emb_embedding_cache").Scan(&count))
			require.Equal(t, 1, count, "paid embedding cache must survive mismatched query")
		})
	}
}

func TestRunDoesNotCreateMissingEmbeddingIndex(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing", "db.sqlite")
	_, err := Run(t.Context(), Options{NoteFormats: testNoteFormats(t), Query: "synthetic", UseVector: true, VaultPath: root, EmbCfg: embeddings.Config{Enabled: true, Provider: "test", Model: "synthetic", Dimensions: 4, IndexPath: path}})
	require.Error(t, err)
	_, err = os.Stat(filepath.Dir(path))
	require.True(t, os.IsNotExist(err), "query must not create index directories")
}

func TestRunDoesNotMigrateOldIntelIndex(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".rhizome", "db.sqlite")
	store, err := semdb.Open(path)
	require.NoError(t, err)
	defer store.Close()
	_, err = store.DB().Exec("DELETE FROM rzm_migration_validation")
	require.NoError(t, err)
	cfg := embeddings.Config{Enabled: false}
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{Code: repoconfig.LocalCodeConfig{Enabled: true}}))
	observer, err := store.DB().Conn(t.Context())
	require.NoError(t, err)
	defer observer.Close()
	var before, after int
	require.NoError(t, observer.QueryRowContext(t.Context(), "PRAGMA data_version").Scan(&before))
	_, err = Run(t.Context(), Options{NoteFormats: testNoteFormats(t), Query: "synthetic", UseIntel: true, VaultPath: root, EmbCfg: cfg})
	require.ErrorContains(t, err, "rzm index")
	require.NoError(t, observer.QueryRowContext(t.Context(), "PRAGMA data_version").Scan(&after))
	require.Equal(t, before, after)
}

func TestRunRequiresNoteFormatsBeforeOpeningIndex(t *testing.T) {
	root := t.TempDir()
	_, err := Run(t.Context(), Options{Query: "synthetic", UseIntel: true, VaultPath: root})
	require.ErrorContains(t, err, "explicit note format runtime")
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
}
