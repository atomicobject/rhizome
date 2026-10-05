package search

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

func NormalizeExplicitSeedPaths(vaultPath string, rawTokens, normalizedTokens []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(rawTokens)+len(normalizedTokens))
	add := func(path string) {
		path = NormalizeLocalityPath(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	for _, raw := range normalizeExplicitSeedTokens(rawTokens) {
		if path, ok := explicitSeedPath(vaultPaths, raw); ok {
			add(path)
		}
	}
	for _, raw := range normalizeExplicitSeedTokens(normalizedTokens) {
		if path, ok := explicitSeedPath(vaultPaths, raw); ok {
			add(path)
		}
	}
	return out
}

func explicitSeedPathFromVault(vaultPath, raw string) (string, bool) {
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	return explicitSeedPath(vaultPaths, raw)
}

func explicitSeedPath(vaultPaths paths.VaultPaths, raw string) (string, bool) {
	if strings.Contains(raw, ":") {
		if handle, err := knowledge.ParseHandle(raw); err == nil {
			if path := explicitSeedPathFromHandle(handle); path != "" {
				return path, true
			}
		}
		return "", false
	}
	// Explicit Markdown path input is a retained compatibility boundary. Other
	// raw paths are identity-only here: their ownership must arrive from the
	// caller in QuerySpec.PathKinds, never from an extension or filesystem probe.
	if isExplicitMarkdownRawSeedCompatibilityPath(raw) {
		if notePath, err := resolveRelAuthoredSeed(vaultPaths, raw); err == nil && notePath != "" {
			return notePath.String(), true
		}
		return "", false
	}
	if path, err := resolveRelSeedPath(vaultPaths, raw); err == nil && path != "" {
		return path.String(), true
	}
	return "", false
}

func explicitSeedPathFromHandle(handle knowledge.Handle) string {
	switch handle.Kind {
	case knowledge.KindFile, knowledge.KindNote, knowledge.KindNoteChunk:
		return NormalizeLocalityPath(handle.ID)
	case knowledge.KindNodeChunk:
		if len(handle.Fragments) > 0 {
			return NormalizeLocalityPath(handle.Fragments[0])
		}
		return ""
	default:
		return ""
	}
}

// ExplicitSeedPathsFromHandles derives local paths without changing an already
// resolved handle's kind or ID.
func ExplicitSeedPathsFromHandles(handles []knowledge.Handle) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, handle := range handles {
		path := explicitSeedPathFromHandle(handle)
		if path == "" {
			continue
		}
		if _, found := seen[path]; found {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

// PathKindsFromHandles derives caller-owned path kinds from resolved handles.
func PathKindsFromHandles(handles []knowledge.Handle) map[string]PathKind {
	if len(handles) == 0 {
		return nil
	}
	out := make(map[string]PathKind)
	for _, handle := range handles {
		path := explicitSeedPathFromHandle(handle)
		if path == "" {
			continue
		}
		switch handle.Kind {
		case knowledge.KindNote, knowledge.KindNoteChunk, knowledge.KindNodeChunk:
			out[path] = PathKindNote
		case knowledge.KindFile:
			out[path] = PathKindCode
		}
	}
	return out
}

// ExplicitSeedPathKind returns the caller-supplied ownership for a path. The
// only fallback is the explicit Markdown input compatibility boundary.
func ExplicitSeedPathKind(spec QuerySpec, path string) PathKind {
	path = NormalizeLocalityPath(path)
	if path == "" {
		return ""
	}
	if kind := spec.PathKinds[path]; kind == PathKindNote || kind == PathKindCode {
		return kind
	}
	if isExplicitMarkdownRawSeedCompatibilityPath(path) {
		return PathKindNote
	}
	return ""
}

func NormalizeLocalityPath(raw string) string {
	raw = strings.TrimSpace(filepath.ToSlash(raw))
	raw = strings.Trim(raw, "/")
	if raw == "." {
		return ""
	}
	return raw
}

func SeedPathProximity(path string, seedPaths []string) int {
	path = NormalizeLocalityPath(path)
	if path == "" || len(seedPaths) == 0 {
		return 0
	}
	best := 0
	for _, seed := range seedPaths {
		seed = NormalizeLocalityPath(seed)
		if seed == "" {
			continue
		}
		seedDir := NormalizeLocalityPath(filepath.Dir(seed))
		seedDirDepth := 0
		if seedDir != "" {
			seedDirDepth = len(strings.Split(seedDir, "/"))
		}
		switch {
		case path == seed:
			return 4
		case filepath.Dir(path) == filepath.Dir(seed):
			best = max(best, 3)
		case strings.HasPrefix(path, seed+"/") || strings.HasPrefix(seed, path+"/"):
			best = max(best, 3)
		default:
			shared := sharedLeadingSegments(path, seed)
			switch {
			case seedDirDepth > 0 && shared >= seedDirDepth:
				best = max(best, 2)
			case seedDirDepth == 0 && shared >= 1:
				best = max(best, 1)
			}
		}
	}
	return best
}

func IsLocalToExplicitSeeds(path string, spec QuerySpec) bool {
	return SeedPathProximity(path, spec.ExplicitSeedPaths) > 0
}

func sharedLeadingSegments(a, b string) int {
	a = NormalizeLocalityPath(a)
	b = NormalizeLocalityPath(b)
	if a == "" || b == "" {
		return 0
	}
	partsA := strings.Split(a, "/")
	partsB := strings.Split(b, "/")
	n := min(len(partsA), len(partsB))
	count := 0
	for i := 0; i < n; i++ {
		if partsA[i] != partsB[i] {
			break
		}
		count++
	}
	return count
}

func normalizeExplicitSeedTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func resolveRelAuthoredSeed(vaultPaths paths.VaultPaths, raw string) (paths.NotePath, error) {
	if vaultPaths.Root() == "" {
		notePath, err := paths.CleanNotePath(raw)
		if err != nil || notePath == "" {
			return paths.NotePath(""), paths.ErrOutsideVault
		}
		return notePath, nil
	}
	return vaultPaths.RelNotePathStrict(raw)
}

func resolveRelSeedPath(vaultPaths paths.VaultPaths, raw string) (paths.RelPath, error) {
	if vaultPaths.Root() == "" {
		rel, err := paths.CleanRelPath(raw)
		if err != nil || rel == "" {
			return paths.RelPath(""), paths.ErrOutsideVault
		}
		return rel, nil
	}
	return vaultPaths.RelStrict(raw)
}

// isExplicitMarkdownRawSeedCompatibilityPath is the narrow compatibility
// admission for raw, untyped seed paths. Ownership-aware callers must pass
// PathKinds instead of relying on an extension.
func isExplicitMarkdownRawSeedCompatibilityPath(raw string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(raw)), ".md")
}
