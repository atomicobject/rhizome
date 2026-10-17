package repoconfig_test

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestSavePathToPreferences(t *testing.T) {

	t.Run("saves new vault path", func(t *testing.T) {
		_, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
		env := testkit.Env(t)
		env.CLIConfigFile = mockCliConfigFile

		vaultDir := t.TempDir()
		vault := repoconfig.Vault{Env: env, Name: "vault-name"}
		err := vault.SavePathToPreferences(vaultDir, false)
		assert.NoError(t, err)

		content, err := os.ReadFile(mockCliConfigFile)
		assert.NoError(t, err)

		cfg := repoconfig.CliConfig{}
		assert.NoError(t, yaml.Unmarshal(content, &cfg))
		assert.Equal(t, vaultDir, cfg.Vaults["vault-name"].Path)
	})

	t.Run("prevents overwrite without force", func(t *testing.T) {
		_, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
		env := testkit.Env(t)
		env.CLIConfigFile = mockCliConfigFile

		vaultDir := t.TempDir()
		existingDir := filepath.Join(vaultDir, "existing")
		_ = os.Mkdir(existingDir, 0755)
		content, err := yaml.Marshal(repoconfig.CliConfig{
			Vaults: map[string]obsidian.VaultDefinition{
				"vault-name": {Path: existingDir},
			},
		})
		assert.NoError(t, err)
		err = os.WriteFile(mockCliConfigFile, content, 0644)
		assert.NoError(t, err)

		newDir := filepath.Join(vaultDir, "new")
		_ = os.Mkdir(newDir, 0755)
		vault := repoconfig.Vault{Env: env, Name: "vault-name"}
		err = vault.SavePathToPreferences(newDir, false)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), repoconfig.RhizomeVaultExistsError)
	})

	t.Run("allows overwrite with force", func(t *testing.T) {
		_, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
		env := testkit.Env(t)
		env.CLIConfigFile = mockCliConfigFile

		vaultDir := t.TempDir()
		existingDir := filepath.Join(vaultDir, "existing")
		_ = os.Mkdir(existingDir, 0755)
		content, err := yaml.Marshal(repoconfig.CliConfig{
			Vaults: map[string]obsidian.VaultDefinition{
				"vault-name": {Path: existingDir},
			},
		})
		assert.NoError(t, err)
		err = os.WriteFile(mockCliConfigFile, content, 0644)
		assert.NoError(t, err)

		newDir := filepath.Join(vaultDir, "new")
		_ = os.Mkdir(newDir, 0755)
		vault := repoconfig.Vault{Env: env, Name: "vault-name"}
		err = vault.SavePathToPreferences(newDir, true)
		assert.NoError(t, err)

		content, err = os.ReadFile(mockCliConfigFile)
		assert.NoError(t, err)
		cfg := repoconfig.CliConfig{}
		assert.NoError(t, yaml.Unmarshal(content, &cfg))
		assert.Equal(t, newDir, cfg.Vaults["vault-name"].Path)
	})

	t.Run("rejects invalid path", func(t *testing.T) {
		_, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
		env := testkit.Env(t)
		env.CLIConfigFile = mockCliConfigFile

		vault := repoconfig.Vault{Env: env, Name: "vault-name"}
		err := vault.SavePathToPreferences(filepath.Join(t.TempDir(), "missing"), false)
		assert.EqualError(t, err, obsidian.RhizomeVaultPathInvalidError)
	})
}

func TestRemoveFromPreferences(t *testing.T) {

	t.Run("removes existing vault", func(t *testing.T) {
		_, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
		env := testkit.Env(t)
		env.CLIConfigFile = mockCliConfigFile

		vaultDir := t.TempDir()
		content, err := yaml.Marshal(repoconfig.CliConfig{
			Vaults: map[string]obsidian.VaultDefinition{
				"vault-name": {Path: vaultDir},
			},
		})
		assert.NoError(t, err)
		err = os.WriteFile(mockCliConfigFile, content, 0644)
		assert.NoError(t, err)

		vault := repoconfig.Vault{Env: env, Name: "vault-name"}
		err = vault.RemoveFromPreferences()
		assert.NoError(t, err)

		content, err = os.ReadFile(mockCliConfigFile)
		assert.NoError(t, err)
		cfg := repoconfig.CliConfig{}
		assert.NoError(t, yaml.Unmarshal(content, &cfg))
		_, exists := cfg.Vaults["vault-name"]
		assert.False(t, exists)
	})

	t.Run("errors when vault missing", func(t *testing.T) {
		_, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
		env := testkit.Env(t)
		env.CLIConfigFile = mockCliConfigFile

		vault := repoconfig.Vault{Env: env, Name: "vault-name"}
		err := vault.RemoveFromPreferences()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), repoconfig.RhizomeVaultNotFoundError)
	})
}
