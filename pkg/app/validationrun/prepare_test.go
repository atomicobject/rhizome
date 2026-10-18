package validationrun

import (
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
			prepared, err := Prepare(obsidian.VaultDefinition{Path: root}, testNoteMetadata(t), []string{test.selector}, 7)
			require.NoError(t, err)
			require.Equal(t, test.want, prepared.Request.Features.QueryRecipesConfigured)
			require.Equal(t, 7, prepared.RunContext.MaxIssues)
			require.Equal(t, []string{"query-recipes"}, prepared.Request.Config.Default.Add)
			require.NoFileExists(t, filepath.Join(root, ".rhizome", "db.sqlite"), "preparation does not acquire a projection")
		})
	}
}
