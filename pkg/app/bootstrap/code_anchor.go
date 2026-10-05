package bootstrap

import (
	"context"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

// CodeAnchorServiceConfig configures code anchor service creation.
type CodeAnchorServiceConfig struct {
	VaultPath        string
	CodeCfg          codeanchor.Config
	Store            *semdb.Store
	TailIndex        *codeanchor.PathTailIndex
	Linker           codeanchor.CodeDocLinker
	WarmCacheContext context.Context
	IncludeIndexers  bool
	WriteAccess      bool // enables index updates (scope recompute, call edge rebuild)
}

// NewCodeAnchorService creates a code anchor service with the given configuration.
func NewCodeAnchorService(cfg CodeAnchorServiceConfig) (*codeanchor.Service, *codeanchor.PathTailIndex) {
	tailIdx := cfg.TailIndex
	if tailIdx == nil {
		tailIdx = codeanchor.NewPathTailIndex(5)
	}

	var indexers []codeanchor.LanguageIndexer
	if cfg.IncludeIndexers {
		limits := cfg.CodeCfg.TreeSitterLimits()
		// Automatic scope covers the whole project, so module names come from
		// the indexers' default source folders instead of the project root.
		pyRoots := []string{"src", "lib"}
		phpRoots := []string{"src", "app", "lib"}
		if !cfg.CodeCfg.AutomaticScope {
			if configured := ResolveRoots(cfg.VaultPath, cfg.CodeCfg.PythonRoots); len(configured) > 0 {
				pyRoots = configured
			}
			if configured := ResolveRoots(cfg.VaultPath, cfg.CodeCfg.PHPRoots); len(configured) > 0 {
				phpRoots = configured
			}
		}
		pyIndexer := codeanchor.NewPythonIndexerWithRootsAndLimits(pyRoots, limits)
		indexers = []codeanchor.LanguageIndexer{
			pyIndexer,
			codeanchor.NewGoIndexer(),
			codeanchor.NewTSIndexerWithRootAndTailIndexAndLimits(cfg.VaultPath, tailIdx, limits),
			codeanchor.NewCSharpIndexerWithRootAndLimits(cfg.VaultPath, limits),
			codeanchor.NewPHPIndexerWithRootsAndLimits(phpRoots, limits),
		}
		// Under automatic scope every language covers the whole project, so
		// code.disabledLanguages is how a repository leaves a language out.
		if cfg.CodeCfg.AutomaticScope {
			kept := indexers[:0]
			for _, indexer := range indexers {
				if !cfg.CodeCfg.LanguageDisabled(indexer.Lang()) {
					kept = append(kept, indexer)
				}
			}
			indexers = kept
		}
	}

	opts := []codeanchor.ServiceOption{
		codeanchor.WithBasePath(cfg.VaultPath),
		codeanchor.WithPathTailIndex(tailIdx),
	}
	if cfg.Linker != nil {
		opts = append(opts, codeanchor.WithCodeDocLinker(cfg.Linker))
	}
	if cfg.WarmCacheContext != nil {
		opts = append(opts, codeanchor.WithWarmCacheContext(cfg.WarmCacheContext))
	} else {
		opts = append(opts, codeanchor.WithoutWarmCache())
	}
	if cfg.WriteAccess {
		opts = append(opts, codeanchor.WithWriteAccess())
	}

	return codeanchor.NewServiceWithOptions(cfg.Store, indexers, opts...), tailIdx
}
