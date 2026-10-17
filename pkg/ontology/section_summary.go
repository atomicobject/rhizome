package ontology

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

import "strings"

// SectionSummaryValues projects display text without changing the section
// binding's canonical refs or source spans. Both indexed and staged reads use it.
func SectionSummaryValues(projection *NodeProjection, field *ontoschema.Field) []string {
	if projection == nil || projection.Snapshot == nil || !ontoschema.IsSectionSummary(field) {
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
