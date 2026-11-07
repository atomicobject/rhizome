package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

func (p *PreparedDocument) validateRecordFields() {
	for _, record := range p.records {
		if !record.valid {
			continue
		}
		for _, field := range record.typ.Fields {
			binding := record.projection.Fields[field.Name]
			if field.RecordContains != nil {
				count := len(binding.ChildRefs)
				if count < field.ContainsMin {
					p.recordDiagnostic(record, field.Name, "collection_cardinality", fmt.Sprintf("collection requires at least %d records", field.ContainsMin))
				}
				if field.ContainsMax >= 0 && count > field.ContainsMax {
					p.recordDiagnostic(record, field.Name, "collection_cardinality", fmt.Sprintf("collection allows at most %d records", field.ContainsMax))
				}
				if field.SectionRequired && !binding.Present {
					p.recordDiagnostic(record, field.Name, "missing_required_field", "required collection is absent")
				}
			} else if field.Kind == ontoschema.FieldKindScalar || field.Kind == ontoschema.FieldKindEnum {
				assessment := AssessScalarFieldValues(p.root.NotePath.String(), record.typ.Name, field, p.schema, binding.Values, binding.Present)
				for _, issue := range assessment.Issues {
					p.recordDiagnostic(record, field.Name, issue.Code, issue.Message)
				}
			} else if field.Required && !binding.Present {
				p.recordDiagnostic(record, field.Name, "missing_required_field", "required reference field is absent")
			}
		}
	}
}
func (p *PreparedDocument) validationIssues() []ValidationIssue {
	var out []ValidationIssue
	for _, d := range p.diagnostics {
		out = append(out, ValidationIssue{Code: d.Code, NotePath: p.root.NotePath.String(), TypeName: d.TypeName, FieldName: d.FieldName, Message: d.Message, NodeID: string(d.Owner), NodeRef: p.root.NotePath.String() + "#" + d.Address})
	}
	return out
}
