package ontology

import (
	"slices"
	"sort"
)

// TypeShape names how a type's records are best viewed by default.
type TypeShape string

const (
	ShapeWorkflow  TypeShape = "workflow"
	ShapeContract  TypeShape = "contract"
	ShapeDated     TypeShape = "dated"
	ShapeCatalog   TypeShape = "catalog"
	ShapeReference TypeShape = "reference"
)

// TypeProfile is what views need to know about a type or interface, derived
// from the compiled schema alone. Generated defaults, type documentation, view
// execution, and the kit all read it instead of guessing from field names.
// Field lists name schema fields in the order views should prefer them.
type TypeProfile struct {
	Shape            TypeShape `json:"shape"`
	LifecycleField   string    `json:"lifecycleField,omitempty"`
	OrderedFields    []string  `json:"orderedFields,omitempty"`
	CategoryFields   []string  `json:"categoryFields,omitempty"`
	SummaryField     string    `json:"summaryField,omitempty"`
	PrimaryDateField string    `json:"primaryDateField,omitempty"`
	PeopleFields     []string  `json:"peopleFields,omitempty"`
	KeyTextFields    []string  `json:"keyTextFields,omitempty"`
	RelationFields   []string  `json:"relationFields,omitempty"`
	ReverseFields    []string  `json:"reverseFields,omitempty"`
	// GapFields are optional KEY fields useful to describe when empty.
	// They exclude required fields, the lifecycle field, and
	// fields @requiresWhen makes conditionally required. An interface field
	// counts as required when every implementing type requires it or makes
	// it conditionally required.
	GapFields []string `json:"gapFields,omitempty"`
}

// DeriveTypeProfile derives the profile of a note type or interface. personType
// is the core identity type whose links count as people (identity.CurrentUserType).
// It returns ok false when the schema has no type or interface with that name.
func DeriveTypeProfile(schema *Schema, typeName string, personType string) (TypeProfile, bool) {
	if schema == nil {
		return TypeProfile{}, false
	}
	var fields []*Field
	var keyField string
	// expected names fields a record must fill: a type's required and
	// @requiresWhen fields, or, for an interface, those every implementing
	// type requires or conditionally requires. They are never gaps.
	expected := map[string]bool{}
	if noteType := schema.Types[typeName]; noteType != nil {
		fields, keyField = noteType.Fields, noteType.KeyField
		expected = expectedFields(noteType)
	} else if iface := schema.Interfaces[typeName]; iface != nil {
		fields = iface.Fields
		counts, implementors := map[string]int{}, 0
		for _, noteType := range schema.Types {
			if noteType == nil || !slices.Contains(noteType.Implements, typeName) {
				continue
			}
			implementors++
			for name := range expectedFields(noteType) {
				counts[name]++
			}
		}
		for name, count := range counts {
			expected[name] = count == implementors
		}
	} else {
		return TypeProfile{}, false
	}

	// KEY fields first, then declaration order.
	ordered := make([]*Field, 0, len(fields))
	for _, field := range fields {
		if field != nil {
			ordered = append(ordered, field)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return isKeyField(ordered[i]) && !isKeyField(ordered[j]) })

	var profile TypeProfile
	if summary := SummaryField(fields); summary != nil {
		profile.SummaryField = summary.Name
	}

	// Lifecycle: the first declared-stage enum, else the first KEY enum with
	// inferred stages. A list cannot hold one lifecycle value.
	var lifecycleStages []EnumValueStage
	for _, declared := range []bool{true, false} {
		for _, field := range ordered {
			if profile.LifecycleField != "" || field.Kind != FieldKindEnum || field.List {
				continue
			}
			enumType := schema.EnumTypes[field.TypeName]
			if declared != enumType.HasDeclaredStages() || (!declared && !isKeyField(field)) {
				continue
			}
			if stages := enumType.Stages(); stages != nil {
				profile.LifecycleField, lifecycleStages = field.Name, stages
			}
		}
	}

	for _, field := range ordered {
		switch {
		case field.Name == profile.LifecycleField:
		case field.Kind == FieldKindEnum:
			if schema.EnumTypes[field.TypeName].Stages() != nil {
				profile.OrderedFields = append(profile.OrderedFields, field.Name)
			} else {
				profile.CategoryFields = append(profile.CategoryFields, field.Name)
			}
		case field.Kind == FieldKindLink && field.TypeName == personType:
			profile.PeopleFields = append(profile.PeopleFields, field.Name)
		case field.Kind == FieldKindLink:
			profile.RelationFields = append(profile.RelationFields, field.Name)
		case field.Kind == FieldKindScalar && field.TypeName == "String" && isKeyField(field) &&
			!field.IsIdentifier && field.Name != "title" && field.Name != keyField && field.Name != profile.SummaryField:
			profile.KeyTextFields = append(profile.KeyTextFields, field.Name)
		}
	}
	for _, field := range ordered {
		authored := field.Kind == FieldKindScalar || field.Kind == FieldKindEnum || field.Kind == FieldKindLink
		if authored && isKeyField(field) && !field.Required && field.Name != profile.LifecycleField && !expected[field.Name] {
			profile.GapFields = append(profile.GapFields, field.Name)
		}
	}
	for _, field := range fields {
		if field != nil && (field.Kind == FieldKindReverse || (field.Kind == FieldKindNeighbor && field.List && field.Direction == NeighborDirectionInbound)) {
			profile.ReverseFields = append(profile.ReverseFields, field.Name)
		}
	}
	profile.PrimaryDateField = primaryDateField(ordered, profile.LifecycleField != "")

	hasActive := false
	for _, stage := range lifecycleStages {
		hasActive = hasActive || stage.Stage == StageActive
	}
	switch {
	case hasActive:
		profile.Shape = ShapeWorkflow
	case profile.LifecycleField != "":
		profile.Shape = ShapeContract
	case profile.PrimaryDateField != "":
		profile.Shape = ShapeDated
	case len(profile.CategoryFields) > 0:
		profile.Shape = ShapeCatalog
	default:
		profile.Shape = ShapeReference
	}
	return profile, true
}

// expectedFields lists a type's required fields and the fields its
// @requiresWhen rules make conditionally required.
func expectedFields(noteType *NoteType) map[string]bool {
	out := map[string]bool{}
	for _, field := range noteType.Fields {
		if field != nil && field.Required {
			out[field.Name] = true
		}
	}
	for _, rule := range noteType.RequiresWhen {
		for _, required := range rule.Require {
			out[required.Field] = true
		}
	}
	return out
}

func isKeyField(field *Field) bool {
	return field.Display.EffectiveImportance() == FieldImportanceKey
}

func isDateField(field *Field) bool {
	return field.Kind == FieldKindScalar && !field.List && (field.TypeName == "Date" || field.TypeName == "DateTime")
}

// primaryDateField picks the first KEY or required date (fields arrive KEY
// first); without a lifecycle, a type's only non-DETAIL date also qualifies.
func primaryDateField(fields []*Field, hasLifecycle bool) string {
	var candidates []string
	for _, field := range fields {
		if !isDateField(field) {
			continue
		}
		if isKeyField(field) || field.Required {
			return field.Name
		}
		if field.Display.EffectiveImportance() != FieldImportanceDetail {
			candidates = append(candidates, field.Name)
		}
	}
	if !hasLifecycle && len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}
