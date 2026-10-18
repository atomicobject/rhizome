package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Env contains the host inputs for one invocation. Paths are resolved once at
// the application boundary; separate invocations never share mutable overrides.
// A supplied Env is complete: missing paths remain unavailable rather than
// falling back to another user's configuration.
type Env struct {
	HomeDir            string
	ConfigDir          string
	WorkingDir         string
	CLIConfigFile      string
	ObsidianConfigFile string
	LookupEnv          func(string) (string, bool)
}

// SystemEnv captures the host paths without reading configuration or secrets.
func SystemEnv() Env {
	home, _ := os.UserHomeDir()
	configDir, _ := os.UserConfigDir()
	cwd, _ := os.Getwd()
	return Env{HomeDir: home, ConfigDir: configDir, WorkingDir: cwd, LookupEnv: os.LookupEnv}
}

func (e Env) CliPath() (string, string, error) {
	if e.CLIConfigFile != "" {
		return filepath.Dir(e.CLIConfigFile), e.CLIConfigFile, nil
	}
	if e.HomeDir == "" {
		return "", "", errors.New(UserConfigDirectoryNotFoundErrorMessage)
	}
	dir := filepath.Join(e.HomeDir, ".config", RhizomeConfigDirectory)
	return dir, filepath.Join(dir, RhizomeConfigFile), nil
}

func (e Env) ObsidianFile() (string, error) {
	if e.ObsidianConfigFile != "" {
		return e.ObsidianConfigFile, nil
	}
	if e.ConfigDir == "" {
		return "", errors.New(UserConfigDirectoryNotFoundErrorMessage)
	}
	return filepath.Join(e.ConfigDir, ObsidianConfigDirectory, ObsidianConfigFile), nil
}

func (e Env) Lookup(key string) (string, bool) {
	if e.LookupEnv == nil {
		return "", false
	}
	return e.LookupEnv(key)
}

type envContextKey struct{}

// WithEnv carries an invocation's explicit environment across application APIs.
func WithEnv(ctx context.Context, env Env) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, envContextKey{}, env)
}

// FromContext resolves system defaults only when no invocation supplied an Env.
func FromContext(ctx context.Context) Env {
	if ctx != nil {
		if env, ok := ctx.Value(envContextKey{}).(Env); ok {
			return env
		}
	}
	return SystemEnv()
}

// IsZero reports whether no invocation environment was supplied.
func (e Env) IsZero() bool {
	return e.HomeDir == "" && e.ConfigDir == "" && e.WorkingDir == "" && e.CLIConfigFile == "" && e.ObsidianConfigFile == "" && e.LookupEnv == nil
}
