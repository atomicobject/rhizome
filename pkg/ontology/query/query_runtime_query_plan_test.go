package query

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_OntologyRuntimeQueryPlanSupportsNoteRoots(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
---
# Alice
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    queryPlan(
      type: "Person"
      filters: [{ field: "name", op: eq, value: "Alice" }]
      select: ["title", "name"]
    ) {
      execution
      projection
      pushedFilters
      warnings { code }
    }
  }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	plan := result.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)
	require.Equal(t, "indexed", plan["execution"])
	require.Equal(t, "not_required", plan["projection"])
	require.Equal(t, []any{"name eq"}, plan["pushedFilters"])
	require.NotContains(t, warningCodes(plan["warnings"].([]any)), "non_embedded_root")
}

func TestExecute_OntologyRuntimeFieldCapabilitiesSplitIndexedAndResidualOps(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
  summary: String @field
}

type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
---
`,
		"notes/tasks.md": `# Tasks

- [ ] Canonical task #action-item
  assignee:: [[people/alice]]
  summary:: Follow up
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    type(name: "ActionItem") {
      fields {
        name
        capability {
          valueKind
          filterOps
          indexedFilterOps
          residualFilterOps
          sortable
          targetType
        }
      }
    }
  }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	fields := result.Data["ontology"].(map[string]any)["type"].(map[string]any)["fields"].([]any)
	summary := ontologyFieldByName(fields, "summary")
	summaryCap := summary["capability"].(map[string]any)
	require.ElementsMatch(t, []any{"eq", "in", "exists", "neq", "contains"}, summaryCap["filterOps"])
	require.ElementsMatch(t, []any{"eq", "in", "exists"}, summaryCap["indexedFilterOps"])
	require.ElementsMatch(t, []any{"neq", "contains"}, summaryCap["residualFilterOps"])
	require.True(t, summaryCap["sortable"].(bool))

	assignee := ontologyFieldByName(fields, "assignee")
	assigneeCap := assignee["capability"].(map[string]any)
	require.Equal(t, "Person", assigneeCap["targetType"])
	require.ElementsMatch(t, []any{"eq", "in", "exists"}, assigneeCap["indexedFilterOps"])
	require.Empty(t, assigneeCap["residualFilterOps"])
}

func TestExecute_OntologyRuntimeQueryPlanProjectionMatchesIndexedExecution(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`, map[string]string{
		"notes/actions.md": `---
title: Actions
---

- [ ] Update docs #action-item
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(first: 1) { title }
  ontology {
    typenamePlan: queryPlan(type: "ActionItem", select: ["__typename"]) {
      projection
    }
    titlePlan: queryPlan(type: "ActionItem", select: ["title"]) {
      projection
    }
  }
}`)
	require.Empty(t, errs)
	deps := env.deps(nil)
	deps.NoteReader = contentFailingQueryNoteReader{inner: &obsidian.Note{}}
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Len(t, result.Data["actionItem"].([]any), 1)
	require.Equal(t, "Update docs", result.Data["actionItem"].([]any)[0].(map[string]any)["title"])
	ontologyResult := result.Data["ontology"].(map[string]any)
	require.Equal(t, "not_required", ontologyResult["typenamePlan"].(map[string]any)["projection"])
	require.Equal(t, "not_required", ontologyResult["titlePlan"].(map[string]any)["projection"])
}

func TestExecute_OntologyRuntimeQueryPlanExplainsIndexedEmbeddedRoots(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: String @field
}
`, map[string]string{
		"notes/actions.md": `---
title: Actions
---

- [ ] Update docs #action-item
  due:: 2026-05-10
  assignee:: Gabe
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    queryPlan(
      type: "ActionItem"
      filters: [
        { field: "assignee", op: eq, value: "Gabe" }
        { field: "title", op: contains, value: "docs" }
      ]
      sort: [{ field: "due", direction: asc }]
      select: ["title", "due", "assignee"]
    ) {
      type
      root
      execution
      projection
      first
      pushedFilters
      residualFilters
      pushedSort
      residualSort
      warnings { code }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	plan := result.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)
	require.Equal(t, "ActionItem", plan["type"])
	require.Equal(t, "actionItem", plan["root"])
	require.Equal(t, "indexed_with_residual", plan["execution"])
	require.Equal(t, "not_required", plan["projection"])
	require.Equal(t, []any{"assignee eq"}, plan["pushedFilters"])
	require.Equal(t, []any{"title contains"}, plan["residualFilters"])
	require.Equal(t, []any{"due asc"}, plan["pushedSort"])
	require.Empty(t, plan["residualSort"])
	warnings := plan["warnings"].([]any)
	require.Len(t, warnings, 1)
	require.Equal(t, "residual_constraints", warnings[0].(map[string]any)["code"])
}
