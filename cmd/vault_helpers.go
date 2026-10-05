package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/semanticops"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func vaultDefOrDefault() (obsidian.VaultDefinition, error) {
	return vaultDefOrDefaultContext(context.Background())
}

func vaultDefOrDefaultContext(ctx context.Context) (obsidian.VaultDefinition, error) {
	env := commandEnvFromContext(ctx)
	if cwd, err := env.Getwd(); err == nil {
		def, defErr := localVaultDefFromCWD(cwd)
		switch {
		case defErr == nil && (vaultName == "" || localVaultMatchesName(def, vaultName)):
			return def, nil
		case defErr != nil && !errors.Is(defErr, obsidian.ErrNoLocalConfig):
			return obsidian.VaultDefinition{}, defErr
		}
	}
	if vaultName == "" {
		v := commandVault(ctx, "")
		defaultName, err := v.DefaultName()
		if err != nil {
			return obsidian.VaultDefinition{}, err
		}
		return commandVault(ctx, defaultName).Definition()
	}
	return commandVault(ctx, vaultName).Definition()
}

func localVaultDefFromCWD(cwd string) (obsidian.VaultDefinition, error) {
	cfgDir, cfg, err := obsidian.FindLocalConfig(cwd)
	if err != nil {
		return obsidian.VaultDefinition{}, err
	}
	def := obsidian.LocalConfigToDefinition(cfgDir, cfg)
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

func loadVaultAndConfigContext(ctx context.Context) (string, obsidian.VaultDefinition, embeddings.Config, error) {
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return "", obsidian.VaultDefinition{}, embeddings.Config{}, err
	}
	vaultPath := vaultDef.BasePath()

	embCfg, err := obsidian.LoadEmbeddingsConfig(vaultPath)
	if err != nil {
		return "", obsidian.VaultDefinition{}, embeddings.Config{}, err
	}
	embCfg.IndexPath = obsidian.UnifiedIndexPath(vaultPath, embCfg.IndexPath)
	return vaultPath, vaultDef, embCfg, nil
}

func prepareProvider(cfg embeddings.Config) (embeddings.Provider, embeddings.ProviderConfig, error) {
	return semanticops.PrepareProvider(cfg, "")
}
