package views

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func keyField(field *ontology.Field) *ontology.Field {
	field.Display.Importance = ontology.FieldImportanceKey
	return field
}

func stagedEnum(name string, values ...[2]string) *ontology.EnumType {
	enumType := &ontology.EnumType{Name: name}
	for _, value := range values {
		enumType.Values = append(enumType.Values, &ontology.EnumValue{Name: value[0], View: ontology.EnumValueView{Stage: ontology.LifecycleStage(value[1])}})
	}
	return enumType
}

func TestGeneratedWorkflowDefaultsGroupByStageAndSortByChange(t *testing.T) {
	schema := &ontology.Schema{
		EnumTypes: map[string]*ontology.EnumType{"WorkStatus": stagedEnum("WorkStatus",
			[2]string{"planned", "open"}, [2]string{"doing", "active"}, [2]string{"shipped", "done"},
			[2]string{"cancelled", "dropped"}, [2]string{"blocked", "active"})},
		Types: map[string]*ontology.NoteType{"Meeting": {Name: "Meeting", PluralLabel: "Review meetings"}},
	}
	noteType := &ontology.NoteType{Name: "Work", Fields: []*ontology.Field{
		{Name: "id", Kind: ontology.FieldKindScalar, TypeName: "String", IsIdentifier: true},
		keyField(&ontology.Field{Name: "status", Kind: ontology.FieldKindEnum, TypeName: "WorkStatus"}),
		// Name-alike fields carry no meaning: only the profile decides.
		{Name: "state", Kind: ontology.FieldKindEnum, TypeName: "Other"},
		{Name: "summary", Kind: ontology.FieldKindScalar, TypeName: "String"},
		{Name: "owner", Kind: ontology.FieldKindLink, TypeName: "Person"},
		{Name: "meetings", Kind: ontology.FieldKindReverse, TypeName: "Meeting", List: true},
		{Name: "due", Kind: ontology.FieldKindScalar, TypeName: "Date"},
	}}
	profile := ontology.TypeProfile{
		Shape: ontology.ShapeWorkflow, LifecycleField: "status", SummaryField: "summary",
		PeopleFields: []string{"owner"}, ReverseFields: []string{"meetings"},
	}

	def := generatedTypeDefault(schema, "Work", noteType, profile)

	require.Equal(t, []viewconfig.ViewColumn{
		{Field: "title"},
		{Field: "id", Label: "ID"},
		{Field: "status", Label: "Status"},
		{Field: "owner", Label: "Owner"},
		{Field: "meetings", Label: "Review meetings"},
		{Field: "due", Label: "Due"},
		{Field: "updatedAt", Label: "Changed"},
	}, def.Variants.Table.Columns)
	require.Equal(t, []viewconfig.SortSpec{{Field: "updatedAt", Direction: "desc"}}, def.Defaults.Sort)
	require.Equal(t, &viewconfig.GroupSpec{Field: "status"}, def.Defaults.Group)
	require.Equal(t, "table", def.Defaults.Variant)
	require.Equal(t, &viewconfig.KanbanVariant{ColumnField: "status"}, def.Variants.Kanban)
	require.Equal(t, &viewconfig.CardSpec{Eyebrow: "id", Title: "title", Preview: "summary"}, def.Variants.Card)
	require.Equal(t, []string{"table", "kanban", "card"}, availableVariants(def))
}

func TestGeneratedColumnsCapAtTenKeepingDateAndChanged(t *testing.T) {
	var fields []*ontology.Field
	profile := ontology.TypeProfile{Shape: ontology.ShapeReference}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} {
		fields = append(fields, &ontology.Field{Name: name, Kind: ontology.FieldKindLink, TypeName: "Thing"})
		profile.RelationFields = append(profile.RelationFields, name)
	}
	fields = append(fields,
		&ontology.Field{Name: "hidden", Kind: ontology.FieldKindLink, TypeName: "Thing", Display: ontology.FieldDisplay{Importance: ontology.FieldImportanceDetail}},
		&ontology.Field{Name: "seen", Kind: ontology.FieldKindScalar, TypeName: "DateTime"},
	)

	columns := generatedColumns(nil, fields, profile, "", false)

	require.Len(t, columns, generatedColumnLimit)
	require.Equal(t, "g", columns[7].Field)
	require.Equal(t, viewconfig.ViewColumn{Field: "seen", Label: "Seen"}, columns[8])
	require.Equal(t, viewconfig.ViewColumn{Field: "updatedAt", Label: "Changed"}, columns[9])
}

func TestGeneratedDefaultsFollowShape(t *testing.T) {
	fields := []*ontology.Field{
		keyField(&ontology.Field{Name: "kind", Kind: ontology.FieldKindEnum, TypeName: "Kind"}),
		keyField(&ontology.Field{Name: "held", Kind: ontology.FieldKindScalar, TypeName: "Date"}),
		keyField(&ontology.Field{Name: "notes", Kind: ontology.FieldKindScalar, TypeName: "String"}),
	}
	for _, tc := range []struct {
		name    string
		profile ontology.TypeProfile
		sort    []viewconfig.SortSpec
		group   *viewconfig.GroupSpec
		columns []string
	}{
		{
			name:    "dated",
			profile: ontology.TypeProfile{Shape: ontology.ShapeDated, PrimaryDateField: "held", CategoryFields: []string{"kind"}, KeyTextFields: []string{"notes"}},
			sort:    []viewconfig.SortSpec{{Field: "held", Direction: "desc"}},
			group:   &viewconfig.GroupSpec{Field: "held", Bucket: viewconfig.GroupBucketMonth},
			columns: []string{"title", "kind", "notes", "held", "updatedAt"},
		},
		{
			name:    "catalog",
			profile: ontology.TypeProfile{Shape: ontology.ShapeCatalog, CategoryFields: []string{"kind"}},
			sort:    []viewconfig.SortSpec{{Field: "title", Direction: "asc"}},
			group:   &viewconfig.GroupSpec{Field: "kind"},
			columns: []string{"title", "kind", "held", "updatedAt"},
		},
		{
			name:    "reference",
			profile: ontology.TypeProfile{Shape: ontology.ShapeReference},
			sort:    []viewconfig.SortSpec{{Field: "title", Direction: "asc"}},
			columns: []string{"title", "held", "updatedAt"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def := generatedTypeDefault(nil, "Thing", &ontology.NoteType{Name: "Thing", Fields: fields}, tc.profile)
			require.Equal(t, tc.sort, def.Defaults.Sort)
			require.Equal(t, tc.group, def.Defaults.Group)
			require.Equal(t, tc.columns, viewColumnFields(def.Variants.Table.Columns))
			require.Nil(t, def.Variants.Kanban)
			require.Nil(t, def.Variants.Card)
			require.Equal(t, []string{"table"}, availableVariants(def))
		})
	}
}

func TestGeneratedInterfaceDefaultsListImplementingTypeAfterIdentifier(t *testing.T) {
	def := generatedInterfaceDefault(nil, "Work", &ontology.InterfaceType{Name: "Work", Fields: []*ontology.Field{
		{Name: "id", Kind: ontology.FieldKindScalar, TypeName: "String", IsPreferredIdentifier: true},
	}}, ontology.TypeProfile{Shape: ontology.ShapeReference})

	require.Equal(t, []string{"title", "id", "resolvedType", "updatedAt"}, viewColumnFields(def.Variants.Table.Columns))
	require.Equal(t, []string{"title", "id", "resolvedType", "updatedAt"}, tableColumnFields(tableColumns(def, nil)))
}

func viewColumnFields(columns []viewconfig.ViewColumn) []string {
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, column.Field)
	}
	return out
}

func tableColumnFields(columns []TableColumn) []string {
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, column.Field)
	}
	return out
}
