package views

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func keyField(field *ontoschema.Field) *ontoschema.Field {
	field.Display.Importance = ontoschema.FieldImportanceKey
	return field
}

func stagedEnum(name string, values ...[2]string) *ontoschema.EnumType {
	enumType := &ontoschema.EnumType{Name: name}
	for _, value := range values {
		enumType.Values = append(enumType.Values, &ontoschema.EnumValue{Name: value[0], View: ontoschema.EnumValueView{Stage: ontoschema.LifecycleStage(value[1])}})
	}
	return enumType
}

func TestGeneratedWorkflowDefaultsGroupByStageAndSortByChange(t *testing.T) {
	schema := &ontoschema.Schema{
		EnumTypes: map[string]*ontoschema.EnumType{"WorkStatus": stagedEnum("WorkStatus",
			[2]string{"planned", "open"}, [2]string{"doing", "active"}, [2]string{"shipped", "done"},
			[2]string{"cancelled", "dropped"}, [2]string{"blocked", "active"})},
		Types: map[string]*ontoschema.NoteType{"Meeting": {Name: "Meeting", PluralLabel: "Review meetings"}},
	}
	noteType := &ontoschema.NoteType{Name: "Work", Fields: []*ontoschema.Field{
		{Name: "id", Kind: ontoschema.FieldKindScalar, TypeName: "String", IsIdentifier: true},
		keyField(&ontoschema.Field{Name: "status", Kind: ontoschema.FieldKindEnum, TypeName: "WorkStatus"}),
		// Name-alike fields carry no meaning: only the profile decides.
		{Name: "state", Kind: ontoschema.FieldKindEnum, TypeName: "Other"},
		{Name: "summary", Kind: ontoschema.FieldKindScalar, TypeName: "String"},
		{Name: "owner", Kind: ontoschema.FieldKindLink, TypeName: "Person"},
		{Name: "meetings", Kind: ontoschema.FieldKindReverse, TypeName: "Meeting", List: true},
		{Name: "due", Kind: ontoschema.FieldKindScalar, TypeName: "Date"},
	}}
	profile := ontoschema.TypeProfile{
		Shape: ontoschema.ShapeWorkflow, LifecycleField: "status", SummaryField: "summary",
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
	var fields []*ontoschema.Field
	profile := ontoschema.TypeProfile{Shape: ontoschema.ShapeReference}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} {
		fields = append(fields, &ontoschema.Field{Name: name, Kind: ontoschema.FieldKindLink, TypeName: "Thing"})
		profile.RelationFields = append(profile.RelationFields, name)
	}
	fields = append(fields,
		&ontoschema.Field{Name: "hidden", Kind: ontoschema.FieldKindLink, TypeName: "Thing", Display: ontoschema.FieldDisplay{Importance: ontoschema.FieldImportanceDetail}},
		&ontoschema.Field{Name: "seen", Kind: ontoschema.FieldKindScalar, TypeName: "DateTime"},
	)

	columns := generatedColumns(nil, fields, profile, "", false)

	require.Len(t, columns, generatedColumnLimit)
	require.Equal(t, "g", columns[7].Field)
	require.Equal(t, viewconfig.ViewColumn{Field: "seen", Label: "Seen"}, columns[8])
	require.Equal(t, viewconfig.ViewColumn{Field: "updatedAt", Label: "Changed"}, columns[9])
}

func TestGeneratedDefaultsFollowShape(t *testing.T) {
	fields := []*ontoschema.Field{
		keyField(&ontoschema.Field{Name: "kind", Kind: ontoschema.FieldKindEnum, TypeName: "Kind"}),
		keyField(&ontoschema.Field{Name: "held", Kind: ontoschema.FieldKindScalar, TypeName: "Date"}),
		keyField(&ontoschema.Field{Name: "notes", Kind: ontoschema.FieldKindScalar, TypeName: "String"}),
	}
	for _, tc := range []struct {
		name    string
		profile ontoschema.TypeProfile
		sort    []viewconfig.SortSpec
		group   *viewconfig.GroupSpec
		columns []string
	}{
		{
			name:    "dated",
			profile: ontoschema.TypeProfile{Shape: ontoschema.ShapeDated, PrimaryDateField: "held", CategoryFields: []string{"kind"}, KeyTextFields: []string{"notes"}},
			sort:    []viewconfig.SortSpec{{Field: "held", Direction: "desc"}},
			group:   &viewconfig.GroupSpec{Field: "held", Bucket: viewconfig.GroupBucketMonth},
			columns: []string{"title", "kind", "notes", "held", "updatedAt"},
		},
		{
			name:    "catalog",
			profile: ontoschema.TypeProfile{Shape: ontoschema.ShapeCatalog, CategoryFields: []string{"kind"}},
			sort:    []viewconfig.SortSpec{{Field: "title", Direction: "asc"}},
			group:   &viewconfig.GroupSpec{Field: "kind"},
			columns: []string{"title", "kind", "held", "updatedAt"},
		},
		{
			name:    "reference",
			profile: ontoschema.TypeProfile{Shape: ontoschema.ShapeReference},
			sort:    []viewconfig.SortSpec{{Field: "title", Direction: "asc"}},
			columns: []string{"title", "held", "updatedAt"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def := generatedTypeDefault(nil, "Thing", &ontoschema.NoteType{Name: "Thing", Fields: fields}, tc.profile)
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
	def := generatedInterfaceDefault(nil, "Work", &ontoschema.InterfaceType{Name: "Work", Fields: []*ontoschema.Field{
		{Name: "id", Kind: ontoschema.FieldKindScalar, TypeName: "String", IsPreferredIdentifier: true},
	}}, ontoschema.TypeProfile{Shape: ontoschema.ShapeReference})

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

// A RANK field orders a type's records whatever its shape.
func TestGeneratedDefaultsSortByRankField(t *testing.T) {
	profile := ontoschema.TypeProfile{Shape: ontoschema.ShapeWorkflow, RankField: "rank"}
	require.Equal(t, []viewconfig.SortSpec{{Field: "rank", Direction: "asc"}, {Field: "title", Direction: "asc"}}, generatedSort(profile))
}
