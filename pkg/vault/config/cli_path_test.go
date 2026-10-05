package config_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/assert"
)

func TestConfigCliPath(t *testing.T) {
	originalUserHomeDirectory := config.UserHomeDirectory
	defer func() { config.UserHomeDirectory = originalUserHomeDirectory }()

	t.Run("UserConfigDir func returns a directory", func(t *testing.T) {
		// Arrange
		config.UserHomeDirectory = func() (string, error) {
			return filepath.Join("home"), nil
		}
		// Act
		obsConfigDir, obsConfigFile, err := config.CliPath()
		// Assert
		assert.Equal(t, nil, err)
		assert.Equal(t, filepath.Join("home", ".config", "rhizome"), obsConfigDir)
		assert.Equal(t, filepath.Join("home", ".config", "rhizome", "config.yml"), obsConfigFile)
	})

	t.Run("UserConfigDir func returns an error", func(t *testing.T) {
		// Arrange
		config.UserHomeDirectory = func() (string, error) {
			return "", errors.New(config.UserConfigDirectoryNotFoundErrorMessage)
		}
		// Act
		obsConfigDir, obsConfigFile, err := config.CliPath()
		// Assert
		assert.Equal(t, config.UserConfigDirectoryNotFoundErrorMessage, err.Error())
		assert.Equal(t, "", obsConfigDir)
		assert.Equal(t, "", obsConfigFile)
	})

}
