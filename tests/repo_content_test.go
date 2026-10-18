//go:build repo_content

package repocontent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

var retiredCoreSkills = []string{"rhizome-note-authoring", "rhizome-onboard", "rhizome-ontology", "rhizome-skill-creator"}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(root, "go.mod"))
	return root
}

func TestManagedRhizomeRouterStructure(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs", "rhizome-md-templates", "RHIZOME.md"))
	require.NoError(t, err)
	text := string(body)
	for _, heading := range []string{"# Rhizome integration", "## Route through the Rhizome skill", "## Integration failures", "### Repo configuration"} {
		require.Contains(t, text, heading)
	}
	require.Contains(t, text, "{{REPO_CONFIG}}")
	require.LessOrEqual(t, len(strings.Split(text, "\n")), 40)
	for _, forbidden := range append(retiredCoreSkills, "rzm agent start --profile") {
		require.NotContains(t, text, forbidden)
	}
}

func TestLiveAgentSkillsExcludeRetiredRoutes(t *testing.T) {
	root := repositoryRoot(t)
	checked := 0
	for _, harness := range []string{".agents", ".claude"} {
		skillRoot := filepath.Join(root, harness, "skills")
		require.DirExists(t, skillRoot)
		err := filepath.WalkDir(skillRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".env") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			for _, retired := range retiredCoreSkills {
				require.NotContains(t, string(body), retired, path)
			}
			return nil
		})
		require.NoError(t, err)
	}
	require.Positive(t, checked)
}

func TestDefaultRepoRecipesValidateAgainstRepoSchema(t *testing.T) {
	root := repositoryRoot(t)
	recipes, issues := queryrecipe.LoadDefaultSources(root)
	require.Empty(t, issues)
	require.NotEmpty(t, recipes)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	executable, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	require.Empty(t, queryrecipe.Validate(recipes, executable).Issues)
}

func TestCoreStarterOwnsSharedRuntimeRecipes(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "pkg", "app", "cli", "init", "templates", "starters")
	ids := func(starter, name string) map[string]bool {
		recipes, issues := queryrecipe.LoadPath(filepath.Join(root, starter, "rhizome", "query-recipes", name))
		require.Empty(t, issues)
		result := map[string]bool{}
		for _, recipe := range recipes {
			result[recipe.ID] = true
		}
		return result
	}
	core := ids("core", "core.yaml")
	actionItems := ids("action-items", "action-items.yaml")
	engineering := ids("agentic-engineering", "spec-driven.yaml")
	for _, id := range []string{"runtime-authoring-context", "runtime-schema-capability-pack"} {
		require.Contains(t, core, id)
		require.NotContains(t, engineering, id)
	}
	require.NotContains(t, core, "assessment-assumptions")
	require.Contains(t, actionItems, "assessment-assumptions")
}

func TestShippedEngineeringRecipesValidateAgainstRepositorySchema(t *testing.T) {
	root := repositoryRoot(t)
	recipes, issues := queryrecipe.LoadPath(filepath.Join(root, "pkg", "app", "cli", "init", "templates", "starters", "agentic-engineering", "rhizome", "query-recipes"))
	require.Empty(t, issues)
	require.NotEmpty(t, recipes)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	executable, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	require.Empty(t, queryrecipe.Validate(recipes, executable).Issues)
}
