package obsidian_test

import (
	"os"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVaultDefaultName(t *testing.T) {
	origCwd, _ := os.Getwd()
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	// Temporarily override the CliConfigPath function
	originalCliConfigPath := obsidian.CliConfigPath
	defer func() { obsidian.CliConfigPath = originalCliConfigPath }()
	t.Run("Get vault name successfully without errors", func(t *testing.T) {
		t.Run("Get vault name from struct", func(t *testing.T) {
			// Arrange
			vault := obsidian.Vault{Name: "my-vault"}
			// Act
			vaultName, err := vault.DefaultName()
			// Assert
			assert.Equal(t, nil, err)
			assert.Equal(t, "my-vault", vaultName)
		})

		t.Run("Get vault name from config file", func(t *testing.T) {
			// Arrange
			mockCliConfigDir, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
			obsidian.CliConfigPath = func() (string, string, error) {
				return mockCliConfigDir, mockCliConfigFile, nil
			}
			require.NoError(t, os.WriteFile(mockCliConfigFile, []byte("default_vault_name: example-rhizome\n"), 0644))
			vault := obsidian.Vault{}
			// Act
			vaultName, err := vault.DefaultName()
			// Assert
			assert.Equal(t, nil, err)
			assert.Equal(t, "example-rhizome", vaultName)
		})

		t.Run("Does not auto-discover parent local config", func(t *testing.T) {
			parent := t.TempDir()
			child := parent + "/child"
			require.NoError(t, os.MkdirAll(parent+"/.rhizome", 0o755))
			require.NoError(t, os.MkdirAll(child, 0o755))
			require.NoError(t, os.WriteFile(parent+"/.rhizome/config.yml", []byte("notes: {}\n"), 0o644))
			require.NoError(t, os.Chdir(child))
			t.Cleanup(func() { _ = os.Chdir(tmp) })

			mockCliConfigDir, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
			obsidian.CliConfigPath = func() (string, string, error) {
				return mockCliConfigDir, mockCliConfigFile, nil
			}
			require.NoError(t, os.WriteFile(mockCliConfigFile, []byte("default_vault_name: example-rhizome\n"), 0644))

			vault := obsidian.Vault{}
			vaultName, err := vault.DefaultName()
			assert.Equal(t, nil, err)
			assert.Equal(t, "example-rhizome", vaultName)
		})
	})

	t.Run("Could not get vault name", func(t *testing.T) {

		t.Run("Error in config.CliPath", func(t *testing.T) {
			// Arrange
			obsidian.CliConfigPath = func() (string, string, error) {
				return "", "", os.ErrNotExist
			}
			vault := obsidian.Vault{}
			// Act
			_, err := vault.DefaultName()
			// Assert
			assert.Equal(t, os.ErrNotExist, err)
		})

		t.Run("Error in reading default vault config file", func(t *testing.T) {
			// Arrange
			mockCliConfigDir, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
			obsidian.CliConfigPath = func() (string, string, error) {
				return mockCliConfigDir, mockCliConfigFile, nil
			}
			vault := obsidian.Vault{}
			// Act
			_, err := vault.DefaultName()
			// Assert
			assert.Equal(t, err.Error(), obsidian.RhizomeConfigReadError)
		})

		t.Run("Error in unmarshalling default vault config file", func(t *testing.T) {
			// Arrange
			mockCliConfigDir, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
			obsidian.CliConfigPath = func() (string, string, error) {
				return mockCliConfigDir, mockCliConfigFile, nil
			}
			require.NoError(t, os.WriteFile(mockCliConfigFile, []byte("default_vault_name: [oops\n"), 0644))
			vault := obsidian.Vault{}
			// Act
			_, err := vault.DefaultName()
			// Assert
			assert.Equal(t, err.Error(), obsidian.RhizomeConfigParseError)
		})

		t.Run("Error DefaultVaultName empty", func(t *testing.T) {
			// Arrange
			mockCliConfigDir, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
			obsidian.CliConfigPath = func() (string, string, error) {
				return mockCliConfigDir, mockCliConfigFile, nil
			}
			require.NoError(t, os.WriteFile(mockCliConfigFile, []byte(`{"default_vault_name":""}`), 0644))
			vault := obsidian.Vault{}
			// Act
			_, err := vault.DefaultName()
			// Assert
			assert.Equal(t, err.Error(), obsidian.RhizomeConfigParseError)
		})
	})
}

func TestVaultSetDefaultName(t *testing.T) {
	// Temporarily override the CliConfigPath function
	originalCliConfigPath := obsidian.CliConfigPath
	defer func() { obsidian.CliConfigPath = originalCliConfigPath }()

	t.Run("Default vault name successfully set without errors", func(t *testing.T) {
		// Arrange
		mockCliConfigDir, mockCliConfigFile := mocks.CreateMockCliConfigDirectories(t)
		obsidian.CliConfigPath = func() (string, string, error) {
			return mockCliConfigDir, mockCliConfigFile, nil
		}
		vault := obsidian.Vault{}
		// Act
		err := vault.SetDefaultName("vault-name")

		// Assert
		assert.Equal(t, nil, err)
		content, err := os.ReadFile(mockCliConfigFile)
		assert.Equal(t, nil, err)
		assert.Equal(t, "default_vault_name: vault-name\n", string(content))
	})

	t.Run("Error in config.CliPath", func(t *testing.T) {
		// Arrange
		obsidian.CliConfigPath = func() (string, string, error) {
			return "", "", os.ErrNotExist
		}
		vault := obsidian.Vault{}
		// Act
		err := vault.SetDefaultName("vault-name")
		// Assert
		assert.Equal(t, os.ErrNotExist, err)
	})

	t.Run("Error in creating default vault config directory", func(t *testing.T) {
		// Arrange
		obsidian.CliConfigPath = func() (string, string, error) {
			return "", "" + "/config.yml", nil
		}
		vault := obsidian.Vault{}
		// Act
		err := vault.SetDefaultName("vault-name")
		// Assert
		assert.Equal(t, err.Error(), obsidian.RhizomeConfigDirWriteError)
	})

	t.Run("Error in writing to default vault config file", func(t *testing.T) {
		// Arrange
		mockCliConfigDir, _ := mocks.CreateMockCliConfigDirectories(t)
		obsidian.CliConfigPath = func() (string, string, error) {
			return mockCliConfigDir + "/unwrittable", mockCliConfigDir + "unwrittable/config.yml", nil
		}
		require.NoError(t, os.Mkdir(mockCliConfigDir+"/unwrittable", 0444))
		vault := obsidian.Vault{}
		// Act
		err := vault.SetDefaultName("vault-name")
		// Assert
		assert.Equal(t, err.Error(), obsidian.RhizomeConfigWriteError)
	})

}
