package repoconfig_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestSaveCliConfigPreservesUnknownKeysAndComments(t *testing.T) {
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(t.TempDir(), "config.yml")
	initial := "# keep this comment\nfuture_key: x\ndefault_vault_name: old\n"
	require.NoError(t, os.WriteFile(env.CLIConfigFile, []byte(initial), 0o600))

	require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{DefaultVaultName: "new"}))

	data, err := os.ReadFile(env.CLIConfigFile)
	require.NoError(t, err)
	require.Equal(t, "# keep this comment\nfuture_key: x\ndefault_vault_name: new\n", string(data))
}

func TestUpdateCliConfigKeepsConcurrentUpdatesToDifferentKeys(t *testing.T) {
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(t.TempDir(), "config.yml")
	const writers = 16
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, repoconfig.UpdateCliConfig(env, func(cfg *repoconfig.CliConfig) error {
				if cfg.Env == nil {
					cfg.Env = map[string]string{}
				}
				cfg.Env[fmt.Sprintf("KEY_%d", i)] = "synthetic"
				return nil
			}))
		}()
	}
	wg.Wait()

	cfg, err := repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Len(t, cfg.Env, writers)
}
