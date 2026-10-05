package obsidian

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/bmatcuk/doublestar/v4"
)

// discoveryFS opens the vault root for discovery walks; tests swap it to count
// directory reads.
var discoveryFS = os.DirFS

// DiscoverFiles returns files that belong to the configured vault's note
// selection. Classic vaults retain their Markdown-only compatibility default;
// collection includes may select any registered note format.
// For collection-style vaults (globs), matching paths are returned relative to cfg.Root.
// For classic vaults (Path), returned paths are relative to cfg.Path.
//
// Both vault types apply unified ignore semantics:
//   - DefaultIgnorePatterns (node_modules, vendor, etc.)
//   - root + nested .gitignore patterns
//   - .rhizome/ignore patterns from vault root (if present; falls back to .obsidianignore)
//   - User-provided Excludes from VaultDefinition
//
// Include globs use doublestar; ignore rules use gitignore semantics. Both
// vault types share one walk that prunes ignored directories before reading
// them.
func DiscoverFiles(cfg VaultDefinition) ([]string, error) {
	for _, pattern := range cfg.Includes {
		if !doublestar.ValidatePattern(filepath.ToSlash(pattern)) {
			return nil, fmt.Errorf("include pattern %q: %w", pattern, doublestar.ErrBadPattern)
		}
	}
	root := cfg.Path
	if cfg.IsCollection() {
		root = cfg.Root
	}
	if root == "" {
		return nil, errors.New(RhizomeVaultPathInvalidError)
	}
	return discoverByWalk(cfg, root)
}

// LoadVaultIgnoreMatcher builds a matcher that is used by file discovery and the cache.
// It applies (in precedence order):
//   - DefaultIgnorePatterns (only when .rhizome/ignore and .obsidianignore are missing/empty)
//   - root + nested .gitignore
//   - .rhizome/ignore (if present; falls back to legacy .obsidianignore)
//   - userExcludes (typically from VaultDefinition.Excludes)
//
// All patterns use gitignore-like syntax (including negations: !pattern).
func LoadVaultIgnoreMatcher(root string, userExcludes []string) *ignore.Matcher {
	cleaned := make([]string, 0, len(userExcludes))
	for _, ex := range userExcludes {
		if ex == "" {
			continue
		}
		cleaned = append(cleaned, ex)
	}
	return ignore.LoadUnifiedMatcher(root, cleaned)
}

// LoadVaultHardIgnoreMatcher builds the visibility matcher for system notes.
// Ordinary note-selection excludes are not part of this matcher.
func LoadVaultHardIgnoreMatcher(root string) *ignore.Matcher {
	return ignore.LoadUnifiedMatcher(root, nil)
}

// NotePathMatchesIncludes reports whether a vault-relative note path is in scope
// for the vault definition's include globs. Classic vaults include all note paths.
func NotePathMatchesIncludes(cfg VaultDefinition, relPath string) bool {
	if !cfg.IsCollection() {
		return true
	}
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	if relPath == "" {
		return false
	}
	includes := cfg.Includes
	if len(includes) == 0 {
		includes = []string{"**/*.md"}
	}
	for _, pattern := range includes {
		variants := markdownExtensionPatternVariants(pattern)
		if pattern == ignore.SystemContextFilename || strings.HasSuffix(filepath.ToSlash(pattern), "/"+ignore.SystemContextFilename) {
			variants = []string{pattern}
		}
		for _, variant := range variants {
			if ok, _ := doublestar.Match(filepath.ToSlash(variant), relPath); ok {
				return true
			}
		}
	}
	return false
}

// NotePathMatchesSelection admits Rhizome's system context convention even
// when ordinary collection includes do not select it.
func NotePathMatchesSelection(cfg VaultDefinition, relPath string) bool {
	return ignore.IsSystemContextPath(relPath) || NotePathMatchesIncludes(cfg, relPath)
}

func noteDiscoveryPatterns(cfg VaultDefinition) []string {
	includes := append([]string(nil), cfg.Includes...)
	if len(includes) == 0 {
		includes = []string{"**/*.md"}
	}
	return append(includes, ignore.SystemContextFilename, "**/"+ignore.SystemContextFilename)
}

// discoverByWalk walks root once, pruning hidden and ignored directories, and
// admits note files the vault definition selects. Collection includes are
// matched per file, so ignored subtrees such as node_modules are never read.
func discoverByWalk(cfg VaultDefinition, root string) ([]string, error) {
	root = filepath.Clean(root)
	vaultPaths, _ := paths.NewVaultPaths(root)
	if vaultPaths.Root() != "" {
		root = vaultPaths.Root()
	}
	matcher := LoadVaultIgnoreMatcher(root, cfg.Excludes)
	hardMatcher := LoadVaultHardIgnoreMatcher(root)

	var results []string
	err := fs.WalkDir(discoveryFS(root), ".", func(relPath string, d fs.DirEntry, err error) error {
		if err != nil {
			if relPath == "." {
				return err
			}
			return fs.SkipDir
		}
		if d.IsDir() {
			if relPath == "." {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			if matcher.IsIgnoredShallow(relPath, true) && hardMatcher.IsIgnoredShallow(relPath, true) {
				return fs.SkipDir
			}
			return nil
		}

		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !cfg.IsCollection() && !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 && !symlinkStaysInside(root, relPath) {
			return nil
		}
		if !NotePathMatchesSelection(cfg, relPath) {
			return nil
		}

		visibilityMatcher := matcher
		if ignore.IsSystemContextPath(relPath) {
			if ignore.IsDefaultInfrastructurePath(relPath) {
				return nil
			}
			visibilityMatcher = hardMatcher
		}
		if visibilityMatcher.IsIgnored(relPath, false) {
			return nil
		}

		results = append(results, paths.NormalizeNotePath(relPath).String())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// symlinkStaysInside reports whether a symlinked entry resolves to a path under
// the (resolved) vault root. RelStrict cannot decide this: it deliberately
// retries against the unresolved root so vaults reached through a symlink keep
// stable keys, which also admits a file that links outside the vault.
func symlinkStaysInside(root, relPath string) bool {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		return false
	}
	return target == resolvedRoot || strings.HasPrefix(target, resolvedRoot+string(filepath.Separator))
}

// markdownExtensionPatternVariants expands a terminal literal Markdown extension
// to its four ASCII casing variants. It leaves directory and basename matching to
// doublestar, so their existing case-sensitive behavior remains unchanged.
func markdownExtensionPatternVariants(pattern string) []string {
	pattern = filepath.ToSlash(pattern)
	const markdownExtension = ".md"
	if len(pattern) < len(markdownExtension) || !strings.EqualFold(pattern[len(pattern)-len(markdownExtension):], markdownExtension) {
		return []string{pattern}
	}
	prefix := pattern[:len(pattern)-len(markdownExtension)]
	return []string{prefix + ".md", prefix + ".mD", prefix + ".Md", prefix + ".MD"}
}

// WatchRoots watches the full collection root because system CONTEXT.md can occur
// at any depth, regardless of configured note include patterns.
func (d VaultDefinition) WatchRoots() []string {
	if d.IsCollection() {
		return []string{d.Root}
	}
	return []string{d.Path}
}
