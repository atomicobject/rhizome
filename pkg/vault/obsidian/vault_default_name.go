package obsidian

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// DefaultName returns the vault name/path to use. Resolution order:
//  1. v.Name if already set
//  2. Local config in current directory (auto-discovery)
//  3. Default from CLI preferences
func (v *Vault) DefaultName() (string, error) {
	if v.Name != "" {
		return v.Name, nil
	}

	// Try auto-discovery from current directory only.
	if cwd, err := os.Getwd(); err == nil {
		if HasLocalConfig(cwd) {
			v.Name = cwd
			return cwd, nil
		}
	}

	cliConfig, err := readCliConfig(false)
	if err != nil {
		return "", err
	}

	if cliConfig.DefaultVaultName == "" {
		return "", errors.New(RhizomeConfigParseError)
	}

	v.Name = cliConfig.DefaultVaultName
	return cliConfig.DefaultVaultName, nil
}

// SetDefaultName sets the default vault. Accepts either:
//   - A vault name (looked up in preferences/Obsidian config)
//   - A path to a directory containing .rhizome/config.yml
func (v *Vault) SetDefaultName(nameOrPath string) error {
	// If it's a path with local config, store the absolute path
	if looksLikePath(nameOrPath) {
		absPath := paths.ResolveSymlinks(nameOrPath).String()
		if absPath == "" {
			absPath = filepath.Clean(nameOrPath)
		}
		if HasLocalConfig(absPath) {
			nameOrPath = absPath
		}
	}

	cliConfig, err := readCliConfig(true)
	if err != nil {
		return err
	}

	cliConfig.DefaultVaultName = nameOrPath
	if err := writeCliConfig(cliConfig); err != nil {
		return err
	}

	v.Name = nameOrPath

	return nil
}
