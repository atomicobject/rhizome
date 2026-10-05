package queryrecipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestLoadPathParsesMarkdownRecipeBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipes.md")
	writeFile(t, path, `# Recipes

`+"```query-recipe"+`
apiVersion: rhizome.query-recipe.v1
id: spec-neighborhood
name: Spec neighborhood
problem: Load a technical spec and linked efforts.
inputSpec:
  mode: required_anchor
  primaryInput: path
  inputs:
    - name: path
      required: true
      kind: string
query:
  graphQL: |
    query SpecNeighborhood($path: String!) {
      technicalSpec(path: $path) { title id }
    }
outputContract:
  empty: The spec was not found.
adaptationGuidance:
  summary: Keep the query centered on one technical spec.
`+"```"+`
`)

	recipes, issues := LoadPath(path)
	require.Empty(t, issues)
	require.Len(t, recipes, 1)
	require.Equal(t, "spec-neighborhood", recipes[0].ID)
	require.Equal(t, path, recipes[0].Source.Path)
	require.Equal(t, 4, recipes[0].Source.Line)
}

func TestLoadDefaultSourcesIncludesSharedRegistry(t *testing.T) {
	vaultPath := t.TempDir()
	recipePath := filepath.Join(vaultPath, ".rhizome", "query-recipes", "spec.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(recipePath), 0o755))
	writeFile(t, recipePath, `apiVersion: rhizome.query-recipe.v1
id: spec-by-path
name: Spec by path
problem: Load a spec by path.
inputSpec:
  mode: none
query:
  graphQL: |
    { technicalSpec(first: 1) { title } }
outputContract:
  empty: No specs found.
adaptationGuidance:
  summary: Keep this query narrow.
`)

	recipes, issues := LoadDefaultSources(vaultPath)
	require.Empty(t, issues)
	require.Len(t, recipes, 1)
	require.Equal(t, "spec-by-path", recipes[0].ID)
	require.Equal(t, recipePath, recipes[0].Source.Path)
}

func TestLoadDefaultSourcesIgnoresNonRecipeSkillYAML(t *testing.T) {
	vaultPath := t.TempDir()
	recipePath := filepath.Join(vaultPath, ".agents", "skills", "skill-with-recipe", "references", "query-recipes.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(recipePath), 0o755))
	writeFile(t, recipePath, `# Recipes

`+"```query-recipe"+`
apiVersion: rhizome.query-recipe.v1
id: skill-recipe
name: Skill recipe
problem: Load one typed thing.
inputSpec:
  mode: none
query:
  graphQL: |
    { notes(first: 1) { title } }
outputContract:
  empty: No notes found.
adaptationGuidance:
  summary: Keep this query broad.
`+"```"+`
`)

	metadataPath := filepath.Join(vaultPath, ".agents", "skills", "skill-with-recipe", "agents", "openai.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(metadataPath), 0o755))
	writeFile(t, metadataPath, `model: gpt-5.1
tools:
  - name: query-recipe
`)

	configPath := filepath.Join(vaultPath, ".agents", "skills", "external-skill", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	writeFile(t, configPath, `name: external-skill
entrypoint: run.sh
`)

	recipes, issues := LoadDefaultSources(vaultPath)
	require.Empty(t, issues)
	require.Len(t, recipes, 1)
	require.Equal(t, "skill-recipe", recipes[0].ID)
	require.Equal(t, recipePath, recipes[0].Source.Path)
}

func TestLoadPathParsesMultiDocumentYAMLRecipes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec-driven.yaml")
	writeFile(t, path, `apiVersion: rhizome.query-recipe.v1
id: effort-execution-context
name: Effort execution context
problem: Load effort scope.
inputSpec:
  mode: none
query:
  graphQL: |
    { effortNote(first: 1) { path title id } }
outputContract:
  empty: No efforts found.
adaptationGuidance:
  summary: Keep effort scope fields available.
---
apiVersion: rhizome.query-recipe.v1
id: frozen-spec-detail-pack
name: Frozen spec detail pack
problem: Load frozen spec details.
inputSpec:
  mode: none
query:
  graphQL: |
    { technicalSpec(first: 1) { path title id } }
outputContract:
  empty: No specs found.
adaptationGuidance:
  summary: Keep spec identity fields available.
`)

	recipes, issues := LoadPath(path)
	require.Empty(t, issues)
	require.Len(t, recipes, 2)
	require.Equal(t, "effort-execution-context", recipes[0].ID)
	require.Equal(t, "frozen-spec-detail-pack", recipes[1].ID)
	require.Equal(t, "document-2", recipes[1].Source.Block)
}

func TestValidateReportsMetadataAndDuplicateIssues(t *testing.T) {
	recipes := []Recipe{
		{APIVersion: APIVersion, ID: "dupe", Name: "One", Problem: "p", InputSpec: InputSpec{Mode: InputModeNone}, Query: QuerySpec{GraphQL: "{ notes(type: \"TechnicalSpec\") { title } }"}, OutputContract: OutputContract{Empty: "empty"}, AdaptationGuidance: AdaptationGuidance{Summary: "adapt"}, Source: Source{Path: "a.yaml", Line: 1}},
		{APIVersion: APIVersion, ID: "dupe", Name: "Two", InputSpec: InputSpec{Mode: "sometimes"}, Query: QuerySpec{GraphQL: "{ notes(type: \"TechnicalSpec\") { title } }"}, Source: Source{Path: "b.yaml", Line: 1}},
	}

	result := Validate(recipes, nil)
	requireIssue(t, result.Issues, "duplicate_recipe_id")
	requireIssue(t, result.Issues, "missing_required_field")
	requireIssue(t, result.Issues, "invalid_input_mode")
}

func TestValidateAcceptsGeneratedRelationCountField(t *testing.T) {
	decisions := &ontology.Field{Name: "decisions", Kind: ontology.FieldKindNeighbor, TypeName: "Decision", List: true}
	schema := &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			"Decision": {Name: "Decision", Role: ontology.TypeRoleNote, ByName: map[string]*ontology.Field{}},
			"Project": {
				Name:   "Project",
				Role:   ontology.TypeRoleNote,
				Fields: []*ontology.Field{decisions},
				ByName: map[string]*ontology.Field{"decisions": decisions},
			},
		},
		Interfaces: map[string]*ontology.InterfaceType{},
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	recipe := Recipe{
		APIVersion:         APIVersion,
		ID:                 "project-decision-counts",
		Name:               "Project decision counts",
		Problem:            "Load relation counts without related nodes.",
		InputSpec:          InputSpec{Mode: InputModeNone},
		Query:              QuerySpec{GraphQL: `{ project(find: "Project", first: 20) { path decisionsCount } }`},
		OutputContract:     OutputContract{Empty: "No projects found."},
		AdaptationGuidance: AdaptationGuidance{Summary: "Keep decision counts."},
	}

	result := Validate([]Recipe{recipe}, execSchema)
	require.Empty(t, result.Issues)
}

func TestCompileBindsDeclaredVariablesAndExtractsDependencies(t *testing.T) {
	execSchema := testExecutableSchema(t)
	recipe := Recipe{
		APIVersion: APIVersion,
		ID:         "spec-by-path",
		Name:       "Spec by path",
		Problem:    "Load one spec.",
		InputSpec: InputSpec{
			Mode:         InputModeRequiredAnchor,
			PrimaryInput: "path",
			Inputs: []Input{{
				Name:     "path",
				Required: true,
				Kind:     "string",
			}},
		},
		Query:              QuerySpec{GraphQL: `query SpecByPath($path: String!) { technicalSpec(path: $path) { title status } }`},
		OutputContract:     OutputContract{Empty: "missing"},
		AdaptationGuidance: AdaptationGuidance{Summary: "keep technicalSpec root"},
	}

	compiled, issues := Compile(recipe, execSchema, map[string]string{"path": `docs/specs/a "quote".md`})
	require.Empty(t, issues)
	require.Equal(t, map[string]any{"path": `docs/specs/a "quote".md`}, compiled.Variables)
	require.Equal(t, []string{"technicalSpec"}, compiled.Dependencies.Roots)
	require.Contains(t, compiled.Dependencies.Fields, "status")
	require.Empty(t, compiled.Dependencies.Placeholders)
}

func TestCompileExtractsRuntimeRootDependencies(t *testing.T) {
	execSchema := testExecutableSchema(t)
	recipe := Recipe{
		APIVersion: APIVersion,
		ID:         "runtime-authoring-context",
		Name:       "Runtime authoring context",
		Problem:    "Load authoring context.",
		InputSpec: InputSpec{
			Mode:         InputModeRequiredAnchor,
			PrimaryInput: "type",
			Inputs:       []Input{{Name: "type", Required: true, Kind: "string"}},
		},
		Query: QuerySpec{GraphQL: `query RuntimeAuthoring($type: String!) {
			ontology {
				authoringGuide(type: $type) { markdown }
				nextId(type: $type) { next errorCode }
			}
			code {
				docsForCode(path: "pkg/ontology/query/schema.go") { available }
			}
		}`},
		OutputContract:     OutputContract{ExpectedPaths: []string{"ontology.authoringGuide.markdown", "code.docsForCode.available"}, Empty: "missing"},
		AdaptationGuidance: AdaptationGuidance{Summary: "keep runtime roots"},
	}

	compiled, issues := Compile(recipe, execSchema, map[string]string{"type": "TechnicalSpec"})
	require.Empty(t, issues)
	require.Empty(t, compiled.Dependencies.Roots)
	require.Equal(t, []string{"code", "ontology"}, compiled.Dependencies.RuntimeRoots)
	require.Contains(t, compiled.Dependencies.Fields, "authoringGuide")
	require.Contains(t, compiled.Dependencies.Fields, "next")
	require.Contains(t, compiled.Dependencies.Fields, "docsForCode")
}

func TestValidateUsesActualEnumValueForSyntheticInputs(t *testing.T) {
	execSchema := testExecutableSchemaWithEnumArg(t)
	recipe := Recipe{
		APIVersion: APIVersion,
		ID:         "specs-by-status",
		Name:       "Specs by status",
		Problem:    "Load specs by status.",
		InputSpec: InputSpec{
			Mode:         InputModeRequiredAnchor,
			PrimaryInput: "status",
			Inputs:       []Input{{Name: "status", Required: true, Kind: "enum"}},
		},
		Query: QuerySpec{GraphQL: `query SpecsByStatus($status: ReviewState!) {
			technicalSpec(path: "docs/specs/a.md", status: $status) {
				path
			}
		}`},
		OutputContract:     OutputContract{ExpectedPaths: []string{"technicalSpec.path"}, Empty: "missing"},
		AdaptationGuidance: AdaptationGuidance{Summary: "keep status filter"},
	}

	result := Validate([]Recipe{recipe}, execSchema)
	require.Empty(t, result.Issues)
}

func TestCompileReportsMissingOutputContractPath(t *testing.T) {
	execSchema := testExecutableSchema(t)
	recipe := Recipe{
		APIVersion: APIVersion,
		ID:         "spec-by-path",
		Name:       "Spec by path",
		Problem:    "Load one spec.",
		InputSpec: InputSpec{
			Mode:         InputModeRequiredAnchor,
			PrimaryInput: "path",
			Inputs:       []Input{{Name: "path", Required: true, Kind: "string"}},
		},
		Query: QuerySpec{GraphQL: `query SpecByPath($path: String!) {
			technicalSpec(path: $path) {
				specTitle: title
			}
		}`},
		OutputContract: OutputContract{
			ExpectedPaths: []string{"technicalSpec.path", "technicalSpec.specTitle"},
			Empty:         "missing",
		},
		AdaptationGuidance: AdaptationGuidance{Summary: "keep technicalSpec root"},
	}

	_, issues := Compile(recipe, execSchema, map[string]string{"path": "docs/specs/a.md"})
	requireIssue(t, issues, "missing_output_path")
	require.Contains(t, issues[0].Message, "technicalSpec.path")

	recipe.OutputContract.ExpectedPaths = []string{"technicalSpec.specTitle"}
	compiled, issues := Compile(recipe, execSchema, map[string]string{"path": "docs/specs/a.md"})
	require.Empty(t, issues)
	require.NotNil(t, compiled)
}

func TestCompileTraversesNamedFragmentsForOutputPathsAndDependencies(t *testing.T) {
	execSchema := testExecutableSchema(t)
	recipe := Recipe{
		APIVersion: APIVersion,
		ID:         "spec-fragment",
		Name:       "Spec fragment",
		Problem:    "Load spec fields through named fragments.",
		InputSpec:  InputSpec{Mode: InputModeNone},
		Query: QuerySpec{GraphQL: `query SpecFragment {
			technicalSpec(first: 1) {
				...SpecFields
			}
		}
		fragment SpecFields on TechnicalSpec {
			path
			title: id
			...SpecStatus
		}
		fragment SpecStatus on TechnicalSpec {
			status
		}`},
		OutputContract: OutputContract{
			ExpectedPaths: []string{"technicalSpec.path", "technicalSpec.title", "technicalSpec.status"},
			Empty:         "missing",
		},
		AdaptationGuidance: AdaptationGuidance{Summary: "keep fragments"},
	}

	compiled, issues := Compile(recipe, execSchema, nil)
	require.Empty(t, issues)
	require.NotNil(t, compiled)
	require.Equal(t, []string{"technicalSpec"}, compiled.Dependencies.Roots)
	require.Contains(t, compiled.Dependencies.Fields, "id")
	require.Contains(t, compiled.Dependencies.Fields, "status")
	require.Contains(t, compiled.Dependencies.TypeNames, "TechnicalSpec")
}

func TestCompileBindsMultiAnchorAsListVariable(t *testing.T) {
	recipe := Recipe{
		APIVersion: APIVersion,
		ID:         "specs-by-path",
		Name:       "Specs by path",
		Problem:    "Load multiple specs.",
		InputSpec: InputSpec{
			Mode:         InputModeMultiAnchor,
			PrimaryInput: "paths",
			Inputs:       []Input{{Name: "paths", Required: true, Kind: "string"}},
		},
		Query: QuerySpec{GraphQL: `query SpecsByPath($paths: [String!]!) {
			nodes(refs: $paths) { items { requestedRef } }
		}`},
		OutputContract:     OutputContract{ExpectedPaths: []string{"nodes.items.requestedRef"}, Empty: "missing"},
		AdaptationGuidance: AdaptationGuidance{Summary: "keep multiple spec paths"},
	}

	inputs, err := MergeAnchorInput(recipe, nil, []string{"docs/specs/a.md", "docs/specs/b.md"})
	require.NoError(t, err)
	variables, resolved, issues := BindVariables(recipe, inputs)
	require.Empty(t, issues)
	require.JSONEq(t, `["docs/specs/a.md","docs/specs/b.md"]`, resolved["paths"])
	require.Equal(t, []string{"docs/specs/a.md", "docs/specs/b.md"}, variables["paths"])
	compiled, compileIssues := Compile(recipe, testExecutableSchema(t), inputs)
	require.Empty(t, compileIssues)
	require.NotNil(t, compiled)
	require.Equal(t, []string{"docs/specs/a.md", "docs/specs/b.md"}, compiled.Variables["paths"])
}

func TestRecipeSummaryPublicContract(t *testing.T) {
	longHint := strings.Repeat("界", 82)
	recipe := Recipe{
		Problem:   strings.Repeat("a", 236) + "界tail",
		InputSpec: InputSpec{PrimaryInput: "path"},
		Examples: []Example{
			{Name: "  Named example  ", Inputs: map[string]string{"path": "ignored.md"}},
			{Inputs: map[string]string{"path": "", "zeta": "Z", "alpha": "A"}},
			{Inputs: map[string]string{"path": "   ", "alpha": "", "beta": longHint}},
			{Inputs: map[string]string{"path": "docs/too-many.md"}},
		},
	}
	summary := Summary(recipe)
	require.Equal(t, strings.Repeat("a", 236)+"界...", summary.Problem)
	require.NotContains(t, summary.Problem, "�")
	require.Equal(t, 4, summary.ExampleCount)
	require.Equal(t, []string{"Named example", "alpha=A", "beta=" + strings.Repeat("界", 72) + "..."}, summary.ExampleHints)
	require.Equal(t, "a b c", Summary(Recipe{Problem: "a\tb\nc"}).Problem)
	require.Equal(t, "abc", Summary(Recipe{Problem: "abc"}).Problem)
	require.Equal(t, []string{"alpha=A"}, Summary(Recipe{Examples: []Example{{Inputs: map[string]string{"zeta": "Z", "alpha": "A"}}}}).ExampleHints)
	require.Equal(t, []string{"path=" + strings.Repeat("界", 72) + "..."}, Summary(Recipe{
		InputSpec: InputSpec{PrimaryInput: "path"},
		Examples:  []Example{{Inputs: map[string]string{"path": longHint, "alpha": "A"}}},
	}).ExampleHints)
}

func TestBindVariables_CoercesListInput(t *testing.T) {
	recipe := Recipe{
		APIVersion: APIVersion,
		ID:         "typed-survey",
		Name:       "Typed survey",
		Problem:    "Survey typed notes.",
		InputSpec: InputSpec{
			Mode: InputModeNone,
			Inputs: []Input{
				{Name: "topics", Required: true, Kind: "list"},
				{Name: "type", Kind: "string", Default: "SpecLike"},
			},
		},
		Query:              QuerySpec{GraphQL: `query TypedSurvey($topics: [String!]!, $type: String = "SpecLike") { notes(type: $type, semantic: $topics) { path } }`},
		OutputContract:     OutputContract{ExpectedPaths: []string{"notes.path"}, Empty: "missing"},
		AdaptationGuidance: AdaptationGuidance{Summary: "keep typed survey"},
	}

	variables, resolved, issues := BindVariables(recipe, map[string]string{"topics": `["alpha","beta"]`})
	require.Empty(t, issues)
	require.JSONEq(t, `["alpha","beta"]`, resolved["topics"])
	require.Equal(t, []string{"alpha", "beta"}, variables["topics"])
	require.Equal(t, "SpecLike", variables["type"])
}

func TestCompileRejectsMissingRequiredInputAndDeprecatedPlaceholder(t *testing.T) {
	execSchema := testExecutableSchema(t)
	recipe := Recipe{
		APIVersion: APIVersion,
		ID:         "bad",
		Name:       "Bad",
		Problem:    "Bad input.",
		InputSpec: InputSpec{
			Mode:         InputModeRequiredAnchor,
			PrimaryInput: "path",
			Inputs:       []Input{{Name: "path", Required: true, Kind: "string"}},
		},
		Query:              QuerySpec{GraphQL: `{ technicalSpec(path: {{missing}}) { title } }`},
		OutputContract:     OutputContract{Empty: "missing"},
		AdaptationGuidance: AdaptationGuidance{Summary: "adapt"},
	}
	_, issues := Compile(recipe, execSchema, nil)
	requireIssue(t, issues, "deprecated_placeholder")

	recipe.Query.GraphQL = `query Bad($path: String!) { technicalSpec(path: $path) { title } }`
	_, issues = Compile(recipe, execSchema, nil)
	requireIssue(t, issues, "missing_required_input")
}

func TestAgenticEngineeringStarterBundleValidatesAgainstRepoSchema(t *testing.T) {
	repoRoot := filepath.Clean("../../..")
	recipePaths := []string{
		filepath.Join(repoRoot, ".rhizome", "query-recipes", "core.yaml"),
		filepath.Join(repoRoot, ".rhizome", "query-recipes", "spec-driven.yaml"),
	}
	var recipes []Recipe
	for _, recipePath := range recipePaths {
		loaded, loadIssues := LoadPath(recipePath)
		require.Empty(t, loadIssues)
		recipes = append(recipes, loaded...)
	}
	recipeIDs := make([]string, 0, len(recipes))
	for _, recipe := range recipes {
		recipeIDs = append(recipeIDs, recipe.ID)
	}
	require.ElementsMatch(t, []string{
		"runtime-authoring-context",
		"runtime-schema-capability-pack",
		"note-by-path",
		"effort-execution-context",
		"frozen-spec-index-pack",
		"frozen-spec-detail-pack",
		"story-acceptance-pack",
		"closure-drift-pack",
		"runtime-code-evidence-pack",
		"runtime-note-code-evidence-pack",
		"topic-typed-survey",
	}, recipeIDs)
	require.NotContains(t, recipeIDs, "assessment-assumptions")

	schema, err := ontology.LoadSchema(repoRoot)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)

	result := Validate(recipes, execSchema)
	require.Empty(t, result.Issues)
}

func TestDefaultRepoRecipesUseGenericNoteByPath(t *testing.T) {
	repoRoot := filepath.Clean("../../..")
	recipes, loadIssues := LoadDefaultSources(repoRoot)
	require.Empty(t, loadIssues)

	ids := make(map[string]struct{}, len(recipes))
	for _, recipe := range recipes {
		if _, exists := ids[recipe.ID]; exists {
			t.Fatalf("duplicate query recipe id %q from %s", recipe.ID, recipe.Source.Path)
		}
		ids[recipe.ID] = struct{}{}
	}

	require.Contains(t, ids, "note-by-path")
	require.NotContains(t, ids, "spec-by-path")
	require.Contains(t, ids, "action-items-in-note")
	require.Contains(t, ids, "action-items-for-context")
	require.Contains(t, ids, "runtime-authoring-context")
	require.Contains(t, ids, "runtime-schema-capability-pack")
	require.Contains(t, ids, "assessment-assumptions")
}

func testExecutableSchema(t *testing.T) *ontologyquery.ExecutableSchema {
	t.Helper()
	schema := &ontology.Schema{
		EnumTypes: map[string]*ontology.EnumType{
			"ReviewState": {
				Name: "ReviewState",
				Values: []*ontology.EnumValue{
					{Name: "OPEN"},
					{Name: "CLOSED"},
				},
			},
		},
		Types: map[string]*ontology.NoteType{
			"TechnicalSpec": {
				Name: "TechnicalSpec",
				Role: ontology.TypeRoleNote,
				Fields: []*ontology.Field{
					{Name: "id", Kind: ontology.FieldKindScalar, TypeName: "String"},
					{Name: "status", Kind: ontology.FieldKindScalar, TypeName: "String"},
				},
			},
		},
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	return execSchema
}

func testExecutableSchemaWithEnumArg(t *testing.T) *ontologyquery.ExecutableSchema {
	t.Helper()
	sdl := `
enum ReviewState {
  OPEN
  CLOSED
}

type TechnicalSpec {
  path: String
}

type Query {
  technicalSpec(path: String, status: ReviewState): [TechnicalSpec!]!
}
`
	parsed, err := gqlparser.LoadSchema(&ast.Source{Name: "test.graphql", Input: sdl})
	require.NoError(t, err)
	return &ontologyquery.ExecutableSchema{
		SDL:       sdl,
		Schema:    parsed,
		RootTypes: map[string]string{"technicalSpec": "TechnicalSpec"},
	}
}

func requireIssue(t *testing.T, issues []Issue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("issue %q not found in %#v", code, issues)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
