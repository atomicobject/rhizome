package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	requireCompileError(t, err, "multiple_preferred_identifiers")
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
	requireCompileError(t, err, "identifier_list")
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
	requireCompileError(t, err, "identifier_field_type")
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
	requireCompileError(t, err, "identifier_frontmatter_source")
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
	requireCompileError(t, err, "identifier_datetime_pad")
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
	requireCompileError(t, err, "identifier_namespace")
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
	requireCompileError(t, err, "identifier_namespace")
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
	requireCompileError(t, err, "identifier_strategy")
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
	requireCompileError(t, err, "identifier_prefix")
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
	requireCompileError(t, err, "identifier_pad")
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
