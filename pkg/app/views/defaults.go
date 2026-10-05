package views

import (
	"slices"
	"sort"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// generatedColumnLimit caps SPEC-0112 generated table columns.
const generatedColumnLimit = 10

func generatedDefaults(schema *ontology.Schema) []viewconfig.ViewDefinition {
	if schema == nil {
		return nil
	}
	// WHY: generated views are an execution convenience for every ontology
	// type/interface, not repo settings. Only explicit .rhizome/views files
	// should create durable config diffs.
	var out []viewconfig.ViewDefinition
	typeNames := make([]string, 0, len(schema.Types))
	for name := range schema.Types {
		typeNames = append(typeNames, name)
	}
	sort.Strings(typeNames)
	for _, name := range typeNames {
		profile, _ := subjectProfile(schema, name)
		out = append(out, generatedTypeDefault(schema, name, schema.Types[name], profile))
	}
	interfaceNames := make([]string, 0, len(schema.Interfaces))
	for name := range schema.Interfaces {
		interfaceNames = append(interfaceNames, name)
	}
	sort.Strings(interfaceNames)
	for _, name := range interfaceNames {
		profile, _ := subjectProfile(schema, name)
		out = append(out, generatedInterfaceDefault(schema, name, schema.Interfaces[name], profile))
	}
	return out
}

func generatedTypeDefault(schema *ontology.Schema, name string, noteType *ontology.NoteType, profile ontology.TypeProfile) viewconfig.ViewDefinition {
	var fields []*ontology.Field
	label, plural := "", ""
	if noteType != nil {
		fields = noteType.Fields
		plural = noteType.PluralLabel
		if noteType.LabelAuthored {
			label = noteType.Label
		}
	}
	return generatedDefault(schema, generatedViewName(name, label, plural), fields, profile,
		viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: name},
		viewconfig.MountSpec{Kind: viewconfig.MountKindType, Type: name, Default: true},
		viewconfig.GeneratedTypeID(name),
	)
}

func generatedInterfaceDefault(schema *ontology.Schema, name string, iface *ontology.InterfaceType, profile ontology.TypeProfile) viewconfig.ViewDefinition {
	var fields []*ontology.Field
	label, plural := "", ""
	if iface != nil {
		fields = iface.Fields
		label, plural = iface.Label, iface.PluralLabel
	}
	return generatedDefault(schema, generatedViewName(name, label, plural), fields, profile,
		viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyInterface, Interface: name},
		viewconfig.MountSpec{Kind: viewconfig.MountKindInterface, Interface: name, Default: true},
		viewconfig.GeneratedInterfaceID(name),
	)
}

// generatedViewName titles a generated view the way the schema names its
// type: the plural label, then an authored label, then the humanized name.
func generatedViewName(typeName, label, plural string) string {
	return firstNonEmpty(plural, label, ontology.HumanizeFieldName(typeName))
}

// generatedDefault builds a type's or interface's runtime view from its
// SPEC-0112 profile alone: sort, grouping, columns, and offered layouts follow
// the profile's shape and never a field's name. The default variant is Table;
// the catalog may open a workflow type as a Board from its record counts.
func generatedDefault(schema *ontology.Schema, name string, fields []*ontology.Field, profile ontology.TypeProfile, source viewconfig.SourceSpec, mount viewconfig.MountSpec, id string) viewconfig.ViewDefinition {
	identifier := generatedIdentifierField(fields)
	variants := viewconfig.VariantSet{
		Table: &viewconfig.TableVariant{Columns: generatedColumns(schema, fields, profile, identifier, source.Kind == viewconfig.SourceKindOntologyInterface)},
	}
	if profile.SummaryField != "" {
		variants.Card = &viewconfig.CardSpec{Eyebrow: identifier, Title: "title"}
		// A DETAIL summary stays available but is never a default preview.
		if summary := schemaFieldByName(fields, profile.SummaryField); summary == nil || summary.Display.EffectiveImportance() != ontology.FieldImportanceDetail {
			variants.Card.Preview = profile.SummaryField
		}
	}
	if profile.LifecycleField != "" {
		variants.Kanban = &viewconfig.KanbanVariant{ColumnField: profile.LifecycleField}
	}
	return viewconfig.ViewDefinition{
		APIVersion: viewconfig.APIVersion,
		ID:         id,
		Name:       name,
		Generated:  true,
		Origin:     viewconfig.OriginGenerated,
		SourceSpec: source,
		Mount:      mount,
		Defaults: viewconfig.DefaultsSpec{
			Variant: "table",
			First:   200,
			Sort:    generatedSort(profile),
			Group:   generatedGroup(profile),
		},
		Variants: variants,
	}
}

func generatedSort(profile ontology.TypeProfile) []viewconfig.SortSpec {
	switch profile.Shape {
	case ontology.ShapeDated:
		return []viewconfig.SortSpec{{Field: profile.PrimaryDateField, Direction: "desc"}}
	case ontology.ShapeWorkflow, ontology.ShapeContract:
		return []viewconfig.SortSpec{{Field: "updatedAt", Direction: "desc"}}
	default:
		return []viewconfig.SortSpec{{Field: "title", Direction: "asc"}}
	}
}

func generatedGroup(profile ontology.TypeProfile) *viewconfig.GroupSpec {
	switch profile.Shape {
	case ontology.ShapeWorkflow, ontology.ShapeContract:
		// Execution orders lifecycle groups by stage (lifecycleGroupRanks).
		return &viewconfig.GroupSpec{Field: profile.LifecycleField}
	case ontology.ShapeDated:
		return &viewconfig.GroupSpec{Field: profile.PrimaryDateField, Bucket: viewconfig.GroupBucketMonth}
	case ontology.ShapeCatalog:
		return &viewconfig.GroupSpec{Field: profile.CategoryFields[0]}
	default:
		return nil
	}
}

// generatedColumns lists, up to ten: title, identifier, implementing type for
// interfaces, lifecycle, KEY ordered and category fields, key text, people,
// relation, and reverse fields, then a date and Changed, which keep their two
// slots when the list runs long. DETAIL fields stay available but hidden.
func generatedColumns(schema *ontology.Schema, fields []*ontology.Field, profile ontology.TypeProfile, identifier string, iface bool) []viewconfig.ViewColumn {
	seen := map[string]bool{}
	add := func(columns []viewconfig.ViewColumn, field, label string) []viewconfig.ViewColumn {
		if field == "" || seen[field] {
			return columns
		}
		seen[field] = true
		if label == "" && field != "title" {
			label = labelForField(field)
		}
		return append(columns, viewconfig.ViewColumn{Field: field, Label: label})
	}
	shown := func(name string) bool {
		field := schemaFieldByName(fields, name)
		return field != nil && field.Display.EffectiveImportance() != ontology.FieldImportanceDetail
	}
	head := add(nil, "title", "")
	head = add(head, identifier, "")
	if iface {
		head = add(head, "resolvedType", "Type")
	}
	head = add(head, profile.LifecycleField, "")
	for _, field := range fields {
		if field != nil && field.Display.EffectiveImportance() == ontology.FieldImportanceKey &&
			(slices.Contains(profile.OrderedFields, field.Name) || slices.Contains(profile.CategoryFields, field.Name)) {
			head = add(head, field.Name, "")
		}
	}
	for _, group := range [][]string{profile.KeyTextFields, profile.PeopleFields, profile.RelationFields} {
		for _, name := range group {
			if shown(name) {
				head = add(head, name, "")
			}
		}
	}
	for _, name := range profile.ReverseFields {
		if shown(name) {
			head = add(head, name, reverseFieldLabel(schema, schemaFieldByName(fields, name)))
		}
	}
	tail := add(nil, generatedDateField(fields, profile), "")
	tail = add(tail, "updatedAt", "Changed")
	if limit := generatedColumnLimit - len(tail); len(head) > limit {
		head = head[:limit]
	}
	return append(head, tail...)
}

// reverseFieldLabel names a reverse count column by the target type's plural
// label, else the humanized field name.
func reverseFieldLabel(schema *ontology.Schema, field *ontology.Field) string {
	if schema != nil {
		if target := schema.Types[field.TypeName]; target != nil && target.PluralLabel != "" {
			return target.PluralLabel
		}
		if target := schema.Interfaces[field.TypeName]; target != nil && target.PluralLabel != "" {
			return target.PluralLabel
		}
	}
	return labelForField(field.Name)
}

// generatedDateField is the profile's primary date, else the first shown
// single Date or DateTime field.
func generatedDateField(fields []*ontology.Field, profile ontology.TypeProfile) string {
	if profile.PrimaryDateField != "" {
		return profile.PrimaryDateField
	}
	for _, field := range fields {
		if isDateField(field) && !field.List && field.Display.EffectiveImportance() != ontology.FieldImportanceDetail {
			return field.Name
		}
	}
	return ""
}

func generatedIdentifierField(fields []*ontology.Field) string {
	for _, field := range fields {
		if field != nil && (field.IsIdentifier || field.IsPreferredIdentifier || field.IsDerivableIdentifier) {
			return field.Name
		}
	}
	return ""
}
