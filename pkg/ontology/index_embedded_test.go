package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBuildIndex_SpecDrivenProductStoriesPackedInlinePropertiesValidate(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum SpecStatus { proposed active superseded archived }
enum UserStoryStatus { draft ready satisfied }

interface SpecLike {
  summary: String!
  id: String!
  specStatus: SpecStatus! @field(source: "spec-status")
}

type NarrativeSection implements Section {}

type RequirementsSection implements Section {}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(level: H5)
}

type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  verification: String @field
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  summary: String! @field
  status: UserStoryStatus! @field
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria", required: true)
}

type ProductSpec implements SpecLike @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
  id: String! @field(source: "id")
  specStatus: SpecStatus! @field(source: "spec-status")
  summarySection: NarrativeSection @contains(level: H2, heading: "Summary", required: true)
  goals: NarrativeSection @contains(level: H2, heading: "Goals", required: true)
  nonGoals: NarrativeSection @contains(level: H2, heading: "Non-Goals", required: true)
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories", required: true)
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)
	writeOntologyNote(t, root, "docs/specs/product/browser.md", `---
type: ProductSpec
summary: Product spec summary
id: SPEC-1000
spec-status: active
---

## Summary

Summary body.

## Goals

Goals body.

## Non-Goals

Non-goals body.

## User Stories

### Browse typed notes

id:: SPEC-1000.US1 summary:: Start from note families instead of guessing from the tree and keep the sentence
wrapped across lines. status:: ready
^spec-1000-us1

#### Acceptance Criteria

##### Shows type families
id:: SPEC-1000.US1.AC1
^spec-1000-us1-ac1

## Requirements

Requirements body.
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	specNode := requireOntologyNode(t, result.Nodes, "ProductSpec", "browser")
	storyNode := requireOntologyNode(t, result.Nodes, "UserStory", "Browse typed notes")
	criterionNode := requireOntologyNode(t, result.Nodes, "AcceptanceCriterion", "Shows type families")
	require.Equal(t, specNode.NodeID, storyNode.ParentNodeID)
	require.Equal(t, storyNode.NodeID, criterionNode.ParentNodeID)
}

func TestBuildIndex_SectionEmbeddedNodeBulletMetadataValidate(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum SpecStatus { active }
enum UserStoryStatus { ready }

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type AcceptanceCriteriaSection implements Section {}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  summary: String! @field
  status: UserStoryStatus! @field
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria", required: true)
}

type ProductSpec @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
  id: String! @field(source: "id")
  specStatus: SpecStatus! @field(source: "spec-status")
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories", required: true)
}
`)
	writeOntologyNote(t, root, "docs/specs/product/browser.md", `---
type: ProductSpec
summary: Product spec summary
id: SPEC-1000
spec-status: active
---

## User Stories

### US1 - Browse typed notes

- id:: ^SPEC-1000-US1
- summary:: Start from note families instead of guessing from the tree.
- status:: ready

#### Acceptance Criteria

Done.
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
}

func TestBuildIndex_SectionScopedMarkerlessListItemsWithItemFields(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum SpecStatus { active }
enum UserStoryStatus { ready }

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(shape: LIST_ITEM)
}

type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  acTitle: String @field(sourceKind: ITEM_TITLE)
  summary: String! @field(sourceKind: ITEM_SUMMARY)
  detail: String @field(sourceKind: ITEM_DETAIL)
  verification: String @field
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  summary: String! @field
  status: UserStoryStatus! @field
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria", required: true)
}

type ProductSpec @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
  id: String! @field(source: "id")
  specStatus: SpecStatus! @field(source: "spec-status")
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories", required: true)
}
`)
	writeOntologyNote(t, root, "docs/specs/product/browser.md", `---
type: ProductSpec
summary: Product spec summary
id: SPEC-1000
spec-status: active
---

## User Stories

### US1 - Browse typed notes

- id:: ^SPEC-1000-US1
- summary:: Start from note families instead of guessing from the tree.
- status:: ready

#### Acceptance Criteria

- **Ranking precedence**: Ranking prefers exact-symbol matches before broader similarity.
  Scenario: exact symbol beats semantic match
  Given a query exactly matches a symbol name
  When search results are ranked
  Then the exact-symbol result appears first
  verification:: go test ./pkg/search

  > [!example]- Gherkin
  > Scenario: exact path beats broad match
  > Given a query includes a file path
  > When search results are ranked
  > Then results from that file appear first

  - Given a nested-list setup
  - When the criterion has examples
  - Then nested bullets remain detail

- Result payloads stay compact and support continuation when truncated.
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)

	criteria := requireOntologyNodes(t, result.Nodes, "AcceptanceCriterion")
	require.Len(t, criteria, 2)
	require.Equal(t, "Ranking precedence", criteria[0].Title)
	require.Equal(t, "Result payloads stay compact and support continuation when truncated.", criteria[1].Title)

	fieldsByNode := map[string]map[string][]string{}
	for _, row := range result.NodeFieldValues {
		if row.TypeName != "AcceptanceCriterion" {
			continue
		}
		if fieldsByNode[row.NodeID] == nil {
			fieldsByNode[row.NodeID] = map[string][]string{}
		}
		fieldsByNode[row.NodeID][row.FieldName] = append(fieldsByNode[row.NodeID][row.FieldName], row.ValueText)
	}
	firstFields := fieldsByNode[criteria[0].NodeID]
	require.Equal(t, []string{"Ranking precedence"}, firstFields["actitle"])
	require.Equal(t, []string{"Ranking prefers exact-symbol matches before broader similarity."}, firstFields["summary"])
	require.Contains(t, firstFields["detail"][0], "Scenario: exact symbol beats semantic match")
	require.Contains(t, firstFields["detail"][0], "> [!example]- Gherkin")
	require.Contains(t, firstFields["detail"][0], "- Given a nested-list setup")
	require.NotContains(t, firstFields["detail"][0], "verification::")
	require.Equal(t, []string{"go test ./pkg/search"}, firstFields["verification"])
	secondFields := fieldsByNode[criteria[1].NodeID]
	require.Empty(t, secondFields["title"])
	require.Equal(t, []string{"Result payloads stay compact and support continuation when truncated."}, secondFields["summary"])
}

func TestBuildIndex_SpecDrivenProductStoriesUnderWrapperSectionValidate(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum SpecStatus { proposed active superseded archived }
enum UserStoryStatus { draft ready satisfied }

interface SpecLike {
  summary: String!
  id: String!
  specStatus: SpecStatus! @field(source: "spec-status")
}

type NarrativeSection implements Section {}

type RequirementsSection implements Section {}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(level: H5)
}

type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  id: ID! @field
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  summary: String! @field
  status: UserStoryStatus! @field
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria", required: true)
}

type ProductSpec implements SpecLike @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
  id: String! @field(source: "id")
  specStatus: SpecStatus! @field(source: "spec-status")
  summarySection: NarrativeSection @contains(level: H2, heading: "Summary", required: true)
  goals: NarrativeSection @contains(level: H2, heading: "Goals", required: true)
  nonGoals: NarrativeSection @contains(level: H2, heading: "Non-Goals", required: true)
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories", required: true)
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)
	writeOntologyNote(t, root, "docs/specs/product/browser.md", `---
type: ProductSpec
summary: Product spec summary
id: SPEC-1000
spec-status: active
---

## Summary

Summary body.

## Goals

Goals body.

## Non-Goals

Non-goals body.

## User Stories

### Browse typed notes

id:: SPEC-1000.US1
summary:: Start from note families instead of guessing from the tree.
status:: ready
^spec-1000-us1

#### Acceptance Criteria

##### Shows type families
id:: SPEC-1000.US1.AC1
^spec-1000-us1-ac1

### Search notes semantically

id:: SPEC-1000.US2
summary:: Discover the right note faster than path-only search.
status:: draft
^spec-1000-us2

#### Acceptance Criteria

##### Supports note-focused search
id:: SPEC-1000.US2.AC1
^spec-1000-us2-ac1

## Requirements

Requirements body.
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)

	for _, issue := range result.ValidationIssues {
		if issue.NotePath == "docs/specs/product/browser.md" {
			t.Fatalf("unexpected validation issue for wrapped product stories: %+v", issue)
		}
	}
	requireOntologyNode(t, result.Nodes, "AcceptanceCriterion", "Shows type families")
	requireOntologyNode(t, result.Nodes, "AcceptanceCriterion", "Supports note-focused search")
}
