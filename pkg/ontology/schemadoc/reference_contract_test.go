package schemadoc_test

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	text, err := schemadoc.RenderReferenceMarkdown(schema, "Project", identity.CurrentUserType)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	text, err := schemadoc.RenderReferenceMarkdown(schema, "Project", identity.CurrentUserType)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	docs, err := schemadoc.SchemaDocs(schema, "Spec", identity.CurrentUserType)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	docs, err := schemadoc.SchemaDocs(schema, "Spec", identity.CurrentUserType)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Len(t, docs[0].Fields, 1)
	require.NotNil(t, docs[0].Fields[0].IdentifierFormat)
	require.Equal(t, ontoschema.IdentifierStrategySequential, docs[0].Fields[0].IdentifierFormat.Strategy)
	require.Equal(t, "SPEC", docs[0].Fields[0].IdentifierFormat.Prefix)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	text, err := schemadoc.RenderReferenceMarkdown(schema, "", identity.CurrentUserType)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	text, err := schemadoc.RenderReferenceMarkdown(schema, "UserStory", identity.CurrentUserType)
	require.NoError(t, err)
	require.Contains(t, text, "role: EMBEDDED_NODE")
	require.Contains(t, text, "structure: embedded graph node persisted inside a parent note body")
}
