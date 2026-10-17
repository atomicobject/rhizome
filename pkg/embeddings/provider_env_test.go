package embeddings

import (
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
	"io"
	"path/filepath"
	"testing"
)

func TestProviderConstructionKeepsInvocationCredentialAndCache(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			cachePath := filepath.Join(root, "vectors.sqlite")
			env := vaultconfig.Env{HomeDir: root, LookupEnv: func(key string) (string, bool) {
				switch key {
				case "OPENAI_API_KEY":
					return "synthetic-" + name, true
				case "RHIZOME_EMBEDDING_CACHE":
					return cachePath, true
				}
				return "", false
			}}
			provider, cfg, err := NewProviderForConfig(env, Config{Provider: "openai", Dimensions: 8}, "")
			require.NoError(t, err)
			t.Cleanup(func() {
				if closer, ok := provider.(io.Closer); ok {
					require.NoError(t, closer.Close())
				}
			})
			require.Equal(t, "synthetic-"+name, cfg.APIKey)
			require.FileExists(t, cachePath)
			// Deliberately absent credentials cannot fall back to a host or sibling Env.
			empty := vaultconfig.Env{HomeDir: t.TempDir()}
			missing, missingCfg, err := NewProviderForConfig(empty, Config{Provider: "openai", Dimensions: 8}, "")
			require.EqualError(t, err, "openai provider requires api key")
			require.Nil(t, missing)
			require.Empty(t, missingCfg.APIKey)
		})
	}
}
