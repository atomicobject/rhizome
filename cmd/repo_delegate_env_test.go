package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRepositoryDotEnvCannotDisableUpdateTrust(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{Version: "v1.2.3"},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte(
		"RZM_SKIP_REPO_DELEGATE=1\nRZM_REPO_DELEGATED=1\n"), 0o600))
	command := exec.Command(os.Args[0], "-test.run=^TestRepositoryDotEnvTrustHelper$")
	command.Dir = repo
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if name != repoSkipDelegateEnv && name != repoDelegatedEnv {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, "RZM_TEST_DOTENV_TRUST_HELPER=1")
	output, err := command.CombinedOutput()
	require.Error(t, err, string(output))
	require.Contains(t, string(output), "rzm trust")
}

func TestRepositoryDotEnvTrustHelper(t *testing.T) {
	if os.Getenv("RZM_TEST_DOTENV_TRUST_HELPER") != "1" {
		return
	}
	cwd, err := os.Getwd()
	require.NoError(t, err)
	_, err = vaultconfig.LoadDotEnvUpwards(cwd)
	require.NoError(t, err)
	require.Equal(t, "1", os.Getenv(repoSkipDelegateEnv))
	require.Equal(t, "1", os.Getenv(repoDelegatedEnv))
	// Help never downloads anything if the guard regresses, while still
	// traversing the same update trust boundary as an executing update.
	os.Args = []string{"rzm", "update", "--help"}
	os.Exit(Execute())
}
