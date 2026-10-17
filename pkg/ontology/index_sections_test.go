package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)

	issue := requireIssue(t, result.ValidationIssues, "field_shape_mismatch", "notes/specs/spec.md")
	require.Equal(t, "stories.owner", issue.FieldName)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	requireEdge(t, result.Edges, "notes/projects/atlas.md", "openQuestions", "notes/questions/api-shape.md", "field", true)
}
