package repoconfig

import (
	"errors"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// DefaultName returns the vault name/path to use. Resolution order:
//  1. v.Name if already set
//  2. Local config in the current directory, or an ancestor up to its git root
//  3. Default from CLI preferences
func (v *Vault) DefaultName() (string, error) {
	if v.Name != "" {
		return v.Name, nil
	}

	if cwd := v.Env.WorkingDir; cwd != "" {
		if dir := localConfigDir(cwd); dir != "" {
			v.Name = dir
			return dir, nil
		}
	}

	cliConfig, err := readCliConfig(v.Env, false)
	if err != nil {
		return "", err
	}

	if cliConfig.DefaultVaultName == "" {
		return "", errors.New(RhizomeConfigParseError)
	}

	v.Name = cliConfig.DefaultVaultName
	return cliConfig.DefaultVaultName, nil
}

// localConfigDir returns the nearest directory from start up to its git root
// that has a local config. Outside a git work tree only start is checked, so
// an unrelated parent folder's config is never adopted.
func localConfigDir(start string) string {
	if HasLocalConfig(start) {
		return start
	}
	dir := paths.ResolveSymlinks(start).String()
	gitRoot := findNearestGitRoot(dir)
	for gitRoot != "" && dir != gitRoot {
		dir = paths.ResolveSymlinks(filepath.Dir(filepath.FromSlash(dir))).String()
		if HasLocalConfig(dir) {
			return dir
		}
	}
	return ""
}

// SetDefaultName sets the default vault. Accepts either:
//   - A vault name (looked up in preferences/Obsidian config)
//   - A path to a directory containing .rhizome/config.yml
func (v *Vault) SetDefaultName(nameOrPath string) error {
	// If it's a path with local config, store the absolute path
	if looksLikePath(nameOrPath) {
		absPath, err := v.ResolvePathName(nameOrPath)
		if err != nil {
			return err
		}
		if HasLocalConfig(absPath) {
			nameOrPath = absPath
		}
	}

	cliConfig, err := readCliConfig(v.Env, true)
	if err != nil {
		return err
	}

	cliConfig.DefaultVaultName = nameOrPath
	if err := writeCliConfig(v.Env, cliConfig); err != nil {
		return err
	}

	v.Name = nameOrPath

	return nil
}
