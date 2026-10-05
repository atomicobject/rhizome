package config

import (
	"errors"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// GlobalConfig captures the home-level Rhizome config fields that generic value
// resolution needs. Unknown fields are ignored when the full config is parsed.
type GlobalConfig struct {
	Env map[string]string `yaml:"env,omitempty"`
}

// LoadGlobalConfig reads ~/.config/rhizome/config.yml and returns the subset of
// fields relevant to lower-level packages.
func LoadGlobalConfig(allowMissing bool) (GlobalConfig, error) {
	_, cliConfigFile, err := CliPath()
	if err != nil {
		return GlobalConfig{}, err
	}

	content, err := os.ReadFile(cliConfigFile)
	if err != nil {
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return GlobalConfig{}, nil
		}
		return GlobalConfig{}, err
	}

	var cfg GlobalConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return GlobalConfig{}, err
	}
	return cfg, nil
}

// ResolveValue returns the first non-empty value for the requested keys.
//
// Precedence:
//  1. Process environment (including project .env values already loaded there)
//  2. Global Rhizome config env block (~/.config/rhizome/config.yml)
func ResolveValue(keys ...string) string {
	// Keep this helper deliberately narrow. Project-local .rhizome/config.yml is
	// parsed by obsidian/local config code; lower-level packages only need a
	// generic "env-like value" lookup that cannot recursively depend on vault
	// discovery.
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}

	cfg, err := LoadGlobalConfig(true)
	if err != nil {
		return ""
	}
	for _, key := range keys {
		if value := strings.TrimSpace(cfg.Env[key]); value != "" {
			return value
		}
	}
	return ""
}
