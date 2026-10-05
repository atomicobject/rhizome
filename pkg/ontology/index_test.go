package ontology

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBuildIndex_BuildsStructuralAndAmbientEdges(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Team @node(paths: ["teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["people/*.md"]) {
  name: String!
  team: Team @link(inverse: "members")
  manager: Person @link(inverse: "reports")
  reports: [Person!] @link(inverse: "manager")
}
`)
	writeOntologyNote(t, root, "teams/Eng.md", `---
type: Team
name: Eng
members:
  - people/Alice.md
  - people/Bob.md
  - people/Carol.md
---
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
team: teams/Eng.md
reports:
  - people/Bob.md
---

Works closely with [[Carol]].
`)
	writeOntologyNote(t, root, "people/Bob.md", `---
type: Person
name: Bob
team: teams/Eng.md
manager: people/Alice.md
---
`)
	writeOntologyNote(t, root, "people/Carol.md", `---
type: Person
name: Carol
team: teams/Eng.md
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)

	require.Len(t, result.NoteTypes, 4)
	requireEdge(t, result.Edges, "people/Alice.md", "team", "teams/Eng.md", "field", true)
	requireEdge(t, result.Edges, "people/Bob.md", "manager", "people/Alice.md", "field", true)
	requireEdge(t, result.Edges, "people/Alice.md", "related", "people/Carol.md", "body_link", false)
	requireEdge(t, result.Edges, "people/Carol.md", "related", "people/Alice.md", "backlink", false)
}

func TestBuildAmbientEdges_UsesProviderResolvedLinksWithoutReparsingContent(t *testing.T) {
	edges := buildAmbientEdges(nil, map[string]*noteDoc{
		"notes/source.md": {
			Path:     "notes/source.md",
			TypeName: "Source",
			Content:  "This content has no link syntax.",
			Links: []notemeta.ResolvedNoteLink{{
				SourcePath:  paths.NotePath("notes/source.md"),
				TargetPath:  paths.NotePath("notes/target.md"),
				TargetInput: "target",
				Kind:        "wikilink",
			}},
		},
		"notes/target.md": {Path: "notes/target.md", TypeName: "Target"},
	}, &Schema{Hash: "test"}, nil)

	requireEdge(t, edges, "notes/source.md", "related", "notes/target.md", "body_link", false)
	requireEdge(t, edges, "notes/target.md", "related", "notes/source.md", "backlink", false)
}

func TestBuildIndex_EmitsNodeScopedEdgesForEmbeddedLinkFields(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
}

type UserStory implements Section @node(locator: EMBEDDED) {
  owner: Person @link
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: UserStoriesSection @contains(level: H2, heading: "Stories")
}
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
---
`)
	writeOntologyNote(t, root, "specs/demo.md", `---
type: Spec
summary: Demo
---

# Demo

## Stories

### Checkout

owner:: [[people/Alice]]
^checkout
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)

	var found codeanchorsqlite.OntologyEdgeRow
	for _, edge := range result.Edges {
		if edge.SrcPath == "specs/demo.md" && edge.RelationName == "owner" && edge.DstPath == "people/Alice.md" {
			found = edge
			break
		}
	}
	require.NotEmpty(t, found.SrcNodeID)
	require.Empty(t, found.DstNodeID)
	require.True(t, found.Structural)
}

func TestBuildIndex_EmitsNodeScopedEdgesForGlobalSourceLinkFields(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
}

type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
---
`)
	writeOntologyNote(t, root, "notes/scratch.md", `# Scratch

- [ ] Call Alice #action-item
  assignee:: [[people/Alice]]
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)

	var found codeanchorsqlite.OntologyEdgeRow
	for _, edge := range result.Edges {
		if edge.SrcPath == "notes/scratch.md" && edge.RelationName == "assignee" && edge.DstPath == "people/Alice.md" {
			found = edge
			break
		}
	}
	require.NotEmpty(t, found.SrcNodeID)
	require.True(t, found.Structural)
	require.Equal(t, "field", found.Provenance)
}

func TestBuildIndex_EmitsSectionScopedFrozenSpecEdges(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
interface SpecLike {
  summary: String!
}

type FrozenSpecSetSection implements Section {
  frozenSpecs: [SpecLike!] @neighbors(direction: OUTBOUND, type: "SpecLike", scope: SUBTREE)
}

type EffortNote @node(paths: ["efforts/*.md"]) {
  summary: String!
  specSetFrozen: FrozenSpecSetSection @contains(level: H2, heading: "Spec Set (Frozen)", required: true)
}

type ProcessSpec implements SpecLike @node(paths: ["specs/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "specs/graph.md", `---
type: ProcessSpec
summary: Graph spec
---
`)
	writeOntologyNote(t, root, "efforts/graph.md", `---
type: EffortNote
summary: Graph effort
---

# Graph effort

## Spec Set (Frozen)

- [[specs/graph]] — SPEC-0001
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)

	var found codeanchorsqlite.OntologyEdgeRow
	for _, edge := range result.Edges {
		if edge.SrcPath == "efforts/graph.md" && edge.RelationName == "frozenSpecs" && edge.DstPath == "specs/graph.md" {
			found = edge
			break
		}
	}
	require.NotEmpty(t, found.SrcNodeID)
	require.Empty(t, found.DstNodeID)
	require.Equal(t, "section_neighbor", found.Provenance)
	require.True(t, found.Structural)
}

func TestBuildIndex_ReportsValidationIssues(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
  manager: Person @link(inverse: "reports")
  reports: [Person!] @link(inverse: "manager")
}
`)
	writeOntologyNote(t, root, "misc/Loose.md", `---
type: Person
manager: people/Missing.md
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidationIssues)
	requireIssueCode(t, result.ValidationIssues, "declared_type_mismatch")
	require.Empty(t, result.NoteTypes)
	require.Empty(t, result.Edges)
}

// Broken links are reported once, by the broken-links check. The ontology
// projection reports schema problems only.
func TestBuildIndex_LeavesBrokenLinksToBrokenLinksCheck(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/projects/roadmap.md", `---
type: Project
name: Roadmap
---

Decision lives in [[missing-decision]].
`)
	writeOntologyNote(t, root, "docs/questions.md", `# Questions

[Missing meeting](../meetings/missing-sync.md)
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireNoteType(t, result.NoteTypes, "notes/projects/roadmap.md", "Project")
	for _, issue := range result.ValidationIssues {
		require.NotEqual(t, "broken_note_link", issue.Code, "%+v", issue)
	}
}

func TestBuildIndex_ReportsGenericConditionalValidationIssues(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum SpecStatus { proposed active superseded archived }
enum EffortStatus { planned active blocked complete archived }
enum AuditStatus { pending complete }
enum BackportStatus { pending complete }

interface SpecLike {
  summary: String!
  specStatus: SpecStatus! @field(source: "spec-status")
  successor: SpecLike @link
}

type ProductSpec
  implements SpecLike
  @node(paths: ["docs/specs/product/*.md"])
  @requiresWhen(field: "specStatus", equals: "superseded", require: [{ field: "successor" }]) {
  summary: String!
  specStatus: SpecStatus! @field(source: "spec-status")
  successor: SpecLike @link
}

type EffortNote
  @node(paths: ["docs/efforts/*.md"])
  @requiresWhen(
    field: "status",
    equals: "complete",
    require: [
      { field: "auditStatus", equals: "complete" },
      { field: "backportStatus", equals: "complete" }
    ]
  ) {
  summary: String!
  status: EffortStatus!
  auditStatus: AuditStatus! @field(source: "audit-status")
  backportStatus: BackportStatus! @field(source: "backport-status")
}
`)
	writeOntologyNote(t, root, "docs/specs/product/new.md", `---
type: ProductSpec
spec-status: active
summary: New spec
---
`)
	writeOntologyNote(t, root, "docs/specs/product/old.md", `---
type: ProductSpec
spec-status: superseded
summary: Old spec
---
`)
	writeOntologyNote(t, root, "docs/efforts/effort.md", `---
type: EffortNote
summary: Finish the slice
status: complete
audit-status: pending
backport-status: complete
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireIssueCode(t, result.ValidationIssues, "conditional_required_field_missing")
	requireIssueCode(t, result.ValidationIssues, "conditional_required_value_mismatch")

	specIssue := requireIssue(t, result.ValidationIssues, "conditional_required_field_missing", "docs/specs/product/old.md")
	require.Equal(t, "successor", specIssue.FieldName)

	effortIssue := requireIssue(t, result.ValidationIssues, "conditional_required_value_mismatch", "docs/efforts/effort.md")
	require.Equal(t, "auditStatus", effortIssue.FieldName)
}

func TestBuildIndex_TreatsLegacyHubPathAsDocumentationHub(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type DocumentationHub @node(paths: ["docs/**/README.md", "docs/hubs/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "docs/hubs/Search (Hub).md", `---
type: DocumentationHub
summary: Search navigation hub
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	requireNoteType(t, result.NoteTypes, "docs/hubs/Search (Hub).md", "DocumentationHub")
}

func TestBuildIndex_ValidatesRequiredSections(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)
	writeOntologyNote(t, root, "notes/specs/good.md", `---
type: Spec
name: Good
---

## Requirements

See [[decision]].
`)
	writeOntologyNote(t, root, "notes/specs/bad.md", `---
type: Spec
name: Bad
---

### Requirements
`)
	writeOntologyNote(t, root, "notes/specs/empty.md", `---
type: Spec
name: Empty
---

## Requirements
`)
	writeOntologyNote(t, root, "notes/decisions/decision.md", `---
type: Decision
name: Decision
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireIssueCode(t, result.ValidationIssues, "wrong_section_level")
	requireIssueCode(t, result.ValidationIssues, "empty_required_section")
}

func TestBuildIndex_ReportsDuplicateSingularSections(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type RequirementsSection implements Section {
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

First.

## Requirements

Second.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireIssueCode(t, result.ValidationIssues, "duplicate_section")
}

func TestBuildIndex_ValidatesRequiredNestedSections(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type DetailsSection implements Section {
}

type RequirementsSection implements Section {
  details: DetailsSection @contains(level: H3, heading: "Details", required: true)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

Summary only.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	issue := requireIssue(t, result.ValidationIssues, "missing_required_section", "notes/specs/spec.md")
	require.Equal(t, "requirements.details", issue.FieldName)
	require.Contains(t, issue.Message, "requirements.details")
}

func TestBuildIndex_ValidatesSectionScalarFields(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum StoryStatus { PLANNED IN_PROGRESS COMPLETE }

type Story implements Section {
  status: StoryStatus! @field
  owner: String @field
}

type UserStoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## User Stories

### Missing status
Owner:: drew

Body.

### Bad status
status:: nonsense
Owner:: drew

Body.

### Good story
status:: IN_PROGRESS
Owner:: drew

Body.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)

	missing := requireIssue(t, result.ValidationIssues, "missing_required_field", "notes/specs/spec.md")
	require.Equal(t, "userStories.stories.status", missing.FieldName)
	require.Contains(t, missing.Message, "userStories.stories.status")

	invalid := requireIssue(t, result.ValidationIssues, "field_type_mismatch", "notes/specs/spec.md")
	require.Equal(t, "userStories.stories.status", invalid.FieldName)
	require.Contains(t, invalid.Message, "nonsense")
	require.Contains(t, invalid.Message, "StoryStatus")
}

func TestBuildIndex_SectionScalarSingleton_RejectsMultipleValues(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Story implements Section {
  owner: String @field
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String!
  stories: [Story!] @contains(level: H3)
}
`)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

### Story 1
owner:: drew
owner:: sam
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)

	issue := requireIssue(t, result.ValidationIssues, "field_shape_mismatch", "notes/specs/spec.md")
	require.Equal(t, "stories.owner", issue.FieldName)
}

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

	schema, err := LoadSchema(root)
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

	schema, err := LoadSchema(root)
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

	schema, err := LoadSchema(root)
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

func TestBuildIndex_GenericAuthoringValidationAnnotations(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum SpecStatus { active }
enum UserStoryStatus { ready }

type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  summary: String! @field(sourceKind: ITEM_SUMMARY) @format(notPattern: "^$")
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(shape: LIST_ITEM, min: 1)
}

type UserStory
  implements Section
  @node(locator: EMBEDDED)
  @title(pattern: "^US[0-9]+ - .+", notPattern: "(?i)^As an?\\b") {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US", populate: ON_CREATE) @authoring(style: LIST_METADATA)
  summary: String! @field @authoring(style: LIST_METADATA)
  status: UserStoryStatus! @field @authoring(style: LIST_METADATA)
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria", required: true)
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3, min: 1)
}

type ProductSpec @node(paths: ["docs/specs/product/*.md"]) {
  summary: String!
  id: String! @field(source: "id") @format(pattern: "^SPEC-[0-9]{4}$")
  specStatus: SpecStatus! @field(source: "spec-status")
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories", required: true)
}
`)
	writeOntologyNote(t, root, "docs/specs/product/good.md", `---
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

- Search returns typed note families.
`)
	writeOntologyNote(t, root, "docs/specs/product/bad.md", `---
type: ProductSpec
summary: Product spec summary
id: bad
spec-status: active
---

## User Stories

### As an implementer starting a task

id:: ^SPEC-1000-US1
summary:: Start from note families instead of guessing from the tree.
status:: ready

#### Acceptance Criteria
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, validationIssuesForPath(result.ValidationIssues, "docs/specs/product/good.md"))

	badIssues := validationIssuesForPath(result.ValidationIssues, "docs/specs/product/bad.md")
	requireIssueCode(t, badIssues, "field_format_mismatch")
	requireIssueCode(t, badIssues, "title_forbidden_pattern")
	requireIssueCode(t, badIssues, "field_authoring_style_mismatch")
	requireIssueCode(t, badIssues, "contains_min_not_met")
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

	schema, err := LoadSchema(root)
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

func TestBuildIndex_SectionTypeReusedAcrossNoteTypesWithDifferentPropertyCase(t *testing.T) {
	// The same section type is referenced from two note types whose
	// propertyCase differs. Each resolution should pick up the inline key
	// using the enclosing note type's propertyCase.
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Story implements Section {
  ownerName: String! @field
}

type SpecKebab @node(paths: ["notes/kebab/*.md"]) {
  name: String!
  stories: [Story!] @contains(level: H3)
}

type SpecCamel @node(paths: ["notes/camel/*.md"], propertyCase: CAMEL) {
  name: String!
  stories: [Story!] @contains(level: H3)
}
`)
	writeOntologyNote(t, root, "notes/kebab/spec.md", `---
type: SpecKebab
name: Kebab
---

### Story 1
owner-name:: drew
`)
	writeOntologyNote(t, root, "notes/camel/spec.md", `---
type: SpecCamel
name: Camel
---

### Story 1
ownerName:: drew
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)

	for _, issue := range result.ValidationIssues {
		if issue.Code == "missing_required_field" && issue.FieldName == "stories.ownerName" {
			t.Fatalf("unexpected missing_required_field for %s (%s)", issue.NotePath, issue.Message)
		}
	}
}

func TestBuildIndex_UsesConfiguredSourceAliases(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/projects/*.md"]) {
  openQuestions: [OpenQuestion!] @link(sources: ["open-questions", "openQuestions"], inverse: "project")
}

type OpenQuestion @node(paths: ["notes/questions/*.md"]) {
  project: Project! @link(inverse: "openQuestions")
}
`)
	writeOntologyNote(t, root, "notes/projects/atlas.md", `---
type: Project
openQuestions:
  - notes/questions/api-shape.md
---
`)
	writeOntologyNote(t, root, "notes/questions/api-shape.md", `---
type: OpenQuestion
project: notes/projects/atlas.md
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	requireEdge(t, result.Edges, "notes/projects/atlas.md", "openQuestions", "notes/questions/api-shape.md", "field", true)
}

func TestBuildIndex_MatchesTypesByTagsAndProperties(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Decision @node(matches: ["tag:type/decision"]) {
  name: String!
}

type Project @node(matches: ["classification:project"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/decisions/One.md", `---
type: Decision
tags: [type/decision]
name: First Decision
---
`)
	writeOntologyNote(t, root, "notes/projects/Roadmap.md", `---
type: Project
classification: project
name: Roadmap Refresh
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	require.Len(t, result.NoteTypes, 2)
}

func TestBuildIndex_KeepsResolvedTypeForInvalidNotes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/projects/valid.md", `---
type: Project
name: Valid
---
`)
	writeOntologyNote(t, root, "notes/projects/broken.md", `---
type: Project
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidationIssues)
	requireIssueCode(t, result.ValidationIssues, "missing_required_field")
	require.Len(t, result.NoteTypes, 2)
	requireNoteType(t, result.NoteTypes, "notes/projects/valid.md", "Project")
	requireNoteType(t, result.NoteTypes, "notes/projects/broken.md", "Project")
	assessment := requireAssessment(t, result.Assessments, "notes/projects/broken.md", "Project")
	require.Len(t, assessment.Issues, 1)
	require.Equal(t, "missing_required_field", assessment.Issues[0].Code)
}

func TestBuildIndex_TypeHintWinsWhenItMatchesCandidates(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(matches: ["classification:project"]) {
  name: String
}

type Initiative @node(matches: ["classification:project", "tag:type/initiative"]) {
  name: String
}
`)
	writeOntologyNote(t, root, "notes/roadmap.md", `---
type: Initiative
classification: project
tags: [type/initiative]
name: Roadmap
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Len(t, result.NoteTypes, 1)
	requireNoteType(t, result.NoteTypes, "notes/roadmap.md", "Initiative")
	requireAssessment(t, result.Assessments, "notes/roadmap.md", "Initiative")
}

func TestBuildIndex_IdentifierGateSkipsPathOnlyClassificationWithoutRequiredIdentifier(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type TechnicalSpec @node(paths: ["docs/specs/technical/**/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  summary: String!
}
`)
	writeOntologyNote(t, root, "docs/specs/technical/search/plan.md", `# Scratch plan

This is freeform planning prose, not a typed technical spec.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.NoteTypes)
	requireNoIssueForPath(t, result.ValidationIssues, "docs/specs/technical/search/plan.md")
}

func TestBuildIndex_IdentifierGateAllowsPathClassificationWithRequiredIdentifier(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type TechnicalSpec @node(paths: ["docs/specs/technical/**/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  summary: String!
}

`)
	writeOntologyNote(t, root, "docs/specs/technical/search/SPEC-0042-query.md", `---
id: SPEC-0042
summary: Query contract
---

# Query contract
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	requireNoteType(t, result.NoteTypes, "docs/specs/technical/search/SPEC-0042-query.md", "TechnicalSpec")
	requireAssessment(t, result.Assessments, "docs/specs/technical/search/SPEC-0042-query.md", "TechnicalSpec")
}

func TestBuildIndex_IdentifierGateAllowsDateTimeStrategyIdentifier(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Effort @node(paths: ["docs/efforts/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  summary: String!
}
`)
	writeOntologyNote(t, root, "docs/efforts/2026-08-05-14-32-query.md", `---
id: EFF-2026-08-05-14-32
summary: Query contract
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	requireNoteType(t, result.NoteTypes, "docs/efforts/2026-08-05-14-32-query.md", "Effort")
	requireAssessment(t, result.Assessments, "docs/efforts/2026-08-05-14-32-query.md", "Effort")
}

func TestBuildIndex_IdentifierGateUsesImplementedInterfaceIdentifiers(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
interface SpecLike {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
}

type ExperienceSpec implements SpecLike @node(paths: ["docs/specs/experience/**/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  summary: String!
}
`)
	writeOntologyNote(t, root, "docs/specs/experience/search/plan.md", `# Scratch plan

This is freeform planning prose, not a typed experience spec.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.NoteTypes)
	requireNoIssueForPath(t, result.ValidationIssues, "docs/specs/experience/search/plan.md")
}

func TestBuildIndex_IdentifierlessTypeStillClassifiesByPathAlone(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type DocumentationHub @node(paths: ["docs/**/README.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "docs/specs/technical/README.md", `---
summary: Technical spec reading order
---

# Technical specs
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	requireNoteType(t, result.NoteTypes, "docs/specs/technical/README.md", "DocumentationHub")
	requireAssessment(t, result.Assessments, "docs/specs/technical/README.md", "DocumentationHub")
}

func TestBuildIndex_DeclaredTypeBypassesIdentifierGateWhenSelectorsMatch(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type TechnicalSpec @node(paths: ["docs/specs/technical/**/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  summary: String!
}
`)
	writeOntologyNote(t, root, "docs/specs/technical/search/draft.md", `---
type: TechnicalSpec
summary: Draft spec
---

# Draft spec
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireNoteType(t, result.NoteTypes, "docs/specs/technical/search/draft.md", "TechnicalSpec")
	requireAssessment(t, result.Assessments, "docs/specs/technical/search/draft.md", "TechnicalSpec")
	issue := requireIssue(t, result.ValidationIssues, "missing_required_field", "docs/specs/technical/search/draft.md")
	require.Equal(t, "id", issue.FieldName)
}

func TestBuildIndex_PlanTypeUsesIdentifierGate(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type NarrativeSection implements Section {}

type Plan @node(paths: ["docs/efforts/**/plan*.md", "docs/specs/**/plan*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "PLAN")
  summary: String!
  goal: NarrativeSection @contains(level: H2, heading: "Goal", required: true)
  approach: NarrativeSection @contains(level: H2, heading: "Approach", required: true)
}
`)
	writeOntologyNote(t, root, "docs/efforts/search/plan.md", `# Scratch plan

No typed fields yet.
`)
	writeOntologyNote(t, root, "docs/efforts/search/plan-v2.md", `---
id: PLAN-0001
summary: Search implementation plan
---

# Search implementation plan

## Goal

Ship search.

## Approach

Do it carefully.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireNoteType(t, result.NoteTypes, "docs/efforts/search/plan-v2.md", "Plan")
	requireAssessment(t, result.Assessments, "docs/efforts/search/plan-v2.md", "Plan")
	requireNoIssueForPath(t, result.ValidationIssues, "docs/efforts/search/plan.md")
}

func TestBuildIndex_PrefersMostSpecificMatchedType(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Decision @node(matches: ["docs/reference/decisions"]) {
  summary: String
}

type ArchitectureDecision @node(matches: ["docs/reference/decisions && decision-domain:architecture"]) {
  summary: String
}
`)
	writeOntologyNote(t, root, "docs/reference/decisions/cache.md", `---
decision-domain: architecture
summary: Cache topology
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	require.Len(t, result.NoteTypes, 1)
	requireNoteType(t, result.NoteTypes, "docs/reference/decisions/cache.md", "ArchitectureDecision")
	requireAssessment(t, result.Assessments, "docs/reference/decisions/cache.md", "ArchitectureDecision")
}

func TestBuildIndex_PrefersMostSpecificProductDecisionType(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Decision @node(matches: ["docs/reference/decisions"]) {
  summary: String
}

type ProductDecision @node(matches: ["docs/reference/decisions && decision-domain:product"]) {
  summary: String
}
`)
	writeOntologyNote(t, root, "docs/reference/decisions/pricing.md", `---
decision-domain: product
summary: Pricing model
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	require.Len(t, result.NoteTypes, 1)
	requireNoteType(t, result.NoteTypes, "docs/reference/decisions/pricing.md", "ProductDecision")
	requireAssessment(t, result.Assessments, "docs/reference/decisions/pricing.md", "ProductDecision")
}

func TestBuildIndex_AmbiguousMatchCreatesAssessmentWithoutResolvedType(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(matches: ["classification:project"]) {
  name: String
}

type Initiative @node(matches: ["classification:project"]) {
  name: String
}
`)
	writeOntologyNote(t, root, "notes/roadmap.md", `---
classification: project
name: Roadmap
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.NoteTypes)
	requireIssueCode(t, result.ValidationIssues, "type_ambiguous")
	assessment := requireAssessment(t, result.Assessments, "notes/roadmap.md", "")
	require.ElementsMatch(t, []string{"Initiative", "Project"}, assessment.CandidateTypes)
}

func TestBuildIndex_DoesNotPreferCandidateWithRedundantBroaderOrClause(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(matches: ["classification:project"]) {
  name: String
}

type ArchitectureProject @node(matches: ["classification:project || classification:project && decision-domain:architecture"]) {
  name: String
}
`)
	writeOntologyNote(t, root, "notes/roadmap.md", `---
classification: project
decision-domain: architecture
name: Roadmap
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.NoteTypes)
	requireIssueCode(t, result.ValidationIssues, "type_ambiguous")
	assessment := requireAssessment(t, result.Assessments, "notes/roadmap.md", "")
	require.ElementsMatch(t, []string{"ArchitectureProject", "Project"}, assessment.CandidateTypes)
}

func TestBuildIndex_DeclaredTypeMismatchStillProducesIssue(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Decision @node(matches: ["docs/reference/decisions"]) {
  summary: String
}

type ProductDecision @node(matches: ["docs/reference/decisions && decision-domain:product"]) {
  summary: String
}

type ArchitectureDecision @node(matches: ["docs/reference/decisions && decision-domain:architecture"]) {
  summary: String
}
`)
	writeOntologyNote(t, root, "docs/reference/decisions/cache.md", `---
type: ProductDecision
decision-domain: architecture
summary: Cache topology
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireIssueCode(t, result.ValidationIssues, "declared_type_mismatch")
	require.Len(t, result.NoteTypes, 1)
	requireNoteType(t, result.NoteTypes, "docs/reference/decisions/cache.md", "ArchitectureDecision")
}

func TestBuildIndexWithStore_RequiresMarkdownSource(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(matches: ["type:project"]) {
  name: String!
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}

type Decision @node(matches: ["type:decision"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/project.md", `---
type: Project
name: Roadmap
---

See [[decision]].
`)
	writeOntologyNote(t, root, "notes/decision.md", `---
type: Decision
name: API choice
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	_, err = BuildIndexWithStore(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, failingNoteReader{}, store, schema, "notes-hash")
	require.ErrorContains(t, err, "read note")
}

func TestEnsureIndexedRequiresExplicitNoteMetadataIndexerBeforeMutation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema string
	}{
		{name: "valid schema", schema: `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`},
		{name: "missing schema"},
		{name: "invalid schema", schema: "type Broken {"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			if tt.schema != "" {
				writeOntologySchema(t, root, tt.schema)
			}
			writeOntologyNote(t, root, "notes/project.md", "---\nname: Roadmap\n---\n")

			store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			ctx := context.Background()
			require.NoError(t, store.ReplaceOntologySnapshot(ctx, codeanchorsqlite.OntologySnapshot{
				SchemaState: codeanchorsqlite.OntologySchemaState{
					SchemaHash:             "sentinel-schema",
					NotesHash:              "sentinel-notes",
					MaterializationVersion: OntologyMaterializationVersion,
					LoadedAt:               1,
					Ready:                  true,
				},
			}))
			beforeMetadata, err := store.GetNoteMetadataState(ctx)
			require.NoError(t, err)
			beforeOntology, err := store.GetOntologySchemaState(ctx)
			require.NoError(t, err)

			_, err = EnsureIndexed(ctx, notemeta.Indexer{}, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
			require.ErrorContains(t, err, "note format runtime is required")

			afterMetadata, err := store.GetNoteMetadataState(ctx)
			require.NoError(t, err)
			afterOntology, err := store.GetOntologySchemaState(ctx)
			require.NoError(t, err)
			require.Equal(t, beforeMetadata, afterMetadata)
			require.Equal(t, beforeOntology, afterOntology)
		})
	}
}

func TestEnsureIndexed_UsesNoteMetadataVersionFromStore(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/project.md", `---
type: Project
name: Roadmap
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)
	require.True(t, state.Ready)

	err = store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash: "old",
			NotesHash:  "old",
			LoadedAt:   state.LoadedAt - 1,
			Ready:      true,
		},
	})
	require.NoError(t, err)

	result, err := EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	updated, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.Equal(t, state.LoadedAt, updated.LoadedAt)

	typ, ok, err := store.GetOntologyTypeByPath(context.Background(), "notes/project.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Project", typ.TypeName)
}

func TestEnsureIndexed_ReusesSnapshotWhenOnlyLoadedAtChanges(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/project.md", `---
type: Project
name: Roadmap
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	noteState, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	_, err = EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	err = store.UpsertOntologySchemaState(context.Background(), codeanchorsqlite.OntologySchemaState{
		SchemaHash:             schema.Hash,
		NotesHash:              noteState.NotesHash,
		MaterializationVersion: OntologyMaterializationVersion,
		LoadedAt:               noteState.LoadedAt - 1,
		Ready:                  true,
	})
	require.NoError(t, err)

	result, err := EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.False(t, result.Dirty)
}

func TestEnsureIndexedRejectsFutureMaterializationVersionWithoutMutation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := codeanchorsqlite.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	want := codeanchorsqlite.OntologySchemaState{
		SchemaHash:             "future-schema",
		NotesHash:              "future-notes",
		MaterializationVersion: OntologyMaterializationVersion + 1,
		LoadedAt:               17,
		Ready:                  true,
		ErrorJSON:              `[{"code":"future"}]`,
	}
	require.NoError(t, store.UpsertOntologySchemaState(ctx, want))

	_, err = EnsureIndexed(ctx, testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, failingNoteReader{}, store)
	require.ErrorIs(t, err, ErrFutureOntologyMaterialization)
	require.ErrorContains(t, err, "upgrade Rhizome")
	got, stateErr := store.GetOntologySchemaState(ctx)
	require.NoError(t, stateErr)
	require.Equal(t, want, got)
}

func TestSyncPublishedPathsRejectsFutureMaterializationVersionWithoutMutation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := codeanchorsqlite.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	want := codeanchorsqlite.OntologySchemaState{
		SchemaHash:             "future-schema",
		NotesHash:              "future-notes",
		MaterializationVersion: OntologyMaterializationVersion + 1,
		LoadedAt:               23,
		Ready:                  true,
		ErrorJSON:              `[{"code":"future"}]`,
	}
	require.NoError(t, store.UpsertOntologySchemaState(ctx, want))

	_, err = SyncPublishedPaths(ctx, testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, failingNoteReader{}, store, nil, nil, nil)
	require.ErrorIs(t, err, ErrFutureOntologyMaterialization)
	require.ErrorContains(t, err, "upgrade Rhizome")
	got, stateErr := store.GetOntologySchemaState(ctx)
	require.NoError(t, stateErr)
	require.Equal(t, want, got)
}

func TestEnsureRuntimeWithStoreErrorFallbackIsNeverReady(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	store, err := codeanchorsqlite.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.UpsertOntologySchemaState(ctx, codeanchorsqlite.OntologySchemaState{
		SchemaHash:             schema.Hash,
		NotesHash:              "notes",
		MaterializationVersion: OntologyMaterializationVersion,
		LoadedAt:               1,
		Ready:                  true,
	}))

	runtime, err := EnsureRuntimeWithStore(ctx, testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, failingNoteReader{}, store)
	require.Error(t, err)
	require.False(t, runtime.Ready)
	require.Nil(t, runtime.Schema)
}

func TestEnsureIndexedRebuildsWhenOntologyStateIsMaterializedIncompletely(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	noteState, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	schema, err := LoadSchema(root)
	require.NoError(t, err)

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, codeanchorsqlite.OntologySnapshot{
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash:             schema.Hash,
			NotesHash:              noteState.NotesHash,
			MaterializationVersion: OntologyMaterializationVersion,
			LoadedAt:               noteState.LoadedAt,
			Ready:                  true,
		},
	}))

	result, err := EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	var embeddedTypes []string
	for _, node := range nodes {
		if node.NodeKind == "EMBEDDED" {
			embeddedTypes = append(embeddedTypes, node.TypeName)
		}
	}
	require.Equal(t, []string{"ActionItem"}, embeddedTypes)
	states, err := store.OntologyNoteStatesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	require.NotEmpty(t, states["notes/pizza-party-2026.md"].InputFingerprint)
}

func TestEnsureIndexedRebuildsWhenOntologyNodesAreMissingButStateIsCurrent(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	first, err := EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, first.Dirty)

	states, err := store.OntologyNoteStatesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	require.NotEmpty(t, states["notes/pizza-party-2026.md"].InputFingerprint)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{"notes/pizza-party-2026.md"},
	}))

	result, err := EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	var embeddedTypes []string
	for _, node := range nodes {
		if node.NodeKind == "EMBEDDED" {
			embeddedTypes = append(embeddedTypes, node.TypeName)
		}
	}
	require.Equal(t, []string{"ActionItem"}, embeddedTypes)
}

func TestEnsureIndexedRebuildsWhenOntologyNodeFieldRowsAreMissingButStateIsCurrent(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	first, err := EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, first.Dirty)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.NodeID)
	}
	fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	require.NotEmpty(t, fields)

	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{"notes/pizza-party-2026.md"},
		Nodes:     nodes,
	}))
	fields, err = store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	require.Empty(t, fields)

	result, err := EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	fields, err = store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	require.NotEmpty(t, fields)
}

func TestEnsureIndexedRefreshesStaleReadyNoteMetadataBeforeOntologyReuse(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/existing.md", "# Existing\n\nNo actions yet.\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)

	result, err := EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	var embeddedTypes []string
	for _, node := range nodes {
		if node.NodeKind == "EMBEDDED" {
			embeddedTypes = append(embeddedTypes, node.TypeName)
		}
	}
	require.Equal(t, []string{"ActionItem"}, embeddedTypes)
	states, err := store.OntologyNoteStatesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	require.NotEmpty(t, states["notes/pizza-party-2026.md"].InputFingerprint)
}

func TestSyncPaths_ReplacesOnlyTouchedStructuralSources(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  decision: Decision @link
}
`)
	writeOntologyNote(t, root, "notes/decisions/a.md", `---
type: Decision
name: A
---
`)
	writeOntologyNote(t, root, "notes/decisions/b.md", `---
type: Decision
name: B
---
`)
	writeOntologyNote(t, root, "notes/projects/one.md", `---
type: Project
name: One
decision: notes/decisions/a.md
---
`)
	writeOntologyNote(t, root, "notes/projects/two.md", `---
type: Project
name: Two
decision: notes/decisions/a.md
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/projects/one.md", `---
type: Project
name: One
decision: notes/decisions/b.md
---
`)

	err = testNoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/projects/one.md"}, nil)
	require.NoError(t, err)
	result, err := SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/projects/one.md"}, nil)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	require.GreaterOrEqual(t, result.Assessed, 1)

	edges, err := store.OntologyEdgesForPaths(context.Background(), []string{"notes/projects/one.md", "notes/projects/two.md"}, true, "", 0)
	require.NoError(t, err)
	requireEdge(t, edges, "notes/projects/one.md", "decision", "notes/decisions/b.md", "field", true)
	requireEdge(t, edges, "notes/projects/two.md", "decision", "notes/decisions/a.md", "field", true)
	for _, edge := range edges {
		require.False(t, edge.SrcPath == "notes/projects/one.md" && edge.DstPath == "notes/decisions/a.md")
	}
}

func TestSyncPaths_UpdatesGlobalSourceNodesForUntypedNotes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
}

type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
---
`)
	writeOntologyNote(t, root, "notes/scratch.md", "# Scratch\n\nNo actions yet.\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(ctx, testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/scratch.md", `# Scratch

- [ ] Call Alice #action-item
  assignee:: [[people/Alice]]
`)
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/scratch.md"}, nil))
	result, err := SyncPaths(ctx, testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/scratch.md"}, nil)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/scratch.md"})
	require.NoError(t, err)
	// The catalog emits a FallbackNote NOTE row for the untyped host alongside
	// the ActionItem EMBEDDED child, so downstream writers don't re-issue a
	// destructive Replace to install the FallbackNote (which would wipe the
	// child).
	var rootKinds, embeddedTypes []string
	for _, n := range nodes {
		switch n.NodeKind {
		case "NOTE":
			rootKinds = append(rootKinds, n.TypeName)
		case "EMBEDDED":
			embeddedTypes = append(embeddedTypes, n.TypeName)
		}
	}
	require.Equal(t, []string{FallbackNoteTypeName}, rootKinds)
	require.Equal(t, []string{"ActionItem"}, embeddedTypes)
	edges, err := store.OntologyEdgesForPaths(ctx, []string{"notes/scratch.md"}, true, "", 0)
	require.NoError(t, err)
	requireEdge(t, edges, "notes/scratch.md", "assignee", "people/Alice.md", "field", true)

	writeOntologyNote(t, root, "notes/scratch.md", "# Scratch\n\nNo actions anymore.\n")
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/scratch.md"}, nil))
	result, err = SyncPaths(ctx, testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/scratch.md"}, nil)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	// After the action items are removed the path still matches the global
	// source pattern, so the FallbackNote NOTE row remains; only the embedded
	// children should be gone.
	nodes, err = store.OntologyNodesByPaths(ctx, []string{"notes/scratch.md"})
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(nodes, "NOTE", FallbackNoteTypeName))
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(nodes, "SECTION", FallbackSectionTypeName))
	edges, err = store.OntologyEdgesForPaths(ctx, []string{"notes/scratch.md"}, true, "", 0)
	require.NoError(t, err)
	for _, edge := range edges {
		require.False(t, edge.SrcPath == "notes/scratch.md" && edge.RelationName == "assignee")
	}
}

func TestEnsureIndexedKeepsFallbackRowsForPlainUntypedNotes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
}
`)
	writeOntologyNote(t, root, "notes/plain.md", "# Plain\n\nNo ontology type here.\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"notes/plain.md"}, nil))
	result, err := SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, []string{"notes/plain.md"}, nil)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	incremental, err := store.OntologyNodesByPaths(ctx, []string{"notes/plain.md"})
	require.NoError(t, err)
	require.Len(t, incremental, 2)
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(incremental, "NOTE", FallbackNoteTypeName))
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(incremental, "SECTION", FallbackSectionTypeName))

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	full, err := store.OntologyNodesByPaths(ctx, []string{"notes/plain.md"})
	require.NoError(t, err)
	require.Len(t, full, 2)
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(full, "NOTE", FallbackNoteTypeName))
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(full, "SECTION", FallbackSectionTypeName))
}

func TestBuildIndex_UsesDefaultNoteTypeForUntypedNotes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type GeneralNote @node(default: true) {
  title: String @field
}

type Project @node(paths: ["projects/*.md"]) {
  name: String! @field
}
`)
	writeOntologyNote(t, root, "notes/plain.md", `---
title: Plain note
---
# Plain

No ontology selector or declared type.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	requireAssessment(t, result.Assessments, "notes/plain.md", "GeneralNote")
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(result.Nodes, "NOTE", "GeneralNote"))
	require.Equal(t, 0, countOntologyTestNodesByKindAndType(result.Nodes, "NOTE", FallbackNoteTypeName))
}

func TestBuildIndex_DefaultNoteDoesNotRescueInvalidDeclaredType(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type GeneralNote @node(default: true) {
  title: String @field
}
`)
	writeOntologyNote(t, root, "notes/plain.md", `---
type: MissingType
title: Plain note
---
# Plain
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	assessment := requireAssessment(t, result.Assessments, "notes/plain.md", "")
	requireIssueCode(t, assessment.Issues, "unknown_declared_type")
	require.Equal(t, 0, countOntologyTestNodesByKindAndType(result.Nodes, "NOTE", "GeneralNote"))
	require.Equal(t, 0, countOntologyTestNodesByKindAndType(result.Nodes, "NOTE", FallbackNoteTypeName))
}

func countOntologyTestNodesByKindAndType(nodes []codeanchor.IntelOntologyNode, kind, typeName string) int {
	count := 0
	for _, node := range nodes {
		if node.NodeKind == kind && node.TypeName == typeName {
			count++
		}
	}
	return count
}

func TestSyncPaths_PersistsFrozenStoryEdgesToEmbeddedNodes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type FrozenStoryScopeSection implements Section {
}

type EffortNote @node(paths: ["efforts/*.md"]) {
  summary: String!
  storiesInScopeFrozen: FrozenStoryScopeSection @contains(level: H2, heading: "Stories In Scope (Frozen)", required: true)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type ProcessSpec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`)
	writeOntologyNote(t, root, "specs/graph.md", `---
type: ProcessSpec
summary: Graph spec
---

# Graph spec

## Stories

### Graph story
id:: SPEC-0001.US1
status:: ready
^graph-story
`)
	writeOntologyNote(t, root, "efforts/graph.md", `---
type: EffortNote
summary: Graph effort
---

# Graph effort

## Stories In Scope (Frozen)

- SPEC-0001.US1 — Graph story, ready
`)
	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	err = testNoteMetadataIndexer(t).SyncPaths(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"specs/graph.md", "efforts/graph.md"}, nil)
	require.NoError(t, err)
	_, err = SyncPaths(ctx, testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"specs/graph.md", "efforts/graph.md"}, nil)
	require.NoError(t, err)

	edges, err := store.OntologyEdgesForPaths(ctx, []string{"efforts/graph.md"}, true, "frozenStories", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "specs/graph.md", edges[0].DstPath)
	require.NotEmpty(t, edges[0].DstNodeID)
	require.Equal(t, "UserStory", edges[0].DstType)
	require.Equal(t, "story_id", edges[0].Provenance)
}

func TestSyncPaths_RebuildsOntologyWhenOnlySchemaChanges(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/item.md", `---
type: Project
name: Item
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologySchema(t, root, `
type Initiative @node(paths: ["notes/*.md"]) {
  name: String!
}
`)

	result, err := SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)
	require.True(t, result.Dirty)

	typ, ok, err := store.GetOntologyTypeByPath(context.Background(), "notes/item.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Initiative", typ.TypeName)
}

func TestSyncPathsSchemaRebuildRefreshesStaleReadyNoteMetadata(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/existing.md", "# Existing\n\nNo actions yet.\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
}
`)

	result, err := SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	var embeddedTypes []string
	for _, node := range nodes {
		if node.NodeKind == "EMBEDDED" {
			embeddedTypes = append(embeddedTypes, node.TypeName)
		}
	}
	require.Equal(t, []string{"ActionItem"}, embeddedTypes)
}

func TestSyncPathsRebuildsWhenOntologyStateIsMaterializedIncompletely(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/pizza-party-2026.md", `# Pizza

- [ ] Order extra firewood #action-item
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	noteState, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	schema, err := LoadSchema(root)
	require.NoError(t, err)

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, codeanchorsqlite.OntologySnapshot{
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash:             schema.Hash,
			NotesHash:              noteState.NotesHash,
			MaterializationVersion: OntologyMaterializationVersion,
			LoadedAt:               noteState.LoadedAt,
			Ready:                  true,
		},
	}))

	result, err := SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)
	require.True(t, result.Dirty)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"notes/pizza-party-2026.md"})
	require.NoError(t, err)
	var embeddedTypes []string
	for _, node := range nodes {
		if node.NodeKind == "EMBEDDED" {
			embeddedTypes = append(embeddedTypes, node.TypeName)
		}
	}
	require.Equal(t, []string{"ActionItem"}, embeddedTypes)
}

func TestSyncPathsRepairsUntouchedMissingOntologyRowsDuringIncrementalSync(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/one.md", "---\nname: One\n---\n")
	writeOntologyNote(t, root, "notes/two.md", "---\nname: Two\n---\n")

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	vaultDef := obsidian.VaultDefinition{Path: root}
	note := &obsidian.Note{}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vaultDef, note, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, note, store)
	require.NoError(t, err)
	require.NoError(t, store.DeleteOntologyForPaths(ctx, []string{"notes/one.md"}))

	writeOntologyNote(t, root, "notes/two.md", "---\nname: Updated\n---\n")
	require.NoError(t, testNoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, note, store, []string{"notes/two.md"}, nil))

	result, err := SyncPaths(ctx, testNoteMetadataIndexer(t), vaultDef, note, store, nil, []string{"notes/two.md"}, nil)
	require.NoError(t, err)
	require.False(t, result.Rebuilt)
	require.Equal(t, 2, result.Assessed)

	paths := []string{"notes/one.md", "notes/two.md"}
	assessments, err := store.OntologyAssessmentsByPaths(ctx, paths)
	require.NoError(t, err)
	require.Len(t, assessments, 2)
	states, err := store.OntologyNoteStatesByPaths(ctx, paths)
	require.NoError(t, err)
	require.Len(t, states, 2)
	types, err := store.OntologyTypesByPaths(ctx, paths)
	require.NoError(t, err)
	require.Len(t, types, 2)
}

func TestSyncPaths_UpdatesSectionValidationIncrementally(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

	type Spec @node(paths: ["notes/specs/*.md"]) {
	  name: String!
	  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
	}
`)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

See [[decision]].
`)
	writeOntologyNote(t, root, "notes/decisions/decision.md", `---
type: Decision
name: Decision
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

### Requirements
`)
	err = testNoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)

	result, err := SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)
	requireIssueCode(t, result.Issues, "wrong_section_level")

	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

See [[decision]].
`)
	err = testNoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)

	result, err = SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)
	for _, issue := range result.Issues {
		require.NotEqual(t, "wrong_section_level", issue.Code)
		require.NotEqual(t, "empty_required_section", issue.Code)
	}

	updatedState, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.NotContains(t, updatedState.ErrorJSON, "wrong_section_level")

	row, ok, err := store.GetOntologyAssessmentByPath(context.Background(), "notes/specs/spec.md")
	require.NoError(t, err)
	require.True(t, ok)
	assessment, err := AssessmentFromJSON(row.AssessmentJSON)
	require.NoError(t, err)
	require.NotNil(t, assessment)
	for _, issue := range assessment.Issues {
		require.NotEqual(t, "wrong_section_level", issue.Code)
		require.NotEqual(t, "empty_required_section", issue.Code)
	}
}

func TestSyncPaths_UpdatesNestedSectionValidationIncrementally(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type DetailsSection implements Section {
}

type RequirementsSection implements Section {
  details: DetailsSection @contains(level: H3, heading: "Details", required: true)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

### Details

Ready.
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

Ready.
`)
	err = testNoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)

	result, err := SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)
	issue := requireIssue(t, result.Issues, "missing_required_section", "notes/specs/spec.md")
	require.Equal(t, "requirements.details", issue.FieldName)

	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
name: Spec
---

## Requirements

### Details

Ready again.
`)
	err = testNoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)

	result, err = SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/specs/spec.md"}, nil)
	require.NoError(t, err)
	for _, issue := range result.Issues {
		require.NotEqual(t, "requirements.details", issue.FieldName)
	}
}

func TestSyncPaths_PersistsValidationIssuesInSchemaState(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/item.md", `---
type: Project
name: Item
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/item.md", `---
type: Project
---
`)
	err = testNoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/item.md"}, nil)
	require.NoError(t, err)

	result, err := SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/item.md"}, nil)
	require.NoError(t, err)
	requireIssueCode(t, result.Issues, "missing_required_field")

	state, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.Contains(t, state.ErrorJSON, "missing_required_field")
}

func TestSyncPaths_PersistsSectionValidationIssuesInSchemaState(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type RequirementsSection implements Section {
}

type Spec @node(paths: ["notes/*.md"]) {
  name: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)
	writeOntologyNote(t, root, "notes/item.md", `---
type: Spec
name: Item
---

## Requirements

Ready.
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	writeOntologyNote(t, root, "notes/item.md", `---
type: Spec
name: Item
---

### Requirements
`)
	err = testNoteMetadataIndexer(t).SyncPaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, []string{"notes/item.md"}, nil)
	require.NoError(t, err)

	result, err := SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, []string{"notes/item.md"}, nil)
	require.NoError(t, err)
	requireIssueCode(t, result.Issues, "wrong_section_level")

	state, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	require.Contains(t, state.ErrorJSON, "wrong_section_level")
}

func TestSyncPaths_PersistsTraversalPoliciesOnSchemaRebuild(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project
  @node(paths: ["notes/*.md"])
  @traversal(intents: ["docs_for_code"], includeAmbient: true, minStructuralHits: 3) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/item.md", `---
type: Project
name: Item
---
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	result, err := SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)

	rows, err := store.OntologyTypePoliciesForPaths(context.Background(), []string{"notes/item.md"})
	require.NoError(t, err)
	row, ok := rows["notes/item.md"]
	require.True(t, ok)

	policy, err := UnmarshalTypePolicy(row.PolicyJSON)
	require.NoError(t, err)
	override, ok := policy.OverrideForIntent("docs_for_code")
	require.True(t, ok)
	require.NotNil(t, override.IncludeAmbient)
	require.True(t, *override.IncludeAmbient)
	require.NotNil(t, override.MinStructuralHits)
	require.Equal(t, 3, *override.MinStructuralHits)
}

func TestSyncPaths_FullRebuildWithZeroNotesStillUpdatesSchemaState(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String
}
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		NoteTypes: []codeanchorsqlite.OntologyNoteTypeRow{
			{NotePath: "notes/stale.md", TypeName: "Project", SchemaHash: "old", UpdatedAt: 1},
		},
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash: "old",
			NotesHash:  "old-notes",
			LoadedAt:   1,
			Ready:      true,
		},
	}))

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	result, err := SyncPaths(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Rebuilt)
	require.True(t, result.Dirty)

	state, err := store.GetOntologySchemaState(context.Background())
	require.NoError(t, err)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, schema.Hash, state.SchemaHash)
	require.True(t, state.Ready)

	_, ok, err := store.GetOntologyTypeByPath(context.Background(), "notes/stale.md")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestEnsureIndexed_PureFrontmatterSchemaProducesNoteRootFieldRows(t *testing.T) {
	// Regression: pure-frontmatter schemas (no section fields, no global
	// sources) used to skip content hydration. Projection rebuilds
	// DocumentSnapshot from doc.Content, so an empty content yielded an empty
	// snapshot.Frontmatter and zero ontology_node_field_values rows for
	// note-root types — even though doc.Frontmatter held the values.
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum SpecStatus {
  proposed
  active
  retired
}

type Spec @node(paths: ["docs/specs/**/*.md"]) {
  status: SpecStatus @field
  lastUpdated: Date @field(source: "last-updated")
}
`)
	writeOntologyNote(t, root, "docs/specs/alpha.md", `---
type: Spec
status: active
last-updated: 2026-04-01
---
# Alpha
`)
	writeOntologyNote(t, root, "docs/specs/beta.md", `---
type: Spec
status: proposed
last-updated: 2026-05-01
---
# Beta
`)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := codeanchorsqlite.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = EnsureIndexed(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	rows, err := store.OntologyNodesByType(context.Background(), "Spec")
	require.NoError(t, err)
	require.Len(t, rows, 2, "expected one catalog row per Spec note")

	nodeIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		require.Equal(t, string(NodeKindNote), row.NodeKind)
		nodeIDs = append(nodeIDs, row.NodeID)
	}

	fields, err := store.OntologyNodeFieldValuesByNodeIDs(context.Background(), nodeIDs, nil)
	require.NoError(t, err)
	require.Len(t, fields, 4, "expected status + lastUpdated rows for each Spec; got %d", len(fields))

	byNodeField := make(map[string]map[string]string)
	for _, row := range fields {
		if byNodeField[row.NodeID] == nil {
			byNodeField[row.NodeID] = map[string]string{}
		}
		byNodeField[row.NodeID][row.FieldName] = row.ValueNorm
	}
	require.Len(t, byNodeField, 2)
	for _, byField := range byNodeField {
		require.Contains(t, byField, "status")
		require.Contains(t, byField, "lastupdated")
	}
}

func writeOntologyTestConfig(t *testing.T, root string) {
	t.Helper()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
}

func writeOntologySchema(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(body), 0o644))
}

func writeOntologyNote(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

func requireEdge(t *testing.T, edges []codeanchorsqlite.OntologyEdgeRow, src, relation, dst, provenance string, structural bool) {
	t.Helper()
	for _, edge := range edges {
		if edge.SrcPath == src && edge.RelationName == relation && edge.DstPath == dst && edge.Provenance == provenance && edge.Structural == structural {
			return
		}
	}
	t.Fatalf("expected edge %s --%s [%s/%t]--> %s", src, relation, provenance, structural, dst)
}

func requireIssueCode(t *testing.T, issues []ValidationIssue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected issue code %s", code)
}

func requireIssue(t *testing.T, issues []ValidationIssue, code string, notePath string) ValidationIssue {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code && issue.NotePath == notePath {
			return issue
		}
	}
	t.Fatalf("expected issue %s for %s", code, notePath)
	return ValidationIssue{}
}

func requireNoIssueForPath(t *testing.T, issues []ValidationIssue, notePath string) {
	t.Helper()
	for _, issue := range issues {
		require.NotEqual(t, notePath, issue.NotePath, "unexpected issue for %s: %#v", notePath, issue)
	}
}

func requireNoteType(t *testing.T, rows []codeanchorsqlite.OntologyNoteTypeRow, notePath, typeName string) {
	t.Helper()
	for _, row := range rows {
		if row.NotePath == notePath && row.TypeName == typeName {
			return
		}
	}
	t.Fatalf("expected note type %s => %s", notePath, typeName)
}

func requireOntologyNode(t *testing.T, nodes []codeanchor.IntelOntologyNode, typeName, title string) codeanchor.IntelOntologyNode {
	t.Helper()
	for _, node := range nodes {
		if node.TypeName == typeName && node.Title == title {
			return node
		}
	}
	t.Fatalf("expected ontology node %s titled %q", typeName, title)
	return codeanchor.IntelOntologyNode{}
}

func requireOntologyNodes(t *testing.T, nodes []codeanchor.IntelOntologyNode, typeName string) []codeanchor.IntelOntologyNode {
	t.Helper()
	out := make([]codeanchor.IntelOntologyNode, 0)
	for _, node := range nodes {
		if node.TypeName == typeName {
			out = append(out, node)
		}
	}
	if len(out) == 0 {
		t.Fatalf("expected ontology nodes of type %s", typeName)
	}
	return out
}

func requireAssessment(t *testing.T, assessments []NoteAssessment, notePath, resolvedType string) NoteAssessment {
	t.Helper()
	for _, assessment := range assessments {
		if assessment.NotePath != notePath {
			continue
		}
		require.Equal(t, resolvedType, assessment.ResolvedType)
		return assessment
	}
	t.Fatalf("expected assessment for %s", notePath)
	return NoteAssessment{}
}

type failingNoteReader struct{}

func (failingNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "", errors.New("should not read note contents")
}

func (failingNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, errors.New("should not list notes")
}

func (failingNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, errors.New("should not stat notes")
}

func (failingNoteReader) Title(path string) (string, bool) {
	return path, true
}

func TestBuildIndex_ManagedDocTemplateLinksUseFutureRepositoryRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	sources := []notemeta.NoteSourceSnapshot{{
		Path:    "pkg/app/cli/init/templates/starters/example/agents/AGENTS.md",
		Content: "# Start here\n\n[README](README.md)\n",
		Title:   "AGENTS",
	}}

	result, err := BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}, sources, schema, "hash")
	require.NoError(t, err)
	for _, issue := range result.ValidationIssues {
		require.NotEqual(t, "broken_note_link", issue.Code)
	}
}
