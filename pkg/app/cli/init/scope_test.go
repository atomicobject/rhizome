package init

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/stretchr/testify/require"
)

func writeScopeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

const scopeIgnore = `node_modules/
vendor/

# rhizome: suggested skips
# test fixtures
/testdata/
`

func TestReadScopeDescribesEveryLayer(t *testing.T) {
	root := t.TempDir()
	writeScopeFile(t, root, ".gitignore", "dist/\n")
	writeScopeFile(t, root, "web/.gitignore", "*.log\n")
	writeScopeFile(t, root, ".rhizome/ignore", scopeIgnore+"# rhizome: keep indexed docs/generated\n\n"+includedSubtreesHeader+"\n!/tools/sdk/\n")

	scope := ReadScope(root, []string{"drafts/**"})

	require.False(t, scope.BuiltInApplies)
	require.Equal(t, []ScopeRule{
		{Layer: ignore.LayerGitignore, Source: ".gitignore", Line: 1, Pattern: "dist/"},
		{Layer: ignore.LayerGitignore, Source: "web/.gitignore", Line: 1, Pattern: "*.log", Folder: "web"},
		{Layer: ignore.LayerRhizome, Source: ".rhizome/ignore", Line: 1, Pattern: "node_modules/"},
		{Layer: ignore.LayerRhizome, Source: ".rhizome/ignore", Line: 2, Pattern: "vendor/"},
		{Layer: ignore.LayerRhizome, Source: ".rhizome/ignore", Line: 6, Pattern: "/testdata/", Reason: reasonFixtures},
		{Layer: ignore.LayerRhizome, Source: ".rhizome/ignore", Line: 10, Pattern: "!/tools/sdk/", Included: true},
		{Layer: ignore.LayerConfig, Source: ".rhizome/config.yml", Pattern: "drafts/**"},
	}, scope.Rules)
	require.Equal(t, []string{"docs/generated"}, scope.KeepIndexed)
}

func TestReadScopeReportsTheBuiltInListWhileItApplies(t *testing.T) {
	scope := ReadScope(t.TempDir(), nil)
	require.True(t, scope.BuiltInApplies)
	require.Equal(t, ignore.LayerDefault, scope.Rules[0].Layer)
	require.Zero(t, scope.Rules[0].Line)
}

func TestChangeScopeAppliesEditsInOneWrite(t *testing.T) {
	root := t.TempDir()
	writeScopeFile(t, root, ".rhizome/ignore", scopeIgnore)
	writeScopeFile(t, root, "generated/api.go", "package api\n")
	writeScopeFile(t, root, "tools/sdk/main.go", "package main\n")

	scope, err := ChangeScope(root, nil, ScopeEdits{
		RemoveRules:    []string{"/testdata/", "vendor/"},
		Skip:           []string{"generated"},
		IncludeIgnored: []string{"tools/sdk"},
	})
	require.NoError(t, err)

	require.Equal(t, `node_modules/

# rhizome: suggested skips
# rhizome: keep indexed testdata
# `+reasonManual+`
/generated/

`+includedSubtreesHeader+`
!/tools/sdk/
`, readIgnoreFile(t, root))
	require.Equal(t, []string{"testdata"}, scope.KeepIndexed)
}

func TestChangeScopeChangesNothingWhenAnEditFails(t *testing.T) {
	root := t.TempDir()
	writeScopeFile(t, root, ".rhizome/ignore", scopeIgnore)
	writeScopeFile(t, root, "generated/api.go", "package api\n")

	for name, edits := range map[string]ScopeEdits{
		"missing rule":  {RemoveRules: []string{"vendor/", "/nope/"}},
		"missing path":  {Skip: []string{"generated", "nope"}},
		"escaping path": {Skip: []string{"../outside"}},
		"comment":       {RemoveRules: []string{"# test fixtures"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ChangeScope(root, nil, edits)
			require.Error(t, err)
			require.Equal(t, scopeIgnore, readIgnoreFile(t, root))
		})
	}
}

func TestChangeScopeWritesTheBuiltInListBeforeTheFirstRule(t *testing.T) {
	root := t.TempDir()
	writeScopeFile(t, root, "generated/api.go", "package api\n")

	_, err := ChangeScope(root, nil, ScopeEdits{Skip: []string{"generated/api.go"}})
	require.NoError(t, err)

	body := readIgnoreFile(t, root)
	require.True(t, strings.HasPrefix(body, ignore.DefaultIgnoreFile()))
	require.Contains(t, body, "/generated/api.go\n")
}

func TestChangeScopeKeepsCommentsWhenItWritesTheBuiltInList(t *testing.T) {
	root := t.TempDir()
	writeScopeFile(t, root, ".rhizome/ignore", "# Team rules go below.\n")
	writeScopeFile(t, root, "generated/api.go", "package api\n")

	_, err := ChangeScope(root, nil, ScopeEdits{Skip: []string{"generated"}})
	require.NoError(t, err)

	body := readIgnoreFile(t, root)
	require.True(t, strings.HasPrefix(body, ignore.DefaultIgnoreFile()))
	require.Contains(t, body, "# Team rules go below.\n")
	require.Contains(t, body, "/generated/\n")
}

func TestChangeScopeRefusesToShadowALegacyIgnoreFile(t *testing.T) {
	root := t.TempDir()
	writeScopeFile(t, root, ".obsidianignore", "private/\n")
	writeScopeFile(t, root, "generated/api.go", "package api\n")

	_, err := ChangeScope(root, nil, ScopeEdits{Skip: []string{"generated"}})
	require.ErrorContains(t, err, ".obsidianignore")
	require.NoFileExists(t, ignoreFilePath(root))
}

func TestPlannedScopeMarksTheRulesAFirstRunWrites(t *testing.T) {
	root := t.TempDir()
	scope, err := plannedScope(root, setup{skips: []skip{{path: "testdata/", reason: reasonFixtures}}})
	require.NoError(t, err)
	require.False(t, scope.BuiltInApplies)
	last := scope.Rules[len(scope.Rules)-1]
	require.Equal(t, ScopeRule{Layer: ignore.LayerRhizome, Source: ".rhizome/ignore", Line: last.Line, Pattern: "/testdata/", Reason: reasonFixtures, Planned: true}, last)
	for _, rule := range scope.Rules {
		require.True(t, rule.Planned, rule.Pattern)
	}

	// Writing the plan produces exactly the planned rules, none still planned.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, writeIgnore(root, setup{skips: []skip{{path: "testdata/", reason: reasonFixtures}}}))
	written := ReadScope(root, nil)
	require.Len(t, written.Rules, len(scope.Rules))
	for _, rule := range written.Rules {
		require.False(t, rule.Planned, rule.Pattern)
	}
}
