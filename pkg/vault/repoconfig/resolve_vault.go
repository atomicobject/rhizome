package repoconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ResolveConfiguration captures vault identity and subsystem settings together.
// A supplied snapshot is authoritative for local YAML and must match the
// selected canonical root; it never reparses a newer local document.
func (v *Vault) ResolveConfiguration(injected *Resolved) (obsidian.VaultDefinition, Resolved, error) {
	if injected != nil {
		resolved := *injected
		if resolved.VaultPath == "" {
			return obsidian.VaultDefinition{}, Resolved{}, errors.New("configuration vault root is required")
		}
		if err := resolved.ValidateFor(resolved.VaultPath); err != nil {
			return obsidian.VaultDefinition{}, Resolved{}, err
		}
		definition := obsidian.VaultDefinition{Name: v.Name, Path: resolved.VaultPath}
		if v.Name != "" {
			if looksLikePath(v.Name) {
				root, err := v.ResolvePathName(v.Name)
				if err != nil {
					return obsidian.VaultDefinition{}, Resolved{}, err
				}
				if err := resolved.ValidateFor(root); err != nil {
					return obsidian.VaultDefinition{}, Resolved{}, err
				}
				definition.Name = filepath.Base(root)
			} else {
				selected, err := v.Definition()
				if err != nil {
					return obsidian.VaultDefinition{}, Resolved{}, err
				}
				if err := resolved.ValidateFor(selected.BasePath()); err != nil {
					return obsidian.VaultDefinition{}, Resolved{}, err
				}
				definition = selected
			}
		}
		if err := requireVaultDirectory(resolved.VaultPath); err != nil {
			return obsidian.VaultDefinition{}, Resolved{}, err
		}
		if v.Name == "" {
			v.Name = resolved.VaultPath
		}
		return configuredDefinition(definition, resolved), resolved, nil
	}
	if v.Name == "" && v.Env.WorkingDir != "" {
		root, local, err := FindLocalConfig(v.Env.WorkingDir)
		if err == nil {
			v.Name = root
			resolved := FromLocal(root, local)
			return configuredDefinition(obsidian.VaultDefinition{Name: filepath.Base(root), Root: root}, resolved), resolved, nil
		}
		if !errors.Is(err, ErrNoLocalConfig) {
			return obsidian.VaultDefinition{}, Resolved{}, err
		}
	}
	if v.Name == "" {
		name, err := v.DefaultName()
		if err != nil {
			return obsidian.VaultDefinition{}, Resolved{}, err
		}
		v.Name = name
	}
	if looksLikePath(v.Name) {
		root, err := v.ResolvePathName(v.Name)
		if err != nil {
			return obsidian.VaultDefinition{}, Resolved{}, err
		}
		if err := requireVaultDirectory(root); err != nil {
			return obsidian.VaultDefinition{}, Resolved{}, err
		}
		local, err := LoadLocalConfig(root)
		if err != nil {
			return obsidian.VaultDefinition{}, Resolved{}, err
		}
		resolved := FromLocal(root, local)
		return configuredDefinition(obsidian.VaultDefinition{Name: filepath.Base(root), Root: root}, resolved), resolved, nil
	}
	definition, err := v.Definition()
	if err != nil {
		return obsidian.VaultDefinition{}, Resolved{}, err
	}
	if err := requireVaultDirectory(definition.BasePath()); err != nil {
		return obsidian.VaultDefinition{}, Resolved{}, err
	}
	resolved, err := Load(definition.BasePath())
	if err != nil {
		return obsidian.VaultDefinition{}, Resolved{}, err
	}
	return configuredDefinition(definition, resolved), resolved, nil
}

func configuredDefinition(definition obsidian.VaultDefinition, resolved Resolved) obsidian.VaultDefinition {
	name := definition.Name
	if name == "" {
		name = filepath.Base(resolved.VaultPath)
	}
	if resolved.Local != nil {
		definition = LocalConfigToDefinition(resolved.VaultPath, resolved.Local)
	} else if definition.IsCollection() {
		definition.Root = resolved.VaultPath
	} else {
		definition.Path = resolved.VaultPath
	}
	definition.Name = name
	return definition
}

func requireVaultDirectory(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("vault root %q is not a directory", root)
	}
	return nil
}
