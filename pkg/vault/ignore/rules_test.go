package ignore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func TestRulesListsEveryLayerInEvaluationOrder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".gitignore", "# build output\ndist/\n")
	writeTestFile(t, root, "web/.gitignore", "*.log\n")
	// A .gitignore inside an ignored or hidden folder never applies.
	writeTestFile(t, root, "dist/.gitignore", "never\n")
	writeTestFile(t, root, ".cache/.gitignore", "never\n")
	writeTestFile(t, root, ".rhizome/ignore", "vendor/\n!/vendor/ours/\n")

	rules := LoadUnifiedMatcher(root, []string{"drafts/"}).Rules()

	require.Equal(t, []RuleRef{
		{Layer: LayerGitignore, Source: ".gitignore", Line: 2, Pattern: "dist/"},
		{Layer: LayerGitignore, Source: "web/.gitignore", Line: 1, Pattern: "*.log"},
		{Layer: LayerRhizome, Source: ".rhizome/ignore", Line: 1, Pattern: "vendor/"},
		{Layer: LayerRhizome, Source: ".rhizome/ignore", Line: 2, Pattern: "!/vendor/ours/"},
		{Layer: LayerConfig, Line: 1, Pattern: "drafts/"},
	}, rules)
}

func TestRulesListTheBuiltInListOnlyWhileItApplies(t *testing.T) {
	root := t.TempDir()
	rules := LoadUnifiedMatcher(root, nil).Rules()
	require.Len(t, rules, len(DefaultIgnorePatterns()))
	require.Equal(t, LayerDefault, rules[0].Layer)

	writeTestFile(t, root, ".rhizome/ignore", "vendor/\n")
	for _, rule := range LoadUnifiedMatcher(root, nil).Rules() {
		require.NotEqual(t, LayerDefault, rule.Layer)
	}
}

func TestLoadMatcherWithRhizomeLinesUsesTheSuppliedRules(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".rhizome/ignore", "on-disk/\n")

	m := LoadMatcherWithRhizomeLines(root, []string{"planned/"}, nil)
	require.True(t, m.IsIgnored("planned/a.md", false))
	require.False(t, m.IsIgnored("on-disk/a.md", false))
	require.Equal(t, []RuleRef{{Layer: LayerRhizome, Source: ".rhizome/ignore", Line: 1, Pattern: "planned/"}}, m.Rules())

	// No supplied rules means the built-in list applies, as with an empty file.
	require.Equal(t, LayerDefault, LoadMatcherWithRhizomeLines(root, nil, nil).Rules()[0].Layer)
}
