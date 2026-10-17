package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	requireNoteType(t, result.NoteTypes, "docs/hubs/Search (Hub).md", "DocumentationHub")
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Len(t, result.NoteTypes, 1)
	requireNoteType(t, result.NoteTypes, "notes/roadmap.md", "Initiative")
	requireAssessment(t, result.Assessments, "notes/roadmap.md", "Initiative")
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireIssueCode(t, result.ValidationIssues, "declared_type_mismatch")
	require.Len(t, result.NoteTypes, 1)
	requireNoteType(t, result.NoteTypes, "docs/reference/decisions/cache.md", "ArchitectureDecision")
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)
	requireAssessment(t, result.Assessments, "notes/plain.md", "GeneralNote")
	require.Equal(t, 1, countOntologyTestNodesByKindAndType(result.Nodes, "NOTE", "GeneralNote"))
	require.Equal(t, 0, countOntologyTestNodesByKindAndType(result.Nodes, "NOTE", ontoschema.FallbackNoteTypeName))
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	assessment := requireAssessment(t, result.Assessments, "notes/plain.md", "")
	requireIssueCode(t, assessment.Issues, "unknown_declared_type")
	require.Equal(t, 0, countOntologyTestNodesByKindAndType(result.Nodes, "NOTE", "GeneralNote"))
	require.Equal(t, 0, countOntologyTestNodesByKindAndType(result.Nodes, "NOTE", ontoschema.FallbackNoteTypeName))
}
