package operations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeModuleRecipe(t *testing.T, root, rel, id string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	body := "apiVersion: rhizome.query-recipe.v1\nid: \"" + id + "\"\nname: n\nproblem: p\ninputSpec:\n  mode: none\nquery:\n  graphQL: \"{ notes(first: 1) { title } }\"\noutputContract:\n  empty: none\nadaptationGuidance:\n  summary: s\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func TestLoadModuleRecipesSelectsTheModulesCommands(t *testing.T) {
	root := t.TempDir()
	writeModuleRecipe(t, root, ".rhizome/query-recipes/flat.yaml", "flat")
	writeModuleRecipe(t, root, ".rhizome/modules/team/recipes/roster.yaml", "roster")
	writeModuleRecipe(t, root, ".rhizome/modules/team/recipes/spaced.yaml", "team search")
	writeModuleRecipe(t, root, ".rhizome/modules/other/recipes/x.yaml", "other-x")

	loaded, err := LoadModuleRecipes(root, "team")
	require.NoError(t, err)
	ids := []string{}
	for _, recipe := range loaded.Commands {
		ids = append(ids, recipe.ID)
	}
	require.Equal(t, []string{"roster"}, ids)
	require.Equal(t, []string{"team search"}, loaded.Unnamed, "an id with a space is reported, not shortened")
	require.Len(t, loaded.All, 4)

	_, err = LoadModuleRecipes(root, "missing")
	require.ErrorContains(t, err, `no module named "missing"`)
	require.False(t, ModuleExists(root, "Bad_Name"))
}
