package query

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/vektah/gqlparser/v2/ast"
)

func (e *executor) resolveSectionStructuralField(ctx context.Context, node *sectionRecord, field *ontoschema.Field, set ast.SelectionSet, path []string) (any, bool) {
	if node == nil {
		return nil, true
	}
	values := node.fieldValues(field, node.PropertyCase)
	if field.Required && len(values) == 0 && (!field.List || !node.hasFieldValue(field, node.PropertyCase)) {
		e.addError(path, fmt.Sprintf("required relation %s.%s is missing", node.TypeName, field.Name))
		return nil, false
	}
	if !field.List && len(values) > 1 {
		e.addError(path, fmt.Sprintf("field %s.%s has multiple values but is declared as singular", node.TypeName, field.Name))
		return nil, false
	}
	cache, err := e.notePathCacheForSections(ctx)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	records := make([]*noteRecord, 0, len(values))
	for _, raw := range values {
		targetPath, ok := cache.ResolveNote(unwrapSectionLinkValue(raw))
		if !ok {
			e.addError(path, fmt.Sprintf("field %s.%s target %q does not resolve to a note", node.TypeName, field.Name, raw))
			return nil, false
		}
		record, err := e.rootNote(ctx, targetPath)
		if err != nil {
			e.addError(path, err.Error())
			return nil, false
		}
		if record == nil {
			e.addError(path, fmt.Sprintf("field %s.%s target %q does not resolve to a typed note", node.TypeName, field.Name, raw))
			return nil, false
		}
		resolvedType := record.TypeName
		if strings.TrimSpace(resolvedType) == "" {
			resolvedType = "untyped note"
		}
		if !sectionLinkTypeMatchesOrImplements(e.schema, record.TypeName, field.TypeName) {
			e.addError(path, fmt.Sprintf("field %s.%s expects %s but %q resolves to %s", node.TypeName, field.Name, field.TypeName, raw, resolvedType))
			return nil, false
		}
		records = append(records, record)
	}
	if field.List {
		return e.resolveNodeList(ctx, records, set, path), true
	}
	if len(records) == 0 {
		return nil, true
	}
	return e.resolveNodeSelectionSet(ctx, records[0], set, path), true
}

// resolveSectionScalarField reads an inline-property scalar/enum declared on a
// section type. It honors enum membership against the schema and mirrors the
// "required when missing" error shape used by resolveScalarField for note
// fields, but it does not consult the assessment cache — section scalar
// issues already surface via assessNestedSectionIssues at index time, and
// reusing the assessment here would require a nested lookup path.
func (e *executor) resolveSectionScalarField(ctx context.Context, node *sectionRecord, field *ontoschema.Field, path []string) (any, bool) {
	_ = ctx
	if node == nil {
		return nil, true
	}
	values := node.fieldValues(field, node.PropertyCase)
	if field.Required && len(values) == 0 && (!field.List || !node.hasFieldValue(field, node.PropertyCase)) {
		e.addError(path, fmt.Sprintf("required field %s.%s is missing", node.TypeName, field.Name))
		return nil, false
	}
	if !field.List && len(values) > 1 {
		e.addError(path, fmt.Sprintf("field %s.%s has multiple values but is declared as singular", node.TypeName, field.Name))
		return nil, false
	}
	validValues, invalidValue, hasInvalid := validateSectionScalarValues(values, field, e.schema)
	if hasInvalid {
		if field.Required {
			e.addError(path, fmt.Sprintf("field %s value %q does not match %s", field.Name, invalidValue, field.TypeName))
			return nil, false
		}
		return nil, true
	}
	return fieldValue(field, validValues), true
}

// validateSectionScalarValues mirrors the note-field assessment rules for
// section inline properties: any invalid raw value suppresses the field for
// optional selections and becomes a GraphQL error for required selections.
func validateSectionScalarValues(values []string, field *ontoschema.Field, schema *ontoschema.Schema) ([]string, string, bool) {
	if len(values) == 0 || field == nil {
		return values, "", false
	}
	valid := make([]string, 0, len(values))
	for _, raw := range values {
		if !sectionScalarValueMatchesType(raw, field, schema) {
			return nil, raw, true
		}
		valid = append(valid, raw)
	}
	return valid, "", false
}

func sectionScalarValueMatchesType(raw string, field *ontoschema.Field, schema *ontoschema.Schema) bool {
	raw = strings.TrimSpace(raw)
	switch field.Kind {
	case ontoschema.FieldKindEnum:
		if schema == nil {
			return true
		}
		members := ontoschema.EnumValuesSet(schema, field.TypeName)
		if len(members) == 0 {
			return true
		}
		_, ok := members[raw]
		return ok
	case ontoschema.FieldKindScalar:
		switch field.TypeName {
		case "String", "ID":
			return raw != ""
		case "URL":
			return obsidian.AnalyzePropertyValue(raw).ValueType == "url"
		case "Date":
			return obsidian.AnalyzePropertyValue(raw).ValueType == "date"
		case "DateTime":
			return obsidian.AnalyzePropertyValue(raw).ValueType == "datetime"
		case "Int":
			_, err := strconv.ParseInt(raw, 10, 64)
			return err == nil
		case "Float":
			_, err := strconv.ParseFloat(raw, 64)
			return err == nil
		case "Boolean":
			_, err := strconv.ParseBool(strings.ToLower(raw))
			return err == nil
		default:
			return raw != ""
		}
	default:
		return true
	}
}

func (e *executor) resolveScalarField(ctx context.Context, node *noteRecord, field *ontoschema.Field, path []string) (any, bool) {
	values := node.fieldValues(field)
	var fieldAssessment ontology.FieldAssessment
	var hasFieldAssessment bool
	if e.readOverlayTouchesPath(node.Path) {
		fieldAssessment = ontology.AssessScalarFieldValues(node.Path, node.TypeName, field, e.schema, values, node.hasField(field))
		hasFieldAssessment = true
	} else {
		assessment := e.noteAssessment(ctx, node.Path)
		fieldAssessment, hasFieldAssessment = assessment.Field(field.Name)
	}
	if hasFieldAssessment && len(fieldAssessment.ValidValues) > 0 {
		values = append([]string(nil), fieldAssessment.ValidValues...)
	}
	if hasFieldAssessment && len(fieldAssessment.Issues) > 0 {
		if field.Required {
			e.addError(path, fieldAssessment.Issues[0].Message)
			return nil, false
		}
		return nil, true
	}
	if field.Required && len(values) == 0 && (!field.List || !node.hasField(field)) {
		e.addError(path, fmt.Sprintf("required field %s.%s is missing", node.TypeName, field.Name))
		return nil, false
	}
	return fieldValue(field, values), true
}

func (e *executor) readOverlayTouchesPath(notePath string) bool {
	overlay := e.deps.ReadOverlay
	if overlay == nil {
		return false
	}
	path := strings.TrimSpace(notePath)
	if path == "" {
		return false
	}
	if _, ok := overlay.Notes[path]; ok {
		return true
	}
	if _, ok := overlay.UpdatedContentByPath[path]; ok {
		return true
	}
	for _, touched := range overlay.TouchedPaths {
		if strings.TrimSpace(touched) == path {
			return true
		}
	}
	return false
}

func (e *executor) resolveStructuralField(ctx context.Context, node *noteRecord, field *ontoschema.Field, set ast.SelectionSet, path []string) (any, bool) {
	// Docs: [[ontology-graphql-query-contract]] keeps
	// @link fields on structural ontology edges instead of ambient note links.
	grouped, err := e.loaders.structuralEdges(ctx, []string{node.Path}, field.Name)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	rows := dedupeOntologyEdgeRows(grouped[node.Path])
	records, err := e.edgeRecords(ctx, rows)
	if err != nil {
		e.addError(path, err.Error())
		return nil, false
	}
	records = e.filterRecordsByType(records, field.TypeName)
	assessment := e.noteAssessment(ctx, node.Path)
	relationAssessment, hasRelationAssessment := assessment.Relation(field.Name)
	if field.List {
		if field.Required && len(records) == 0 && relationAssessmentHasStrictIssue(hasRelationAssessment, relationAssessment) {
			e.addError(path, relationAssessment.Issues[0].Message)
			return nil, false
		}
		if field.Required && len(records) == 0 && !node.hasField(field) {
			e.addError(path, fmt.Sprintf("required relation %s.%s is missing", node.TypeName, field.Name))
			return nil, false
		}
		return e.resolveNodeList(ctx, records, set, path), true
	}
	if len(records) == 0 {
		if field.Required && relationAssessmentHasStrictIssue(hasRelationAssessment, relationAssessment) {
			e.addError(path, relationAssessment.Issues[0].Message)
			return nil, false
		}
		if field.Required {
			e.addError(path, fmt.Sprintf("required relation %s.%s is missing", node.TypeName, field.Name))
			return nil, false
		}
		return nil, true
	}
	return e.resolveNodeSelectionSet(ctx, records[0], set, path), true
}
