package config

import (
	"errors"
	"os"
	"path/filepath"
)

func ObsidianFile() (obsidianConfigFile string, err error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New(UserConfigDirectoryNotFoundErrorMessage)
	}
	obsidianConfigFile = filepath.Join(userConfigDir, ObsidianConfigDirectory, ObsidianConfigFile)
	return obsidianConfigFile, nil
}
