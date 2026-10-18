package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSchema_CompilesOntologyFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "people.graphql"), []byte(`
"""Organization team."""
type Team @node(paths: ["teams/*.md"], label: "Team") {
  name: String!
  members: [Person!] @link(inverse: "team")
}

"""Person note."""
type Person @node(paths: ["people/*.md"], keyField: "name", color: "#336699") @semantics(kind: BEHAVIORAL) {
  """Canonical name."""
  name: String!
  role: String @field(sourceKind: INLINE)
  team: Team @link(inverse: "members")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.NotEmpty(t, schema.Hash)
	require.Len(t, schema.Files, 1)
	require.Contains(t, schema.Types, "Person")
	require.Contains(t, schema.Types, "Team")

	person := schema.Types["Person"]
	require.Equal(t, "Person note.", person.Description)
	require.Equal(t, SemanticsKindBehavioral, person.Semantics)
	require.Equal(t, "name", person.KeyField)
	require.Equal(t, "#336699", person.Color)
	require.Equal(t, []string{"people/*.md"}, person.Paths)
	require.Empty(t, person.Matches)
	require.Equal(t, FieldKindScalar, person.ByName["name"].Kind)
	require.Equal(t, "Canonical name.", person.ByName["name"].Description)
	require.True(t, person.ByName["name"].Required)
	require.Equal(t, FieldSourceInline, person.ByName["role"].SourceKind)
	require.Equal(t, FieldKindLink, person.ByName["team"].Kind)
	require.Equal(t, "members", person.ByName["team"].Inverse)
}

func TestSchemaSourceHashTracksEditsAndFileSet(t *testing.T) {
	root := t.TempDir()
	dir := OntologyDir(root)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	first := filepath.Join(dir, "first.graphql")
	require.NoError(t, os.WriteFile(first, []byte(`type Alpha @node(paths: ["a/*.md"]) { name: String }`), 0o644))
	check := func() string {
		t.Helper()
		hash, err := SchemaSourceHash(root)
		require.NoError(t, err)
		schema, err := LoadSchema(root)
		require.NoError(t, err)
		require.Equal(t, schema.Hash, hash)
		return hash
	}
	initial := check()
	require.NoError(t, os.WriteFile(first, []byte(`type Bravo @node(paths: ["a/*.md"]) { name: String }`), 0o644))
	changed := check()
	require.NotEqual(t, initial, changed)
	second := filepath.Join(dir, "second.graphql")
	require.NoError(t, os.WriteFile(second, []byte(`type Delta @node(paths: ["d/*.md"]) { name: String }`), 0o644))
	require.NotEqual(t, changed, check())
	require.NoError(t, os.Rename(second, filepath.Join(dir, "second.txt")))
	require.Equal(t, changed, check())
}

func TestLoadSchema_EnumValueViewMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status.graphql"), []byte(`
enum WorkStatus {
  ready @view(label: "Ready", order: 10, tone: "info")
  done @view(label: "Finished", order: 20, collapsed: true)
}

type Task @node(paths: ["tasks/*.md"]) {
  status: WorkStatus
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	status := schema.EnumTypes["WorkStatus"]
	require.NotNil(t, status)
	require.Equal(t, "Ready", status.ByName["ready"].View.Label)
	require.Equal(t, 10, status.ByName["ready"].View.Order)
	require.Equal(t, "info", status.ByName["ready"].View.Tone)
	require.Nil(t, status.ByName["ready"].View.Collapsed)
	require.NotNil(t, status.ByName["done"].View.Collapsed)
	require.True(t, *status.ByName["done"].View.Collapsed)
}

func TestLoadSchemaRejectsInvalidEnumValueTone(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status.graphql"), []byte(`
enum WorkStatus {
  ready @view(tone: "urgent")
}

type Task @node(paths: ["tasks/*.md"]) {
  status: WorkStatus
}
`), 0o644))

	_, err := LoadSchema(root)
	requireCompileError(t, err, "enum_view_tone")
}

func TestLoadSchema_CapturesGuidanceAndPolicy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
"""Spec lifecycle state."""
enum SpecStatus @guidance(meaning: "Tracks where a spec sits in the delivery lifecycle.") {
  """Ready for normal iteration."""
  active
  """Frozen execution contract."""
  frozen
    @guidance(
      meaning: "The spec is acting as the normative execution contract.",
      authoring: "Avoid semantic edits unless the frozen contract is being deliberately reopened.",
      agentImplications: "Agents should confirm with a user before making semantic changes."
    )
    @policy(
      requiresUserConfirmation: true,
      forbidAutonomousSemanticEdits: true,
      reason: "Frozen specs should not be changed autonomously."
    )
}

"""Normative spec note."""
type Spec
  @node(paths: ["notes/specs/*.md"])
  @guidance(
    meaning: "Defines the intended world-state for a bounded change.",
    authoring: "Keep the document stable enough to freeze into an effort.",
    agentImplications: "Agents should preserve lifecycle semantics when editing."
  ) {
  """Lifecycle state for this spec."""
  status: SpecStatus!
    @guidance(
      meaning: "Controls whether the spec is still evolving or treated as fixed.",
      authoring: "Choose the narrowest state that matches current workflow reality.",
      agentImplications: "Use the enum value semantics when deciding whether edits are safe."
    )
    @policy(
      requiresUserConfirmation: true,
      reason: "Some statuses may require explicit approval before edits."
    )
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	spec := schema.Types["Spec"]
	require.NotNil(t, spec)
	require.Equal(t, "Normative spec note.", spec.Description)
	require.NotNil(t, spec.Guidance)
	require.Equal(t, "Normative spec note.", spec.Guidance.Summary)
	require.Contains(t, spec.Guidance.Meaning, "intended world-state")
	require.Contains(t, spec.Guidance.Authoring, "stable enough to freeze")
	require.Contains(t, spec.Guidance.AgentImplications, "preserve lifecycle semantics")

	status := spec.ByName["status"]
	require.NotNil(t, status)
	require.NotNil(t, status.Guidance)
	require.Equal(t, "Lifecycle state for this spec.", status.Guidance.Summary)
	require.Contains(t, status.Guidance.Meaning, "Controls whether the spec is still evolving")
	require.NotNil(t, status.Policy)
	require.True(t, status.Policy.RequiresUserConfirmation)
	require.Equal(t, "Some statuses may require explicit approval before edits.", status.Policy.Reason)

	EnumTypeForName := schema.EnumTypes["SpecStatus"]
	require.NotNil(t, EnumTypeForName)
	require.NotNil(t, EnumTypeForName.Guidance)
	require.Equal(t, "Spec lifecycle state.", EnumTypeForName.Guidance.Summary)
	require.Contains(t, EnumTypeForName.Guidance.Meaning, "delivery lifecycle")
	require.Len(t, EnumTypeForName.Values, 2)

	frozen := EnumTypeForName.ByName["frozen"]
	require.NotNil(t, frozen)
	require.NotNil(t, frozen.Guidance)
	require.Equal(t, "Frozen execution contract.", frozen.Guidance.Summary)
	require.Contains(t, frozen.Guidance.Authoring, "Avoid semantic edits")
	require.Contains(t, frozen.Guidance.AgentImplications, "confirm with a user")
	require.NotNil(t, frozen.Policy)
	require.True(t, frozen.Policy.RequiresUserConfirmation)
	require.True(t, frozen.Policy.ForbidAutonomousSemanticEdits)
	require.Equal(t, "Frozen specs should not be changed autonomously.", frozen.Policy.Reason)
}

func TestLoadSchema_CompilesNoteTypeMatches(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "decision.graphql"), []byte(`
type Decision @node(matches: ["tag:type/decision", "classification:decision"]) {
  name: String!
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	decision := schema.Types["Decision"]
	require.NotNil(t, decision)
	require.Empty(t, decision.Paths)
	require.Equal(t, []string{"tag:type/decision", "classification:decision"}, decision.Matches)
	require.Len(t, decision.Matchers, 2)
}
