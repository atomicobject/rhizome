package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
		name   string
		schema string
		code   string
	}{
		{
			name: "invalid title regex",
			schema: `
type Story implements Section @node(locator: EMBEDDED) @title(pattern: "[") {
  status: String @field
}
type Spec @node(paths: ["specs/*.md"]) { story: Story @contains(level: H2, heading: "Story") }
`,
			code: "invalid_pattern",
		},
		{
			name: "contains max below min",
			schema: `
type Child implements Section {}
type Spec @node(paths: ["specs/*.md"]) {
  children: [Child!] @contains(level: H2, min: 2, max: 1)
}
`,
			code: "contains_bounds",
		},
		{
			name: "list metadata on note field",
			schema: `
type Spec @node(paths: ["specs/*.md"]) {
  id: String @field @authoring(style: LIST_METADATA)
}
`,
			code: "authoring_list_metadata_owner",
		},
		{
			name: "format on link",
			schema: `
type Person @node(paths: ["people/*.md"]) {
  manager: Person @link @format(pattern: ".+")
}
`,
			code: "link_format",
		},
		{
			name: "requires unknown field",
			schema: `
enum Status { active archived }
type Spec @node(paths: ["specs/*.md"]) @requiresWhen(field: "status", equals: "archived", require: [{ field: "successor" }]) {
  status: Status
}
`,
			code: "required_when_unknown_field",
		},
		{
			name: "requires enum value outside enum",
			schema: `
enum Status { active archived }
type Spec @node(paths: ["specs/*.md"]) @requiresWhen(field: "status", equals: "done", require: [{ field: "status" }]) {
  status: Status
}
`,
			code: "required_when_trigger_enum",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, tc.schema)
			_, err := LoadSchema(root)
			require.Error(t, err)
			requireCompileError(t, err, tc.code)
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
	requireCompileError(t, err, "item_field_type")
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
	requireCompileError(t, err, "contains_heading")
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
	requireCompileError(t, err, "section_field_source_kind")
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
	requireCompileError(t, err, "section_field_source")
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
	requireCompileError(t, err, "section_field_binding")
}
