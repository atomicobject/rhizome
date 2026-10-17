package query

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrepare_RejectsUnsupportedQueryForms(t *testing.T) {
	env := newQueryTestEnv(t)

	_, errs := Prepare(env.execSchema, "{ notes { nodes { path } } }")
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "requires at least one of")

	_, errs = Prepare(env.execSchema, "{ project { name } }")
	require.Empty(t, errs)

	_, errs = Prepare(env.execSchema, "{ ... on Query { project { name } } }")
	require.Empty(t, errs)
}

func TestPrepareWithVariables_ValidatesAndExecutesVariableArguments(t *testing.T) {
	env := newQueryTestEnv(t)

	prepared, errs := PrepareWithVariables(env.execSchema, `
query ProjectByPath($path: String!, $first: Int = 1, $neighborType: String!) {
  project(path: $path, first: $first) {
    path
    name
    decisions {
      name
    }
    linked(type: $neighborType, first: $first) {
      title
    }
  }
}
`, map[string]any{
		"path":         "notes/projects/roadmap-refresh.md",
		"neighborType": "Decision",
	})
	require.Empty(t, errs)
	require.Equal(t, map[string]any{
		"path":         "notes/projects/roadmap-refresh.md",
		"first":        int64(1),
		"neighborType": "Decision",
	}, prepared.Variables)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	project := rows[0].(map[string]any)
	require.Equal(t, "Roadmap Refresh", project["name"])
	require.Len(t, project["linked"].([]any), 1)
}

func TestPrepareWithVariables_AllowsRuntimeRootArguments(t *testing.T) {
	env := newQueryTestEnv(t)

	prepared, errs := PrepareWithVariables(env.execSchema, `
query RuntimeContext($type: String!, $path: String!, $first: Int = 2) {
  ontology {
    schemaHash
    authoringGuide(type: $type) { available warnings { code } }
    nextId(type: $type, count: 3, paths: ["specs/2026-08-05-14-32-a.md", "specs/2026-08-05-14-32-b.md"]) {
      available errorCode ids count last paths
      allocations { path id base disambiguator }
    }
  }
  code {
    docsForCode(path: $path, first: $first) { available warnings { code path } }
  }
}
`, map[string]any{
		"type": "Project",
		"path": "pkg/ontology/query/schema.go",
	})
	require.Empty(t, errs)
	require.NotNil(t, prepared)
}

func TestPrepareWithVariables_RejectsMissingAndUnboundedVariables(t *testing.T) {
	env := newQueryTestEnv(t)

	_, errs := PrepareWithVariables(env.execSchema, `query Test($path: String!) { note(path: $path) { path } }`, nil)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "must be defined")

	_, errs = PrepareWithVariables(env.execSchema, `query Test($path: String) { project(path: $path) { name } }`, nil)
	require.Empty(t, errs)
}

func TestPrepare_DetectsSemanticInsideRootInlineFragment(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  ... on Query {
    project(semantic: "roadmap") {
      name
    }
  }
}
`)
	require.Empty(t, errs)
	require.True(t, prepared.UsesSemantic)
}

func TestPrepareWithVariables_AllowsSemanticListAndDetectsSemantic(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := PrepareWithVariables(env.execSchema, `
query Survey($topics: [String!]) {
  project(semantic: $topics) { name }
}
`, map[string]any{"topics": []any{"roadmap", "planning"}})
	require.Empty(t, errs)
	require.True(t, prepared.UsesSemantic)

	_, errs = PrepareWithVariables(env.execSchema, `
query Survey($topics: [String!]) {
  project(semantic: $topics) { name }
}
`, map[string]any{"topics": []any{}})
	require.Empty(t, errs)
}

func TestPrepareWithVariables_PreservesSemanticStringVariableCompatibility(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := PrepareWithVariables(env.execSchema, `
query Survey($topic: String!) {
  semantic: project(semantic: $topic) { name score }
  exact: project(find: $topic) { name score }
}
`, map[string]any{"topic": "roadmap"})
	require.Empty(t, errs)
	require.True(t, prepared.UsesSemantic)

	result := Execute(context.Background(), env.deps(fakeSemanticSearcher{
		surveyResults: []semantic.Result{{SourceRef: search.SourceRef{NoteID: "notes/projects/roadmap-refresh.md"}, Score: 0.91}},
	}), env.schema, prepared)
	require.Empty(t, result.Errors)
	semanticRows := result.Data["semantic"].([]any)
	require.Len(t, semanticRows, 1)
	require.Equal(t, "Roadmap Refresh", semanticRows[0].(map[string]any)["name"])
	require.InDelta(t, 0.91, semanticRows[0].(map[string]any)["score"], 0.001)

	exactRows := result.Data["exact"].([]any)
	require.Len(t, exactRows, 1)
	require.Equal(t, "Roadmap Refresh", exactRows[0].(map[string]any)["name"])
	require.Nil(t, exactRows[0].(map[string]any)["score"])
}

func TestPrepare_SearchSetsUsesSearchNotSemantic(t *testing.T) {
	env := newQueryTestEnv(t)

	prepared, errs := Prepare(env.execSchema, `
{
  search(query: "roadmap") {
    nodes { title }
  }
}
`)
	require.Empty(t, errs)
	require.True(t, prepared.UsesSearch)
	require.False(t, prepared.UsesSemantic)

	_, errs = Prepare(env.execSchema, `
{
  search(query: "roadmap", mode: SEMANTIC) {
    nodes { title }
  }
}
`)
	require.NotEmpty(t, errs)
}

func TestPrepare_RejectsOversizedPublicFirstLimit(t *testing.T) {
	env := newQueryTestEnv(t)
	_, errs := Prepare(env.execSchema, `{ notes(type: "Project", first: 5001) { nodes { title } } }`)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "first must be <= 5000")
}

func TestPrepare_RejectsOversizedNestedFirstLimit(t *testing.T) {
	env := newQueryTestEnv(t)
	_, errs := Prepare(env.execSchema, `{ search(query: "roadmap", first: 201) { nodes { title } } }`)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "first must be <= 200")
}
