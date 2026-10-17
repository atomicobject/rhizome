package init

import (
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"

	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func initTestEnv(t *testing.T) vaultconfig.Env {
	t.Helper()
	return vaultconfig.Env{HomeDir: t.TempDir(), LookupEnv: os.LookupEnv}
}

func readCliConfigForTest(t *testing.T, env vaultconfig.Env) repoconfig.CliConfig {
	t.Helper()
	_, path, err := env.CliPath()
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return repoconfig.CliConfig{}
	}
	require.NoError(t, err)
	var cfg repoconfig.CliConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	return cfg
}
