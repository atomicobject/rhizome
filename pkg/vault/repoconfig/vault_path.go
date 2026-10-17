package repoconfig

import (
	"encoding/json"
	"errors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

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
func (v *Vault) Definition() (obsidian.VaultDefinition, error) {
	if v.Name == "" {
		return obsidian.VaultDefinition{}, errors.New(RhizomeVaultNameRequiredError)
	}

	// Path-shaped names are resolved first so repo-local config wins over any
	// global vault preference with the same trailing name.
	if def, ok, err := v.definitionFromLocalConfig(); ok || err != nil {
		return def, err
	}

	if def, ok, err := v.definitionFromCliConfig(); err != nil {
		return obsidian.VaultDefinition{}, err
	} else if ok {
		return def, nil
	}

	path, err := v.pathFromObsidianConfig()
	if err != nil {
		return obsidian.VaultDefinition{}, err
	}

	return obsidian.VaultDefinition{
		Name: v.Name,
		Path: path,
	}, nil
}

// definitionFromLocalConfig checks if v.Name is a path to a directory
// containing a .rhizome/config.yml file.
// ResolvePathName resolves a path-shaped name against this invocation's
// working directory. It never consults the process working directory.
func (v *Vault) ResolvePathName(name string) (string, error) {
	if !filepath.IsAbs(name) && (v.Env.WorkingDir == "" || !filepath.IsAbs(v.Env.WorkingDir)) {
		return "", errors.New("working directory unavailable")
	}
	absolute := paths.AbsFromInput(v.Env.WorkingDir, name).String()
	if resolved := paths.ResolveSymlinks(absolute); resolved != "" {
		absolute = resolved.String()
	}
	return absolute, nil
}

func (v *Vault) definitionFromLocalConfig() (obsidian.VaultDefinition, bool, error) {
	if !looksLikePath(v.Name) {
		return obsidian.VaultDefinition{}, false, nil
	}
	path, err := v.ResolvePathName(v.Name)
	if err != nil {
		return obsidian.VaultDefinition{}, false, err
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return obsidian.VaultDefinition{}, false, nil
	}
	def, err := LoadDefinitionFromPath(path)
	if err != nil {
		return obsidian.VaultDefinition{}, false, err
	}
	return def, true, nil
}

func looksLikePath(s string) bool {
	return strings.ContainsAny(s, "/\\") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
}

func (v *Vault) definitionFromCliConfig() (obsidian.VaultDefinition, bool, error) {
	cfg, err := readCliConfig(v.Env, true)
	if err != nil {
		return obsidian.VaultDefinition{}, false, err
	}

	entry, ok := cfg.Vaults[v.Name]
	if !ok {
		return obsidian.VaultDefinition{}, false, nil
	}

	if entry.Path == "" && entry.Root == "" {
		return obsidian.VaultDefinition{}, false, errors.New(RhizomeConfigParseError)
	}

	if entry.Name == "" {
		entry.Name = v.Name
	}

	return entry, true, nil
}

func (v *Vault) pathFromObsidianConfig() (string, error) {
	obsidianConfigFile, err := v.Env.ObsidianFile()
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
	return setCliVaultPath(v.Env, v.Name, path, force)
}

// SaveDefinitionToPreferences saves a full VaultDefinition to CLI preferences.
func SaveDefinitionToPreferences(env config.Env, def obsidian.VaultDefinition, force bool) error {
	return setCliVaultDefinition(env, def, force)
}

func (v *Vault) RemoveFromPreferences() error {
	return removeCliVaultPath(v.Env, v.Name)
}

func ListPreferenceVaults(env config.Env) (map[string]obsidian.VaultDefinition, string, error) {
	return listCliVaults(env)
}

func ListObsidianVaults(env config.Env) (map[string]VaultPathEntry, error) {
	obsidianConfigFile, err := env.ObsidianFile()
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
