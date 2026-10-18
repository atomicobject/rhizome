package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.NotEmpty(t, result.ValidationIssues)
	requireIssueCode(t, result.ValidationIssues, "declared_type_mismatch")
	require.Empty(t, result.NoteTypes)
	require.Empty(t, result.Edges)
}

func TestBuildIndex_UnresolvedLinkRetainsAssessmentWithoutEdge(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  decision: Note @link
}
`)
	writeOntologyNote(t, root, "notes/projects/roadmap.md", `---
type: Project
name: Roadmap
decision: "[[missing-decision]]"
---

Decision lives in [[missing-decision]].
`)
	writeOntologyNote(t, root, "docs/questions.md", `# Questions

[Missing meeting](../meetings/missing-sync.md)
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireNoteType(t, result.NoteTypes, "notes/projects/roadmap.md", "Project")
	assessment := requireAssessment(t, result.Assessments, "notes/projects/roadmap.md", "Project")
	relation, ok := assessment.Relation("decision")
	require.True(t, ok)
	require.True(t, relation.Present)
	require.Equal(t, []string{"[[missing-decision]]"}, relation.Values)
	require.Empty(t, relation.Targets)
	requireIssueCode(t, relation.Issues, "link_target_missing")
	require.Empty(t, result.Edges)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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
