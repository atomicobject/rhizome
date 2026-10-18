package query

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_AllowsLargePublicRootFirstLimit(t *testing.T) {
	notes := make(map[string]string, 250)
	for i := 1; i <= 250; i++ {
		notes[fmt.Sprintf("notes/projects/project-%03d.md", i)] = fmt.Sprintf(`---
type: Project
name: Project %03d
---
`, i)
	}
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}


`, notes)

	prepared, errs := Prepare(env.execSchema, `
{
  notes(type: "Project", first: 250) {
    pageInfo { requestedFirst returnedCount maxFirst truncated }
    nodes { title }
  }
  project(find: "Project", first: 250) {
    title
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	connection := result.Data["notes"].(map[string]any)
	pageInfo := connection["pageInfo"].(map[string]any)
	require.Equal(t, 250, pageInfo["requestedFirst"])
	require.Equal(t, 250, pageInfo["returnedCount"])
	require.Equal(t, publicRootFirstMax, pageInfo["maxFirst"])
	require.Equal(t, false, pageInfo["truncated"])
	require.Len(t, connection["nodes"].([]any), 250)
	require.Len(t, result.Data["project"].([]any), 250)
}

func TestExecute_TypedRootCoverageAndContinuation(t *testing.T) {
	notes := make(map[string]string, 101)
	for i := 0; i < 101; i++ {
		notes[fmt.Sprintf("notes/projects/project-%03d.md", i)] = fmt.Sprintf("---\ntype: Project\nname: Project %03d\n---\n", i)
	}
	env := newCustomQueryTestEnv(t, `type Project @node(paths: ["notes/projects/*.md"]) { name: String! }`, notes)
	prepared, errs := Prepare(env.execSchema, `{
	  empty: project(find: "absent", first: 100) { path }
	  below: project(first: 102) { path }
	  exact: project(first: 101) { path }
	  capped: project(first: 100) { path }
	  next: project(first: 100, offset: 100) { path }
	  pathPastEnd: project(path: "notes/projects/project-000.md", first: 1, offset: 1) { path }
	}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	coverage := result.Extensions["typedRoots"].(map[string]typedRootCoverage)
	for _, tc := range []struct {
		alias, status string
		count         int
		hasMore       bool
		hasNext       bool
	}{
		{alias: "empty", status: "complete", count: 0},
		{alias: "below", status: "complete", count: 101},
		{alias: "exact", status: "complete", count: 101},
		{alias: "capped", status: "capped", count: 100, hasMore: true, hasNext: true},
		{alias: "next", status: "complete", count: 1},
		{alias: "pathPastEnd", status: "complete", count: 0},
	} {
		require.Len(t, result.Data[tc.alias].([]any), tc.count, tc.alias)
		page := coverage[tc.alias]
		require.Equal(t, tc.status, page.Status, tc.alias)
		require.Equal(t, tc.count, page.ReturnedCount, tc.alias)
		require.NotNil(t, page.HasMore, tc.alias)
		require.Equal(t, tc.hasMore, *page.HasMore, tc.alias)
		if tc.hasNext {
			require.NotNil(t, page.NextOffset, tc.alias)
			require.Equal(t, 100, *page.NextOffset, tc.alias)
		} else {
			require.Nil(t, page.NextOffset, tc.alias)
		}
	}
	require.Equal(t, "notes/projects/project-100.md", result.Data["next"].([]any)[0].(map[string]any)["path"])
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"extensions":{"typedRoots":`)
	require.Contains(t, string(encoded), `"nextOffset":100`)

	_, errs = Prepare(env.execSchema, `{ project(first: 10, offset: -1) { path } }`)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "offset must be >= 0")
}

func TestExecute_TypedSelectorAppliesFiltersAndSortBeforePage(t *testing.T) {
	notes := make(map[string]string, 5)
	for i := 0; i < 5; i++ {
		notes[fmt.Sprintf("notes/projects/project-%03d.md", i)] = fmt.Sprintf("---\ntype: Project\nname: Project %03d\n---\n", i)
	}
	env := newCustomQueryTestEnv(t, `type Project @node(paths: ["notes/projects/*.md"]) { name: String @field }`, notes)
	prepared, errs := Prepare(env.execSchema, `{
	  later: project(find: "Project", first: 1, filters: [{field: "name", op: eq, value: "Project 004"}]) { path }
	  sorted: project(find: "Project", first: 1, sort: [{field: "name", direction: desc}]) { path }
	  second: project(find: "Project", first: 1, offset: 1, sort: [{field: "name", direction: desc}]) { path }
	}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, "notes/projects/project-004.md", result.Data["later"].([]any)[0].(map[string]any)["path"])
	require.Equal(t, "notes/projects/project-004.md", result.Data["sorted"].([]any)[0].(map[string]any)["path"])
	require.Equal(t, "notes/projects/project-003.md", result.Data["second"].([]any)[0].(map[string]any)["path"])
	coverage := result.Extensions["typedRoots"].(map[string]typedRootCoverage)
	require.Equal(t, "complete", coverage["later"].Status)
	require.Equal(t, "capped", coverage["sorted"].Status)
	require.Equal(t, 1, *coverage["sorted"].NextOffset)
	require.Equal(t, "capped", coverage["second"].Status)
	require.Equal(t, 2, *coverage["second"].NextOffset)
}

func TestExecute_InterfaceTypeFilterOverfetchesOnlyOnePageRecord(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  name: String!
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project implements SummaryDoc @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`, map[string]string{
		"notes/decisions/one.md": `---
type: Decision
name: Decision One
---
`,
		"notes/decisions/two.md": `---
type: Decision
name: Decision Two
---
`,
		"notes/projects/one.md": `---
type: Project
name: Project One
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ notes(type: "SummaryDoc", first: 1) { nodes { path title } } }`)
	require.Empty(t, errs)

	spy := &spyQueryStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy
	deps.ExactNoteMetadataRows = nil

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.LessOrEqual(t, spy.maxMetadataPathsBatch, 2)
	rows := result.Data["notes"].(map[string]any)["nodes"].([]any)
	require.Equal(t, "notes/decisions/one.md", rows[0].(map[string]any)["path"])
	require.Len(t, rows, 1)
}
