package actions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// InstallIgnoreOptions controls writing the default .rhizome/ignore file.
type InstallIgnoreOptions struct {
	Force bool
}

// InstallDefaultIgnore writes the built-in ignore file to the vault root.
// If the file already exists and Force is false, an error is returned.
func InstallDefaultIgnore(vault obsidian.VaultManager, opts InstallIgnoreOptions) (string, error) {
	vaultPath, err := vault.Path()
	if err != nil {
		return "", err
	}

	dest := filepath.Join(vaultPath, obsidian.RhizomeDirName, obsidian.RhizomeIgnoreFilename)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(dest); err == nil && !opts.Force {
		return "", fmt.Errorf("%s already exists; re-run with --force to overwrite", dest)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	content := obsidian.DefaultIgnoreFile()
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		return "", err
	}

	return dest, nil
}
