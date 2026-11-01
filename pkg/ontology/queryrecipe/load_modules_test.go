package queryrecipe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func moduleRecipe(id string) string {
	return "apiVersion: rhizome.query-recipe.v1\nid: " + id + "\nname: " + id + "\nproblem: p\ninputSpec:\n  mode: none\nquery:\n  graphQL: \"{ notes(first: 1) { title } }\"\noutputContract:\n  empty: none\nadaptationGuidance:\n  summary: s\n"
}

func writeRecipeFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestRegistryFindsRecipesInModuleCreatedAfterItWasBuilt(t *testing.T) {
	root := t.TempDir()
	writeRecipeFile(t, root, ".rhizome/query-recipes/flat.yaml", moduleRecipe("flat"))
	registry := NewRegistry(DefaultSourceRoots(root))
	recipes, issues := registry.Catalog()
	require.Empty(t, issues)
	require.Len(t, recipes, 1)

	path := writeRecipeFile(t, root, ".rhizome/modules/team/recipes/roster.yaml", moduleRecipe("roster"))
	// Only <module>/recipes/** is scanned: files elsewhere in a module and
	// folders with invalid module names are not recipes.
	writeRecipeFile(t, root, ".rhizome/modules/team/views/roster.yaml", "not: a recipe\n")
	writeRecipeFile(t, root, ".rhizome/modules/team/notes.yaml", "not: a recipe\n")
	writeRecipeFile(t, root, ".rhizome/modules/Bad_Name/recipes/bad.yaml", moduleRecipe("bad"))

	recipes, issues = registry.Catalog()
	require.Empty(t, issues)
	ids := []string{}
	for _, recipe := range recipes {
		ids = append(ids, recipe.ID)
	}
	require.ElementsMatch(t, []string{"flat", "roster"}, ids)
	for _, recipe := range recipes {
		if recipe.ID == "roster" {
			require.Equal(t, path, recipe.Source.Path)
		}
	}
}

func TestModuleRecipeYAMLWithoutAPIVersionIsReported(t *testing.T) {
	root := t.TempDir()
	writeRecipeFile(t, root, ".rhizome/modules/team/recipes/roster.yaml", "id: roster\nname: Roster\n")
	// The file is loaded, not skipped silently, so validation reports it.
	recipes, issues := LoadDefaultSources(root)
	require.Empty(t, issues)
	require.Len(t, recipes, 1)
	requireIssue(t, Validate(recipes, nil).Issues, "unsupported_api_version")
}

func TestDuplicateRecipeIDAcrossFlatAndModuleIsReported(t *testing.T) {
	root := t.TempDir()
	writeRecipeFile(t, root, ".rhizome/query-recipes/roster.yaml", moduleRecipe("roster"))
	writeRecipeFile(t, root, ".rhizome/modules/team/recipes/roster.yaml", moduleRecipe("roster"))
	recipes, issues := LoadDefaultSources(root)
	require.Empty(t, issues)
	require.Len(t, recipes, 2)
	requireIssue(t, Validate(recipes, nil).Issues, "duplicate_recipe_id")
}

func TestModuleRecipesFolderReportsYAMLWithoutAPIVersion(t *testing.T) {
	root := t.TempDir()
	writeRecipeFile(t, root, ".rhizome/modules/team/recipes/broken.yaml", "id: broken\nname: broken\n")
	for _, dir := range []string{".rhizome/modules/team/recipes", ".rhizome/modules/team/recipes/nested"} {
		require.True(t, rootUsesExplicitRecipeFiles(filepath.Join(root, filepath.FromSlash(dir))), dir)
	}
	recipes, _ := LoadRoots([]string{filepath.Join(root, ".rhizome", "modules", "team", "recipes")})
	require.Len(t, recipes, 1, "a module recipes folder checked directly loads YAML without apiVersion so validation reports it")
	require.False(t, rootUsesExplicitRecipeFiles(filepath.Join(root, ".rhizome", "modules", "team", "views")))
}

func TestDefaultDiscoveryReportsAModuleRecipesPathThatIsNotAFolder(t *testing.T) {
	root := t.TempDir()
	writeRecipeFile(t, root, ".rhizome/modules/team/recipes", "not a folder\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "modules", "gone"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, ".rhizome", "modules", "gone", "recipes")))
	_, issues := LoadDefaultSources(root)
	paths := []string{}
	for _, issue := range issues {
		require.Equal(t, "recipe_path_error", issue.Code)
		paths = append(paths, filepath.Base(filepath.Dir(issue.Path)))
	}
	require.ElementsMatch(t, []string{"team", "gone"}, paths)
	require.True(t, HasDefaultSources(root), "validation runs so it can report the broken folders")
}
