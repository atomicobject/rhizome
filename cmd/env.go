package cmd

import (
	"context"
	"errors"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type commandEnv struct {
	Getwd  func() (string, error)
	Config vaultconfig.Env
}
type commandEnvContextKey struct{}

func commandEnvFromContext(ctx context.Context) commandEnv {
	if ctx != nil {
		if env, ok := ctx.Value(commandEnvContextKey{}).(commandEnv); ok {
			return env
		}
	}
	return commandEnv{Config: vaultconfig.FromContext(ctx)}.withWorkingDirectory()
}

func contextWithCommandEnv(ctx context.Context, env commandEnv) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if env.Config.IsZero() {
		env.Config = vaultconfig.FromContext(ctx)
	}
	if env.Getwd != nil {
		if cwd, err := env.Getwd(); err == nil {
			env.Config.WorkingDir = cwd
		} else {
			env.Config.WorkingDir = ""
		}
	}
	env = env.withWorkingDirectory()
	return context.WithValue(vaultconfig.WithEnv(ctx, env.Config), commandEnvContextKey{}, env)
}

func (env commandEnv) withWorkingDirectory() commandEnv {
	if env.Getwd == nil {
		cwd := env.Config.WorkingDir
		env.Getwd = func() (string, error) {
			if cwd == "" {
				return "", errors.New("working directory unavailable")
			}
			return cwd, nil
		}
	}
	return env
}

func commandVault(ctx context.Context, name string) *repoconfig.Vault {
	env := commandEnvFromContext(ctx)
	return &repoconfig.Vault{Name: name, Env: env.Config}
}
