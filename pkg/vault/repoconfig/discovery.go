package repoconfig

import (
	"bytes"

	"errors"
	"fmt"

	"os"
	"path/filepath"

	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"

	"github.com/atomicobject/rhizome/pkg/paths"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"gopkg.in/yaml.v3"
)

// FindLocalConfig searches for a .rhizome/config.yml file starting from dir
// and walking up to parent directories. Returns the directory containing
// the config and the parsed config, or ErrNoLocalConfig if not found.
func FindLocalConfig(startDir string) (string, *LocalConfig, error) {
	dir := paths.ResolveSymlinks(startDir).String()
	if dir == "" {
		return "", nil, errors.New("start directory required")
	}
	gitRoot := findNearestGitRoot(dir)

	for {
		cfg, err := LoadLocalConfig(dir)
		if err == nil {
			return dir, cfg, nil
		}
		if !errors.Is(err, ErrNoLocalConfig) {
			return "", nil, err
		}
		if gitRoot != "" && dir == gitRoot {
			// Stop at the nearest repo root so tools launched in a nested project
			// do not inherit a parent checkout's Rhizome config.
			return "", nil, ErrNoLocalConfig
		}

		dirOS := filepath.FromSlash(dir)
		parent := filepath.Dir(dirOS)
		if parent == dirOS {
			// Reached filesystem root
			return "", nil, ErrNoLocalConfig
		}
		dir = paths.ResolveSymlinks(parent).String()
	}
}

// FindLocalConfigForDelegation loads only repo-binary selection fields. The
// selected binary, not the invoking global binary, owns the full config schema.
func FindLocalConfigForDelegation(startDir string) (string, *LocalConfig, error) {
	dir := paths.ResolveSymlinks(startDir).String()
	if dir == "" {
		return "", nil, errors.New("start directory required")
	}
	gitRoot := findNearestGitRoot(dir)

	for {
		configPath := rhizomeConfigPath(dir)
		data, err := fileio.ReadFile(configPath)
		if err == nil {
			var selection struct {
				Rhizome LocalRhizomeConfig `yaml:"rhizome,omitempty"`
			}
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			if err := decoder.Decode(&selection); err != nil {
				return "", nil, err
			}
			if err := ensureSingleYAMLDocument(decoder); err != nil {
				return "", nil, err
			}
			selection.Rhizome.BinaryManager = strings.TrimSpace(selection.Rhizome.BinaryManager)
			if _, err := selection.Rhizome.UsesExternalBinaryManager(); err != nil {
				return "", nil, fmt.Errorf("%s: %w", configPath, err)
			}
			return dir, &LocalConfig{Rhizome: selection.Rhizome}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}
		if gitRoot != "" && dir == gitRoot {
			return "", nil, ErrNoLocalConfig
		}
		dirOS := filepath.FromSlash(dir)
		parent := filepath.Dir(dirOS)
		if parent == dirOS {
			return "", nil, ErrNoLocalConfig
		}
		dir = paths.ResolveSymlinks(parent).String()
	}
}

func findNearestGitRoot(startDir string) string {
	dir := paths.ResolveSymlinks(startDir).String()
	if dir == "" {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(filepath.FromSlash(dir), ".git")); err == nil {
			return dir
		}
		dirOS := filepath.FromSlash(dir)
		parent := filepath.Dir(dirOS)
		if parent == dirOS {
			return ""
		}
		dir = paths.ResolveSymlinks(parent).String()
	}
}

// LocalConfigToDefinition converts a LocalConfig to a VaultDefinition,
// resolving relative paths against the config file's directory.
func LocalConfigToDefinition(configDir string, cfg *LocalConfig) obsidian.VaultDefinition {
	// The local config format is repo-rooted: includes/excludes stay as authoring
	// globs and are interpreted by discovery/ignore code relative to Root.
	def := obsidian.VaultDefinition{
		Root:     configDir,
		Includes: cfg.Notes.Includes,
		Excludes: cfg.Notes.Excludes,
		Links:    cfg.Notes.Links,
	}

	if len(def.Includes) == 0 {
		def.Includes = []string{"**/*.md"}
	}

	return def
}

// LoadDefinitionFromPath attempts to load a VaultDefinition from a directory
// containing a local config file. Returns ErrNoLocalConfig if not found.
func LoadDefinitionFromPath(dir string) (obsidian.VaultDefinition, error) {
	absDir := paths.ResolveSymlinks(dir).String()
	if absDir == "" {
		return obsidian.VaultDefinition{}, errors.New("directory required")
	}

	cfg, err := LoadLocalConfig(absDir)
	if err != nil {
		return obsidian.VaultDefinition{}, err
	}

	def := LocalConfigToDefinition(absDir, cfg)
	// Use directory name as vault name if not otherwise specified
	def.Name = filepath.Base(absDir)
	return def, nil
}

// HasLocalConfig checks if a directory contains a local config file.
func HasLocalConfig(dir string) bool {
	_, err := os.Stat(rhizomeConfigPath(dir))
	return err == nil
}
