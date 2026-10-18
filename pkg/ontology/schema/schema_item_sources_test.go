package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	require.True(t, actionItem.SourceMatchers[0].Matches(&Subject{Path: "notes/work/team/today.md"}))
	require.False(t, actionItem.SourceMatchers[0].Matches(&Subject{Path: "notes/personal/today.md"}))
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
	requireCompileError(t, err, "source_node_role")
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
	requireCompileError(t, err, "source_marker")
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
	requireCompileError(t, err, "contains_item_list")
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
	requireCompileError(t, err, "contains_item_marker")
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
