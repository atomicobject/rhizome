package ignore

import (
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// LoadUnifiedMatcher builds a matcher from (in precedence order):
//   - DefaultIgnorePatterns() (only when .rhizome/ignore and .obsidianignore are missing/empty)
//   - Root + nested .gitignore (if present)
//   - .rhizome/ignore (if present; otherwise falls back to legacy .obsidianignore)
//   - userPatterns (typically from vault config)
//
// Later rules override earlier rules, including via gitignore-style negations (!pattern).
func LoadUnifiedMatcher(root string, userPatterns []string) *Matcher {
	root = filepath.Clean(root)

	vaultPaths, _ := paths.NewVaultPaths(root)
	if vaultPaths.Root() != "" {
		// Resolve the root once so nested .gitignore domains and caller-supplied
		// relative paths are evaluated against the same filesystem identity.
		root = vaultPaths.Root()
	}

	rhizomePath := filepath.Join(root, ".rhizome", "ignore")
	rhizomeLines := readIgnoreLines(rhizomePath)
	rhizomePatterns := parsePatternsWithSource(rhizomeLines, nil, LayerRhizome, ".rhizome/ignore")
	if len(rhizomePatterns) == 0 {
		// Legacy compatibility: .rhizome/ignore is the source of truth when
		// present; .obsidianignore is only consulted for older vaults.
		rhizomeLines = readIgnoreLines(filepath.Join(root, ".obsidianignore"))
		rhizomePatterns = parsePatternsWithSource(rhizomeLines, nil, LayerRhizome, ".obsidianignore")
	}

	var defaults []patternWithMeta
	if len(rhizomePatterns) == 0 {
		// User ignore files replace defaults so teams can intentionally opt back
		// into paths that Rhizome would normally hide.
		defaults = append(defaults, parsePatternsWithSource(DefaultIgnorePatterns(), nil, LayerDefault, "")...)
	}
	user := parsePatternsWithSource(userPatterns, nil, LayerConfig, "")
	return NewMatcherWithRoot(root, defaults, rhizomePatterns, user)
}
