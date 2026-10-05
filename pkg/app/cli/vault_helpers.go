package actions

import (
	"fmt"
	"os"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func resolveVaultDefinition(vault obsidian.VaultManager) (obsidian.VaultDefinition, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		if v, ok := vault.(*obsidian.Vault); ok {
			if stat, statErr := os.Stat(v.Name); statErr == nil && stat.IsDir() {
				return obsidian.VaultDefinition{Name: v.Name, Path: v.Name}, nil
			}
		}
		return obsidian.VaultDefinition{}, fmt.Errorf("failed to get vault path: %w", err)
	}
	return vaultDef, nil
}

func resolveVaultPaths(vaultDef obsidian.VaultDefinition) (paths.VaultPaths, error) {
	vaultPath := vaultDef.BasePath()
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		if err == nil {
			err = fmt.Errorf("invalid vault path %q", vaultPath)
		}
		return paths.VaultPaths{}, err
	}
	return vaultPaths, nil
}
