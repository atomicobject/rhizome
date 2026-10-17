package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func (commandOptions *vaultCommandOptions) vaultDefOrDefaultContext(ctx context.Context) (obsidian.VaultDefinition, error) {
	env := commandEnvFromContext(ctx)
	if cwd, err := env.Getwd(); err == nil {
		def, defErr := localVaultDefFromCWD(cwd)
		switch {
		case defErr == nil && (commandOptions.vaultName == "" || localVaultMatchesName(def, commandOptions.vaultName)):
			return def, nil
		case defErr != nil && !errors.Is(defErr, repoconfig.ErrNoLocalConfig):
			return obsidian.VaultDefinition{}, defErr
		}
	}
	if commandOptions.vaultName == "" {
		v := commandVault(ctx, "")
		defaultName, err := v.DefaultName()
		if err != nil {
			return obsidian.VaultDefinition{}, err
		}
		return commandVault(ctx, defaultName).Definition()
	}
	return commandVault(ctx, commandOptions.vaultName).Definition()
}

func localVaultDefFromCWD(cwd string) (obsidian.VaultDefinition, error) {
	cfgDir, cfg, err := repoconfig.FindLocalConfig(cwd)
	if err != nil {
		return obsidian.VaultDefinition{}, err
	}
	def := repoconfig.LocalConfigToDefinition(cfgDir, cfg)
	if strings.TrimSpace(def.Name) == "" {
		def.Name = filepath.Base(cfgDir)
	}
	return def, nil
}

func localVaultMatchesName(def obsidian.VaultDefinition, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	return name == strings.TrimSpace(def.Name) || name == filepath.Base(def.BasePath())
}

func (commandOptions *vaultCommandOptions) loadVaultAndConfigContext(ctx context.Context) (string, obsidian.VaultDefinition, repoconfig.Resolved, error) {
	vaultDef, err := commandOptions.vaultDefOrDefaultContext(ctx)
	if err != nil {
		return "", obsidian.VaultDefinition{}, repoconfig.Resolved{}, err
	}
	vaultPath := vaultDef.BasePath()
	resolved, err := repoconfig.Load(vaultPath)
	return vaultPath, vaultDef, resolved, err
}
