package query

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_SectionNeighborsStayInsideSubtree(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

See [[outside-decision]].

## Requirements

See [[inside-decision]].
` + "\n```md\n[[fenced-decision]]\n```\n\nInline example `[[inline-decision]]`.\n",
		"notes/decisions/inside-decision.md": `---
type: Decision
summary: Inside
---
`,
		"notes/decisions/outside-decision.md": `---
type: Decision
summary: Outside
---
`,
		"notes/decisions/fenced-decision.md": "---\ntype: Decision\nsummary: Fenced\n---\n",
		"notes/decisions/inline-decision.md": "---\ntype: Decision\nsummary: Inline\n---\n",
	})

	prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { requirements { decisions { title } } } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	requirements := record["requirements"].(map[string]any)
	require.Equal(t, []string{"inside-decision"}, titles(requirements["decisions"].([]any)))
}

func TestExecute_SectionInboundNeighborsResolveAnchoredBacklinks(t *testing.T) {
	for _, tc := range []struct {
		name, heading, block, target string
	}{
		{"block ID", "Alpha", "^US-001\n", "spec#^US-001"},
		{"heading", "Alpha Story", "", "spec#Alpha Story"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCustomQueryTestEnv(t, `
type Effort @node(paths: ["notes/efforts/*.md"]) {
  summary: String!
}

type StorySection implements Section {
  efforts: [Effort!] @neighbors(direction: INBOUND, type: "Effort", scope: SUBTREE)
}

type StoriesSection implements Section {
  stories: [StorySection!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories", required: true)
}
`, map[string]string{
				"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Stories

### ` + tc.heading + `

` + tc.block + `

Alpha body.
`,
				"notes/efforts/effort.md": `---
type: Effort
summary: Effort summary
---

Targets [[` + tc.target + `]].
`,
			})

			prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { stories { stories { efforts { title } } } } }`)
			require.Empty(t, errs)

			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			rows := result.Data["spec"].([]any)
			require.Len(t, rows, 1)
			record := rows[0].(map[string]any)
			storiesSection := record["stories"].(map[string]any)
			stories := storiesSection["stories"].([]any)
			require.Len(t, stories, 1)
			story := stories[0].(map[string]any)
			require.Equal(t, []string{"effort"}, titles(story["efforts"].([]any)))
		})
	}
}

func TestExecute_SectionInboundNeighborsResolveAuthoredStoryLinks(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type EffortNote @node(paths: ["notes/efforts/*.md"]) {
  summary: String!
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
  efforts: [EffortNote!] @neighbors(direction: INBOUND, type: "EffortNote", scope: SUBTREE)
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## User Stories

### Alpha

id:: SPEC-1234.US1
status:: ready
^spec-1234-us1
`,
		"notes/efforts/effort.md": `---
type: EffortNote
summary: Effort summary
---

## Stories In Scope (Frozen)

- [[notes/specs/spec#^spec-1234-us1]]
`,
	})

	prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { userStories { stories { efforts { title } } } } }`)
	require.Empty(t, errs)

	edges, err := env.store.OntologyAmbientEdgesByTargets(context.Background(), []string{"notes/specs/spec.md"}, "", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	storiesSection := record["userStories"].(map[string]any)
	stories := storiesSection["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, []string{"effort"}, titles(story["efforts"].([]any)))
}

func TestExecute_ResolvesNestedTypedSubsections(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type DetailsSection implements Section {
}

type RequirementsSection implements Section {
  details: [DetailsSection!] @contains(level: H3, heading: "Details")
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Requirements

Top.

### Details

First details.

### Details

Second details.
`,
	})

	prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { requirements { details { title content } } } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	requirements := record["requirements"].(map[string]any)
	details := requirements["details"].([]any)
	require.Len(t, details, 2)
	require.Contains(t, details[0].(map[string]any)["content"], "First details.")
	require.Contains(t, details[1].(map[string]any)["content"], "Second details.")
}

func TestExecute_ChildrenPreserveConcreteSectionTypes(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type DetailSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type RequirementsSection implements Section {
  details: DetailSection @contains(level: H3, heading: "Details")
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md":             "---\ntype: Spec\nsummary: Spec summary\n---\n\n## Requirements\n\nTop.\n\n### Details\n\nSee [[decision-one]].\n\n### Misc\n\nUnmatched child.\n",
		"notes/decisions/decision-one.md": "---\ntype: Decision\nsummary: Decision summary\n---\n",
	})

	prepared, errs := Prepare(env.execSchema, `
{
  spec(path: "notes/specs/spec.md") {
    requirements {
      children {
        title
        ... on DetailSection {
          decisions { title }
        }
      }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	requirements := record["requirements"].(map[string]any)
	children := requirements["children"].([]any)
	require.Len(t, children, 2)
	details := children[0].(map[string]any)
	require.Equal(t, "Details", details["title"])
	require.Equal(t, []string{"decision-one"}, titles(details["decisions"].([]any)))

	misc := children[1].(map[string]any)
	require.Equal(t, "Misc", misc["title"])
	_, ok := misc["decisions"]
	require.False(t, ok)
}

func TestExecute_ChildrenFallBackToSectionWhenMatchIsAmbiguous(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type DetailSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type AlternateDetailSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type RequirementsSection implements Section {
  details: DetailSection @contains(level: H3, heading: "Details")
  alternateDetails: AlternateDetailSection @contains(level: H3, heading: "Details")
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md":             "---\ntype: Spec\nsummary: Spec summary\n---\n\n## Requirements\n\n### Details\n\nSee [[decision-one]].\n",
		"notes/decisions/decision-one.md": "---\ntype: Decision\nsummary: Decision summary\n---\n",
	})

	prepared, errs := Prepare(env.execSchema, `
{
  spec(path: "notes/specs/spec.md") {
    requirements {
      children {
        title
        ... on DetailSection {
          decisions { title }
        }
      }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	requirements := record["requirements"].(map[string]any)
	children := requirements["children"].([]any)
	require.Len(t, children, 1)
	child := children[0].(map[string]any)
	require.Equal(t, "Details", child["title"])
	_, ok := child["decisions"]
	require.False(t, ok)
}

func TestExecute_SingularSectionDuplicateReturnsError(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
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

## Requirements

See [[decision-two]].
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: One
---
`,
		"notes/decisions/decision-two.md": `---
type: Decision
summary: Two
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { summary requirements { title } } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "must match a single heading")

	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "Spec summary", record["summary"])
	_, ok := record["requirements"]
	require.False(t, ok)
}
