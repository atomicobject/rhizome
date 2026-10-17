package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	requireCompileError(t, err, "section_relationship_binding")
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
	requireGraphQLCompileError(t, err)
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
	requireCompileError(t, err, "contains_interface_target")
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
