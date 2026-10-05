package ontology

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestDisplayDirectiveParsing(t *testing.T) {
	doc, err := gqlparser.LoadSchema(
		&ast.Source{Name: "prelude.graphql", BuiltIn: true, Input: ontologyPreludeInput(true)},
		&ast.Source{Name: "display.graphql", Input: `
type Entry @display(singular: "Entry", plural: "Entries", group: "Work", parent: "Root") {
  summary: String @display(role: SUMMARY, importance: KEY, hover: false)
  detail: String @display
}`})
	require.NoError(t, err)
	def := doc.Types["Entry"]
	require.NotNil(t, def)

	singular, plural, group, parent := displayPresentationFromDirective(def.Directives.ForName("display"))
	require.Equal(t, []string{"Entry", "Entries", "Work", "Root"}, []string{singular, plural, group, parent})
	require.Equal(t, FieldDisplay{
		Role: FieldDisplayRoleSummary, Importance: FieldImportanceKey, HideHover: true,
	}, fieldDisplayFromDirective(def.Fields.ForName("summary").Directives.ForName("display")))
	require.Equal(t, FieldDisplay{
		Importance: FieldImportanceNormal,
	}, fieldDisplayFromDirective(def.Fields.ForName("detail").Directives.ForName("display")))
	require.Equal(t, FieldDisplay{}, fieldDisplayFromDirective(nil))
	require.Equal(t, FieldImportanceNormal, (FieldDisplay{}).EffectiveImportance())
	singular, plural, group, parent = displayPresentationFromDirective(nil)
	require.Equal(t, []string{"", "", "", ""}, []string{singular, plural, group, parent})
}

func TestLoadSchemaDisplayValidation(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		message string
	}{
		{
			name: "duplicate summaries",
			body: `type Entry @node(paths: ["notes/*.md"]) {
  summary: String @display(role: SUMMARY)
  abstract: String @display(role: SUMMARY)
}`,
			message: "type Entry declares role SUMMARY on more than one field (Entry.summary and Entry.abstract)",
		},
		{
			name: "duplicate inherited summaries",
			body: `interface Described { abstract: String @display(role: SUMMARY) }
type Entry implements Described @node(paths: ["notes/*.md"]) {
  abstract: String
  summary: String @display(role: SUMMARY)
}`,
			message: "type Entry declares role SUMMARY on more than one field (Described.abstract and Entry.summary)",
		},
		{
			name:    "list summary",
			body:    `type Entry @node(paths: ["notes/*.md"]) { summary: [String!] @display(role: SUMMARY) }`,
			message: "field Entry.summary declares role SUMMARY but must be a singular String, ID, or URL scalar or a singular section @contains field",
		},
		{
			name:    "enum summary",
			body:    `enum State { active } type Entry @node(paths: ["notes/*.md"]) { state: State @display(role: SUMMARY) }`,
			message: "field Entry.state declares role SUMMARY but must be a singular String, ID, or URL scalar or a singular section @contains field",
		},
		{
			name:    "link summary",
			body:    `type Other @node(paths: ["other/*.md"]) { title: String } type Entry @node(paths: ["notes/*.md"]) { other: Other @link @display(role: SUMMARY) }`,
			message: "field Entry.other declares role SUMMARY but must be a singular String, ID, or URL scalar or a singular section @contains field",
		},
		{
			name:    "neighbor summary",
			body:    `type Other @node(paths: ["other/*.md"]) { title: String } type Entry @node(paths: ["notes/*.md"]) { others: [Other!] @neighbors(direction: OUTBOUND, type: "Other") @display(role: SUMMARY) }`,
			message: "field Entry.others declares role SUMMARY but must be a singular String, ID, or URL scalar or a singular section @contains field",
		},
		{
			name:    "contains display",
			body:    `type Part implements Section { detail: String @field } type Entry @node(paths: ["notes/*.md"]) { parts: [Part!] @contains(level: H2) @display(role: SUMMARY, importance: KEY) }`,
			message: "field Entry.parts declares role SUMMARY but must be a singular String, ID, or URL scalar or a singular section @contains field",
		},
		{
			name:    "type field argument",
			body:    `type Entry @node(paths: ["notes/*.md"]) @display(role: SUMMARY) { summary: String }`,
			message: "object Entry cannot declare field-level @display argument role",
		},
		{
			name:    "interface field argument",
			body:    `interface Described @display(hover: false) { summary: String } type Entry implements Described @node(paths: ["notes/*.md"]) { summary: String }`,
			message: "interface Described cannot declare field-level @display argument hover",
		},
		{
			name:    "field type argument",
			body:    `type Entry @node(paths: ["notes/*.md"]) { summary: String @display(singular: "Summary") }`,
			message: "field Entry.summary cannot declare type-level @display argument singular",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, tc.body)
			_, err := LoadSchema(root)
			require.ErrorContains(t, err, tc.message)
			require.Contains(t, err.Error(), "schema.graphql:")
		})
	}
}

func TestLoadSchemaParentRolePlacement(t *testing.T) {
	accepted := []struct {
		name     string
		body     string
		typeName string
		field    string
	}{
		{
			name:     "self link",
			body:     `type Area @node(paths: ["areas/*.md"]) { parent: Area @link @display(role: PARENT) }`,
			typeName: "Area", field: "parent",
		},
		{
			name: "implemented interface",
			body: `interface Grouping { title: String }
type Area implements Grouping @node(paths: ["areas/*.md"]) { title: String parent: Grouping @link @display(role: PARENT) }`,
			typeName: "Area", field: "parent",
		},
		{
			name: "interface targets parent interface",
			body: `interface Node2 { title: String }
interface Grouping implements Node2 { title: String within: Node2 @link @display(role: PARENT) }
type Area implements Grouping & Node2 @node(paths: ["areas/*.md"]) { title: String within: Node2 @link }`,
			typeName: "Area", field: "within",
		},
		{
			name: "inherited from interface",
			body: `interface Grouping { within: Grouping @link @display(role: PARENT) }
type Area implements Grouping @node(paths: ["areas/*.md"]) { within: Grouping @link }`,
			typeName: "Area", field: "within",
		},
	}
	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, tc.body)
			schema, err := LoadSchema(root)
			require.NoError(t, err)
			require.Same(t, schema.Types[tc.typeName].ByName[tc.field], ParentField(schema.Types[tc.typeName].Fields))
		})
	}

	rejected := []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "other type",
			body:    `type Other @node(paths: ["other/*.md"]) { title: String } type Area @node(paths: ["areas/*.md"]) { parent: Other @link @display(role: PARENT) }`,
			message: "field Area.parent declares role PARENT but targets Other; it must target Area or an interface Area implements",
		},
		{
			name:    "any note",
			body:    `type Area @node(paths: ["areas/*.md"]) { parent: Note @link @display(role: PARENT) }`,
			message: "field Area.parent declares role PARENT but targets Note",
		},
		{
			name:    "list link",
			body:    `type Area @node(paths: ["areas/*.md"]) { parents: [Area!] @link @display(role: PARENT) }`,
			message: "field Area.parents declares role PARENT but must be a single-valued @link field",
		},
		{
			name:    "scalar",
			body:    `type Area @node(paths: ["areas/*.md"]) { parent: String @display(role: PARENT) }`,
			message: "field Area.parent declares role PARENT but must be a single-valued @link field",
		},
		{
			name:    "neighbors",
			body:    `type Area @node(paths: ["areas/*.md"]) { near: [Area!] @neighbors(direction: OUTBOUND, type: "Area") @display(role: PARENT) }`,
			message: "field Area.near declares role PARENT but must be a single-valued @link field",
		},
		{
			name: "two parents",
			body: `type Area @node(paths: ["areas/*.md"]) {
  parent: Area @link @display(role: PARENT)
  within: Area @link @display(role: PARENT)
}`,
			message: "type Area declares role PARENT on more than one field (Area.parent and Area.within)",
		},
		{
			name: "interface targets implementor",
			body: `interface Grouping { within: Area @link @display(role: PARENT) }
type Area implements Grouping @node(paths: ["areas/*.md"]) { within: Area @link }`,
			message: "field Grouping.within declares role PARENT but targets Area; it must target Grouping or an interface Grouping implements",
		},
		{
			name: "embedded type",
			body: `interface StoryParent { summary: String }
type Area @node(paths: ["areas/*.md"]) { stories: StoriesSection @contains(level: H2, heading: "Stories") }
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Story implements Section & StoryParent @node(locator: EMBEDDED) { summary: String @field parent: StoryParent @link @display(role: PARENT) }`,
			message: "field Story.parent declares role PARENT but Story is not a file-backed note type",
		},
		{
			name: "interface implemented by an embedded type",
			body: `interface Grouping { within: Grouping @link @display(role: PARENT) }
type Area implements Grouping @node(paths: ["areas/*.md"]) { within: Grouping @link }
type Story implements Section & Grouping @node(locator: EMBEDDED) { within: Grouping @link }`,
			message: "field Grouping.within declares role PARENT but its target Grouping includes Story, which is not a file-backed note type",
		},
		{
			name: "target admits embedded records",
			body: `interface Grouping { name: String }
type Area implements Grouping @node(paths: ["areas/*.md"]) { name: String parent: Grouping @link @display(role: PARENT) }
type Story implements Section & Grouping @node(locator: EMBEDDED) { name: String @field }`,
			message: "field Area.parent declares role PARENT but its target Grouping includes Story, which is not a file-backed note type",
		},
	}
	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, tc.body)
			_, err := LoadSchema(root)
			require.ErrorContains(t, err, tc.message)
			require.Contains(t, err.Error(), "schema.graphql:")
		})
	}
}

func TestSummaryFieldUsesEffectiveInterfaceDisplay(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, `
interface Described { abstract: String @display(role: SUMMARY) }
type Entry implements Described @node(paths: ["notes/*.md"]) {
  summary: String
  abstract: String
}`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Same(t, schema.Types["Entry"].ByName["abstract"], SummaryField(schema.Types["Entry"].Fields))
	require.Equal(t, FieldDisplayRoleSummary, schema.Types["Entry"].ByName["abstract"].Display.Role)

	fields := []*Field{{Name: "summary"}, {Name: "abstract", Display: FieldDisplay{Role: FieldDisplayRoleSummary}}}
	require.Same(t, fields[1], SummaryField(fields))
	require.Same(t, fields[0], SummaryField(fields[:1]))
	require.Nil(t, SummaryField(nil))
}

func TestSchemaDocsSurfaceFieldDisplayHints(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, `type Entry @node(paths: ["notes/*.md"]) {
  abstract: String @display(role: SUMMARY, importance: KEY, hover: false)
  detail: String
}`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	docs, err := SchemaDocs(schema, "Entry")
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.NotNil(t, docs[0].Fields[0].Display)
	require.Equal(t, FieldDisplayRoleSummary, docs[0].Fields[0].Display.Role)
	require.Equal(t, FieldImportanceKey, docs[0].Fields[0].Display.Importance)
	require.True(t, docs[0].Fields[0].Display.HideHover)
	require.Nil(t, docs[0].Fields[1].Display)
}

func TestHumanizeFieldName(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"id":             "ID",
		"url":            "URL",
		"apiKey":         "API key",
		"htmlNote":       "HTML note",
		"openQuestions":  "Open questions",
		"release-stage":  "Release stage",
		"release_stage":  "Release stage",
		"version2URL":    "Version 2 URL",
		"API key":        "API key",
		"Already spaced": "Already spaced",
	}
	for input, want := range cases {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			require.Equal(t, want, HumanizeFieldName(input))
			require.Equal(t, want, HumanizeFieldName(want))
		})
	}
}
