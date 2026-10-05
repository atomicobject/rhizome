package ontology

import (
	"os"
	"path/filepath"
	"strings"
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
	require.ErrorContains(t, err, "invalid @view directive on WorkStatus.ready")
	require.ErrorContains(t, err, "tone must be one of neutral, info, progress, success, warning, risk, or muted")
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

	enumType := schema.EnumTypes["SpecStatus"]
	require.NotNil(t, enumType)
	require.NotNil(t, enumType.Guidance)
	require.Equal(t, "Spec lifecycle state.", enumType.Guidance.Summary)
	require.Contains(t, enumType.Guidance.Meaning, "delivery lifecycle")
	require.Len(t, enumType.Values, 2)

	frozen := enumType.ByName["frozen"]
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

func TestLoadSchema_AllowsAuthoredNoteInterfaceAsUniversalNoteFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
interface Note {
  summary: String
  tags: [String!]
}

type Project @node(paths: ["projects/*.md"]) {
  name: String!
}

type Scratch @node(paths: ["scratch/*.md"]) {
  title: String
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.True(t, schema.NoteInterfaceAuthored)
	require.Contains(t, schema.Interfaces, "Note")

	project := schema.Types["Project"]
	require.NotNil(t, project)
	require.NotNil(t, project.ByName["summary"])
	require.False(t, project.ByName["summary"].Required)
	require.NotNil(t, project.ByName["tags"])
	require.True(t, project.ByName["tags"].List)

	scratch := schema.Types["Scratch"]
	require.NotNil(t, scratch)
	require.NotNil(t, scratch.ByName["summary"])
	require.False(t, scratch.ByName["summary"].Required)
	require.True(t, typeMatchesOrImplements(schema, "Project", "Note"))
	require.True(t, typeMatchesOrImplements(schema, "Scratch", "Note"))
}

func TestLoadSchema_AuthoredNoteInterfaceFieldsExtendObjectExtensions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
interface Note {
  summary: String
}

type Project {
  name: String!
}

extend type Project @node(paths: ["projects/*.md"]) {
  owner: String
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	project := schema.Types["Project"]
	require.NotNil(t, project)
	require.NotNil(t, project.ByName["summary"])
	require.NotNil(t, project.ByName["owner"])
}

func TestLoadSchema_RejectsConcreteNoteType(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type Note @node(paths: ["notes/*.md"]) {
  title: String
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "interface Note")
	require.Contains(t, err.Error(), "default")
}

func TestLoadSchema_RejectsRequiredNoteInterfaceFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
interface Note {
  summary: String!
}

type Project implements Note @node(paths: ["projects/*.md"]) {
  name: String!
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Note")
	require.Contains(t, err.Error(), "nullable")
}

func TestLoadSchema_CompilesSingleSelectorFreeDefaultNoteType(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type GeneralNote @node(default: true) {
  title: String
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, "GeneralNote", schema.DefaultNoteType)
	require.True(t, schema.Types["GeneralNote"].Default)
	require.Empty(t, schema.Types["GeneralNote"].Paths)
	require.Empty(t, schema.Types["GeneralNote"].Matches)
}

func TestLoadSchema_RejectsDefaultNoteWithSelectors(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type GeneralNote @node(default: true, paths: ["notes/*.md"]) {
  title: String
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "default")
	require.Contains(t, err.Error(), "paths")
}

func TestLoadSchema_RejectsMultipleDefaultNoteTypes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type GeneralNote @node(default: true) {
  title: String
}

type OtherNote @node(default: true) {
  title: String
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "default")
	require.Contains(t, err.Error(), "GeneralNote")
	require.Contains(t, err.Error(), "OtherNote")
}

func TestLoadSchema_CompilesNeighborFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "graph.graphql"), []byte(`
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	project := schema.Types["Project"]
	require.NotNil(t, project)
	require.Equal(t, FieldKindNeighbor, project.ByName["decisions"].Kind)
	require.Equal(t, NeighborDirectionBoth, project.ByName["decisions"].Direction)
}

func TestLoadSchema_CapturesDescriptionsAndContextInclude(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docs.graphql"), []byte(`
"""
Design note.

Captures a decision and the constraints it imposes.
"""
type Design @node(paths: ["notes/design/*.md"]) {
  """Primary heading shown in the vault."""
  name: String!

  """
  Decisions that this design depends on.
  Pull these when drafting or reviewing.
  """
  decisions: [Decision!] @link(inverse: "designs", contextInclude: true)

  """Neighbor runbooks for operational context."""
  runbooks: [Runbook!] @neighbors(direction: BOTH, type: "Runbook", contextInclude: true)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
  designs: [Design!] @link(inverse: "decisions")
}

type Runbook @node(paths: ["notes/runbooks/*.md"]) {
  name: String!
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	design := schema.Types["Design"]
	require.NotNil(t, design)
	require.Contains(t, design.Description, "Design note.")
	require.Contains(t, design.Description, "Captures a decision")

	nameField := design.ByName["name"]
	require.NotNil(t, nameField)
	require.Equal(t, "Primary heading shown in the vault.", strings.TrimSpace(nameField.Description))
	require.False(t, nameField.ContextInclude)

	decisions := design.ByName["decisions"]
	require.NotNil(t, decisions)
	require.Contains(t, decisions.Description, "Decisions that this design depends on.")
	require.Contains(t, decisions.Description, "Pull these when drafting")
	require.True(t, decisions.ContextInclude)

	runbooks := design.ByName["runbooks"]
	require.NotNil(t, runbooks)
	require.Equal(t, "Neighbor runbooks for operational context.", strings.TrimSpace(runbooks.Description))
	require.True(t, runbooks.ContextInclude)
}

func TestLoadSchema_CompilesDisplayHierarchyAndWorkspaceMembers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workspace.graphql"), []byte(`
interface Effort @display(singular: "Effort", plural: "Efforts", group: "Delivery") {
  name: String!
}

type Material @node(paths: ["materials/*.md"])
  @display(singular: "Material", plural: "Materials", parent: "Workspace") {
  name: String!
}

type Workspace implements Effort @node(paths: ["efforts/*.md"])
  @display(singular: "Workspace", plural: "Workspaces") {
  name: String!
  plan: Material! @link @workspaceMember(label: "Implementation plan")
  materials: [Material!] @link @workspaceMember
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, "Delivery", schema.Interfaces["Effort"].DisplayGroup)
	require.Equal(t, "Workspace", schema.Types["Material"].DisplayParent)
	require.Equal(t, "Workspaces", schema.Types["Workspace"].PluralLabel)
	require.True(t, schema.Types["Workspace"].ByName["plan"].WorkspaceMember)
	require.Equal(t, "Implementation plan", schema.Types["Workspace"].ByName["plan"].WorkspaceMemberLabel)
	require.True(t, schema.Types["Workspace"].ByName["materials"].WorkspaceMember)
	require.Empty(t, schema.Types["Workspace"].ByName["materials"].WorkspaceMemberLabel)
}

func TestLoadSchema_RejectsWorkspaceMemberOnNonLinkField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Workspace @node(paths: ["efforts/*.md"]) {
  name: String! @workspaceMember(label: "Overview")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot declare @link, @neighbors, or @workspaceMember")
}

func TestLoadSchema_RejectsWorkspaceMemberOnInterfaceField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
interface Workspace {
  plan: Material! @link @workspaceMember(label: "Implementation plan")
}

type Effort implements Workspace @node(paths: ["efforts/*.md"]) {
  plan: Material! @link
}

type Material @node(paths: ["materials/*.md"]) {
  name: String!
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "interface field Workspace.plan cannot declare @workspaceMember")
}

func TestLoadSchema_CompilesInterfacesAndSectionTypes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
interface SummaryDoc @display(singular: "Summary document", plural: "Summary documents") {
  summary: String!
}

type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) @display(singular: "Spec", plural: "Specs") {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Contains(t, schema.Interfaces, "SummaryDoc")
	require.Equal(t, TypeRoleInterface, schema.Interfaces["SummaryDoc"].Role)
	require.Equal(t, "Summary document", schema.Interfaces["SummaryDoc"].Label)
	require.Equal(t, "Summary documents", schema.Interfaces["SummaryDoc"].PluralLabel)
	require.Equal(t, []string{"SummaryDoc"}, schema.Types["Spec"].Implements)
	require.Equal(t, "Spec", schema.Types["Spec"].Label)
	require.Equal(t, "Specs", schema.Types["Spec"].PluralLabel)
	require.Equal(t, TypeRoleSection, schema.Types["RequirementsSection"].Role)
	require.Equal(t, FieldKindSection, schema.Types["Spec"].ByName["requirements"].Kind)
	require.Equal(t, SectionLevelH2, schema.Types["Spec"].ByName["requirements"].SectionLevel)
	require.Equal(t, "Requirements", schema.Types["Spec"].ByName["requirements"].SectionHeading)
	require.True(t, schema.Types["Spec"].ByName["requirements"].SectionRequired)
	require.Equal(t, NeighborScopeSubtree, schema.Types["RequirementsSection"].ByName["decisions"].Scope)
}

func TestLoadSchema_CompilesEmbeddedNodeTypes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "embedded.graphql"), []byte(`
type EffortNote @node(paths: ["notes/efforts/*.md"]) {
  summary: String!
}

type UserStory implements Section @node(locator: EMBEDDED) {
  storyId: String! @field
  efforts: [EffortNote!] @neighbors(direction: INBOUND, type: "EffortNote", scope: SUBTREE)
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, TypeRoleEmbeddedNode, schema.Types["UserStory"].Role)
	require.Equal(t, TypeRoleSection, schema.Types["UserStoriesSection"].Role)
	require.Equal(t, FieldKindNeighbor, schema.Types["UserStory"].ByName["efforts"].Kind)
	require.Equal(t, "EffortNote", schema.Types["UserStory"].ByName["efforts"].TypeName)
	require.Equal(t, NeighborDirectionInbound, schema.Types["UserStory"].ByName["efforts"].Direction)
}

func TestLoadSchema_RejectsStructuralSectionLinkFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Effort @node(paths: ["notes/efforts/*.md"]) {
  summary: String!
}

type UserStoriesSection implements Section {
  effort: Effort @link
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must use @contains or @neighbors")
}

func TestLoadSchema_RejectsInterfaceContractViolations(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
interface SummaryDoc {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must have type String!")
}

func TestLoadSchema_AllowsInboundSectionNeighbors(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: INBOUND, type: "Decision", scope: SUBTREE)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.NotNil(t, schema)
	require.Equal(t, NeighborDirectionInbound, schema.Types["RequirementsSection"].ByName["decisions"].Direction)
	require.Equal(t, NeighborScopeSubtree, schema.Types["RequirementsSection"].ByName["decisions"].Scope)
}

func TestLoadSchema_RejectsInterfaceSectionTargets(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
interface RequirementsContract {
  title: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsContract @contains(level: H2, heading: "Requirements", required: true)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot target interface RequirementsContract")
}

func TestLoadSchema_AllowsExplicitSectionBuiltins(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "section.graphql"), []byte(`
type RequirementsSection implements Section {
  title: String!
  content: String!
  children(first: Int = 20): [Section!]!
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, TypeRoleSection, schema.Types["RequirementsSection"].Role)
	require.Equal(t, NeighborScopeSubtree, schema.Types["RequirementsSection"].ByName["decisions"].Scope)
}

func TestLoadSchema_AllowsSectionTypeCompiledAfterNoteType(t *testing.T) {
	// Regression: when a note type sorts alphabetically before its section type,
	// the section type is not yet in schema.Types when the note's fields are
	// validated, causing a false "must target Section or a type that implements
	// Section" error. Admission must use the complete SDL rather than the
	// partially compiled schema.
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	// "ANote" sorts before "ZSection" alphabetically, so ANote is compiled first.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ordering.graphql"), []byte(`
type ZSection implements Section {
  title: String!
  content: String!
  children(first: Int = 20): [Section!]!
}

type ANote @node(paths: ["notes/a/*.md"]) {
  name: String!
  body: ZSection @contains(level: H2, heading: "Body")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, TypeRoleSection, schema.Types["ZSection"].Role)
	require.Equal(t, FieldKindSection, schema.Types["ANote"].ByName["body"].Kind)
}

func TestLoadSchema_CompilesListSectionFieldWithoutHeading(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stories.graphql"), []byte(`
type Story implements Section {
  title: String!
  content: String!
  children(first: Int = 20): [Section!]!
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	field := schema.Types["Spec"].ByName["stories"]
	require.NotNil(t, field)
	require.Equal(t, FieldKindSection, field.Kind)
	require.True(t, field.List)
	require.Equal(t, SectionLevelH3, field.SectionLevel)
	require.Empty(t, field.SectionHeading)
}

func TestLoadSchema_CompilesListSectionFieldWithHeading(t *testing.T) {
	// A list-typed section field with an explicit heading matches every section
	// in the scope that has that heading at the given level. This is the
	// "repeated heading" form (e.g. multiple `## Requirements` sections).
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stories.graphql"), []byte(`
type Story implements Section {
  title: String!
  content: String!
  children(first: Int = 20): [Section!]!
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3, heading: "Story")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	field := schema.Types["Spec"].ByName["stories"]
	require.NotNil(t, field)
	require.Equal(t, FieldKindSection, field.Kind)
	require.True(t, field.List)
	require.Equal(t, SectionLevelH3, field.SectionLevel)
	require.Equal(t, "Story", field.SectionHeading)
}

func TestLoadSchema_CompilesCheckboxItemContainsField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "actions.graphql"), []byte(`
type ActionItem implements Section @node(locator: EMBEDDED) {
  id: ID!
  notePath: String!
  title: String!
  level: SectionLevel!
  content: String!
  children(first: Int = 20): [Section!]!
  text: String @field
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	field := schema.Types["Conversation"].ByName["actionItems"]
	require.NotNil(t, field)
	require.Equal(t, FieldKindSection, field.Kind)
	require.True(t, field.List)
	require.Equal(t, EmbeddedSourceShapeCheckboxItem, field.EmbeddedSourceShape)
	require.Equal(t, "#action-item", field.EmbeddedSourceMarker)
	require.Empty(t, field.SectionLevel)
	require.Empty(t, field.SectionHeading)
}

func TestLoadSchema_CompilesGlobalItemSourceOnEmbeddedNode(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "actions.graphql"), []byte(`
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  id: ID!
  notePath: String!
  title: String!
  level: SectionLevel!
  content: String!
  children(first: Int = 20): [Section!]!
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	actionItem := schema.Types["ActionItem"]
	require.NotNil(t, actionItem)
	require.Equal(t, EmbeddedSourceShapeCheckboxItem, actionItem.SourceShape)
	require.Equal(t, "#action-item", actionItem.SourceMarker)
}

func TestLoadSchema_CompilesGlobalSourcePathsToMatchers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "actions.graphql"), []byte(`
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/work/**/*.md"]) {
  id: ID!
  notePath: String!
  title: String!
  level: SectionLevel!
  content: String!
  children(first: Int = 20): [Section!]!
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	actionItem := schema.Types["ActionItem"]
	require.NotNil(t, actionItem)
	require.Equal(t, []string{"notes/work/**/*.md"}, actionItem.SourcePaths)
	require.Len(t, actionItem.SourceMatchers, 1)
	require.True(t, actionItem.SourceMatchers[0].matches(&noteDoc{Path: "notes/work/team/today.md"}))
	require.False(t, actionItem.SourceMatchers[0].matches(&noteDoc{Path: "notes/personal/today.md"}))
}

func TestLoadSchema_RejectsGlobalSourceOnFileBackedNode(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type DailyNote @node(paths: ["notes/*.md"]) @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  title: String!
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "@source")
	require.Contains(t, err.Error(), "embedded")
}

func TestLoadSchema_RejectsGlobalItemSourceWithoutMarker(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM) {
  id: ID!
  notePath: String!
  title: String!
  level: SectionLevel!
  content: String!
  children(first: Int = 20): [Section!]!
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "@source")
	require.Contains(t, err.Error(), "marker")
}

func TestLoadSchema_RejectsSingularItemContainsField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type ActionItem implements Section @node(locator: EMBEDDED) {
  id: ID!
  notePath: String!
  title: String!
  level: SectionLevel!
  content: String!
  children(first: Int = 20): [Section!]!
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
  actionItem: ActionItem @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "item-shaped contains field")
	require.Contains(t, err.Error(), "must be a list")
}

func TestLoadSchema_RejectsItemContainsFieldWithoutMarker(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type ActionItem implements Section @node(locator: EMBEDDED) {
  id: ID!
  notePath: String!
  title: String!
  level: SectionLevel!
  content: String!
  children(first: Int = 20): [Section!]!
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "item-shaped contains field")
	require.Contains(t, err.Error(), "must declare marker")
}

func TestLoadSchema_AllowsSectionScopedListItemContainsWithoutMarker(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  acTitle: String @field(sourceKind: ITEM_TITLE)
  summary: String! @field(sourceKind: ITEM_SUMMARY)
  detail: String @field(sourceKind: ITEM_DETAIL)
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(shape: LIST_ITEM)
}

type Spec @node(paths: ["specs/*.md"]) {
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H2, heading: "Acceptance Criteria")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	field := schema.Types["AcceptanceCriteriaSection"].ByName["criteria"]
	require.Equal(t, EmbeddedSourceShapeListItem, field.EmbeddedSourceShape)
	require.Empty(t, field.EmbeddedSourceMarker)
	require.Equal(t, FieldSourceItemTitle, schema.Types["AcceptanceCriterion"].ByName["acTitle"].SourceKind)
	require.Equal(t, FieldSourceItemSummary, schema.Types["AcceptanceCriterion"].ByName["summary"].SourceKind)
	require.Equal(t, FieldSourceItemDetail, schema.Types["AcceptanceCriterion"].ByName["detail"].SourceKind)
}

func TestLoadSchema_CompilesValidationAnnotations(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, `
enum StoryStatus { ready satisfied }

type Criterion implements Section @node(locator: EMBEDDED) {
  summary: String! @field(sourceKind: ITEM_SUMMARY) @authoring(style: ITEM_TEXT) @format(notPattern: "^$")
}

type CriteriaSection implements Section {
  criteria: [Criterion!] @contains(shape: LIST_ITEM, min: 1, max: 5)
}

type Story
  implements Section
  @node(locator: EMBEDDED)
  @title(pattern: "^US[0-9]+ - .+", notPattern: "(?i)^As an?\\b")
  @requiresWhen(field: "status", equals: "satisfied", require: [{ field: "criteria" }]) {
  id: ID! @field @authoring(style: LIST_METADATA)
  status: StoryStatus! @field @authoring(style: LIST_METADATA)
  criteria: CriteriaSection @contains(level: H4, heading: "Acceptance Criteria", required: true)
}

type Stories implements Section {
  stories: [Story!] @contains(level: H3, min: 1)
}

type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @format(pattern: "^SPEC-[0-9]{4}$")
  stories: Stories @contains(level: H2, heading: "Stories", required: true)
}
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	story := schema.Types["Story"]
	require.NotNil(t, story.Title)
	require.Equal(t, "^US[0-9]+ - .+", story.Title.Pattern)
	require.Equal(t, "(?i)^As an?\\b", story.Title.NotPattern)
	require.Len(t, story.RequiresWhen, 1)
	require.Equal(t, FieldAuthoringStyleListMetadata, story.ByName["id"].AuthoringStyle)

	criteria := schema.Types["CriteriaSection"].ByName["criteria"]
	require.Equal(t, 1, criteria.ContainsMin)
	require.Equal(t, 5, criteria.ContainsMax)
	require.Equal(t, FieldAuthoringStyleItemText, schema.Types["Criterion"].ByName["summary"].AuthoringStyle)
	require.NotNil(t, schema.Types["Spec"].ByName["id"].Format)
}

func TestLoadSchema_RejectsInvalidValidationAnnotationUse(t *testing.T) {
	cases := []struct {
		name    string
		schema  string
		message string
	}{
		{
			name: "invalid title regex",
			schema: `
type Story implements Section @node(locator: EMBEDDED) @title(pattern: "[") {
  status: String @field
}
type Spec @node(paths: ["specs/*.md"]) { story: Story @contains(level: H2, heading: "Story") }
`,
			message: "invalid @title directive",
		},
		{
			name: "contains max below min",
			schema: `
type Child implements Section {}
type Spec @node(paths: ["specs/*.md"]) {
  children: [Child!] @contains(level: H2, min: 2, max: 1)
}
`,
			message: "max must be >= min",
		},
		{
			name: "list metadata on note field",
			schema: `
type Spec @node(paths: ["specs/*.md"]) {
  id: String @field @authoring(style: LIST_METADATA)
}
`,
			message: "requires a section or embedded-node field",
		},
		{
			name: "format on link",
			schema: `
type Person @node(paths: ["people/*.md"]) {
  manager: Person @link @format(pattern: ".+")
}
`,
			message: "cannot declare @format",
		},
		{
			name: "requires unknown field",
			schema: `
enum Status { active archived }
type Spec @node(paths: ["specs/*.md"]) @requiresWhen(field: "status", equals: "archived", require: [{ field: "successor" }]) {
  status: Status
}
`,
			message: "required field \"successor\" is not declared",
		},
		{
			name: "requires enum value outside enum",
			schema: `
enum Status { active archived }
type Spec @node(paths: ["specs/*.md"]) @requiresWhen(field: "status", equals: "done", require: [{ field: "status" }]) {
  status: Status
}
`,
			message: "is not a member of enum Status",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, tc.schema)
			_, err := LoadSchema(root)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.message)
		})
	}
}

func TestLoadSchema_RejectsItemSourceFieldUnlessSingularString(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  detail: [String!] @field(sourceKind: ITEM_DETAIL)
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(shape: LIST_ITEM)
}

type Spec @node(paths: ["specs/*.md"]) {
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H2, heading: "Acceptance Criteria")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "item-source field")
	require.Contains(t, err.Error(), "singular String")
}

func TestLoadSchema_RejectsSingularSectionFieldWithoutHeading(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Story implements Section {
  title: String!
  content: String!
  children(first: Int = 20): [Section!]!
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  story: Story @contains(level: H3)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "singular contains field")
	require.Contains(t, err.Error(), "must declare a heading")
}

func TestLoadSchema_CompilesSectionTypeScalarFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stories.graphql"), []byte(`
enum StoryStatus { PLANNED IN_PROGRESS COMPLETE }

type Story implements Section {
  status: StoryStatus @field
  owner: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	story := schema.Types["Story"]
	require.NotNil(t, story)
	require.Equal(t, TypeRoleSection, story.Role)

	status := story.ByName["status"]
	require.NotNil(t, status)
	require.Equal(t, FieldKindEnum, status.Kind)
	require.Equal(t, FieldSourceInline, status.SourceKind)
	require.Empty(t, status.Source, "section scalar field Source is resolved lazily at use site")

	owner := story.ByName["owner"]
	require.NotNil(t, owner)
	require.Equal(t, FieldKindScalar, owner.Kind)
	require.Equal(t, FieldSourceInline, owner.SourceKind)
	require.Empty(t, owner.Source)
}

func TestLoadSchema_RejectsSectionTypeFieldWithFrontmatterSource(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Story implements Section {
  status: String @field(sourceKind: FRONTMATTER)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot declare sourceKind")
}

func TestLoadSchema_AllowsSectionTypeFieldWithExplicitInlineSourceKind(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
type Story implements Section {
  status: String @field(sourceKind: INLINE)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, FieldSourceInline, schema.Types["Story"].ByName["status"].SourceKind)
}

func TestLoadSchema_RejectsSectionTypeFieldWithExplicitSource(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Story implements Section {
  status: String @field(source: "Status")
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot override @field source")
}

func TestLoadSchema_RejectsSectionTypeFieldWithoutFieldDirective(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Story implements Section {
  status: String
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must declare @field, @contains, or @neighbors")
}

func TestLoadSchema_CompilesPreviewOnSectionType(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stories.graphql"), []byte(`
enum StoryStatus { PLANNED IN_PROGRESS COMPLETE }

type Story implements Section @preview(template: "[[{{title}}]] - [[{{status}}]]", collapsed: true) {
  status: StoryStatus @field
  owner: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	story := schema.Types["Story"]
	require.NotNil(t, story)
	require.NotNil(t, story.Preview)
	require.Equal(t, "[[{{title}}]] - [[{{status}}]]", story.Preview.Template)
	require.Equal(t, []string{"title", "status"}, story.Preview.Placeholders)
	require.True(t, story.Preview.Collapsed)
}

func TestLoadSchema_PreviewDefaultsCollapsedTrue(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stories.graphql"), []byte(`
type Story implements Section @preview(template: "{{title}}") {
  owner: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.True(t, schema.Types["Story"].Preview.Collapsed)
}

// Regression: the section-prelude injector used to require `implements
// Section` and the opening `{` on the same line. Splitting the header across
// lines (a natural formatting for long directives like `@preview`) would
// silently skip the synthetic builtin-field injection and trip gqlparser's
// interface-conformance check instead. Multi-line headers must work.
func TestLoadSchema_CompilesMultiLineSectionHeader(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stories.graphql"), []byte(`
type Story
  implements Section
  @preview(template: "{{title}} · {{status}}", collapsed: true) {
  status: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	story := schema.Types["Story"]
	require.NotNil(t, story)
	require.NotNil(t, story.Preview)
	require.Equal(t, "{{title}} · {{status}}", story.Preview.Template)
}

func TestLoadSchema_RejectsPreviewOnNoteType(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) @preview(template: "{{title}}") {
  summary: String!
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "only valid on section-role types")
}

func TestLoadSchema_RejectsPreviewUnknownPlaceholder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Story implements Section @preview(template: "{{title}} · {{nonexistent}}") {
  owner: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown placeholder")
}

func TestLoadSchema_RejectsPreviewEmptyTemplate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Story implements Section @preview(template: "") {
  owner: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-empty template")
}

func TestLoadSchema_CompilesInterfaceInheritance(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "interfaces.graphql"), []byte(`
interface Entity {
  summary: String!
}

interface SummaryDoc implements Entity {
  summary: String!
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, []string{"Entity"}, schema.Interfaces["SummaryDoc"].Implements)
}

func TestLoadSchema_RejectsObjectMissingInheritedInterface(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.graphql"), []byte(`
interface Entity {
  summary: String!
}

interface SummaryDoc implements Entity {
  summary: String!
}

type Project implements SummaryDoc @node(paths: ["notes/projects/*.md"]) {
  summary: String!
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must implement Entity")
}

func TestLoadSchema_RejectsUnsupportedDefinitions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
input EntityInput {
  id: ID
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported GraphQL definition kind")
}

func TestLoadSchema_PreservesRetrievalAndTraversalAnnotations(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.graphql"), []byte(`
type Project
  @node(paths: ["notes/projects/*.md"])
  @retrieval(intents: ["search"], boost: 1.2, relationBoost: 1.1)
  @traversal(intents: ["related_to_seed"], includeAmbient: true, minStructuralHits: 2) {
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision") @retrieval(intents: ["docs_for_code"], propertyBoost: 1.3)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	project := schema.Types["Project"]
	require.NotNil(t, project)
	require.NotNil(t, project.Annotations["retrieval"])
	require.InDelta(t, 1.2, project.Annotations["retrieval"]["boost"], 0.0001)
	require.Equal(t, true, project.Annotations["traversal"]["includeAmbient"])
	require.EqualValues(t, 2, project.Annotations["traversal"]["minStructuralHits"])
	require.InDelta(t, 1.3, project.ByName["decisions"].Annotations["retrieval"]["propertyBoost"], 0.0001)
}

func TestLoadSchema_DefaultsPropertyNamesToKebabCaseAndSupportsOverrides(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.graphql"), []byte(`
type Project @node(paths: ["notes/projects/*.md"]) {
  openQuestions: [OpenQuestion!] @link(inverse: "project")
  ownerName: String
  legacyStatus: String @field(sources: ["legacy-status", "legacyStatus"])
}

type OpenQuestion @node(paths: ["notes/questions/*.md"]) {
  project: Project! @link(inverse: "openQuestions")
}

type CamelProject @node(paths: ["notes/camel/*.md"], propertyCase: CAMEL) {
  releaseStage: String
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	project := schema.Types["Project"]
	require.NotNil(t, project)
	require.Equal(t, PropertyCaseKebab, project.PropertyCase)
	require.Equal(t, "open-questions", project.ByName["openQuestions"].Source)
	require.Equal(t, "owner-name", project.ByName["ownerName"].Source)
	require.Equal(t, "legacy-status", project.ByName["legacyStatus"].Source)
	require.Equal(t, []string{"legacyStatus"}, project.ByName["legacyStatus"].SourceAliases)

	camel := schema.Types["CamelProject"]
	require.NotNil(t, camel)
	require.Equal(t, PropertyCaseCamel, camel.PropertyCase)
	require.Equal(t, "releaseStage", camel.ByName["releaseStage"].Source)
}

func TestLoadSchema_PreservesAuthoredEmbeddedIDField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "embedded.graphql"), []byte(`
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  status: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  stories: [UserStory!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	story := schema.Types["UserStory"]
	require.NotNil(t, story)
	require.Contains(t, story.ByName, "id")
	require.Equal(t, FieldSourceInline, story.ByName["id"].SourceKind)
}

func TestLoadSchema_CompilesCompanionDocs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.graphql"), []byte(`
type Project
  @node(paths: ["notes/projects/*.md"])
  @companionDocs(paths: ["docs/reference/guides/Project workflow.md"], purpose: "workflow") {
  name: String!
  summary: String!
  owner: Person! @link(inverse: "projects") @companionDocs(paths: ["docs/reference/guides/Owner expectations.md"], purpose: "field guidance")
}

type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
  projects: [Project!] @link(inverse: "owner")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	project := schema.Types["Project"]
	require.NotNil(t, project)
	require.Len(t, project.CompanionDocs, 1)
	require.Equal(t, "docs/reference/guides/Project workflow.md", project.CompanionDocs[0].Path)
	require.Equal(t, "workflow", project.CompanionDocs[0].Purpose)

	owner := project.ByName["owner"]
	require.NotNil(t, owner)
	require.Len(t, owner.CompanionDocs, 1)
	require.Equal(t, "docs/reference/guides/Owner expectations.md", owner.CompanionDocs[0].Path)
	require.Equal(t, "field guidance", owner.CompanionDocs[0].Purpose)
}

func TestLoadSchema_RejectsInvalidNeighborFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  decision: Decision @neighbors(direction: BOTH, type: "Decision")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a list")
}

func TestLoadSchema_RejectsInverseThatIsNotLinkField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Team @node(paths: ["teams/*.md"]) {
  name: String!
}

type Person @node(paths: ["people/*.md"]) {
  team: Team @link(inverse: "name")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must point to a @link field")
}

func TestLoadSchema_ReverseRequiresNamedAuthoredLink(t *testing.T) {
	for _, tc := range []struct{ name, field, source, message string }{
		{"valid", `evidence: [Evidence!] @reverse(field: "opportunities")`, `opportunities: [Opportunity!] @link`, ""},
		{"missing source", `evidence: [Evidence!] @reverse(field: "missing")`, `opportunities: [Opportunity!] @link`, "requires Evidence.missing"},
		{"wrong source kind", `evidence: [Evidence!] @reverse(field: "opportunities")`, `opportunities: [Opportunity!] @neighbors(direction: OUTBOUND, type: "Opportunity")`, "authored @link"},
		{"singular", `evidence: Evidence @reverse(field: "opportunities")`, `opportunities: [Opportunity!] @link`, "must be a list"},
		{"mixed contains", `evidence: [Evidence!] @contains(level: H2) @reverse(field: "opportunities")`, `opportunities: [Opportunity!] @link`, "cannot declare @reverse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".rhizome", "ontology")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			body := `type Opportunity @node(paths: ["opportunities/*.md"]) { ` + tc.field + ` }
type Evidence @node(paths: ["evidence/*.md"]) { ` + tc.source + ` }`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "reverse.graphql"), []byte(body), 0o644))
			_, err := LoadSchema(root)
			if tc.message == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.message)
			}
		})
	}
}

func TestLoadSchema_ReverseAcceptsEmbeddedSourcesCompiledLater(t *testing.T) {
	for _, tc := range []struct {
		name, target, declaration string
	}{
		{
			name:   "concrete embedded node",
			target: "ZEmbedded",
			declaration: `type ZEmbedded implements Section @node(locator: EMBEDDED) {
  parent: Parent @link
}`,
		},
		{
			name:   "Section-derived interface",
			target: "ZEmbeddedInterface",
			declaration: `interface ZEmbeddedInterface implements Section {
  parent: Parent @link
}
type ZEmbedded implements ZEmbeddedInterface & Section @node(locator: EMBEDDED) {
  parent: Parent @link
}`,
		},
		{
			name:   "note interface",
			target: "ZEvidence",
			declaration: `interface ZEvidence {
  parent: Parent @link
}
type ZEvidenceNote implements ZEvidence @node(paths: ["evidence/*.md"]) {
  parent: Parent @link
}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, `type Parent @node(paths: ["parents/*.md"]) {
  children: [`+tc.target+`!] @reverse(field: "parent")
}
`+tc.declaration)
			_, err := LoadSchema(root)
			require.NoError(t, err)
		})
	}
}

func TestLoadSchema_RejectsInverseWithWrongTargetType(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Project @node(paths: ["projects/*.md"]) {
  name: String
}

type Team @node(paths: ["teams/*.md"]) {
  project: Project @link
}

type Person @node(paths: ["people/*.md"]) {
  team: Team @link(inverse: "project")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.ErrorContains(t, err, "must target Person, got Project")
}

func TestLoadSchema_CompilesIdentifierDirective(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  specId: String @field @identifier(preferred: true)
  legacyId: String @field @identifier
  name: String!
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	spec := schema.Types["Spec"]
	require.NotNil(t, spec)

	specID := spec.ByName["specId"]
	require.NotNil(t, specID)
	require.True(t, specID.IsIdentifier)
	require.True(t, specID.IsPreferredIdentifier)

	legacy := spec.ByName["legacyId"]
	require.NotNil(t, legacy)
	require.True(t, legacy.IsIdentifier)
	require.False(t, legacy.IsPreferredIdentifier)

	name := spec.ByName["name"]
	require.NotNil(t, name)
	require.False(t, name.IsIdentifier)
	require.False(t, name.IsPreferredIdentifier)
}

func TestLoadSchema_IdentifierDirectiveRejectsDoublePreferred(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  specId: String @field @identifier(preferred: true)
  altId: String @field @identifier(preferred: true)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "more than one @identifier(preferred: true)")
}

func TestLoadSchema_IdentifierDirectiveRejectsListField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  ids: [String!] @field @identifier
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "@identifier")
	require.Contains(t, err.Error(), "list")
}

func TestLoadSchema_IdentifierDirectiveRejectsNonString(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  specNumber: Int @field @identifier
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "@identifier")
	require.Contains(t, err.Error(), "String")
}

func TestLoadSchema_IdentifierDirectiveRejectsInlineSource(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  specId: String @field(sourceKind: INLINE) @identifier
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "@identifier")
	require.Contains(t, err.Error(), "FRONTMATTER")
}

func TestLoadSchema_IdentifierDirectiveAllowsEmbeddedInlineField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}

type StorySection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  stories: StorySection @contains(level: H2, heading: "Stories")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	storyID := schema.Types["UserStory"].ByName["id"]
	require.NotNil(t, storyID)
	require.Equal(t, FieldSourceInline, storyID.SourceKind)
	require.True(t, storyID.IsIdentifier)
	require.True(t, storyID.IsPreferredIdentifier)
}

func TestLoadSchema_IdentifierFormatDefaults(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  specId: String @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	spec := schema.Types["Spec"].ByName["specId"]
	require.NotNil(t, spec.IdentifierFormat)
	require.Equal(t, IdentifierStrategySequential, spec.IdentifierFormat.Strategy)
	require.Equal(t, "SPEC", spec.IdentifierFormat.Prefix)
	require.Equal(t, "-", spec.IdentifierFormat.Separator)
	require.Equal(t, 4, spec.IdentifierFormat.Pad)
}

func TestLoadSchema_IdentifierStrategyExplicitSequential(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "SPEC")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	metadata := schema.Types["Spec"].ByName["id"].IdentifierFormat
	require.NotNil(t, metadata)
	require.Equal(t, IdentifierStrategySequential, metadata.Strategy)
}

func TestLoadSchema_IdentifierStrategyDateTime(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "effort.graphql"), []byte(`
type Effort @node(paths: ["efforts/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	metadata := schema.Types["Effort"].ByName["id"].IdentifierFormat
	require.NotNil(t, metadata)
	require.Equal(t, IdentifierStrategyDateTime, metadata.Strategy)
}

func TestLoadSchema_IdentifierStrategyDateTimeRejectsExplicitPad(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "effort.graphql"), []byte(`
type Effort @node(paths: ["efforts/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF", pad: 4)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pad")
	require.Contains(t, err.Error(), "DATETIME")
}

func TestLoadSchema_IdentifierNamespaceRejectsMixedStrategies(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "identifiers.graphql"), []byte(`
type SequentialEffort @node(paths: ["sequential/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "EFF")
}
type DateTimeEffort @node(paths: ["datetime/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "identifier namespace")
	require.Contains(t, err.Error(), "EFF-")
}

func TestLoadSchema_IdentifierNamespaceRejectsIncompatibleSequentialFormats(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "identifiers.graphql"), []byte(`
type ShortSpec @node(paths: ["short/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", pad: 4)
}
type LongSpec @node(paths: ["long/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", pad: 6)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "identifier namespace")
	require.Contains(t, err.Error(), "SPEC-")
}

func TestLoadSchema_IdentifierStrategyRejectsUnknownValue(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: RANDOM, prefix: "SPEC")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "IdentifierStrategy")
}

func TestLoadSchema_IdentifierFormatCustomArgs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Ticket @node(paths: ["tickets/*.md"]) {
  ticketId: String @field @identifier(preferred: true, prefix: "T", pad: 6, separator: "_")
  name: String!
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	ticket := schema.Types["Ticket"].ByName["ticketId"]
	require.NotNil(t, ticket.IdentifierFormat)
	require.Equal(t, "T", ticket.IdentifierFormat.Prefix)
	require.Equal(t, "_", ticket.IdentifierFormat.Separator)
	require.Equal(t, 6, ticket.IdentifierFormat.Pad)
	require.Equal(t, "T_000007", ticket.IdentifierFormat.Format(7))
	n, ok := ticket.IdentifierFormat.Parse("T_000042")
	require.True(t, ok)
	require.Equal(t, 42, n)
}

func TestLoadSchema_IdentifierFormatRejectsBadPrefix(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  specId: String @field @identifier(preferred: true, prefix: "spec")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prefix")
}

func TestLoadSchema_IdentifierFormatRejectsOutOfRangePad(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  specId: String @field @identifier(preferred: true, prefix: "SPEC", pad: 0)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pad")
}

func TestLoadSchema_IdentifierFormatOmittedKeepsLegacyShape(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) {
  specId: String @field @identifier(preferred: true)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	spec := schema.Types["Spec"].ByName["specId"]
	require.True(t, spec.IsPreferredIdentifier)
	require.Nil(t, spec.IdentifierFormat)
}
