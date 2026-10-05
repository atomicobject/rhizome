package reference

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestRenderMarkdown_IncludesEnumValuesAndAnnotations(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
enum ProjectStatus {
  ACTIVE
  PAUSED
}

type Project
  @node(paths: ["notes/projects/*.md"])
  @retrieval(intents: ["search"], boost: 1.2) {
  status: ProjectStatus!
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	text, err := RenderMarkdown(schema, "Project")
	require.NoError(t, err)
	require.Contains(t, text, "# Ontology Reference")
	require.Contains(t, text, "enum: ACTIVE, PAUSED")
	require.Contains(t, text, "@retrieval")
}

func TestRenderMarkdown_IncludesCompanionDocs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type Project
  @node(paths: ["notes/projects/*.md"])
  @companionDocs(paths: ["docs/reference/guides/Project workflow.md"], purpose: "workflow") {
  summary: String!
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	text, err := RenderMarkdown(schema, "Project")
	require.NoError(t, err)
	require.Contains(t, text, "companion docs")
	require.Contains(t, text, "`docs/reference/guides/Project workflow.md` (workflow)")
}

func TestRenderMarkdown_OmitsDefaultPaneDisplayFromContainsAnnotations(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type RequirementsSection implements Section {
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
  summary: RequirementsSection @contains(level: H2, heading: "Summary", display: INLINE)
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	text, err := RenderMarkdown(schema, "Spec")
	require.NoError(t, err)
	require.Contains(t, text, `@contains {"heading":"Requirements","level":"H2","required":true}`)
	require.NotContains(t, text, `@contains {"display":"PANE","heading":"Requirements","level":"H2","required":true}`)
	require.Contains(t, text, `@contains {"display":"INLINE","heading":"Summary","level":"H2","required":false}`)
}

func TestRenderMarkdown_IncludesEmbeddedSourceShape(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type ActionItem implements Section @node(locator: EMBEDDED) {
}

type Meeting @node(paths: ["notes/meetings/*.md"]) {
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	text, err := RenderMarkdown(schema, "Meeting")
	require.NoError(t, err)
	require.Contains(t, text, "binding: CHECKBOX_ITEM source span")
	require.Contains(t, text, "marker: #action-item")
}

func TestRenderMarkdown_IncludesLayeredGuidanceAndEnumPolicy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
"""Spec lifecycle state."""
enum SpecStatus @guidance(meaning: "Shared status meaning.") {
  """Active work."""
  active
  """Frozen contract."""
  frozen
    @guidance(
      meaning: "Acts as the frozen execution contract.",
      authoring: "Avoid semantic edits unless reopening.",
      agentImplications: "Confirm with a user before making semantic changes."
    )
    @policy(requiresUserConfirmation: true, forbidAutonomousSemanticEdits: true, reason: "Frozen means stop and confirm.")
}

"""Spec note."""
type Spec
  @node(paths: ["notes/specs/*.md"])
  @guidance(
    meaning: "Defines the intended world-state.",
    authoring: "Keep stable enough to freeze.",
    agentImplications: "Preserve lifecycle semantics."
  ) {
  """Lifecycle state."""
  status: SpecStatus!
    @guidance(
      meaning: "Tracks whether the spec is still evolving.",
      authoring: "Choose the narrowest truthful state.",
      agentImplications: "Use enum semantics when deciding whether edits are safe."
    )
}
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	text, err := RenderMarkdown(schema, "Spec")
	require.NoError(t, err)
	require.Contains(t, text, "meaning: Defines the intended world-state.")
	require.Contains(t, text, "authoring: Keep stable enough to freeze.")
	require.Contains(t, text, "agent implications: Use enum semantics when deciding whether edits are safe.")
	require.Contains(t, text, "### Enums")
	require.Contains(t, text, "`frozen`: Frozen contract.")
	require.Contains(t, text, "forbids autonomous semantic edits")
}

func TestTypeDocumentationReportsTheTypeProfile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
enum TaskStatus { todo @view(stage: "open") doing @view(stage: "active") }
type Person @node(paths: ["people/*.md"]) { name: String }
type Task @node(paths: ["tasks/*.md"]) {
  status: TaskStatus @display(importance: KEY)
  owner: Person @link
}
`), 0o644))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	docs, err := (&ontology.Service{Schema: schema}).TypeDocs("Task")
	require.NoError(t, err)
	require.NotNil(t, docs[0].Profile)
	require.Equal(t, ontology.ShapeWorkflow, docs[0].Profile.Shape)
	require.Equal(t, []string{"owner"}, docs[0].Profile.PeopleFields, "people are links to the core identity type")

	payload, err := RenderJSON(schema, "Task")
	require.NoError(t, err)
	require.Contains(t, string(payload), `"profile":{"shape":"workflow","lifecycleField":"status"`)
	text, err := RenderMarkdown(schema, "Task")
	require.NoError(t, err)
	require.Contains(t, text, "- view profile: workflow; lifecycle status; people owner")
}
