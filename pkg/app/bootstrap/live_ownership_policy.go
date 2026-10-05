package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type liveNoteSelectionState struct {
	policy cache.SelectionPolicy
}

func (rt *LiveRuntime) setNoteSelectionPolicy(policy cache.SelectionPolicy) {
	if rt != nil {
		rt.noteSelection.Store(&liveNoteSelectionState{policy: policy})
	}
}

func (rt *LiveRuntime) currentOwnershipVaultDefinition() obsidian.VaultDefinition {
	if rt != nil {
		if current := rt.ownershipVaultDef.Load(); current != nil {
			return *current
		}
		return rt.VaultDef
	}
	return obsidian.VaultDefinition{}
}

func (rt *LiveRuntime) reloadOwnershipSelectionLocked() error {
	updated, err := loadWatcherVaultDefinition(rt.VaultPath, rt.currentOwnershipVaultDefinition())
	if err != nil {
		return err
	}
	codeCfg, err := obsidian.LoadCodeConfig(rt.VaultPath)
	if err != nil {
		if current := rt.codeCfg.Load(); current != nil {
			codeCfg = *current
		} else {
			codeCfg = codeanchor.DefaultConfig(rt.VaultPath)
		}
	}
	codeCfg.IndexPath = obsidian.UnifiedIndexPath(rt.VaultPath, codeCfg.IndexPath)
	policy, err := liveCacheSelectionPolicy(updated, rt.noteFormats, codeCfg)
	if err != nil {
		return err
	}
	if hub := rt.hub.Load(); hub != nil {
		hub.ReplaceUserExcludes(updated.Excludes)
	}
	if cacheService := rt.cache.Load(); cacheService != nil {
		cacheService.ReplaceSelectionPolicy(policy)
	}
	rt.codeCfg.Store(&codeCfg)
	rt.ownershipVaultDef.Store(&updated)
	rt.setNoteSelectionPolicy(policy)
	return nil
}

func (rt *LiveRuntime) leaderOwnershipConfiguration() (obsidian.VaultDefinition, codeanchor.Config, error) {
	if rt == nil {
		return obsidian.VaultDefinition{}, codeanchor.Config{}, nil
	}
	rt.ownershipConfigMu.Lock()
	defer rt.ownershipConfigMu.Unlock()
	err := rt.reloadOwnershipSelectionLocked()
	vaultDef := rt.currentOwnershipVaultDefinition()
	codeCfg := codeanchor.DefaultConfig(rt.VaultPath)
	if current := rt.codeCfg.Load(); current != nil {
		codeCfg = *current
	}
	return vaultDef, codeCfg, err
}

func (rt *LiveRuntime) publishPhase3OwnershipConfiguration() (codeanchor.Config, error) {
	if rt == nil {
		return codeanchor.Config{}, nil
	}
	rt.ownershipConfigMu.Lock()
	defer rt.ownershipConfigMu.Unlock()

	codeCfg := loadLiveCodeConfig(rt.VaultPath)
	if cacheService := rt.cache.Load(); cacheService != nil {
		policy, policyErr := liveCacheSelectionPolicy(rt.currentOwnershipVaultDefinition(), rt.noteFormats, codeCfg)
		if policyErr != nil {
			return codeanchor.Config{}, policyErr
		}
		cacheService.ReplaceSelectionPolicy(policy)
		rt.setNoteSelectionPolicy(policy)
	}
	rt.codeCfg.Store(&codeCfg)
	return codeCfg, nil
}

// NotePathOwned reports whether the current vault and code configuration
// assign a canonical path to an executable note provider. Startup readiness
// uses this same selector as live cache and ownership discovery.
func (rt *LiveRuntime) NotePathOwned(rawPath string) bool {
	if rt == nil {
		return false
	}
	notePath, err := paths.CleanNotePath(rawPath)
	if err != nil || notePath.String() != rawPath {
		return false
	}
	policy, err := rt.noteSelectionPolicy()
	return err == nil && policy.Admit(notePath)
}

// UsableNoteMetadataCount evaluates the active provider and ownership policy
// once, then counts durable exact-note rows that remain readable.
func (rt *LiveRuntime) UsableNoteMetadataCount(ctx context.Context) (int64, error) {
	if rt == nil {
		return 0, nil
	}
	store := rt.IntelStore()
	if store == nil {
		return 0, nil
	}
	policy, err := rt.noteSelectionPolicy()
	if err != nil {
		return 0, err
	}
	return rt.noteMetadataIndexer.UsableNoteMetadataCount(ctx, store, func(path string) bool {
		notePath, cleanErr := paths.CleanNotePath(path)
		return cleanErr == nil && notePath.String() == path && policy.Admit(notePath)
	})
}

func (rt *LiveRuntime) noteSelectionPolicy() (cache.SelectionPolicy, error) {
	if current := rt.noteSelection.Load(); current != nil {
		return current.policy, nil
	}
	codeCfg := rt.codeCfg.Load()
	if codeCfg == nil {
		loaded := loadLiveCodeConfig(rt.VaultPath)
		codeCfg = &loaded
	}
	policy, err := liveCacheSelectionPolicy(rt.currentOwnershipVaultDefinition(), rt.noteFormats, *codeCfg)
	if err == nil {
		rt.setNoteSelectionPolicy(policy)
	}
	return policy, err
}

// liveCacheSelectionPolicy makes the cache a projection of the ownership
// selector. Cache rejection never suppresses the raw event consumed by the
// ownership coordinator.
func liveCacheSelectionPolicy(vaultDef obsidian.VaultDefinition, runtime noteformat.Runtime, codeCfg codeanchor.Config) (cache.SelectionPolicy, error) {
	roots := liveCodeRoots(vaultDef.BasePath(), codeCfg)
	selector, err := noteownership.CompileSelector(noteownership.SelectorInput{
		VaultDefinition: vaultDef,
		Registry:        runtime.Registry(),
		CodeRoots:       roots,
		CodeLanguage: func(ref paths.CodePathRef) codeanchor.Lang {
			return detectCodeLang(ref.Abs.String())
		},
	})
	if err != nil {
		return cache.SelectionPolicy{}, err
	}
	policy := cache.SelectionPolicy{
		UserExcludes: vaultDef.Excludes,
		Admit: func(path paths.NotePath) bool {
			selection, err := selector.Select(paths.RelPath(path))
			return err == nil && selection.Owner == notediscovery.Note && runtime.CanProject(selection.Provider)
		},
	}
	if vaultDef.IsCollection() {
		policy.DiscoverFiles = func() ([]string, error) { return obsidian.DiscoverFiles(vaultDef) }
	}
	return policy, nil
}

func liveCodeRoots(vaultPath string, codeCfg codeanchor.Config) []paths.AbsPath {
	configured := append([]string{}, codeCfg.PythonRoots...)
	configured = append(configured, codeCfg.GoRoots...)
	configured = append(configured, codeCfg.TSRoots...)
	configured = append(configured, codeCfg.CSharpRoots...)
	configured = append(configured, codeCfg.PHPRoots...)
	seen := make(map[string]struct{}, len(configured))
	roots := make([]paths.AbsPath, 0, len(configured))
	for _, root := range configured {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			root = filepath.Join(vaultPath, root)
		}
		canonical := paths.NormalizeAbsPathForCompare(root)
		if canonical == "" {
			continue
		}
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		roots = append(roots, paths.AbsPath(root))
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].String() < roots[j].String() })
	return roots
}

// loadLiveCodeConfig normalizes the default used during initial composition.
// Follower reloads separately retain their last valid config on load failure.
func loadLiveCodeConfig(vaultPath string) codeanchor.Config {
	codeCfg, err := obsidian.LoadCodeConfig(vaultPath)
	if err != nil {
		codeCfg = codeanchor.DefaultConfig(vaultPath)
	}
	codeCfg.IndexPath = obsidian.UnifiedIndexPath(vaultPath, codeCfg.IndexPath)
	return codeCfg
}

func (w *unifiedSemanticWatcher) reloadOwnershipSelection() error {
	updated, err := loadWatcherVaultDefinition(w.vaultPath, w.vaultDef)
	if err != nil {
		return fmt.Errorf("reload vault definition: %w", err)
	}
	codeCfg, err := obsidian.LoadCodeConfig(w.vaultPath)
	if err != nil {
		// Ignore-file changes still replace the selector when code configuration
		// is absent. Keep the last known code rules rather than rejecting the
		// new note scope because no code section was configured.
		codeCfg = w.codeCfg
		if codeCfg.IndexPath == "" {
			codeCfg = codeanchor.DefaultConfig(w.vaultPath)
		}
	}
	codeCfg.IndexPath = obsidian.UnifiedIndexPath(w.vaultPath, codeCfg.IndexPath)
	policy, err := liveCacheSelectionPolicy(updated, w.noteRuntime, codeCfg)
	if err != nil {
		return fmt.Errorf("compile cache ownership selection: %w", err)
	}
	if w.watchHub != nil {
		w.watchHub.ReplaceUserExcludes(updated.Excludes)
	}
	w.cacheService.ReplaceSelectionPolicy(policy)
	w.vaultDef = updated
	w.codeCfg = codeCfg
	if w.onNoteSelectionChanged != nil {
		w.onNoteSelectionChanged(policy)
	}
	return nil
}
