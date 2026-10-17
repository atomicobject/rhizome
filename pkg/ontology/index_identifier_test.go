package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	requireNoteType(t, result.NoteTypes, "docs/efforts/search/plan-v2.md", "Plan")
	requireAssessment(t, result.Assessments, "docs/efforts/search/plan-v2.md", "Plan")
	requireNoIssueForPath(t, result.ValidationIssues, "docs/efforts/search/plan.md")
}
