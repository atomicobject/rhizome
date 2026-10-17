package query

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_ItemBackedEmbeddedNodesNestedUnderSectionAndRoundTripRef(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}

type ActionItemsSection implements Section {
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
  actionItemsSection: ActionItemsSection @contains(level: H2, heading: "Action Items")
}
`, map[string]string{
		"meetings/sync.md": `---
type: Conversation
title: Sync
---

## Action Items

- [ ] Update docs #action-item
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  conversation(find: "Sync") {
    actionItemsSection {
      actionItems {
        title
        done
        ref { kind fragment }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	conversations := result.Data["conversation"].([]any)
	rows := conversations[0].(map[string]any)["actionItemsSection"].(map[string]any)["actionItems"].([]any)
	require.Len(t, rows, 1)
	row := rows[0].(map[string]any)
	require.Equal(t, false, row["done"])
	ref := row["ref"].(map[string]any)
	require.Equal(t, "EMBEDDED", ref["kind"])
	require.Contains(t, ref["fragment"], "item-")

	nodeRef := "meetings/sync.md#" + ref["fragment"].(string)
	prepared, errs = PrepareWithVariables(env.execSchema, `query($ref: String!) {
  node(ref: $ref) {
    nodeKind
    resolvedType
    ... on ActionItem {
      done
    }
  }
}`, map[string]any{"ref": nodeRef})
	require.Empty(t, errs)

	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	node := result.Data["node"].(map[string]any)
	require.Equal(t, "EMBEDDED", node["nodeKind"])
	require.Equal(t, "ActionItem", node["resolvedType"])
	require.Equal(t, false, node["done"])
}

// TestExecute_SectionScalarField_ReusedAcrossPropertyCases verifies that the
// same section type can be reused under note types with different property
// cases — the inline key is derived lazily from the enclosing note type.
func TestExecute_SectionScalarField_ReusedAcrossPropertyCases(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Story implements Section {
  storyStatus: String @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type SpecKebab @node(paths: ["notes/kebab/*.md"], propertyCase: KEBAB) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type SpecCamel @node(paths: ["notes/camel/*.md"], propertyCase: CAMEL) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/kebab/spec.md": `---
type: SpecKebab
summary: kebab summary
---

## Stories

### One

story-status:: kebab-value
`,
		"notes/camel/spec.md": `---
type: SpecCamel
summary: camel summary
---

## Stories

### Two

storyStatus:: camel-value
`,
	})

	query := func(path string) map[string]any {
		prepared, errs := Prepare(env.execSchema, `{
  note(path: "`+path+`") {
    ... on SpecKebab { stories { stories { title ... on Story { storyStatus } } } }
    ... on SpecCamel { stories { stories { title ... on Story { storyStatus } } } }
  }
}`)
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.Empty(t, result.Errors)
		return result.Data["note"].(map[string]any)
	}

	kebab := query("notes/kebab/spec.md")
	kebabStories := kebab["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, kebabStories, 1)
	require.Equal(t, "kebab-value", kebabStories[0].(map[string]any)["storyStatus"])

	camel := query("notes/camel/spec.md")
	camelStories := camel["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, camelStories, 1)
	require.Equal(t, "camel-value", camelStories[0].(map[string]any)["storyStatus"])
}

func TestExecute_EmbeddedNodeLinkField_ResolvesTypedTarget(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Effort @node(paths: ["notes/efforts/*.md"]) {
  summary: String!
}

type Story implements Section @node(locator: EMBEDDED) {
  status: String @field
  effort: Effort @link
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: spec summary
---

## Stories

### Story one

status:: ready
effort:: [[notes/efforts/effort.md]]
`,
		"notes/efforts/effort.md": `---
type: Effort
summary: effort summary
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        title
        ... on Story {
          status
          effort { path summary }
        }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "ready", story["status"])
	effort := story["effort"].(map[string]any)
	require.Equal(t, "notes/efforts/effort.md", effort["path"])
	require.Equal(t, "effort summary", effort["summary"])
}

func TestExecute_EmbeddedNodePackedInlineProperties(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum StoryStatus { ready draft }

type Story implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  summary: String! @field
  status: StoryStatus! @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: spec summary
---

## Stories

### Fleet-wide cart health

id:: SPEC-0013.US1 summary:: A manager can review aggregate fleet health and open a detail view of carts that need
attention. status:: ready
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        title
        ... on Story {
          summary
          status
        }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "A manager can review aggregate fleet health and open a detail view of carts that need attention.", story["summary"])
	require.Equal(t, "ready", story["status"])
}

func TestExecute_EmbeddedNodeAuthoredIDFieldOverridesSyntheticSectionID(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Story implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  status: String @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: spec summary
---

## Stories

### Story one

id:: SPEC-42.US1
status:: ready
`,
	})
	idCount := 0
	for _, field := range env.execSchema.Schema.Types["Story"].Fields {
		if field.Name == "id" {
			idCount++
			require.Equal(t, "ID!", field.Type.String())
		}
	}
	require.Equal(t, 1, idCount)

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        ... on Story { id status }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "SPEC-42.US1", story["id"])
	require.Equal(t, "ready", story["status"])
}

func TestExecute_SectionScalarField_RequiredInvalidEnumReturnsError(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum StoryStatus { PLANNED IN_PROGRESS COMPLETE }

type Story implements Section {
  status: StoryStatus! @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Stories

### Bad story

Status:: STARTED
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        title
        ... on Story { status }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, `field status value "STARTED" does not match StoryStatus`)

	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "Bad story", story["title"])
	_, hasStatus := story["status"]
	require.False(t, hasStatus)
}

func TestExecute_SectionScalarField_InvalidScalarDoesNotLeakRawString(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Story implements Section {
  points: Int @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Stories

### Bad story

Points:: not-a-number
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        title
        ... on Story { points }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "Bad story", story["title"])
	value, hasPoints := story["points"]
	require.True(t, hasPoints)
	require.Nil(t, value)
}
