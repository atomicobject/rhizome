package obsidian

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/vault/config"
	"gopkg.in/yaml.v3"
)

var CliConfigPath = config.CliPath

// readCliConfig loads rhizome preferences. If allowMissing is true, a missing file
// returns an empty config and nil error.
func readCliConfig(allowMissing bool) (CliConfig, error) {
	_, cliConfigFile, err := CliConfigPath()
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

func writeCliConfig(cfg CliConfig) error {
	obsConfigDir, obsConfigFile, err := CliConfigPath()
	if err != nil {
		return err
	}

	if obsConfigDir == "" {
		return errors.New(RhizomeConfigDirWriteError)
	}

	if err := os.MkdirAll(obsConfigDir, 0o700); err != nil {
		return errors.New(RhizomeConfigDirWriteError)
	}

	yamlContent, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.New(RhizomeConfigGenerateError)
	}

	if err := WriteFileAtomic(obsConfigFile, yamlContent, 0o600); err != nil {
		return errors.New(RhizomeConfigWriteError)
	}

	return nil
}

func validateVaultPath(path string) error {
	if path == "" {
		return errors.New(RhizomeVaultPathInvalidError)
	}
	cleaned := filepath.Clean(path)
	info, err := os.Stat(cleaned)
	if err != nil || !info.IsDir() {
		return errors.New(RhizomeVaultPathInvalidError)
	}
	return nil
}

// setCliVaultPath stores/overwrites a vault path in the CLI preferences.
func setCliVaultPath(name string, path string, force bool) error {
	return setCliVaultDefinition(VaultDefinition{Name: name, Path: path}, force)
}

// setCliVaultDefinition stores/overwrites a vault definition in the CLI preferences.
func setCliVaultDefinition(def VaultDefinition, force bool) error {
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

	cfg, err := readCliConfig(true)
	if err != nil {
		return err
	}

	if cfg.Vaults == nil {
		cfg.Vaults = make(map[string]VaultDefinition)
	}

	if existing, ok := cfg.Vaults[def.Name]; ok && existing.BasePath() != basePath && !force {
		return fmt.Errorf("%s: %s", RhizomeVaultExistsError, def.Name)
	}

	cfg.Vaults[def.Name] = def
	return writeCliConfig(cfg)
}

// removeCliVaultPath removes a vault entry from CLI preferences.
func removeCliVaultPath(name string) error {
	if name == "" {
		return errors.New(RhizomeVaultNameRequiredError)
	}

	cfg, err := readCliConfig(true)
	if err != nil {
		return err
	}

	if len(cfg.Vaults) == 0 {
		return fmt.Errorf("%s: %s", RhizomeVaultNotFoundError, name)
	}

	if _, ok := cfg.Vaults[name]; !ok {
		return fmt.Errorf("%s: %s", RhizomeVaultNotFoundError, name)
	}

	delete(cfg.Vaults, name)
	return writeCliConfig(cfg)
}

// listCliVaults returns manual vault mappings stored in preferences.
func listCliVaults() (map[string]VaultDefinition, string, error) {
	cfg, err := readCliConfig(true)
	if err != nil {
		return nil, "", err
	}

	vaults := cfg.Vaults
	if vaults == nil {
		vaults = make(map[string]VaultDefinition)
	}

	return vaults, cfg.DefaultVaultName, nil
}

// LoadCliConfig loads rhizome CLI preferences.
func LoadCliConfig(allowMissing bool) (CliConfig, error) {
	return readCliConfig(allowMissing)
}

// SaveCliConfig writes rhizome CLI preferences.
func SaveCliConfig(cfg CliConfig) error {
	return writeCliConfig(cfg)
}
