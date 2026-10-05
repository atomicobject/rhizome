package actions

import (
	"context"
	"os"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
)

// ScanConfiguredCodeRefsForInputs scans only explicit code-file inputs that
// match the vault's coderef include/exclude policy. It resolves links against
// the live note set, including aliases when the reader exposes them.
func ScanConfiguredCodeRefsForInputs(ctx context.Context, vaultDef obsidian.VaultDefinition, noteReader obsidian.NoteReader, inputs []string) (map[string][]coderefs.CodeRef, error) {
	localCfg, err := obsidian.LoadLocalConfig(vaultDef.BasePath())
	if err != nil {
		return nil, err
	}
	includes, excludes := obsidian.NormalizeCodeRefPatterns(*localCfg)
	if len(includes) == 0 {
		return nil, nil
	}

	notes, err := noteReader.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	var aliasesByPath map[string][]string
	if provider, ok := noteReader.(obsidian.NoteEntriesProvider); ok {
		if entries, snapshotErr := provider.NoteEntriesSnapshot(ctx); snapshotErr == nil {
			aliasesByPath = obsidian.AliasesFromNoteEntries(entries)
		}
	}
	noteCache := obsidian.BuildNotePathCacheWithAliases(notes, aliasesByPath)
	vaultPaths, err := paths.NewVaultPaths(vaultDef.BasePath())
	if err != nil {
		return nil, err
	}

	refsByFile := make(map[string][]coderefs.CodeRef)
	for _, input := range inputs {
		rel, abs, resolveErr := paths.ResolveCodeInputWithVaultPaths(vaultPaths, input)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if !matchesAnyCodeRefPattern(rel.String(), includes) || matchesAnyCodeRefPattern(rel.String(), excludes) {
			continue
		}
		info, statErr := os.Stat(abs.String())
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() {
			continue
		}
		content, readErr := os.ReadFile(abs.String())
		if readErr != nil {
			return nil, readErr
		}
		refs, scanErr := coderefs.ScanFile(rel.String(), content, noteCache)
		if scanErr != nil {
			return nil, scanErr
		}
		if len(refs) > 0 {
			refsByFile[rel.String()] = refs
		}
	}

	return refsByFile, nil
}

func matchesAnyCodeRefPattern(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, err := doublestar.Match(pattern, path); err == nil && matched {
			return true
		}
	}
	return false
}
