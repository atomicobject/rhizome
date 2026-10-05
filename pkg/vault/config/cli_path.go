package config

import (
	"errors"
	"os"
	"path/filepath"
)

var UserHomeDirectory = os.UserHomeDir

func CliPath() (cliConfigDir string, cliConfigFile string, err error) {
	home, err := UserHomeDirectory()
	if err != nil {
		return "", "", errors.New(UserConfigDirectoryNotFoundErrorMessage)
	}
	cliConfigDir = filepath.Join(home, ".config", RhizomeConfigDirectory)
	cliConfigFile = filepath.Join(cliConfigDir, RhizomeConfigFile)
	return cliConfigDir, cliConfigFile, nil
}
