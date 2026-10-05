package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

const commandBrokenRecipe = "```query-recipe\napiVersion: rhizome.query-recipe.v1\nid: example\nname: Example\nproblem: Verify recipe discovery.\ninputSpec:\n  mode: none\nquery:\n  graphQL: '{ noSuchRoot { title } }'\noutputContract:\n  empty: None.\nadaptationGuidance:\n  summary: Repair schema drift.\n```\n"

func TestValidationCommandsDiscoverRecipesInDefaultAndAllComposition(t *testing.T) {
	validRecipe := strings.Replace(commandBrokenRecipe, "noSuchRoot { title }", `notes(find: "example", first: 1) { nodes { title } }`, 1)
	for _, tc := range []struct {
		name, issue string
		files       map[string]string
		emptyRoots  bool
	}{
		{"registry", "query_compile_error", map[string]string{".rhizome/query-recipes/example.md": commandBrokenRecipe}, false},
		{"docs", "query_compile_error", map[string]string{"docs/query-recipes/example.md": commandBrokenRecipe}, false},
		{"templates", "query_compile_error", map[string]string{"docs/rhizome-md-templates/example.md": commandBrokenRecipe}, false},
		{"skills", "query_compile_error", map[string]string{".agents/skills/example/references/query-recipes.md": commandBrokenRecipe}, false},
		{"duplicate-ids", "duplicate_recipe_id", map[string]string{"docs/rhizome-md-templates/example.md": validRecipe, ".agents/skills/example/references/query-recipes.md": validRecipe}, false},
		{"malformed-block", "recipe_parse_error", map[string]string{".agents/skills/example/references/query-recipes.md": "```query-recipe\napiVersion: [\n```\n"}, false},
		{"invalid-root", "recipe_path_error", map[string]string{".agents/skills": "not a directory"}, false},
		{"unrelated-skills", "", map[string]string{".agents/skills/example/SKILL.md": "# Example\nNo recipe here.\n", ".agents/skills/example/config.yaml": "name: example\n", ".agents/skills/example/agents/openai.yaml": "tools: [\n"}, false},
		{"empty-roots", "", nil, true},
		{"missing-roots", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{
				".rhizome/config.yml":              "notes:\n  includes: ['people/**/*.md']\nvalidation:\n  default:\n    add: [query-recipes]\n",
				".rhizome/ontology/schema.graphql": `type Person @node(paths: ["people/*.md"]) { name: String! }`,
				"people/alice.md":                  "---\ntype: Person\nname: Alice\n---\n",
			}
			for path, content := range tc.files {
				files[path] = content
			}
			vault := setupAgentTestVault(t, files)
			if tc.emptyRoots {
				for _, path := range queryrecipe.DefaultSourceRoots(vault.path) {
					require.NoError(t, os.MkdirAll(path, 0o755))
				}
			}
			for _, surface := range []struct {
				name    string
				command []string
				json    bool
			}{
				{"local", []string{"validate"}, false},
				{"agent", []string{"agent", "validate"}, true},
				{"ci", []string{"ci"}, true},
			} {
				command, _, err := rootCmd.Find(surface.command)
				require.NoError(t, err)
				command.SetOut(nil)
				command.SetErr(nil)
				t.Cleanup(func() { command.SetOut(nil); command.SetErr(nil) })
				for _, selector := range []string{"default", "all"} {
					t.Run(surface.name+"/"+selector, func(t *testing.T) {
						args := []string{selector, "--vault", vault.name}
						var stdout, stderr string
						var err error
						if surface.name == "ci" {
							command := newCIProductCmdWithRunner(newProductionValidationRunner())
							out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
							command.SetOut(out)
							command.SetErr(errOut)
							command.SetArgs(append(args, "--format", "json"))
							err = command.Execute()
							stdout, stderr = out.String(), errOut.String()
						} else {
							stdout, stderr, err = runRootCLI(t, nil, append(append([]string(nil), surface.command...), args...))
						}
						require.Empty(t, stderr)
						if tc.issue == "" {
							require.NoError(t, err)
						} else {
							var coded interface{ ExitCode() int }
							require.ErrorAs(t, err, &coded)
							require.Equal(t, actions.ValidationExitFindings, coded.ExitCode())
						}
						require.Contains(t, stdout, "query-recipes")
						if !surface.json {
							return
						}
						var result actions.ValidationResult
						require.NoError(t, json.Unmarshal([]byte(stdout), &result))
						require.Equal(t, tc.issue == "", result.OK)
						found := false
						for _, outcome := range result.Outcomes {
							if outcome.Check == "query-recipes" {
								found = true
								if tc.issue == "" {
									require.Equal(t, validate.CheckOutcomeNotApplicable, outcome.Outcome)
								} else {
									require.Equal(t, validate.CheckOutcomeCompleted, outcome.Outcome)
								}
							}
						}
						require.True(t, found)
						if tc.issue == "" {
							require.Zero(t, result.IssueCount)
						} else {
							require.Equal(t, 1, result.IssueCount, "%+v", result.Checks)
							found = false
							for _, check := range result.Checks {
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
		})
	}
}
