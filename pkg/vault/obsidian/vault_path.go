package obsidian

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/config"
)

var ObsidianConfigFile = config.ObsidianFile

func (v *Vault) Path() (string, error) {
	def, err := v.Definition()
	if err != nil {
		return "", err
	}
	if def.Path != "" {
		return def.Path, nil
	}
	if def.Root != "" {
		return def.Root, nil
	}
	return "", errors.New(RhizomeConfigParseError)
}

// Definition resolves the vault definition from:
//  1. A path to a directory containing .rhizome/config.yml (if Name looks like a path)
//  2. CLI preferences (by vault name)
//  3. Obsidian's own config (by vault name)
func (v *Vault) Definition() (VaultDefinition, error) {
	if v.Name == "" {
		return VaultDefinition{}, errors.New(RhizomeVaultNameRequiredError)
	}

	// Path-shaped names are resolved first so repo-local config wins over any
	// global vault preference with the same trailing name.
	if def, ok := v.definitionFromLocalConfig(); ok {
		return def, nil
	}

	if def, ok, err := v.definitionFromCliConfig(); err != nil {
		return VaultDefinition{}, err
	} else if ok {
		return def, nil
	}

	path, err := v.pathFromObsidianConfig()
	if err != nil {
		return VaultDefinition{}, err
	}

	return VaultDefinition{
		Name: v.Name,
		Path: path,
	}, nil
}

// definitionFromLocalConfig checks if v.Name is a path to a directory
// containing a .rhizome/config.yml file.
func (v *Vault) definitionFromLocalConfig() (VaultDefinition, bool) {
	// Check if Name looks like a path (contains path separator or starts with . or /)
	if !looksLikePath(v.Name) {
		return VaultDefinition{}, false
	}

	// Check if the path exists and is a directory
	info, err := os.Stat(v.Name)
	if err != nil || !info.IsDir() {
		return VaultDefinition{}, false
	}

	// Try to load local config from this directory
	def, err := LoadDefinitionFromPath(v.Name)
	if err != nil {
		return VaultDefinition{}, false
	}

	return def, true
}

// looksLikePath returns true if s appears to be a filesystem path rather than a vault name.
func looksLikePath(s string) bool {
	return strings.ContainsAny(s, "/\\") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
}

func (v *Vault) definitionFromCliConfig() (VaultDefinition, bool, error) {
	cfg, err := readCliConfig(true)
	if err != nil {
		return VaultDefinition{}, false, err
	}

	entry, ok := cfg.Vaults[v.Name]
	if !ok {
		return VaultDefinition{}, false, nil
	}

	if entry.Path == "" && entry.Root == "" {
		return VaultDefinition{}, false, errors.New(RhizomeConfigParseError)
	}

	if entry.Name == "" {
		entry.Name = v.Name
	}

	return entry, true, nil
}

func (v *Vault) pathFromObsidianConfig() (string, error) {
	configFile := ObsidianConfigFile
	if v.ObsidianConfigFile != nil {
		configFile = v.ObsidianConfigFile
	}
	obsidianConfigFile, err := configFile()
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(obsidianConfigFile)
	if err != nil {
		return "", errors.New(ObsidianConfigReadError)
	}

	vaultsContent := ObsidianVaultConfig{}
	err = json.Unmarshal(content, &vaultsContent)

	if err != nil {
		return "", errors.New(ObsidianConfigParseError)
	}

	for _, element := range vaultsContent.Vaults {
		// Obsidian stores vaults keyed by UUID-ish ids; historically Rhizome
		// matched by the path suffix users type as the vault name.
		if strings.HasSuffix(element.Path, v.Name) {
			return element.Path, nil
		}
	}

	return "", errors.New(ObsidianConfigVaultNotFoundError)
}

func (v *Vault) SavePathToPreferences(path string, force bool) error {
	return setCliVaultPath(v.Name, path, force)
}

// SaveDefinitionToPreferences saves a full VaultDefinition to CLI preferences.
func SaveDefinitionToPreferences(def VaultDefinition, force bool) error {
	return setCliVaultDefinition(def, force)
}

func (v *Vault) RemoveFromPreferences() error {
	return removeCliVaultPath(v.Name)
}

func ListPreferenceVaults() (map[string]VaultDefinition, string, error) {
	return listCliVaults()
}

func ListObsidianVaults() (map[string]VaultPathEntry, error) {
	obsidianConfigFile, err := ObsidianConfigFile()
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(obsidianConfigFile)
	if err != nil {
		return nil, errors.New(ObsidianConfigReadError)
	}

	vaultsContent := ObsidianVaultConfig{}
	if err := json.Unmarshal(content, &vaultsContent); err != nil {
		return nil, errors.New(ObsidianConfigParseError)
	}

	if vaultsContent.Vaults == nil {
		vaultsContent.Vaults = make(map[string]VaultPathEntry)
	}
	return vaultsContent.Vaults, nil
}
