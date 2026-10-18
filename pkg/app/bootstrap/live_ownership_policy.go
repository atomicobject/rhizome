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
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
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

// publishOwnershipConfiguration changes the selection and its public snapshot
// only after proving that existing resources remain valid for the new settings.
func (rt *LiveRuntime) publishOwnershipConfiguration(definition obsidian.VaultDefinition, resolved repoconfig.Resolved, policy cache.SelectionPolicy) error {
	rt.ownershipConfigMu.Lock()
	defer rt.ownershipConfigMu.Unlock()
	if err := resolved.ValidateFor(rt.VaultPath); err != nil {
		return err
	}
	if err := validateLiveResourceConfiguration(rt.Configuration(), resolved); err != nil {
		return err
	}
	if hub := rt.hub.Load(); hub != nil {
		hub.ReplaceUserExcludes(definition.Excludes)
	}
	if service := rt.cache.Load(); service != nil {
		service.ReplaceSelectionPolicy(policy)
	}
	rt.resolvedConfig.Store(&resolved)
	codeCfg := resolved.Code
	rt.codeCfg.Store(&codeCfg)
	rt.ownershipVaultDef.Store(&definition)
	rt.setNoteSelectionPolicy(policy)
	return nil
}

func (rt *LiveRuntime) publishPhase3OwnershipConfiguration() (codeanchor.Config, error) {
	if rt == nil {
		return codeanchor.Config{}, nil
	}
	rt.ownershipConfigMu.Lock()
	defer rt.ownershipConfigMu.Unlock()

	codeCfg := rt.Configuration().Code
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
		loaded := rt.Configuration().Code
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

func (w *unifiedSemanticWatcher) reloadOwnershipSelection() error {
	resolved, err := repoconfig.Load(w.vaultPath)
	if err != nil {
		return fmt.Errorf("reload configuration: %w", err)
	}
	updated := definitionForConfiguration(w.vaultPath, w.vaultDef, resolved)
	codeCfg := resolved.Code
	policy, err := liveCacheSelectionPolicy(updated, w.noteRuntime, codeCfg)
	if err != nil {
		return fmt.Errorf("compile cache ownership selection: %w", err)
	}
	if w.configuration != nil {
		if err := validateLiveResourceConfiguration(*w.configuration, resolved); err != nil {
			return err
		}
	}
	if w.onConfigurationChanged != nil {
		if err := w.onConfigurationChanged(updated, resolved, policy); err != nil {
			return err
		}
	} else {
		if w.watchHub != nil {
			w.watchHub.ReplaceUserExcludes(updated.Excludes)
		}
		w.cacheService.ReplaceSelectionPolicy(policy)
	}
	w.vaultDef = updated
	w.codeCfg = codeCfg
	w.configuration = &resolved

	return nil
}

func definitionForConfiguration(vaultPath string, current obsidian.VaultDefinition, resolved repoconfig.Resolved) obsidian.VaultDefinition {
	if resolved.Local == nil {
		return current
	}
	updated := repoconfig.LocalConfigToDefinition(vaultPath, resolved.Local)
	if updated.Name == "" {
		updated.Name = current.Name
	}
	return updated
}
