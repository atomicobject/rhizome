package query

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
	"testing"
)

func requireSchemaField(t *testing.T, exec *ExecutableSchema, owner, name, valueType string) *ast.FieldDefinition {
	t.Helper()
	typeDef := exec.Schema.Types[owner]
	require.NotNil(t, typeDef, owner)
	field := typeDef.Fields.ForName(name)
	require.NotNil(t, field, owner+"."+name)
	require.Equal(t, valueType, field.Type.String(), owner+"."+name)
	return field
}

func TestBuildExecutableSchema_IncludesBuiltinsAndNeighborFields(t *testing.T) {
	env := newQueryTestEnv(t)
	exec := env.execSchema
	require.Equal(t, ast.Interface, exec.Schema.Types["NoteNode"].Kind)
	require.Equal(t, "BEHAVIORAL", exec.Schema.Types["Project"].Directives.ForName("semantics").Arguments.ForName("kind").Value.Raw)
	require.Equal(t, "Project note doc.", exec.Schema.Types["Project"].Description)
	for _, field := range []struct{ owner, name, valueType string }{
		{"NoteNode", "linked", "[NoteNode!]!"}, {"Project", "decisions", "[Decision!]"},
		{"Query", "ontology", "OntologyRuntime!"}, {"Query", "code", "CodeRuntime!"},
		{"NoteNode", "score", "Float"}, {"NoteNode", "format", "String!"},
		{"NoteNode", "sourceRepresentation", "String!"}, {"NoteNode", "evidenceRepresentation", "String!"},
		{"NoteNode", "sourceCapabilities", "[String!]!"}, {"FieldFilterInput", "op", "FieldFilterOperator"},
		{"SortInput", "direction", "SortDirection"}, {"Query", "project", "[Project!]!"},
	} {
		requireSchemaField(t, exec, field.owner, field.name, field.valueType)
	}
	require.Nil(t, exec.Schema.Query.Fields.ForName("agent"))
	require.Nil(t, exec.Schema.Types["AgentRuntime"])
	require.Equal(t, "eq", exec.Schema.Types["FieldFilterInput"].Fields.ForName("op").DefaultValue.Raw)
	require.Equal(t, "asc", exec.Schema.Types["SortInput"].Fields.ForName("direction").DefaultValue.Raw)
	project := exec.Schema.Query.Fields.ForName("project")
	for _, arg := range []string{"path", "find", "property", "semantic", "first", "offset", "filters", "sort"} {
		require.NotNil(t, project.Arguments.ForName(arg))
	}
	require.Equal(t, "0", project.Arguments.ForName("offset").DefaultValue.Raw)
	require.Equal(t, RuntimeRootOntology, exec.RuntimeRoots["ontology"])
}

func TestBuildExecutableSchema_RootOnlyOntologySupportsSectionFragments(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Report @node(paths: ["reports/*.html"]) {
  title: String!
}
`, nil)
	prepared, errs := Prepare(env.execSchema, `{
  node(ref: "reports/example.html") {
    ... on Section { content notePath level }
  }
}`)
	require.Empty(t, errs)
	require.NotNil(t, prepared)
}

func TestBuildExecutableSchema_AuthoredNoteInterfaceFieldsAreUniversalOnNoteNode(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface Note {
  summary: String
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}

type Scratch @node(paths: ["notes/scratch/*.md"]) {
  label: String
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
summary: Shared note field
---
`,
		"notes/scratch/plain.md": `---
type: Scratch
label: Plain
---
`,
	})

	for _, owner := range []string{"Note", "NoteNode", "Project", "Scratch"} {
		requireSchemaField(t, env.execSchema, owner, "summary", "String")
		if owner != "Note" {
			require.Contains(t, env.execSchema.Schema.Types[owner].Interfaces, "Note")
		}
		require.Contains(t, env.execSchema.Schema.Types[owner].Interfaces, "Node")
	}
}

func TestBuildExecutableSchema_AuthoredNoteInterfaceSkipsRuntimeFieldDuplicates(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface Note {
  summary: String
  tags: [String!]
  content: String
  title: String
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
summary: Shared note field
tags:
  - planning
---
# Roadmap
`,
	})

	for _, owner := range []string{"NoteNode", "Project"} {
		require.Contains(t, env.execSchema.Schema.Types[owner].Interfaces, "Note")
		requireSchemaField(t, env.execSchema, owner, "summary", "String")
		for name, valueType := range map[string]string{"tags": "[String!]!", "content": "String!", "title": "String!"} {
			requireSchemaField(t, env.execSchema, owner, name, valueType)
			count := 0
			for _, field := range env.execSchema.Schema.Types[owner].Fields {
				if field.Name == name {
					count++
				}
			}
			require.Equal(t, 1, count, owner+"."+name)
		}
	}
}

func TestBuildExecutableSchema_RuntimeRootsAreReserved(t *testing.T) {
	root := t.TempDir()
	writeQueryTestConfig(t, root)
	writeQueryTestSchema(t, root, `
type Ontology @node(paths: ["notes/ontology/*.md"]) {
  name: String!
}
`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	_, err = BuildExecutableSchema(schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), `generated query root "ontology"`)
}

func TestBuildExecutableSchema_RuntimeHelperTypeNamesAreReserved(t *testing.T) {
	root := t.TempDir()
	writeQueryTestConfig(t, root)
	writeQueryTestSchema(t, root, `
type CodeRuntime @node(paths: ["notes/code-runtime/*.md"]) {
  name: String!
}
`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	_, err = BuildExecutableSchema(schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), `ontology type "CodeRuntime" conflicts with built-in runtime GraphQL type`)
}

func TestExecutePrepared_DispatchesIntrospection(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `{ __type(name: "Project") { name kind } }`)
	require.Empty(t, errs)
	require.True(t, prepared.Introspection)

	result := ExecutePrepared(context.Background(), env.deps(nil), env.schema, env.execSchema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, "Project", result.Data["__type"].(map[string]any)["name"])
	require.Equal(t, "OBJECT", result.Data["__type"].(map[string]any)["kind"])
}

func TestExecutePrepared_IntrospectionOmitsQueryMetaFields(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `{ __type(name: "Query") { fields { name } } }`)
	require.Empty(t, errs)

	result := ExecutePrepared(context.Background(), env.deps(nil), env.schema, env.execSchema, prepared)
	require.Empty(t, result.Errors)
	queryType := result.Data["__type"].(map[string]any)
	fields := queryType["fields"].([]any)
	for _, raw := range fields {
		name := raw.(map[string]any)["name"]
		require.NotEqual(t, "__schema", name)
		require.NotEqual(t, "__type", name)
	}
}

func TestBuildExecutableSchema_EmitsDescriptionsFromAST(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
"""
Hub note for a subsystem.
Collects design + runbooks for shared context.
"""
type Hub @node(paths: ["notes/hubs/*.md"]) {
  """Primary heading shown in the vault."""
  name: String!
}
`, map[string]string{
		"notes/hubs/cache.md": `---
type: Hub
name: Cache Hub
---

Shared cache docs.
`,
	})
	require.Equal(t, "Hub note for a subsystem.\nCollects design + runbooks for shared context.", env.execSchema.Schema.Types["Hub"].Description)
	require.Equal(t, "Primary heading shown in the vault.", requireSchemaField(t, env.execSchema, "Hub", "name", "String!").Description)
}

func TestBuildExecutableSchemaEmitsRelationCountsAndPreservesAuthoredCollision(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision")
  decisionsCount: String @field
  related: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision")
}
`, map[string]string{
		"notes/projects/a.md":    "---\ndecisionsCount: authored\n---\n",
		"notes/decisions/one.md": "# One\n",
	})

	require.Equal(t, "Count of the related relation.", requireSchemaField(t, env.execSchema, "Project", "relatedCount", "Int!").Description)
	requireSchemaField(t, env.execSchema, "Project", "decisionsCount", "String")
	prepared, errs := Prepare(env.execSchema, `{ __type(name: "Project") { fields { name } } }`)
	require.Empty(t, errs)
	result := ExecutePrepared(context.Background(), env.deps(nil), env.schema, env.execSchema, prepared)
	require.Empty(t, result.Errors)
	fieldNames := map[string]bool{}
	for _, raw := range result.Data["__type"].(map[string]any)["fields"].([]any) {
		fieldNames[raw.(map[string]any)["name"].(string)] = true
	}
	require.True(t, fieldNames["relatedCount"])
}

func TestBuildExecutableSchemaEmitsRelationCountsOnInterfaces(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Spec @node(paths: ["specs/*.md"]) {
  name: String
}

interface Portfolio {
  specs: [Spec!] @neighbors(direction: INBOUND, type: "Spec")
}

type FeatureArea implements Portfolio @node(paths: ["areas/*.md"]) {
  specs: [Spec!] @neighbors(direction: INBOUND, type: "Spec")
}
`, map[string]string{
		"areas/one.md": "# One\n",
		"specs/one.md": "# Spec\n",
	})
	require.Equal(t, ast.Interface, env.execSchema.Schema.Types["Portfolio"].Kind)
	for _, owner := range []string{"Portfolio", "FeatureArea"} {
		requireSchemaField(t, env.execSchema, owner, "specsCount", "Int!")
	}
}

func TestBuildExecutableSchema_EmitsEnumAndEnumValueDescriptions(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
"""Lifecycle for public API specs."""
enum SpecStatus {
  """Still being shaped."""
  PROPOSED
  """Ready for implementation."""
  ACTIVE
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  """Current lifecycle state."""
  status: SpecStatus!
}
`, map[string]string{
		"notes/specs/api.md": `---
status: PROPOSED
---
`,
	})

	status := env.execSchema.Schema.Types["SpecStatus"]
	require.Equal(t, "Lifecycle for public API specs.", status.Description)
	require.Equal(t, "Still being shaped.", status.EnumValues.ForName("PROPOSED").Description)
	require.Equal(t, "Ready for implementation.", status.EnumValues.ForName("ACTIVE").Description)
	require.Equal(t, "Current lifecycle state.", requireSchemaField(t, env.execSchema, "Spec", "status", "SpecStatus!").Description)
}

func TestBuildExecutableSchema_IncludesInterfacesAndSectionTypes(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Requirements

See [[decision-one]].
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: Decision summary
---
`,
	})

	require.Equal(t, ast.Interface, env.execSchema.Schema.Types["SummaryDoc"].Kind)
	require.Contains(t, env.execSchema.Schema.Types["RequirementsSection"].Interfaces, "Section")
	requireSchemaField(t, env.execSchema, "Spec", "requirements", "RequirementsSection!")
}

func TestBuildExecutableSchema_IncludesBuiltinsForEmbeddedSectionNodes(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
  related: Spec @link
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
aliases:
  - SPEC-ALIAS
---

## User Stories

### Story one

Status:: planned
`,
	})

	require.Contains(t, env.execSchema.Schema.Types["UserStory"].Interfaces, "Section")
	requireSchemaField(t, env.execSchema, "UserStory", "id", "ID!")
	requireSchemaField(t, env.execSchema, "UserStory", "notePath", "String!")
	requireSchemaField(t, env.execSchema, "UserStory", "locator", "NodeLocator!")
	children := requireSchemaField(t, env.execSchema, "UserStory", "children", "[Section!]!")
	require.Equal(t, "20", children.Arguments.ForName("first").DefaultValue.Raw)
}
