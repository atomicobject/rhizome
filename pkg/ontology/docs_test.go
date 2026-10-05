package ontology_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/stretchr/testify/require"
)

func TestRenderReferenceMarkdown_IncludesDescriptionsAndSemantics(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
"""Project documentation."""
type Project @node(paths: ["notes/projects/*.md"]) @semantics(kind: BEHAVIORAL) {
  """Project display name."""
  name: String!
  """Background details."""
  notes: String @semantics(kind: DOCUMENTARY)
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	text, err := reference.RenderMarkdown(schema, "Project")
	require.NoError(t, err)
	require.Contains(t, text, "# Ontology Reference")
	require.Contains(t, text, "## Project")
	require.Contains(t, text, "Project documentation.")
	require.Contains(t, text, "Project display name.")
	require.Contains(t, text, "BEHAVIORAL")
	require.Contains(t, text, "DOCUMENTARY")
}

func TestRenderReferenceMarkdown_IncludesPropertyCaseAndSourceAliases(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type Project @node(paths: ["notes/projects/*.md"], propertyCase: CAMEL) {
  legacyStatus: String @field(sources: ["legacyStatus", "legacy-status"])
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	text, err := reference.RenderMarkdown(schema, "Project")
	require.NoError(t, err)
	require.Contains(t, text, "propertyCase: CAMEL")
	require.Contains(t, text, "source: legacyStatus")
	require.Contains(t, text, "source aliases: legacy-status")
}

func TestSchemaDocs_IncludeGuidanceAndEnumValuePolicy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
"""Spec lifecycle state."""
enum SpecStatus {
  """Normal iteration."""
  active
  """Frozen contract."""
  frozen
    @guidance(
      meaning: "Execution contract is frozen.",
      authoring: "Avoid semantic edits unless explicitly reopening.",
      agentImplications: "Confirm with a user before modifying semantics."
    )
    @policy(requiresUserConfirmation: true, forbidAutonomousSemanticEdits: true, reason: "Frozen means stop and confirm.")
}

"""Spec note."""
type Spec @node(paths: ["notes/specs/*.md"]) @guidance(meaning: "Normative spec meaning.", authoring: "Keep concise.", agentImplications: "Preserve lifecycle state.") {
  """Lifecycle state."""
  status: SpecStatus! @guidance(meaning: "Field meaning.", authoring: "Pick the narrowest state.", agentImplications: "Respect enum value rules.")
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	docs, err := ontology.SchemaDocs(schema, "Spec")
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "Spec note.", docs[0].Summary)
	require.Equal(t, "Spec note.", docs[0].Description)
	require.Equal(t, "Normative spec meaning.", docs[0].Meaning)
	require.Equal(t, "Keep concise.", docs[0].Authoring)
	require.Equal(t, "Preserve lifecycle state.", docs[0].AgentImplications)
	require.Len(t, docs[0].Fields, 1)
	require.Equal(t, "Lifecycle state.", docs[0].Fields[0].Summary)
	require.Equal(t, "Field meaning.", docs[0].Fields[0].Meaning)
	require.NotNil(t, docs[0].Fields[0].Enum)
	require.Len(t, docs[0].Fields[0].Enum.Values, 2)
	require.Equal(t, "Frozen contract.", docs[0].Fields[0].Enum.Values[1].Summary)
	require.Equal(t, "Execution contract is frozen.", docs[0].Fields[0].Enum.Values[1].Meaning)
	require.NotNil(t, docs[0].Fields[0].Enum.Values[1].Policy)
	require.True(t, docs[0].Fields[0].Enum.Values[1].Policy.RequiresUserConfirmation)
}

func TestSchemaDocs_ExposeIdentifierStrategyMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "SPEC")
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	docs, err := ontology.SchemaDocs(schema, "Spec")
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Len(t, docs[0].Fields, 1)
	require.NotNil(t, docs[0].Fields[0].IdentifierFormat)
	require.Equal(t, ontology.IdentifierStrategySequential, docs[0].Fields[0].IdentifierFormat.Strategy)
	require.Equal(t, "SPEC", docs[0].Fields[0].IdentifierFormat.Prefix)
}

func TestSchemaDocs_SpecDrivenSchemaCarriesLayeredGuidance(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))

	schema, err := ontology.LoadSchema(repoRoot)
	require.NoError(t, err)

	effortDocs, err := ontology.SchemaDocs(schema, "EffortNote")
	require.NoError(t, err)
	require.Len(t, effortDocs, 1)
	require.Contains(t, effortDocs[0].Meaning, "Bounded execution artifact")
	require.Contains(t, effortDocs[0].AgentImplications, "primary execution contract")
	fieldByName := make(map[string]ontology.FieldDoc, len(effortDocs[0].Fields))
	for _, field := range effortDocs[0].Fields {
		fieldByName[field.Name] = field
	}
	require.Contains(t, fieldByName["createdAt"].Meaning, "Canonical timestamp")
	require.Contains(t, fieldByName["planApprovedBy"].Meaning, "real human approver")
	require.Contains(t, fieldByName["closureChecklist"].Meaning, "closure evidence")
	_, hasAuditStatus := fieldByName["auditStatus"]
	_, hasBackportStatus := fieldByName["backportStatus"]
	_, hasCompoundStatus := fieldByName["compoundStatus"]
	require.False(t, hasAuditStatus)
	require.False(t, hasBackportStatus)
	require.False(t, hasCompoundStatus)
	_, hasDate := fieldByName["date"]
	_, hasTime := fieldByName["time"]
	require.False(t, hasDate)
	require.False(t, hasTime)

	var executionNotes ontology.FieldDoc
	foundExecutionNotes := false
	for _, field := range effortDocs[0].Fields {
		if field.Name == "executionNotes" {
			executionNotes = field
			foundExecutionNotes = true
			break
		}
	}
	require.True(t, foundExecutionNotes)
	require.Contains(t, executionNotes.Meaning, "Append-only effort-local journal")
	require.NotNil(t, executionNotes.Policy)
	require.True(t, executionNotes.Policy.ForbidAutonomousSemanticEdits)
	require.Equal(t, "APPEND_ONLY", executionNotes.Policy.EditScope)

	specDocs, err := ontology.SchemaDocs(schema, "ProductSpec")
	require.NoError(t, err)
	require.Len(t, specDocs, 1)
	var specID ontology.FieldDoc
	foundSpecID := false
	var specStatus ontology.FieldDoc
	foundSpecStatus := false
	for _, field := range specDocs[0].Fields {
		if field.Name == "id" {
			specID = field
			foundSpecID = true
		}
		if field.Name == "specStatus" {
			specStatus = field
			foundSpecStatus = true
		}
	}
	require.True(t, foundSpecID)
	require.True(t, specID.Identifier)
	require.True(t, specID.PreferredIdentifier)
	require.True(t, foundSpecStatus)
	require.Contains(t, specStatus.Meaning, "Contract-lifecycle marker")
	require.NotNil(t, specStatus.Enum)
	require.Equal(t, "proposed", specStatus.Enum.Values[0].Name)
	require.Contains(t, specStatus.Enum.Values[0].Meaning, "still being shaped")
	require.Equal(t, "active", specStatus.Enum.Values[1].Name)
	require.Contains(t, specStatus.Enum.Values[1].AgentImplications, "authoritative")
	require.Equal(t, "superseded", specStatus.Enum.Values[2].Name)
	require.Contains(t, specStatus.Enum.Values[2].Meaning, "successor")

	var successor ontology.FieldDoc
	foundSuccessor := false
	for _, field := range specDocs[0].Fields {
		if field.Name == "successor" {
			successor = field
			foundSuccessor = true
			break
		}
	}
	require.True(t, foundSuccessor)
	require.Contains(t, successor.Meaning, "successor")
	for _, field := range specDocs[0].Fields {
		require.NotEqual(t, "version", field.Name)
	}

	storyDocs, err := ontology.SchemaDocs(schema, "UserStory")
	require.NoError(t, err)
	require.Len(t, storyDocs, 1)
	var storyStatus ontology.FieldDoc
	foundStoryStatus := false
	var storyIncrement ontology.FieldDoc
	foundStoryIncrement := false
	for _, field := range storyDocs[0].Fields {
		if field.Name == "status" {
			storyStatus = field
			foundStoryStatus = true
		}
		if field.Name == "increment" {
			storyIncrement = field
			foundStoryIncrement = true
		}
	}
	require.True(t, foundStoryStatus)
	require.True(t, foundStoryIncrement)
	require.NotNil(t, storyStatus.Enum)
	require.Equal(t, "ready", storyStatus.Enum.Values[1].Name)
	require.Contains(t, storyStatus.Enum.Values[1].Meaning, "selected into a bounded effort")
	require.Contains(t, storyIncrement.Meaning, "delivery increment")
	require.Contains(t, storyIncrement.Authoring, "local to the spec")
}

func TestProcessDocsUseClosureChecklistContract(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	paths := []string{
		"docs/specs/process/compounding-work.md",
		"pkg/app/cli/init/templates/starters/agentic-engineering/agents/skills/agentic-engineering/references/closure.md",
	}

	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(repoRoot, rel))
			require.NoError(t, err)
			text := string(body)

			require.Contains(t, strings.ToLower(text), "closure checklist")
			require.NotContains(t, text, "audit-status")
			require.NotContains(t, text, "backport-status")
			require.NotContains(t, text, "compound-status")
			require.NotContains(t, text, "current-blocker")
		})
	}
}

func TestRenderReferenceMarkdown_IncludesInterfacesAndSections(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
interface SummaryDoc @display(singular: "Summary document", plural: "Summary documents") {
  summary: String!
}

type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) @display(singular: "Spec", plural: "Specs") {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	text, err := reference.RenderMarkdown(schema, "")
	require.NoError(t, err)
	require.Contains(t, text, "## Section")
	require.Contains(t, text, "built-in contract for heading-derived section nodes")
	require.Contains(t, text, "## SummaryDoc")
	require.Contains(t, text, "label: Summary document")
	require.Contains(t, text, "plural label: Summary documents")
	require.Contains(t, text, "role: INTERFACE")
	require.Contains(t, text, "## RequirementsSection")
	require.Contains(t, text, "scope: SUBTREE (section-local subtree traversal)")
	require.Contains(t, text, "binding: heading-derived subtree")
	require.Contains(t, text, "heading: Requirements")
	require.Contains(t, text, "level: H2")
	require.Contains(t, text, "matching: exact heading text + exact level")
}

func TestRenderReferenceMarkdown_IncludesEmbeddedNodeRole(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type UserStory implements Section
  @node(locator: EMBEDDED) {
  storyId: String! @field
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  stories: [UserStory!] @contains(level: H3)
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	text, err := reference.RenderMarkdown(schema, "UserStory")
	require.NoError(t, err)
	require.Contains(t, text, "role: EMBEDDED_NODE")
	require.Contains(t, text, "structure: embedded graph node persisted inside a parent note body")
}
