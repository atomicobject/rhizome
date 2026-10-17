package operations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type localCfgInfo struct {
	dir string
	cfg *repoconfig.LocalConfig
}

type resolvedContextInputs struct {
	VaultDef         obsidian.VaultDefinition
	VaultPath        string
	ProjectRoot      string
	ProjectRootPaths paths.VaultPaths
	FileCfg          repoconfig.LocalFileContextConfig
	LocalCfg         *localCfgInfo
	Profile          ContextProfile
}

func loadLocalConfigBestEffort(vaultPath string) (*localCfgInfo, error) {
	if vaultPath == "" {
		return nil, repoconfig.ErrNoLocalConfig
	}
	cfgDir, cfg, err := repoconfig.FindLocalConfig(vaultPath)
	if err != nil || cfg == nil {
		return nil, err
	}
	return &localCfgInfo{dir: cfgDir, cfg: cfg}, nil
}

func resolveContextInputs(vault repoconfig.VaultManager, profile ContextProfile) (resolvedContextInputs, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		return resolvedContextInputs{}, err
	}
	vaultPath := vaultDef.BasePath()

	projectRoot := vaultPath
	fileCfg := repoconfig.FileContextConfigDefaults
	localCfg, localCfgErr := loadLocalConfigBestEffort(vaultPath)
	if localCfgErr != nil && !errors.Is(localCfgErr, repoconfig.ErrNoLocalConfig) {
		return resolvedContextInputs{}, localCfgErr
	}
	if localCfg != nil {
		projectRoot = localCfg.dir
		fileCfg = repoconfig.FileContextConfigFromLocalOrDefault(localCfg.cfg)
	}
	if projectRoot == "" {
		projectRoot = vaultPath
	}

	projectRootPaths, _ := paths.NewVaultPaths(projectRoot)
	return resolvedContextInputs{
		VaultDef:         vaultDef,
		VaultPath:        vaultPath,
		ProjectRoot:      projectRoot,
		ProjectRootPaths: projectRootPaths,
		FileCfg:          fileCfg,
		LocalCfg:         localCfg,
		Profile:          resolveProfile(profile, localCfg, projectRoot),
	}, nil
}

func defaultContextGraphOptions(skipAnchors, skipEmbeds bool) vaultgraph.GraphAnalysisOptions {
	return vaultgraph.GraphAnalysisOptions{
		WikilinkOptions: markdownsyntax.WikilinkOptions{
			SkipAnchors: skipAnchors,
			SkipEmbeds:  skipEmbeds,
		},
		IncludeTags:       true,
		RecencyCascade:    true,
		RecencyCascadeSet: true,
	}
}

func resolveProfile(want ContextProfile, localCfg *localCfgInfo, projectRoot string) ContextProfile {
	switch want {
	case ContextProfileVault, ContextProfileCode:
		return want
	default:
	}
	// Auto: treat as code if code scanning is configured OR common code markers exist.
	if localCfg != nil && localCfg.cfg != nil {
		if len(localCfg.cfg.Code.Scan) > 0 || localCfg.cfg.Code.Go != nil || localCfg.cfg.Code.Python != nil || localCfg.cfg.Code.TypeScript != nil || localCfg.cfg.Code.JavaScript != nil {
			return ContextProfileCode
		}
	}
	for _, marker := range []string{"go.mod", "package.json", "pyproject.toml", "Cargo.toml"} {
		if _, err := os.Stat(filepath.Join(projectRoot, marker)); err == nil {
			return ContextProfileCode
		}
	}
	return ContextProfileVault
}

// loadCodeAnchorForContext returns a best-effort codeanchor service for the vault.
// Caller is responsible for closing the returned cleanup func when non-nil.
func loadCodeAnchorForContext(vaultPath string, store *semdb.Store) (*codeanchor.Service, func(), error) {
	basePath := openableVaultBasePath(vaultPath)
	if basePath == "" {
		return nil, nil, nil
	}
	var cleanup func()
	if store == nil {
		resolvedConfig2, configErr2 := repoconfig.Load(basePath)
		cfg, err := resolvedConfig2.Code, configErr2
		if err != nil {
			return nil, nil, err
		}
		opened, closeFn, err := indexstore.Open(basePath, indexstore.Options{Access: indexstore.ReadOnly, Config: &cfg, RequireEnabled: true})
		if err != nil {
			return nil, nil, err
		}
		if opened == nil {
			return nil, nil, nil
		}
		store = opened
		cleanup = closeFn
	}
	svc := codeanchor.NewServiceWithOptions(
		store,
		nil,
		codeanchor.WithBasePath(basePath),
		codeanchor.WithoutWarmCache(),
	)

	timeout := 5 * time.Second
	{
		resolvedConfig1, configErr1 := repoconfig.Load(basePath)
		cfg, err := resolvedConfig1.Code, configErr1
		if err == nil {
			if candidate := cfg.EffectiveRecomputeTimeout(); candidate > 0 {
				timeout = candidate
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = svc.RecomputeAnchorScopes(ctx) // best-effort; fall back to existing scopes on error

	return svc, cleanup, nil
}
