package validationrun

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestPreparedVaultFeatureFactsRequireCodeAnchorRoots(t *testing.T) {
	t.Parallel()

	off, err := vaultFeatureFacts(t.TempDir(), &repoconfig.LocalConfig{}, false)
	require.NoError(t, err)
	require.False(t, off.CodeAnchorRootsConfigured)

	// Code on without folders covers the whole project, so anchors apply.
	enabledOnly, err := vaultFeatureFacts(t.TempDir(), &repoconfig.LocalConfig{Code: repoconfig.LocalCodeConfig{Enabled: true, Scan: []string{"**/*.go"}}}, false)
	require.NoError(t, err)
	require.True(t, enabledOnly.CodeConfigured)
	require.True(t, enabledOnly.CodeAnchorRootsConfigured)

	withRoots, err := vaultFeatureFacts(t.TempDir(), &repoconfig.LocalConfig{Code: repoconfig.LocalCodeConfig{Go: &repoconfig.LocalCodeLangConfig{Roots: []string{"."}}}}, false)
	require.NoError(t, err)
	require.True(t, withRoots.CodeAnchorRootsConfigured)
}

func TestPreparedVaultFeatureFactsSeeModuleAssets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	none, err := vaultFeatureFacts(root, &repoconfig.LocalConfig{}, true)
	require.NoError(t, err)
	require.False(t, none.OntologyConfigured || none.ViewsConfigured || none.TasksPresent || none.QueryRecipesConfigured)

	for _, kind := range []string{"ontology", "views", "tasks"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "modules", "team", kind), 0o755))
	}
	recipe := filepath.Join(root, ".rhizome", "modules", "team", "recipes", "roster.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(recipe), 0o755))
	require.NoError(t, os.WriteFile(recipe, []byte("apiVersion: rhizome.query-recipe.v1\nid: roster\n"), 0o644))
	facts, err := vaultFeatureFacts(root, &repoconfig.LocalConfig{}, true)
	require.NoError(t, err)
	require.True(t, facts.OntologyConfigured)
	require.True(t, facts.ViewsConfigured)
	require.True(t, facts.TasksPresent)
	require.True(t, facts.QueryRecipesConfigured)
}

func TestPreparedVaultFeatureFactsKeepABrokenAssetFolderForItsOwnCheck(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "modules", "demo", "tasks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "modules", "demo", "views"), []byte("not a folder"), 0o644))
	facts, err := vaultFeatureFacts(root, &repoconfig.LocalConfig{}, false)
	require.NoError(t, err, "a broken views folder must not stop unrelated checks")
	require.True(t, facts.ViewsConfigured, "the views check runs and reports the broken folder")
	require.True(t, facts.TasksPresent)
}

func TestPrepareDiscoversRecipeFactsOnlyForSelectedChecks(t *testing.T) {
	for _, test := range []struct {
		selector string
		want     bool
	}{
		{"broken-links", false},
		{"query-recipes", true},
		{"default", true},
	} {
		t.Run(test.selector, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, writeFixtureFiles(root, map[string]string{
				".rhizome/config.yml":       "validation:\n  default:\n    add: [query-recipes]\n",
				"docs/query-recipes/bad.md": "```query-recipe\napiVersion: [\n```\n",
			}))
			prepared, err := Prepare(obsidian.VaultDefinition{Path: root}, testNoteMetadata(t), []string{test.selector}, 7, nil)
			require.NoError(t, err)
			require.Equal(t, test.want, prepared.Request.Features.QueryRecipesConfigured)
			require.Equal(t, 7, prepared.RunContext.MaxIssues)
			require.Equal(t, []string{"query-recipes"}, prepared.Request.Config.Default.Add)
			require.NoFileExists(t, filepath.Join(root, ".rhizome", "db.sqlite"), "preparation does not acquire a projection")
		})
	}
}
