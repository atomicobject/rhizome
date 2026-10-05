package ontology

import "fmt"

// AssessScalarFieldValues applies the indexed field rules to already-extracted current values.
func AssessScalarFieldValues(notePath, typeName string, field *Field, schema *Schema, values []string, present bool) FieldAssessment {
	fieldAssessment := baseFieldAssessment(field)
	fieldAssessment.Present = present
	fieldAssessment.Values = append([]string(nil), values...)
	if len(values) == 0 {
		// Derivable identifiers get their semantic values from structure
		// without requiring an authored id line (SPEC-0023.US8).
		if field.Required && !fieldAllowsMissingAuthoredValue(field) && (!field.List || !fieldAssessment.Present) {
			issue := withTypeFieldVariant(ValidationIssue{
				Code:      "missing_required_field",
				NotePath:  notePath,
				TypeName:  typeName,
				FieldName: field.Name,
				Message:   fmt.Sprintf("required field %s is missing", field.Name),
			})
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			return fieldAssessment
		}
		return fieldAssessment
	}
	if !field.List && len(values) > 1 {
		issue := withTypeFieldVariant(ValidationIssue{
			Code:      "field_shape_mismatch",
			NotePath:  notePath,
			TypeName:  typeName,
			FieldName: field.Name,
			Message:   fmt.Sprintf("field %s expects a single value", field.Name),
		})
		fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
		return fieldAssessment
	}

	for _, raw := range values {
		if !validateScalarValue(raw, field, schema) {
			issue := withTypeFieldVariant(ValidationIssue{
				Code:      "field_type_mismatch",
				NotePath:  notePath,
				TypeName:  typeName,
				FieldName: field.Name,
				Message:   fmt.Sprintf("field %s value %q does not match %s", field.Name, raw, field.TypeName),
			})
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			continue
		}
		fieldAssessment.ValidValues = append(fieldAssessment.ValidValues, raw)
	}
	return fieldAssessment
}
