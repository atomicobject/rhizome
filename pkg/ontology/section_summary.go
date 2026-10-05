package ontology

import "strings"

// IsSectionSummary identifies the singular authored section used for compact
// presentation. Its GraphQL field remains a Section, including its full content.
func IsSectionSummary(field *Field) bool {
	return field != nil && field.Kind == FieldKindSection && !field.List &&
		field.Display.Role == FieldDisplayRoleSummary &&
		(field.EmbeddedSourceShape == "" || field.EmbeddedSourceShape == EmbeddedSourceShapeSection)
}

// SectionSummaryValues projects display text without changing the section
// binding's canonical refs or source spans. Both indexed and staged reads use it.
func SectionSummaryValues(projection *NodeProjection, field *Field) []string {
	if projection == nil || projection.Snapshot == nil || !IsSectionSummary(field) {
		return nil
	}
	binding := projection.Fields[field.Name]
	if len(binding.SectionNodes) != 1 {
		return nil
	}
	ref := binding.SectionNodes[0]
	for _, section := range projection.Snapshot.SectionsByID {
		if section.StartByte == ref.StartByte && section.EndByte == ref.EndByte {
			return []string{strings.Join(strings.Fields(SectionBody(section)), " ")}
		}
	}
	return nil
}
