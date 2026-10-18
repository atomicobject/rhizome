package query

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_ListItemAcceptanceCriteriaUnderStory(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum UserStoryStatus { ready }

type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  acTitle: String @field(sourceKind: ITEM_TITLE)
  summary: String! @field(sourceKind: ITEM_SUMMARY)
  detail: String @field(sourceKind: ITEM_DETAIL)
  verification: String @field
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(shape: LIST_ITEM)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String! @field
  status: UserStoryStatus! @field
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria", required: true)
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
---

## User Stories

### US1 - Unified context

- summary:: Search code and notes together.
- status:: ready

#### Acceptance Criteria

- **Mixed result handles**: Search returns mixed code-anchor and doc-section results.
  Scenario: docs and code both match
  Given a task mentions a known symbol
  When search runs
  Then code anchors and doc sections both appear
  verification:: go test ./pkg/search

  > [!example]- Gherkin
  > Scenario: exact symbol beats prose
  > Given a query exactly matches a symbol
  > When results are ranked
  > Then the symbol appears first

  - Given nested context
  - Then nested bullets remain detail

- Payloads truncate with continuation support.
`,
	})
	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    userStories {
      stories {
        title
        summary
        status
        acceptanceCriteria {
          criteria {
            title
            acTitle
            summary
            detail
            verification
          }
        }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	stories := rows[0].(map[string]any)["userStories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	require.Equal(t, "US1 - Unified context", stories[0].(map[string]any)["title"])
	require.Equal(t, "Search code and notes together.", stories[0].(map[string]any)["summary"])
	criteria := stories[0].(map[string]any)["acceptanceCriteria"].(map[string]any)["criteria"].([]any)
	require.Len(t, criteria, 2)
	firstCriterion := criteria[0].(map[string]any)
	require.Equal(t, "Mixed result handles", firstCriterion["title"])
	require.Equal(t, "Mixed result handles", firstCriterion["acTitle"])
	require.Equal(t, "Search returns mixed code-anchor and doc-section results.", firstCriterion["summary"])
	require.Contains(t, firstCriterion["detail"], "Scenario: docs and code both match")
	require.Contains(t, firstCriterion["detail"], "> [!example]- Gherkin")
	require.Contains(t, firstCriterion["detail"], "- Given nested context")
	require.NotContains(t, firstCriterion["detail"], "verification::")
	require.Equal(t, "go test ./pkg/search", firstCriterion["verification"])
	secondCriterion := criteria[1].(map[string]any)
	require.Equal(t, "Payloads truncate with continuation support.", secondCriterion["title"])
	require.Nil(t, secondCriterion["acTitle"])
	require.Equal(t, "Payloads truncate with continuation support.", secondCriterion["summary"])
}

func TestExecute_ListSectionsPreserveOrderAndResolveMarkdownLinks(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type ReferenceSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  references: [ReferenceSection!] @contains(level: H2, heading: "Reference")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Reference

[First](../decisions/decision-one.md)

## Reference

[[decision-two]]
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

	prepared, errs := Prepare(env.execSchema, `
{
  notes(type: "SummaryDoc", find: "notes/specs/spec") {
    nodes {
      ... on SummaryDoc { summary }
      ... on Spec {
        references {
          content
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

	rows := result.Data["notes"].(map[string]any)["nodes"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "Spec summary", record["summary"])
	references := record["references"].([]any)
	require.Len(t, references, 2)
	require.Contains(t, references[0].(map[string]any)["content"], "[First]")
	require.Equal(t, []string{"decision-one"}, titles(references[0].(map[string]any)["decisions"].([]any)))
	require.Contains(t, references[1].(map[string]any)["content"], "[[decision-two]]")
	require.Equal(t, []string{"decision-two"}, titles(references[1].(map[string]any)["decisions"].([]any)))
}

// TestExecute_ListSectionWithoutHeadingMatchesDirectChildren verifies the
// new list-no-heading semantics: `[X!] @contains(level: H3)` picks up every
// direct child of the enclosing scope at that level, in document order.
func TestExecute_ListSectionWithoutHeadingMatchesDirectChildren(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum UserStoryStatus { PLANNED IN_PROGRESS COMPLETE }

type UserStory implements Section {
  status: UserStoryStatus @field
  owner: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## User Stories

### Resume after compaction

Status:: IN_PROGRESS
Owner:: drew

Body for first story.

### Schedule future indexing

Status:: PLANNED

Body for second story.

### Other thing

Status:: COMPLETE
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    userStories {
      stories {
        title
        ... on UserStory { status owner }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	userStories := record["userStories"].(map[string]any)
	stories := userStories["stories"].([]any)
	require.Len(t, stories, 3)

	first := stories[0].(map[string]any)
	require.Equal(t, "Resume after compaction", first["title"])
	require.Equal(t, "IN_PROGRESS", first["status"])
	require.Equal(t, "drew", first["owner"])

	second := stories[1].(map[string]any)
	require.Equal(t, "Schedule future indexing", second["title"])
	require.Equal(t, "PLANNED", second["status"])
	require.Nil(t, second["owner"])

	third := stories[2].(map[string]any)
	require.Equal(t, "Other thing", third["title"])
	require.Equal(t, "COMPLETE", third["status"])
}

func TestExecute_ListSectionWithoutHeadingUnwrapsSingleH1(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section {
  status: String @field
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  stories: [UserStory!] @contains(level: H2)
}
`, map[string]string{
		"notes/specs/spec.md": `# Spec title

## First story

Status:: in-progress

## Second story

Status:: planned
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      title
      ... on UserStory { status }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	stories := result.Data["spec"].([]any)[0].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 2)
	require.Equal(t, "First story", stories[0].(map[string]any)["title"])
	require.Equal(t, "Second story", stories[1].(map[string]any)["title"])
}

// TestExecute_ListSectionWithoutHeading_EmptyIsNotAnError verifies that a
// non-required list-no-heading field returns an empty list when the parent
// has no matching children, rather than raising an error.
func TestExecute_ListSectionWithoutHeading_EmptyIsNotAnError(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section {
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## User Stories

Nothing under here.
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    userStories { stories { title } }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["userStories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 0)
}
