package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_IndexedRootNotePathFilterPreservesAuthoredExtension(t *testing.T) {
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
		"notes/other.md": `---
title: Other
---

- [ ] Skip me #action-item
`,
	})

	cases := []struct {
		name  string
		value string
		found bool
	}{
		{"bare", "notes/actions", false},
		{"with-md", "notes/actions.md", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared, errs := Prepare(env.execSchema, fmt.Sprintf(`{
  actionItem(filters: [{ field: "notePath", op: eq, value: %q }]) {
    title
    notePath
  }
}`, tc.value))
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			rows := result.Data["actionItem"].([]any)
			if !tc.found {
				require.Empty(t, rows)
				return
			}
			require.Len(t, rows, 1)
			require.Equal(t, "Update docs", rows[0].(map[string]any)["title"])
			require.Equal(t, "notes/actions.md", rows[0].(map[string]any)["notePath"])
		})
	}

	// `in` retains authored identity and does not invent a Markdown suffix.
	prepared, errs := Prepare(env.execSchema, `{
  actionItem(filters: [{ field: "path", op: in, values: ["notes/actions", "notes/other.md"] }]) {
    title
  }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 1)
}

func TestExecute_IndexedRecordsUsePropertyCaseFromEnclosingNoteType(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Sprint @node(paths: ["sprints/*.md"], propertyCase: SNAKE) {
  name: String!
}

type ActionItem implements Section
  @node(locator: EMBEDDED, propertyCase: KEBAB)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  followUp: String @field
}
`, map[string]string{
		"sprints/sprint-7.md": `---
type: Sprint
name: Sprint 7
---

- [ ] Update docs #action-item
  follow_up:: ping reviewer
`,
	})

	loaders, err := newLoaders(env.deps(nil), env.schema)
	require.NoError(t, err)
	exec := &executor{
		schema:         env.schema,
		deps:           env.deps(nil),
		loaders:        loaders,
		sectionsByPath: make(map[string][]*ontology.SectionNode),
	}

	rows, err := env.store.OntologyNodesByTypePlan(context.Background(), readmodel.NodeQueryPlan{TypeNames: []string{"ActionItem"}, Limit: 100})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	records, err := exec.sectionRecordsFromCatalogRows(context.Background(), rows)
	require.NoError(t, err)
	require.NotEmpty(t, records)
	for _, record := range records {
		require.Equal(t, ontoschema.PropertyCaseSnake, record.PropertyCase, "indexed record %q must inherit enclosing note type's case", record.Note.Path)
	}
}

func TestNormalizeNotePathPreservesMixedCaseAuthoredExtensions(t *testing.T) {
	t.Parallel()

	vaultPaths, err := paths.NewVaultPaths(t.TempDir())
	require.NoError(t, err)

	for _, input := range []string{"notes/Decision.MD", "notes/Reference.HTML"} {
		t.Run(input, func(t *testing.T) {
			got, err := normalizeNotePath(vaultPaths, input)
			require.NoError(t, err)
			require.Equal(t, input, got)
		})
	}
}

func TestExecute_IndexedEmbeddedRootBindsFilterAndSortVariables(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: String @field
}
`, map[string]string{
		"notes/team.md": `# Team

- [ ] Alpha #action-item
  due:: 2026-05-08
  assignee:: Maya
- [ ] Beta #action-item
  due:: 2026-05-12
  assignee:: Maya
- [ ] Gamma #action-item
  due:: 2026-05-10
  assignee:: Drew
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query AssignedDescending($assignee: String!, $direction: SortDirection!) {
  actionItem(
    filters: [{ field: "assignee", op: eq, value: $assignee }]
    sort: [{ field: "due", direction: $direction }]
  ) {
    title
    due
    done
    assignee
    notePath
    ref { kind typeName }
  }
}`, map[string]any{"assignee": "Maya", "direction": "desc"})
	require.Empty(t, errs)

	// Use the contentFailingQueryNoteReader so any markdown projection
	// surfaces as an error. The indexed plan satisfies the listed
	// selections without projection, so a clean response proves the
	// indexed path absorbed the variable-bound filter and sort.
	result := Execute(context.Background(), Deps{
		VaultDef:   obsidian.VaultDefinition{Path: env.root},
		NoteReader: contentFailingQueryNoteReader{inner: &obsidian.Note{}},
		Store:      env.store,
	}, env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "Beta", rows[0].(map[string]any)["title"])
	require.Equal(t, "2026-05-12", rows[0].(map[string]any)["due"])
	require.Equal(t, "Alpha", rows[1].(map[string]any)["title"])
	require.Equal(t, "2026-05-08", rows[1].(map[string]any)["due"])
	for _, row := range rows {
		item := row.(map[string]any)
		require.Equal(t, false, item["done"])
		require.Equal(t, "Maya", item["assignee"])
		require.Equal(t, "notes/team.md", item["notePath"])
		ref := item["ref"].(map[string]any)
		require.Equal(t, "EMBEDDED", ref["kind"])
		require.Equal(t, "ActionItem", ref["typeName"])
	}

	// queryPlan with the same variable-bound filter/sort should report the
	// indexed execution and pushed plan items.
	planQuery, errs := PrepareWithVariables(env.execSchema, `query Plan($assignee: String!, $direction: SortDirection!) {
  ontology {
    queryPlan(
      type: "ActionItem"
      filters: [{ field: "assignee", op: eq, value: $assignee }]
      sort: [{ field: "due", direction: $direction }]
      select: ["title", "due", "notePath"]
    ) {
      execution
      projection
      pushedFilters
      pushedSort
      residualFilters
      residualSort
    }
  }
}`, map[string]any{"assignee": "Maya", "direction": "desc"})
	require.Empty(t, errs)
	planResult := Execute(context.Background(), env.deps(nil), env.schema, planQuery)
	require.Empty(t, planResult.Errors)
	plan := planResult.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)
	require.Equal(t, "indexed", plan["execution"])
	require.Equal(t, "not_required", plan["projection"])
	require.Equal(t, []any{"assignee eq"}, plan["pushedFilters"])
	require.Equal(t, []any{"due desc"}, plan["pushedSort"])
	require.Empty(t, plan["residualFilters"])
	require.Empty(t, plan["residualSort"])
}

func TestExecute_IndexedEmbeddedLinkFilterNormalizesInputs(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
  aliases: [String!] @field
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
aliases:
  - A. Example
---
# Alice
`,
		"people/bob.md": `---
type: Person
name: Bob
aliases:
  - B. Example
---
# Bob
`,
		"notes/tasks.md": `# Tasks

- [ ] Alias task #action-item
  assignee:: Alice
- [ ] Wikilink task #action-item
  assignee:: [[people/alice]]
- [ ] Bob task #action-item
  assignee:: [[people/bob]]
`,
	})

	query := `query Assigned($assignee: String!) {
  actionItem(filters: [{ field: "assignee", op: eq, value: $assignee }]) {
    title
  }
}`
	cases := map[string][]string{
		"Alice":            {"Alias task", "Wikilink task"},
		"A. Example":       {"Alias task", "Wikilink task"},
		"B. Example":       {"Bob task"},
		"people/alice":     {"Alias task", "Wikilink task"},
		"people/alice.md":  {"Alias task", "Wikilink task"},
		"[[people/alice]]": {"Alias task", "Wikilink task"},
	}
	for assignee, wantTitles := range cases {
		t.Run(assignee, func(t *testing.T) {
			prepared, errs := PrepareWithVariables(env.execSchema, query, map[string]any{"assignee": assignee})
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			rows := result.Data["actionItem"].([]any)
			require.ElementsMatch(t, wantTitles, titles(rows))
		})
	}

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(filters: [{ field: "assignee", op: in, values: ["A. Example", "people/bob"] }]) {
    title
  }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.ElementsMatch(t, []string{"Alias task", "Wikilink task", "Bob task"}, titles(result.Data["actionItem"].([]any)))
}

func TestExecute_IndexedEmbeddedLinkFilterDoesNotWidenAmbiguousAliases(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
  aliases: [String!] @field
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
aliases:
  - Shared Alias
---
# Alice
`,
		"people/alicia.md": `---
type: Person
name: Alicia
aliases:
  - Shared Alias
---
# Alicia
`,
		"notes/tasks.md": `# Tasks

- [ ] Canonical task #action-item
  assignee:: [[people/alice]]
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query Assigned($assignee: String!) {
  actionItem(filters: [{ field: "assignee", op: eq, value: $assignee }]) {
    title
  }
}`, map[string]any{"assignee": "Shared Alias"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "resolved to multiple")
	require.Nil(t, result.Data["actionItem"], "ambiguous author-facing inputs must not widen to every possible target")

	planQuery, errs := PrepareWithVariables(env.execSchema, `query Plan($assignee: String!) {
  ontology {
    queryPlan(
      type: "ActionItem"
      filters: [{ field: "assignee", op: eq, value: $assignee }]
      select: ["title"]
    ) {
      warnings { code }
    }
  }
}`, map[string]any{"assignee": "Shared Alias"})
	require.Empty(t, errs)
	planResult := Execute(context.Background(), env.deps(nil), env.schema, planQuery)
	require.Empty(t, planResult.Errors)
	warnings := planResult.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)["warnings"].([]any)
	require.Contains(t, warningCodes(warnings), "link_filter_ambiguous")
}

func TestExecute_IndexedEmbeddedRootResidualSortLoadsFullCandidateSet(t *testing.T) {
	// Regression: when the sort key is not pushable into the field index
	// (e.g. title), residual sort runs on the candidate set returned by the
	// store. The planner used to leave plan.Limit at the requested `first`,
	// so the store returned only the catalog-order top-N and the residual
	// sort could only re-order that subset. Rows that ranked higher under
	// the residual sort but appeared later in catalog order were silently
	// dropped. The fix sets plan.Limit to the residual cap so the residual
	// sort sees the full bounded candidate set before truncation.
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`, map[string]string{
		// Catalog/document order is Zulu, Alpha, Yankee, Bravo. Title-sorted
		// asc is Alpha, Bravo, Yankee, Zulu. Top-2 by sort != top-2 by catalog.
		"notes/team.md": `# Team

- [ ] Zulu #action-item
- [ ] Alpha #action-item
- [ ] Yankee #action-item
- [ ] Bravo #action-item
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(sort: [{ field: "title", direction: asc }], first: 2) {
    title
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "Alpha", rows[0].(map[string]any)["title"])
	require.Equal(t, "Bravo", rows[1].(map[string]any)["title"])
}
