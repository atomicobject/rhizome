package schema

// IsSectionSummary identifies the singular authored section used for compact
// presentation. Its GraphQL field remains a Section, including its full content.
func IsSectionSummary(field *Field) bool {
	return field != nil && field.Kind == FieldKindSection && !field.List &&
		field.Display.Role == FieldDisplayRoleSummary &&
		(field.EmbeddedSourceShape == "" || field.EmbeddedSourceShape == EmbeddedSourceShapeSection)
}
