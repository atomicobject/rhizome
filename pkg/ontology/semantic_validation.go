package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

func semanticValidationIssues(doc *noteDoc, noteType *ontoschema.NoteType, assessment *NoteAssessment, schema *ontoschema.Schema) []ValidationIssue {
	if doc == nil || noteType == nil || assessment == nil || schema == nil {
		return nil
	}

	issues := make([]ValidationIssue, 0)
	issues = append(issues, validateTitleConstraint(doc.Path, noteType, "", doc.Title, "")...)
	issues = append(issues, validateAssessmentFieldFormats(doc, noteType, assessment)...)
	issues = append(issues, validateConditionalRequirements(doc, noteType, assessment)...)
	return issues
}

func validateTitleConstraint(notePath string, noteType *ontoschema.NoteType, nodeRef string, title string, structural string) []ValidationIssue {
	if noteType == nil || noteType.Title == nil {
		return nil
	}
	title = strings.TrimSpace(title)
	issues := make([]ValidationIssue, 0, 2)
	patternMismatch, forbidden := noteType.Title.Violations(title)
	if patternMismatch {
		issues = append(issues, withNameVariant(ValidationIssue{
			Code:       "title_pattern_mismatch",
			NotePath:   notePath,
			TypeName:   noteType.Name,
			NodeRef:    nodeRef,
			Structural: structural,
			Message:    fmt.Sprintf("title %q must match %s", title, noteType.Title.Pattern),
		}, noteType.Name))
	}
	if forbidden {
		issues = append(issues, withNameVariant(ValidationIssue{
			Code:       "title_forbidden_pattern",
			NotePath:   notePath,
			TypeName:   noteType.Name,
			NodeRef:    nodeRef,
			Structural: structural,
			Message:    fmt.Sprintf("title %q must not match %s", title, noteType.Title.NotPattern),
		}, noteType.Name))
	}
	return issues
}

func validateAssessmentFieldFormats(doc *noteDoc, noteType *ontoschema.NoteType, assessment *NoteAssessment) []ValidationIssue {
	if doc == nil || noteType == nil || assessment == nil {
		return nil
	}
	issues := make([]ValidationIssue, 0)
	for _, assessed := range assessment.Fields {
		field := noteType.ByName[assessed.Name]
		if field == nil || field.Format == nil {
			continue
		}
		for _, value := range assessed.ValidValues {
			if issue := validateFormattedValue(doc.Path, noteType.Name, field.Name, "", "", value, field.Format); issue != nil {
				issues = append(issues, *issue)
			}
		}
	}
	return issues
}

func validateFormattedValue(notePath string, typeName string, fieldName string, nodeRef string, structural string, value string, format *ontoschema.FieldFormatConstraint) *ValidationIssue {
	if format == nil {
		return nil
	}
	value = strings.TrimSpace(value)
	patternMismatch, forbidden := format.Violations(value)
	if patternMismatch {
		issue := withTypeFieldVariant(ValidationIssue{
			Code:       "field_format_mismatch",
			NotePath:   notePath,
			TypeName:   typeName,
			FieldName:  fieldName,
			NodeRef:    nodeRef,
			Structural: structural,
			Message:    fmt.Sprintf("field %s value %q must match %s", fieldName, value, format.Pattern),
		})
		return &issue
	}
	if forbidden {
		issue := withTypeFieldVariant(ValidationIssue{
			Code:       "field_forbidden_pattern",
			NotePath:   notePath,
			TypeName:   typeName,
			FieldName:  fieldName,
			NodeRef:    nodeRef,
			Structural: structural,
			Message:    fmt.Sprintf("field %s value %q must not match %s", fieldName, value, format.NotPattern),
		})
		return &issue
	}
	return nil
}

func validateConditionalRequirements(doc *noteDoc, noteType *ontoschema.NoteType, assessment *NoteAssessment) []ValidationIssue {
	if doc == nil || noteType == nil || assessment == nil || len(noteType.RequiresWhen) == 0 {
		return nil
	}
	issues := make([]ValidationIssue, 0)
	for _, rule := range noteType.RequiresWhen {
		if !assessmentFieldHasValue(assessment, rule.Field, rule.Equals) {
			continue
		}
		for _, required := range rule.Require {
			field := noteType.ByName[required.Field]
			if field == nil {
				continue
			}
			if required.Equals == "" {
				if assessmentFieldPresent(assessment, field) {
					continue
				}
				issues = append(issues, withTypeFieldVariant(ValidationIssue{
					Code:      "conditional_required_field_missing",
					NotePath:  doc.Path,
					TypeName:  noteType.Name,
					FieldName: field.Name,
					Message:   fmt.Sprintf("field %s is required when %s is %s", field.Name, rule.Field, rule.Equals),
				}))
				continue
			}
			if assessmentFieldHasValue(assessment, field.Name, required.Equals) {
				continue
			}
			issues = append(issues, withTypeFieldVariant(ValidationIssue{
				Code:      "conditional_required_value_mismatch",
				NotePath:  doc.Path,
				TypeName:  noteType.Name,
				FieldName: field.Name,
				Message:   fmt.Sprintf("field %s must be %s when %s is %s", field.Name, required.Equals, rule.Field, rule.Equals),
			}))
		}
	}
	return issues
}

func assessmentFieldPresent(assessment *NoteAssessment, field *ontoschema.Field) bool {
	if assessment == nil || field == nil {
		return false
	}
	if field.Kind == ontoschema.FieldKindLink || field.Kind == ontoschema.FieldKindNeighbor {
		relation, ok := assessment.Relation(field.Name)
		return ok && len(relation.Targets) > 0
	}
	assessed, ok := assessment.Field(field.Name)
	return ok && (assessed.Present || len(assessed.ValidValues) > 0)
}

func assessmentFieldHasValue(assessment *NoteAssessment, fieldName string, expected string) bool {
	field, ok := assessment.Field(fieldName)
	if !ok {
		return false
	}
	for _, value := range field.ValidValues {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(expected)) {
			return true
		}
	}
	return false
}
