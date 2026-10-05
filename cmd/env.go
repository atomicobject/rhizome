package cmd

import (
	"context"
	"os"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type commandEnv struct {
	Getwd              func() (string, error)
	ObsidianConfigFile func() (string, error)
}

type commandEnvContextKey struct{}

func defaultCommandEnv() commandEnv {
	return commandEnv{
		Getwd:              os.Getwd,
		ObsidianConfigFile: obsidian.ObsidianConfigFile,
	}
}

func commandEnvFromContext(ctx context.Context) commandEnv {
	if ctx != nil {
		if env, ok := ctx.Value(commandEnvContextKey{}).(commandEnv); ok {
			return env.withDefaults()
		}
	}
	return defaultCommandEnv()
}

func contextWithCommandEnv(ctx context.Context, env commandEnv) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, commandEnvContextKey{}, env.withDefaults())
}

func (env commandEnv) withDefaults() commandEnv {
	defaults := defaultCommandEnv()
	if env.Getwd == nil {
		env.Getwd = defaults.Getwd
	}
	if env.ObsidianConfigFile == nil {
		env.ObsidianConfigFile = defaults.ObsidianConfigFile
	}
	return env
}

func commandVault(ctx context.Context, name string) *obsidian.Vault {
	env := commandEnvFromContext(ctx)
	return &obsidian.Vault{
		Name:               name,
		ObsidianConfigFile: env.ObsidianConfigFile,
	}
}
