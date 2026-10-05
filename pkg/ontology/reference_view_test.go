package ontology

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const typeDocSchema = `
enum AreaStatus {
  draft @view(label: "Draft", order: 0, tone: "neutral")
  active @view(order: 1, tone: "progress")
  retired @view(collapsed: true)
  other
}

interface Grouping { title: String }

type Area implements Grouping @node(paths: ["areas/*.md"])
  @requiresWhen(field: "status", equals: "active", require: [{ field: "owner" }])
  @requiresWhen(field: "status", equals: "retired", require: [{ field: "owner", equals: "nobody" }, { field: "reason" }]) {
  title: String
  summary: String
  status: AreaStatus!
  owner: String
  reason: String
  within: Grouping @link @display(role: PARENT)
}

type Note2 @node(paths: ["other/*.md"]) {
  headline: String @display(role: SUMMARY)
  summary: String
}
`

func loadTypeDoc(t *testing.T, typeName string) TypeDoc {
	t.Helper()
	root := t.TempDir()
	writeOntologySchema(t, root, typeDocSchema)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	docs, err := SchemaDocs(schema, typeName)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	return docs[0]
}

func fieldDocByName(t *testing.T, doc TypeDoc, name string) FieldDoc {
	t.Helper()
	for _, field := range doc.Fields {
		if field.Name == name {
			return field
		}
	}
	t.Fatalf("field %s not documented", name)
	return FieldDoc{}
}

func TestTypeDocReportsEnumViewMetadata(t *testing.T) {
	doc := loadTypeDoc(t, "Area")
	status := fieldDocByName(t, doc, "status")
	require.NotNil(t, status.Enum)
	raw, err := json.Marshal(status.Enum.Values)
	require.NoError(t, err)
	require.JSONEq(t, `[
		{"name": "draft", "label": "Draft", "order": 0, "tone": "neutral", "stage": "open"},
		{"name": "active", "order": 1, "tone": "progress", "stage": "active"},
		{"name": "retired", "collapsed": true, "stage": "dropped"},
		{"name": "other", "stage": "open"}
	]`, string(raw), "inferred stages are reported without filling tone or collapsed")
}

func TestTypeDocReportsRequiredWhen(t *testing.T) {
	doc := loadTypeDoc(t, "Area")
	require.Equal(t, []FieldCondition{{Field: "status", Equals: "active"}, {Field: "status", Equals: "retired"}}, fieldDocByName(t, doc, "owner").RequiredWhen)
	require.Equal(t, []FieldCondition{{Field: "status", Equals: "retired"}}, fieldDocByName(t, doc, "reason").RequiredWhen)
	require.Empty(t, fieldDocByName(t, doc, "title").RequiredWhen)
}

func TestTypeDocReportsSummaryAndParentRoles(t *testing.T) {
	area := loadTypeDoc(t, "Area")
	require.Equal(t, "summary", area.SummaryField, "the conventional summary field is the effective summary")
	require.Equal(t, "within", area.ParentField)
	require.Equal(t, FieldDisplayRoleSummary, fieldDocByName(t, area, "summary").Display.Role)
	require.Equal(t, FieldDisplayRoleParent, fieldDocByName(t, area, "within").Display.Role)
	require.Nil(t, fieldDocByName(t, area, "title").Display)

	explicit := loadTypeDoc(t, "Note2")
	require.Equal(t, "headline", explicit.SummaryField)
	require.Empty(t, explicit.ParentField)
	require.Nil(t, fieldDocByName(t, explicit, "summary").Display, "an explicit SUMMARY role displaces the convention")
}
