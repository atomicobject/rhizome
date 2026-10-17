package mcp

import (
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"path/filepath"
	"strings"
)

// docMatcher helpers

type docMatcher struct {
	patterns []string
}

func newDocMatcher(config *repoconfig.Resolved) docMatcher {
	patterns := repoconfig.FileContextConfigDefaults.DocPatterns
	if config != nil {
		patterns = config.FileContext.DocPatterns
	}
	return docMatcher{patterns: normalizeDocPatterns(patterns)}
}

func effectiveVaultRoot(cfg Config) string {
	if base := strings.TrimSpace(cfg.VaultDef.BasePath()); base != "" {
		return base
	}
	if root := strings.TrimSpace(cfg.VaultPath); root != "" {
		return root
	}
	return ""
}

func normalizeDocPatterns(patterns []string) []string {
	patterns = obsidian.NormalizeDocPatterns(patterns)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(filepath.ToSlash(trimmed))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (m docMatcher) IsDoc(path, typ string) bool {
	if typ == "note" {
		return true
	}
	return obsidian.MatchDocPatternNormalized(path, m.patterns)
}

func (m docMatcher) IsPrimaryDoc(path string) bool {
	if len(m.patterns) == 0 {
		return false
	}
	primary := strings.TrimSpace(m.patterns[0])
	if primary == "" {
		return false
	}
	return obsidian.MatchDocPatternNormalized(path, []string{primary})
}

func isFetchIntent(intent search.Intent) bool {
	switch intent {
	case search.IntentGoToDef,
		search.IntentFindUsages,
		search.IntentCallers,
		search.IntentCallees,
		search.IntentTestsForCode,
		search.IntentImplementers,
		search.IntentOverrides,
		search.IntentImports:
		return true
	default:
		return false
	}
}

func safeJoinVaultPath(vaultPath, rel string) (string, bool) {
	vaultPath = strings.TrimSpace(vaultPath)
	if vaultPath == "" {
		return "", false
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", false
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return "", false
	}
	relPath, err := vaultPaths.RelStrict(rel)
	if err != nil || relPath == "" {
		return "", false
	}
	abs, err := vaultPaths.Abs(relPath)
	if err != nil || abs == "" {
		return "", false
	}
	return abs.String(), true
}
