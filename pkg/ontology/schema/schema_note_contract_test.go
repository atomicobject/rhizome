package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	require.True(t, TypeMatchesOrImplements(schema, "Project", "Note"))
	require.True(t, TypeMatchesOrImplements(schema, "Scratch", "Note"))
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
	requireCompileError(t, err, "reserved_note_type")
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
	requireCompileError(t, err, "note_field_required")
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
	requireCompileError(t, err, "default_node_selectors")
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
	requireCompileError(t, err, "multiple_default_types")
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
	requireCompileError(t, err, "scalar_relation_directive")
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
	requireCompileError(t, err, "interface_workspace_member")
}
