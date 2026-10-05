package validationproduct

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const liveBrokenRecipe = "```query-recipe\napiVersion: rhizome.query-recipe.v1\nid: example\nname: Example\nproblem: Verify recipe discovery.\ninputSpec:\n  mode: none\nquery:\n  graphQL: '{ noSuchRoot { title } }'\noutputContract:\n  empty: None.\nadaptationGuidance:\n  summary: Repair schema drift.\n```\n"

func TestRunLiveRecipeDiscoveryMatchesSupportedSources(t *testing.T) {
	validRecipe := strings.Replace(liveBrokenRecipe, "noSuchRoot { title }", `notes(find: "example", first: 1) { nodes { title } }`, 1)
	for _, tc := range []struct {
		name, issue string
		files       map[string]string
		emptyRoots  bool
	}{
		{"registry", "query_compile_error", map[string]string{".rhizome/query-recipes/example.md": liveBrokenRecipe}, false},
		{"docs", "query_compile_error", map[string]string{"docs/query-recipes/example.md": liveBrokenRecipe}, false},
		{"templates", "query_compile_error", map[string]string{"docs/rhizome-md-templates/example.md": liveBrokenRecipe}, false},
		{"skills", "query_compile_error", map[string]string{".agents/skills/example/references/query-recipes.md": liveBrokenRecipe}, false},
		{"duplicate-ids", "duplicate_recipe_id", map[string]string{"docs/rhizome-md-templates/example.md": validRecipe, ".agents/skills/example/references/query-recipes.md": validRecipe}, false},
		{"malformed-block", "recipe_parse_error", map[string]string{".agents/skills/example/references/query-recipes.md": "```query-recipe\napiVersion: [\n```\n"}, false},
		{"invalid-root", "recipe_path_error", map[string]string{".agents/skills": "not a directory"}, false},
		{"unrelated-skills", "", map[string]string{".agents/skills/example/SKILL.md": "# Example\nNo recipe here.\n", ".agents/skills/example/config.yaml": "name: example\n", ".agents/skills/example/agents/openai.yaml": "tools: [\n"}, false},
		{"empty-roots", "", nil, true},
		{"missing-roots", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeProjectionVault(t)
			require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{Validation: obsidian.LocalValidationConfig{
				Default: obsidian.LocalValidationSuiteConfig{Add: []string{"query-recipes"}},
			}}))
			for path, content := range tc.files {
				full := filepath.Join(root, filepath.FromSlash(path))
				require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
				require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
			}
			if tc.emptyRoots {
				for _, path := range queryrecipe.DefaultSourceRoots(root) {
					require.NoError(t, os.MkdirAll(path, 0o755))
				}
			}
			run, err := RunLive(context.Background(), testProjectionNoteMetadata(t), obsidian.VaultDefinition{Path: root}, 100)
			require.NoError(t, err)
			outcomeFound := false
			for _, outcome := range run.Result.Outcomes {
				if outcome.Check == "query-recipes" {
					outcomeFound = true
					if tc.issue == "" {
						require.Equal(t, validate.CheckOutcomeNotApplicable, outcome.Outcome)
					} else {
						require.Equal(t, validate.CheckOutcomeCompleted, outcome.Outcome)
					}
				}
			}
			require.True(t, outcomeFound)
			if tc.issue == "" {
				require.True(t, run.Result.OK)
				require.Zero(t, run.Result.IssueCount)
				for _, check := range run.Result.Checks {
					require.NotEqual(t, "query-recipes", check.Name)
				}
			} else {
				require.False(t, run.Result.OK)
				require.Equal(t, 1, run.Result.IssueCount, "%+v", run.Result.Checks)
				found := false
				for _, check := range run.Result.Checks {
					if check.Name == "query-recipes" {
						found = true
						require.Len(t, check.Issues, 1)
						require.Equal(t, tc.issue, check.Issues[0].Code)
					}
				}
				require.True(t, found)
			}
		})
	}
}

func TestRunLiveReportsDanglingRecipeRoots(t *testing.T) {
	for index := range queryrecipe.DefaultSourceRoots("") {
		t.Run(queryrecipe.DefaultSourceRoots("")[index], func(t *testing.T) {
			root := writeProjectionVault(t)
			require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{Validation: obsidian.LocalValidationConfig{
				Default: obsidian.LocalValidationSuiteConfig{Add: []string{"query-recipes"}},
			}}))
			path := queryrecipe.DefaultSourceRoots(root)[index]
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			if err := os.Symlink(filepath.Join(root, "missing-target"), path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			run, err := RunLive(context.Background(), testProjectionNoteMetadata(t), obsidian.VaultDefinition{Path: root}, 100)
			require.NoError(t, err)
			require.False(t, run.Result.OK)
			require.Equal(t, 1, run.Result.IssueCount)
			found := false
			for _, check := range run.Result.Checks {
				if check.Name == "query-recipes" {
					found = true
					require.Len(t, check.Issues, 1)
					require.Equal(t, "recipe_path_error", check.Issues[0].Code)
					require.Equal(t, filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator))), check.Issues[0].Path)
				}
			}
			require.True(t, found)
		})
	}
}
