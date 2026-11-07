package repoconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"gopkg.in/yaml.v3"
)

// readCliConfig loads rhizome preferences. If allowMissing is true, a missing file
// returns an empty config and nil error.
func readCliConfig(env config.Env, allowMissing bool) (CliConfig, error) {
	_, cliConfigFile, err := env.CliPath()
	if err != nil {
		return CliConfig{}, err
	}

	content, err := os.ReadFile(cliConfigFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && allowMissing {
			return CliConfig{}, nil
		}
		return CliConfig{}, errors.New(RhizomeConfigReadError)
	}

	cfg := CliConfig{}
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return CliConfig{}, errors.New(RhizomeConfigParseError)
	}

	return cfg, nil
}

func validateVaultPath(path string) error {
	if path == "" {
		return errors.New(obsidian.RhizomeVaultPathInvalidError)
	}
	cleaned := filepath.Clean(path)
	info, err := os.Stat(cleaned)
	if err != nil || !info.IsDir() {
		return errors.New(obsidian.RhizomeVaultPathInvalidError)
	}
	return nil
}

// setCliVaultPath stores/overwrites a vault path in the CLI preferences.
func setCliVaultPath(env config.Env, name string, path string, force bool) error {
	return setCliVaultDefinition(env, obsidian.VaultDefinition{Name: name, Path: path}, force)
}

// setCliVaultDefinition stores/overwrites a vault definition in the CLI preferences.
func setCliVaultDefinition(env config.Env, def obsidian.VaultDefinition, force bool) error {
	if def.Name == "" {
		return errors.New(RhizomeVaultNameRequiredError)
	}

	// Validate base path
	basePath := def.BasePath()
	if err := validateVaultPath(basePath); err != nil {
		return err
	}

	// Clean paths
	if def.Path != "" {
		def.Path = filepath.Clean(def.Path)
	}
	if def.Root != "" {
		def.Root = filepath.Clean(def.Root)
	}

	return UpdateCliConfig(env, func(cfg *CliConfig) error {
		if cfg.Vaults == nil {
			cfg.Vaults = make(map[string]obsidian.VaultDefinition)
		}
		if existing, ok := cfg.Vaults[def.Name]; ok && existing.BasePath() != basePath && !force {
			return fmt.Errorf("%s: %s", RhizomeVaultExistsError, def.Name)
		}
		cfg.Vaults[def.Name] = def
		return nil
	})
}

// removeCliVaultPath removes a vault entry from CLI preferences.
func removeCliVaultPath(env config.Env, name string) error {
	if name == "" {
		return errors.New(RhizomeVaultNameRequiredError)
	}

	return UpdateCliConfig(env, func(cfg *CliConfig) error {
		if _, ok := cfg.Vaults[name]; !ok {
			return fmt.Errorf("%s: %s", RhizomeVaultNotFoundError, name)
		}
		delete(cfg.Vaults, name)
		return nil
	})
}

// listCliVaults returns manual vault mappings stored in preferences.
func listCliVaults(env config.Env) (map[string]obsidian.VaultDefinition, string, error) {
	cfg, err := readCliConfig(env, true)
	if err != nil {
		return nil, "", err
	}

	vaults := cfg.Vaults
	if vaults == nil {
		vaults = make(map[string]obsidian.VaultDefinition)
	}

	return vaults, cfg.DefaultVaultName, nil
}

// LoadCliConfig loads rhizome CLI preferences.
func LoadCliConfig(env config.Env, allowMissing bool) (CliConfig, error) {
	return readCliConfig(env, allowMissing)
}

// SaveCliConfig replaces the modeled fields of the CLI preferences with cfg,
// keeping unmodeled keys. Read-modify-write callers use UpdateCliConfig so a
// stale copy cannot overwrite a concurrent change.
func SaveCliConfig(env config.Env, cfg CliConfig) error {
	return UpdateCliConfig(env, func(current *CliConfig) error {
		*current = cfg
		return nil
	})
}
