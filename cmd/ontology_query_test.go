package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOntologyQuerySchemaCommandPrintsExecutableSDL(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "query-schema", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "interface NoteNode")
	require.Contains(t, stdout, "interface Section")
	require.Contains(t, stdout, "interface SummaryDoc")
	require.Contains(t, stdout, "type RequirementsSection implements Section")
	require.Contains(t, stdout, `decisions(first: Int): [Decision!]`)
	require.Contains(t, stdout, `context: Section!`)
	require.Contains(t, stdout, `followUps: Section!`)
	require.Contains(t, stdout, `linked(type: String, first: Int = 20): [NoteNode!]!`)
	require.Contains(t, stdout, `project(path: String, find: String, property: PropertyFilterInput, semantic: [String!], first: Int = 20, offset: Int = 0, filters: [FieldFilterInput!], sort: [SortInput!]): [Project!]!`)
}

func TestOntologyQueryCommandExecutesGraphQL(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"ontology", "query", "--vault", vault.name,
		"--query", `{ project(path: "notes/projects/roadmap-refresh.md") { name decisions { title } } }`,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var result query.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	note := rows[0].(map[string]any)
	require.Equal(t, "Roadmap Refresh", note["name"])
	require.Len(t, note["decisions"].([]any), 2)
}

func TestOntologyQueryCommandExecutesGraphQLWithVariables(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	varsPath := filepath.Join(vault.path, "vars.json")
	require.NoError(t, os.WriteFile(varsPath, []byte(`{"path":"notes/projects/roadmap-refresh.md"}`), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"ontology", "query", "--vault", vault.name,
		"--query", `query Project($path: String!) { project(path: $path) { name } }`,
		"--variables-file", varsPath,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var result query.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "Roadmap Refresh", rows[0].(map[string]any)["name"])
}

func TestAgentOntologyQueryAllocatesDateTimeIdentifiersInPathOrder(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Effort @node(paths: ["efforts/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`,
		"efforts/existing.md": `---
type: Effort
summary: existing effort
id: EFF-2026-08-05-14-32
aliases:
  - EFF-2026-08-05-14-32
---
# Existing
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "ontology-query", "--vault", vault.name,
		"--query", `{
  ontology {
    nextId(type: "Effort", paths: ["efforts/2026-08-05-14-32-second.md", "efforts/2026-08-05-14-32-first.md"]) {
      available errorCode strategy next ids count last paths
      allocations { path id base disambiguator }
    }
  }
}`,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var result query.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Empty(t, result.Errors)
	next := result.Data["ontology"].(map[string]any)["nextId"].(map[string]any)
	require.Equal(t, true, next["available"])
	require.Empty(t, next["errorCode"])
	require.Equal(t, "DATETIME", next["strategy"])
	require.Equal(t, "EFF-2026-08-05-14-32-2", next["next"])
	require.Equal(t, []any{"EFF-2026-08-05-14-32-2", "EFF-2026-08-05-14-32-3"}, next["ids"])
	require.Equal(t, float64(2), next["count"])
	require.Equal(t, "EFF-2026-08-05-14-32-3", next["last"])
	require.Equal(t, []any{"efforts/2026-08-05-14-32-second.md", "efforts/2026-08-05-14-32-first.md"}, next["paths"])
	allocations := next["allocations"].([]any)
	require.Len(t, allocations, 2)
	require.Equal(t, map[string]any{
		"path":          "efforts/2026-08-05-14-32-second.md",
		"id":            "EFF-2026-08-05-14-32-2",
		"base":          "EFF-2026-08-05-14-32",
		"disambiguator": float64(2),
	}, allocations[0])
	require.Equal(t, map[string]any{
		"path":          "efforts/2026-08-05-14-32-first.md",
		"id":            "EFF-2026-08-05-14-32-3",
		"base":          "EFF-2026-08-05-14-32",
		"disambiguator": float64(3),
	}, allocations[1])
}

func TestAgentOntologyQueryReturnsStructuredDateTimeInputErrors(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Effort @node(paths: ["efforts/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "ontology-query", "--vault", vault.name,
		"--query", `{
  ontology {
    malformed: nextId(type: "Effort", paths: ["efforts/no-leading-stamp.md"]) {
      available errorCode error paths allocations { path id }
    }
    counted: nextId(type: "Effort", count: 2, paths: ["efforts/2026-08-05-14-32-example.md"]) {
      available errorCode error paths allocations { path id }
    }
  }
}`,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var result query.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Empty(t, result.Errors)
	root := result.Data["ontology"].(map[string]any)
	malformed := root["malformed"].(map[string]any)
	require.Equal(t, false, malformed["available"])
	require.Equal(t, "invalid_input", malformed["errorCode"])
	require.NotEmpty(t, malformed["error"])
	require.Equal(t, []any{"efforts/no-leading-stamp.md"}, malformed["paths"])
	require.Empty(t, malformed["allocations"])
	counted := root["counted"].(map[string]any)
	require.Equal(t, false, counted["available"])
	require.Equal(t, "invalid_input", counted["errorCode"])
	require.NotEmpty(t, counted["error"])
	require.Equal(t, []any{"efforts/2026-08-05-14-32-example.md"}, counted["paths"])
	require.Empty(t, counted["allocations"])
}

func TestQueryRecipeValidateCommand(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	recipePath := filepath.Join(vault.path, "recipes.md")
	require.NoError(t, os.WriteFile(recipePath, []byte(projectRecipeMarkdown()), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"query-recipe", "validate", "--vault", vault.name,
		"--path", recipePath, "--json",
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp struct {
		OK           bool `json:"ok"`
		IssueCount   int  `json:"issueCount"`
		Dependencies map[string]struct {
			Roots        []string `json:"roots"`
			Fields       []string `json:"fields"`
			Placeholders []string `json:"placeholders"`
		} `json:"dependencies"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.True(t, resp.OK)
	require.Equal(t, 0, resp.IssueCount)
	require.Equal(t, []string{"project"}, resp.Dependencies["project-by-path"].Roots)
	require.Contains(t, resp.Dependencies["project-by-path"].Fields, "decisions")
	require.Empty(t, resp.Dependencies["project-by-path"].Placeholders)
}

func TestAgentQueryRecipeListIsCompact(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	recipePath := filepath.Join(vault.path, "recipes.md")
	require.NoError(t, os.WriteFile(recipePath, []byte(projectRecipeMarkdown()), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "query-recipe", "list", "--vault", vault.name,
		"--path", recipePath,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp struct {
		Recipes []map[string]any `json:"recipes"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.Len(t, resp.Recipes, 1)

	recipe := resp.Recipes[0]
	require.Equal(t, "project-by-path", recipe["id"])
	require.Equal(t, "Project by path", recipe["name"])
	require.Contains(t, recipe["problem"], "project")
	require.NotContains(t, recipe, "query")
	require.NotContains(t, recipe, "outputContract")
	require.NotContains(t, recipe, "adaptationGuidance")
	require.NotContains(t, stdout, "query ProjectByPath")
	require.NotContains(t, stdout, "Keep the recipe centered")

	input := recipe["input"].(map[string]any)
	require.Equal(t, "required_anchor", input["mode"])
	require.Equal(t, "path", input["primaryInput"])
	require.Equal(t, []any{"path"}, input["required"])
	require.Contains(t, recipe["source"].(map[string]any)["path"], "recipes.md")
}

func TestAgentValidateAcceptsQueryRecipesCheck(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "validate", "query-recipes", "--vault", vault.name,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, `"query-recipes"`)
}

func TestAgentQueryRecipeRunCommand(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	recipePath := filepath.Join(vault.path, "recipes.md")
	require.NoError(t, os.WriteFile(recipePath, []byte(projectRecipeMarkdown()), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "query-recipe", "run", "--vault", vault.name,
		"--path", recipePath,
		"--id", "project-by-path",
		"--anchor", "notes/projects/roadmap-refresh.md",
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp struct {
		Recipe struct {
			Problem string `json:"problem"`
			Query   struct {
				GraphQL string `json:"graphQL"`
			} `json:"query"`
			AdaptationGuidance struct {
				Summary string `json:"summary"`
			} `json:"adaptationGuidance"`
		} `json:"recipe"`
		OutputContract struct {
			Empty string `json:"empty"`
		} `json:"outputContract"`
		Query  string       `json:"query"`
		Result query.Result `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.Contains(t, resp.Recipe.Problem, "project")
	require.Contains(t, resp.Recipe.Query.GraphQL, "query ProjectByPath")
	require.Contains(t, resp.Recipe.AdaptationGuidance.Summary, "one project")
	require.Contains(t, resp.Query, "query ProjectByPath")
	require.NotEmpty(t, resp.OutputContract.Empty)
	rows := resp.Result.Data["project"].([]any)
	require.Len(t, rows, 1)
	project := rows[0].(map[string]any)
	require.Equal(t, "Roadmap Refresh", project["name"])
	require.Len(t, project["decisions"].([]any), 2)
}

func TestAgentQueryRecipeUsesDefaultRegistryAndInputsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	recipeDir := filepath.Join(vault.path, ".rhizome", "query-recipes")
	require.NoError(t, os.MkdirAll(recipeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(recipeDir, "project.md"), []byte(projectRecipeMarkdown()), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "query-recipe", "run", "--vault", vault.name,
		"--id", "project-by-path",
		"--inputs-json", `{"path":"notes/projects/roadmap-refresh.md"}`,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp struct {
		Variables map[string]any `json:"variables"`
		Result    query.Result   `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.Equal(t, map[string]any{"path": "notes/projects/roadmap-refresh.md"}, resp.Variables)
	rows := resp.Result.Data["project"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "Roadmap Refresh", rows[0].(map[string]any)["name"])
}

func TestAgentQueryRecipeRunReportsMissingAnchor(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	recipePath := filepath.Join(vault.path, "recipes.md")
	require.NoError(t, os.WriteFile(recipePath, []byte(projectRecipeMarkdown()), 0o644))

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "query-recipe", "run", "--vault", vault.name,
		"--path", recipePath,
		"--id", "project-by-path",
	})
	require.Error(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "missing_required_input")
	require.Contains(t, stdout, "path is required")
}

func TestAgentOntologyQuerySchemaCommandOutputsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "ontology-query-schema", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.Contains(t, payload["schema"], "type Query")
	require.Contains(t, payload["schema"], "type Project implements NoteNode")
	require.Contains(t, payload["schema"], "Project note doc.")
	require.Contains(t, payload["schema"], `project(path: String, find: String, property: PropertyFilterInput, semantic: [String!], first: Int = 20, offset: Int = 0, filters: [FieldFilterInput!], sort: [SortInput!]): [Project!]!`)
}

func TestOntologyReferenceCommandRendersMarkdown(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "reference", "--vault", vault.name, "--type", "Project"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "# Ontology Reference")
	require.Contains(t, stdout, "## Project")
	require.Contains(t, stdout, "Project note doc.")
	require.Contains(t, stdout, "enum: ACTIVE, PAUSED")
}

func TestOntologyReferenceCommandRendersBuiltInSectionContract(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "reference", "--vault", vault.name, "--type", "Section"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "## Section")
	require.Contains(t, stdout, "heading-derived section node")
	require.Contains(t, stdout, "structure: built-in contract for heading-derived section nodes")
	require.Contains(t, stdout, "children")
}

func TestOntologyReferenceCommandRendersDecisionSectionBindings(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "reference", "--vault", vault.name, "--type", "Decision"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "## Decision")
	require.Contains(t, stdout, "heading: Context")
	require.Contains(t, stdout, "heading: Follow-ups")
	require.Contains(t, stdout, "binding: heading-derived subtree")
	require.Contains(t, stdout, "section required: true")
}

func TestAgentOntologyQueryCommandOutputsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "ontology-query", "--vault", vault.name,
		"--query", `{ project(find: "roadmap") { name linked(type: "Decision") { title } } }`,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var result query.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "Roadmap Refresh", rows[0].(map[string]any)["name"])
}

func TestAgentOntologyQueryCommandAcceptsJSONVariables(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "ontology-query", "--vault", vault.name,
		"--json", `{"query":"query Project($path: String!) { project(path: $path) { name } }","variables":{"path":"notes/projects/roadmap-refresh.md"}}`,
	})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var result query.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "Roadmap Refresh", rows[0].(map[string]any)["name"])
}

func TestAgentOntologyReferenceCommandOutputsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "ontology-reference", "--vault", vault.name, "--type", "Project"})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var payload map[string][]map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.Len(t, payload["types"], 1)
	require.Equal(t, "Project", payload["types"][0]["name"])
	require.Len(t, payload["types"][0]["companionDocs"].([]any), 1)
}

func TestOntologyAuthoringGuideCommandRendersRequestedTypeAndSupportingTypes(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "authoring-guide", "--vault", vault.name, "Project"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "# Ontology Authoring Guide")
	require.Contains(t, stdout, "## Project")
	require.Contains(t, stdout, "`status`")
	require.Contains(t, stdout, "enum values: `ACTIVE`, `PAUSED`")
	require.Contains(t, stdout, "`decisions`: query-only neighbor set; do not write it into note markdown")
	require.Contains(t, stdout, "## Supporting Types")
	require.Contains(t, stdout, "### Person")
	require.Contains(t, stdout, "inverse `projects` should also be true on the target note")
	require.Contains(t, stdout, "docs/reference/guides/Project workflow.md")
	require.Contains(t, stdout, "How to update the project hub, linked decisions, and open questions together.")
}

func TestAgentOntologyAuthoringGuideCommandOutputsMarkdown(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "ontology-authoring-guide", "--vault", vault.name, "--type", "Person"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "# Ontology Authoring Guide")
	require.Contains(t, stdout, "## Person")
	require.Contains(t, stdout, "inline property `role`")
}

func TestAgentOntologyAuthoringGuideSuggestsNextIdentifierForFormattedType(t *testing.T) {
	vault := setupAgentTestVault(t, nextIDFixtureFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "ontology-authoring-guide", "--vault", vault.name, "--type", "Spec"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "next available id: `SPEC-0008`")
	require.Contains(t, stdout, "id: SPEC-0008")
}

func TestOntologyAuthoringGuideCommandRendersDecisionStatusGuidance(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "authoring-guide", "--vault", vault.name, "Decision"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "## Decision")
	require.Contains(t, stdout, "`status`")
	require.Contains(t, stdout, "enum values: `ACCEPTED`, `PROPOSED`, `SUPERSEDED`")
	require.Contains(t, stdout, "heading `## Context`")
	require.Contains(t, stdout, "heading `## Decision`")
	require.Contains(t, stdout, "heading `## Consequences`")
	require.Contains(t, stdout, "heading `## Follow-ups`")
	require.Contains(t, stdout, "missing `followUps` raises `missing_required_section`")
}

func TestOntologyAuthoringGuideCommandRendersSectionStructureGuidance(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "authoring-guide", "--vault", vault.name, "Spec"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "## Spec")
	require.Contains(t, stdout, "### SummaryDoc")
	require.Contains(t, stdout, "heading `## Requirements`")
	require.Contains(t, stdout, "heading `### Details`")
	require.Contains(t, stdout, "## Requirements\n\n### Details")
	require.Contains(t, stdout, "missing `requirements.details` raises `missing_required_section`")
}

func TestOntologyInspectCommandSupportsFinderInputs(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "inspect", "--vault", vault.name, "find:roadmap"})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var payload ontology.InspectResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.True(t, payload.OntologyAvailable)
	require.Len(t, payload.Notes, 1)
	require.Equal(t, "notes/projects/roadmap-refresh.md", payload.Notes[0].Path)
	require.Equal(t, "Project", payload.Notes[0].ResolvedType)
	require.NotNil(t, payload.Notes[0].Assessment)
	require.NotNil(t, payload.Notes[0].TypeDoc)
	require.Equal(t, "Project", payload.Notes[0].TypeDoc.Name)
}

func TestOntologyInspectCommandSupportsAbsolutePaths(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	abs := filepath.Join(vault.path, "notes/projects/roadmap-refresh.md")

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "inspect", "--vault", vault.name, abs})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var payload ontology.InspectResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.Len(t, payload.Notes, 1)
	require.Equal(t, "notes/projects/roadmap-refresh.md", payload.Notes[0].Path)
	require.Equal(t, "Project", payload.Notes[0].ResolvedType)
}

func TestOntologyInspectCommandUsesLocalVaultWhenCWDHasRhizomeConfig(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(vault.path))
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "inspect", `find:"roadmap"`})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var payload ontology.InspectResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.True(t, payload.OntologyAvailable)
	require.Len(t, payload.Notes, 1)
	require.Equal(t, "notes/projects/roadmap-refresh.md", payload.Notes[0].Path)
}

func TestOntologyInspectCommandUsesLocalVaultWhenVaultNameMatchesRepo(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(vault.path))
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	origConfig := obsidian.ObsidianConfigFile
	missingConfig := filepath.Join(t.TempDir(), "missing-obsidian.json")
	obsidian.ObsidianConfigFile = func() (string, error) { return missingConfig, nil }
	t.Cleanup(func() {
		obsidian.ObsidianConfigFile = origConfig
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"ontology", "inspect", "--vault", filepath.Base(vault.path), `find:"roadmap"`})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var payload ontology.InspectResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.True(t, payload.OntologyAvailable)
	require.Len(t, payload.Notes, 1)
	require.Equal(t, "notes/projects/roadmap-refresh.md", payload.Notes[0].Path)
}

func TestAgentOntologyInspectCommandOutputsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "ontology-inspect", "--vault", vault.name, "--input", "find:roadmap"})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var payload ontology.InspectResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.True(t, payload.OntologyAvailable)
	require.Len(t, payload.Notes, 1)
	require.Equal(t, "notes/projects/roadmap-refresh.md", payload.Notes[0].Path)
}

func TestAgentOntologyQueryReportsSemanticUnavailableAsJSON(t *testing.T) {
	vault := setupAgentTestVault(t, ontologyCommandFiles())

	stdout, stderr, err := runRootCLI(t, nil, []string{
		"agent", "ontology-query", "--vault", vault.name,
		"--query", `{ project(semantic: "roadmap") { path } }`,
	})
	require.Error(t, err)
	require.Empty(t, stdout)

	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(stderr), &payload))
	require.Contains(t, payload["error"], "embedding")
}

func ontologyCommandFiles() map[string]string {
	return map[string]string{
		".rhizome/ontology/schema.graphql": `
enum ProjectStatus {
  ACTIVE
  PAUSED
}

enum DecisionStatus {
  PROPOSED
  ACCEPTED
  SUPERSEDED
}

interface SummaryDoc {
  summary: String!
}

type Team @node(paths: ["notes/teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
  role: String @field(source: "role", sourceKind: INLINE)
  team: Team @link(inverse: "members")
  projects: [Project!] @link(inverse: "owner")
}

"""Decision note doc."""
type DetailsSection implements Section {
}

type RequirementsSection implements Section {
  details: DetailsSection @contains(level: H3, heading: "Details", required: true)
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  name: String!
  summary: String!
  status: DecisionStatus
  context: Section @contains(level: H2, heading: "Context", required: true)
  decision: Section @contains(level: H2, heading: "Decision", required: true)
  consequences: Section @contains(level: H2, heading: "Consequences", required: true)
  followUps: Section @contains(level: H2, heading: "Follow-ups", required: true)
}

"""Project note doc."""
type Project implements SummaryDoc
  @node(paths: ["notes/projects/*.md"])
  @semantics(kind: BEHAVIORAL)
  @companionDocs(paths: ["docs/reference/guides/Project workflow.md"], purpose: "workflow")
  @retrieval(intents: ["search"], boost: 1.2) {
  name: String!
  summary: String!
  status: ProjectStatus!
  owner: Person! @link(source: "owner", inverse: "projects")
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`,
		"docs/reference/guides/Project workflow.md": `---
summary: How to update the project hub, linked decisions, and open questions together.
---

# Project workflow
`,
		"notes/teams/platform-team.md": `---
type: Team
name: Platform Team
members:
  - notes/people/alice.md
---
`,
		"notes/people/alice.md": `---
type: Person
name: Alice
team: notes/teams/platform-team.md
projects:
  - notes/projects/roadmap-refresh.md
---

role:: Staff Engineer
`,
		"notes/projects/roadmap-refresh.md": `---
type: Project
name: Roadmap Refresh
summary: Refresh the roadmap with typed docs.
status: ACTIVE
owner: notes/people/alice.md
---

Primary decision is [[structured-ontology-decision]].
`,
		"notes/decisions/structured-ontology-decision.md": `---
type: Decision
name: Structured Ontology Decision
summary: Use typed ontology documents.
---

## Context

Project docs needed a typed semantic layer.

## Decision

Decision linked from the project.

## Consequences

Query/docs surfaces now derive from SDL.

## Follow-ups

- Update the project docs after rollout.
`,
		"notes/decisions/backlink-decision.md": `---
type: Decision
name: Backlink Decision
summary: Record backlink-based discovery.
---

## Context

Backlink traversal needed a durable note.

## Decision

This note references [[roadmap-refresh]] from the reverse direction.

## Consequences

Ambient discovery can find the project from this note.

## Follow-ups

- Keep backlink discovery coverage current.
`,
		"notes/specs/search-rewrite.md": `---
type: Spec
summary: Capture the search rewrite contract.
---

## Requirements

Keep the query shape stable.

### Details

Coordinate with [[structured-ontology-decision]].
`,
	}
}

func projectRecipeMarkdown() string {
	return `# Recipes

` + "```query-recipe" + `
apiVersion: rhizome.query-recipe.v1
id: project-by-path
name: Project by path
problem: Load one project and its decisions before editing project workflow docs.
inputSpec:
  mode: required_anchor
  primaryInput: path
  inputs:
    - name: path
      required: true
      kind: string
      description: Vault-relative project note path.
query:
  graphQL: |
    query ProjectByPath($path: String!) {
      project(path: $path) {
        path
        name
        decisions {
          path
          name
        }
      }
    }
outputContract:
  expectedPaths:
    - project.path
    - project.decisions.path
  empty: The project path did not resolve.
  partial: Use the project and call out missing decisions.
  highVolume: Keep the first page and ask before widening.
adaptationGuidance:
  summary: Keep the recipe centered on one project and its decisions.
  rules:
    - If Project is renamed, use the replacement project root.
` + "```" + `
`
}
