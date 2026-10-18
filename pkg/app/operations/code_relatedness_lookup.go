package operations

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// lookupSymbolsForFile best-effort resolves symbols for absolute or vault-relative paths.
func lookupSymbolsForFile(ctx context.Context, intel CodeRelatednessIntel, vaultPath, file string) []string {
	if intel == nil {
		return nil
	}
	var out []string
	for _, p := range candidatePathsForLookup(vaultPath, file) {
		syms, err := intel.SymbolsByFile(ctx, p)
		if err != nil {
			continue
		}
		if len(syms) == 0 {
			continue
		}
		out = append(out, syms...)
	}
	return out
}

// candidatePathsForLookup returns abs + relative variants to maximize matching against the intel store.
func candidatePathsForLookup(vaultPath, file string) []string {
	var out []string
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	if vaultPaths.Root() != "" {
		if ref, err := paths.ResolveCodeRefWithVaultPaths(vaultPaths, file); err == nil {
			if ref.Abs != "" {
				out = append(out, ref.Abs.String())
			}
			if ref.Rel != "" {
				out = append(out, ref.Rel.String())
			}
		}
	} else if rel, err := paths.CleanRelPath(file); err == nil && rel.String() != "" {
		out = append(out, string(paths.NormalizeCode(rel.String())))
	}
	for i := 0; i < len(out); i++ {
		out[i] = filepath.ToSlash(out[i])
	}
	return dedupeStrings(out)
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func matchingAnchorIDsForFile(ctx context.Context, intel CodeRelatednessIntel, vaultPath, file string, ancestorsCache map[string][]string) map[int64]struct{} {
	ids := make(map[int64]struct{})
	if intel == nil {
		return ids
	}

	// Symbols + ancestors.
	symbols := lookupSymbolsForFile(ctx, intel, vaultPath, file)
	seen := make(map[string]struct{}, len(symbols))
	var all []string
	for _, fqn := range symbols {
		if fqn == "" {
			continue
		}
		if _, ok := seen[fqn]; ok {
			continue
		}
		seen[fqn] = struct{}{}
		all = append(all, fqn)

		anc, ok := ancestorsCache[fqn]
		if !ok {
			anc, _ = intel.Ancestors(ctx, fqn)
			ancestorsCache[fqn] = anc
		}
		for _, a := range anc {
			if a == "" {
				continue
			}
			if _, ok := seen[a]; ok {
				continue
			}
			seen[a] = struct{}{}
			all = append(all, a)
		}
	}

	matchedBySymbol, err := intel.AnchorsBySymbols(ctx, all)
	if err == nil {
		for id := range matchedBySymbol {
			ids[id] = struct{}{}
		}
	}

	for _, lookup := range candidatePathsForLookup(vaultPath, file) {
		callAnchors, err := intel.AnchorsByCallFiles(ctx, []string{lookup})
		if err == nil {
			for _, id := range callAnchors {
				ids[id] = struct{}{}
			}
		}
		pathAnchors, err := intel.AnchorsByPathPrefix(ctx, lookup)
		if err == nil {
			for _, id := range pathAnchors {
				ids[id] = struct{}{}
			}
		}
		globAnchors, err := intel.AnchorsByGlobMatch(ctx, lookup)
		if err == nil {
			for _, id := range globAnchors {
				ids[id] = struct{}{}
			}
		}
	}
	return ids
}

func readGoModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}
