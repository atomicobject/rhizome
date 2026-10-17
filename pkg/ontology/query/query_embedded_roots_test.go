package query

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestExecute_ItemBackedEmbeddedNodesAsQueryRows(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: String @field
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}
`, map[string]string{
		"meetings/sync.md": `---
type: Conversation
title: Sync
---

## Action Items

- [ ] Update docs assignee:: Gabe #action-item
- [x] Already done assignee:: Maya #action-item ^done-1
- [ ] Ordinary checkbox
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  conversation(find: "Sync") {
    actionItems {
      title
      content
      done
      assignee
      notePath
      ref { kind nodeId fragment typeName }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	conversations := result.Data["conversation"].([]any)
	require.Len(t, conversations, 1)
	rows := conversations[0].(map[string]any)["actionItems"].([]any)
	require.Len(t, rows, 2)

	first := rows[0].(map[string]any)
	require.Equal(t, false, first["done"])
	require.Equal(t, "Gabe", first["assignee"])
	require.Contains(t, first["content"], "Update docs")
	firstRef := first["ref"].(map[string]any)
	require.Equal(t, "EMBEDDED", firstRef["kind"])
	require.Equal(t, "ActionItem", firstRef["typeName"])
	require.Contains(t, firstRef["fragment"], "item-")

	second := rows[1].(map[string]any)
	require.Equal(t, true, second["done"])
	require.Equal(t, "Maya", second["assignee"])
	secondRef := second["ref"].(map[string]any)
	require.Equal(t, "^done-1", secondRef["fragment"])
}

func TestExecute_EmbeddedRootFiltersSupportInAndComparisons(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: String @field
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
}
`, map[string]string{
		"meetings/sync.md": `---
type: Conversation
title: Sync
---

## Action Items

- [ ] First #action-item
  due:: 2026-05-08
  assignee:: Gabe
- [ ] Second #action-item
  due:: 2026-05-10
  assignee:: Maya
- [ ] Third #action-item
  due:: 2026-05-12
  assignee:: Drew
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(
    filters: [
      { field: "assignee", op: in, values: ["Maya", "Drew"] }
      { field: "due", op: gt, value: "2026-05-10" }
    ]
  ) {
    title
    due
    assignee
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "Third", rows[0].(map[string]any)["title"])

	sorted, errs := Prepare(env.execSchema, `{
  actionItem(
    filters: [
      { field: "done", op: eq, value: "false" }
      { field: "due", op: exists }
    ]
    sort: [{ field: "due", direction: desc }]
    first: 2
  ) {
    title
    due
    done
  }
}`)
	require.Empty(t, errs)
	sortedResult := Execute(context.Background(), env.deps(nil), env.schema, sorted)
	require.Empty(t, sortedResult.Errors)
	sortedRows := sortedResult.Data["actionItem"].([]any)
	require.Len(t, sortedRows, 2)
	require.Equal(t, "Third", sortedRows[0].(map[string]any)["title"])
	require.Equal(t, "Second", sortedRows[1].(map[string]any)["title"])

	bad, errs := Prepare(env.execSchema, `{
  actionItem(filters: [{ field: "due", op: startsWith, value: "2026" }]) {
    title
  }
}`)
	require.Nil(t, bad)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "startsWith")
}

func TestExecute_EmbeddedRootIndexedLinkEqualityAndTieBreaks(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: Person! @link
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
aliases:
  - Alice Example
---
`,
		"people/bob.md": `---
type: Person
name: Bob
---
`,
		"notes/b.md": `---
title: B
---

- [ ] Beta #action-item
  due:: 2026-05-10
  assignee:: [[people/alice]]
`,
		"notes/a.md": `---
title: A
---

- [ ] Alpha #action-item
  due:: 2026-05-10
  assignee:: [[people/alice]]
- [ ] Done #action-item
  due:: 2026-05-09
  assignee:: [[people/bob]]
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(
    filters: [
      { field: "assignee", op: in, values: ["people/alice"] }
      { field: "due", op: gte, value: "2026-05-10" }
      { field: "due", op: lte, value: "2026-05-10" }
    ]
    sort: [{ field: "due", direction: asc }, { field: "notePath", direction: desc }]
    first: 2
  ) {
    title
    due
    notePath
    assignee { path name }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "Beta", rows[0].(map[string]any)["title"])
	require.Equal(t, "notes/b.md", rows[0].(map[string]any)["notePath"])
	require.Equal(t, "people/alice.md", rows[0].(map[string]any)["assignee"].(map[string]any)["path"])
	require.Equal(t, "Alpha", rows[1].(map[string]any)["title"])
	require.Equal(t, "notes/a.md", rows[1].(map[string]any)["notePath"])
}

func TestExecute_GenericEmbeddedRootIndexedFieldsCoverTypedValues(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum RiskStatus {
  open
  mitigated
}

type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type RiskItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: LIST_ITEM, marker: "#risk") {
  status: RiskStatus @field
  active: Boolean @field
  due: Date @field
  owner: Person @link
  labels: [String!] @field
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
---
`,
		"people/bob.md": `---
type: Person
name: Bob
---
`,
		"notes/risks.md": `# Risks

- Cache invalidation #risk
  status:: open
  active:: true
  due:: 2026-05-08
  owner:: [[people/alice]]
  labels:: api
  labels:: indexed
- Archive cleanup #risk
  status:: mitigated
  active:: false
  due:: 2026-05-06
  owner:: [[people/bob]]
  labels:: storage
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  riskItem(
    filters: [
      { field: "status", op: eq, value: "open" }
      { field: "active", op: eq, value: "true" }
      { field: "owner", op: eq, value: "people/alice" }
      { field: "labels", op: in, values: ["indexed"] }
    ]
    sort: [{ field: "due", direction: asc }]
  ) {
    title
    status
    active
    due
    labels
    owner { path name }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["riskItem"].([]any)
	require.Len(t, rows, 1)
	row := rows[0].(map[string]any)
	require.Equal(t, "Cache invalidation", row["title"])
	require.Equal(t, "open", row["status"])
	require.Equal(t, true, row["active"])
	require.Equal(t, "2026-05-08", row["due"])
	require.Equal(t, []any{"api", "indexed"}, row["labels"])
	require.Equal(t, "people/alice.md", row["owner"].(map[string]any)["path"])
}

func TestExecute_OrdinaryNoteActionItemsUseIndexedSummaryWithoutProjection(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: Person @link
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
---
`,
		"people/bob.md": `---
type: Person
name: Bob
---
`,
		"notes/team.md": `# Team notes

- [ ] Alice later #action-item
  due:: 2026-05-12
  assignee:: [[people/alice]]
- [ ] Bob task #action-item
  due:: 2026-05-01
  assignee:: [[people/bob]]
`,
		"notes/personal.md": `# Personal

- [ ] Alice earlier #action-item
  due:: 2026-05-08
  assignee:: [[people/alice]]
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(
    filters: [
      { field: "done", op: eq, value: "false" }
      { field: "assignee", op: eq, value: "people/alice" }
    ]
    sort: [{ field: "due", direction: asc }]
  ) {
    title
    done
    due
    notePath
    ref { kind typeName }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), Deps{
		VaultDef:   obsidian.VaultDefinition{Path: env.root},
		NoteReader: contentFailingQueryNoteReader{inner: &obsidian.Note{}},
		Store:      env.store,
	}, env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "Alice earlier", rows[0].(map[string]any)["title"])
	require.Equal(t, "2026-05-08", rows[0].(map[string]any)["due"])
	require.Equal(t, "Alice later", rows[1].(map[string]any)["title"])
	require.Equal(t, "2026-05-12", rows[1].(map[string]any)["due"])
}

func TestExecute_EmbeddedRootIndexedLevelSelectionForcesProjection(t *testing.T) {
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
  actionItem(filters: [{ field: "assignee", op: eq, value: "Gabe" }]) {
    title
    level
  }
}`)
	require.Empty(t, errs)

	// contentFailingQueryNoteReader fails any markdown projection. Selecting
	// `level` must force projection because indexed catalog rows do not carry
	// the section heading depth; the projection error proves the indexed path
	// did not silently return level="".
	result := Execute(context.Background(), Deps{
		VaultDef:   obsidian.VaultDefinition{Path: env.root},
		NoteReader: contentFailingQueryNoteReader{inner: &obsidian.Note{}},
		Store:      env.store,
	}, env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	containsProjection := false
	for _, err := range result.Errors {
		if strings.Contains(err.Message, "unexpected markdown projection") {
			containsProjection = true
			break
		}
	}
	require.True(t, containsProjection, "expected projection attempt; errors=%v", result.Errors)

	planQuery, errs := Prepare(env.execSchema, `{
  ontology {
    queryPlan(
      type: "ActionItem"
      select: ["level"]
    ) {
      projection
    }
  }
}`)
	require.Empty(t, errs)
	planResult := Execute(context.Background(), env.deps(nil), env.schema, planQuery)
	require.Empty(t, planResult.Errors)
	plan := planResult.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)
	require.Equal(t, "required", plan["projection"])
}
