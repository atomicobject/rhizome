package ontology

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func resolveNoteAssessment(doc *noteDoc, schema *Schema) *NoteAssessment {
	if doc == nil || schema == nil {
		return nil
	}
	candidates := matchCandidateTypes(doc, schema)
	selectorCandidates := matchSelectorCandidateTypes(doc, schema)
	declaredType := strings.TrimSpace(doc.TypeName)
	if declaredType == "" && len(candidates) == 0 {
		if schema.DefaultNoteType != "" && schema.Types[schema.DefaultNoteType] != nil {
			return &NoteAssessment{
				NotePath:     doc.Path,
				DeclaredType: declaredType,
				ResolvedType: schema.DefaultNoteType,
			}
		}
		if len(selectorCandidates) == 0 {
			return nil
		}
		return &NoteAssessment{NotePath: doc.Path, SelectorCandidateTypes: selectorCandidates}
	}

	assessment := &NoteAssessment{
		NotePath:               doc.Path,
		DeclaredType:           declaredType,
		CandidateTypes:         candidates,
		SelectorCandidateTypes: selectorCandidates,
	}
	switch {
	case declaredType != "":
		if schema.Types[declaredType] == nil {
			assessment.Issues = append(assessment.Issues, withNameVariant(ValidationIssue{
				Code:     "unknown_declared_type",
				NotePath: doc.Path,
				TypeName: declaredType,
				Message:  fmt.Sprintf("note declares unknown type %q", declaredType),
			}, declaredType))
		} else if containsString(candidates, declaredType) {
			assessment.ResolvedType = declaredType
		} else {
			issue := ValidationIssue{
				Code:     "declared_type_mismatch",
				NotePath: doc.Path,
				TypeName: declaredType,
				Message:  fmt.Sprintf("note %q declares %s but selectors match %s", doc.Path, declaredType, strings.Join(candidates, ", ")),
			}
			issue.VariantKey, issue.VariantLabel = declaredTypeVariant(declaredType, candidates)
			assessment.Issues = append(assessment.Issues, issue)
		}
	}
	if assessment.ResolvedType == "" {
		switch len(candidates) {
		case 1:
			assessment.ResolvedType = candidates[0]
		case 0:
			// unresolved
		default:
			if winner := mostSpecificCandidate(doc, candidates, schema); winner != "" {
				assessment.ResolvedType = winner
			} else {
				issue := ValidationIssue{
					Code:           "type_ambiguous",
					NotePath:       doc.Path,
					Message:        fmt.Sprintf("note %q matches multiple ontology types: %s", doc.Path, strings.Join(candidates, ", ")),
					CandidateTypes: slices.Sorted(slices.Values(candidates)),
				}
				issue.VariantKey, issue.VariantLabel = candidateTypesVariant(candidates)
				assessment.Issues = append(assessment.Issues, issue)
			}
		}
	}
	return assessment
}

// matchSelectorCandidateTypes records selector evidence before the required
// identifier gate is applied. Resolution continues to use CandidateTypes; the
// broader evidence lets migration tooling distinguish a schema-strategy
// mismatch from a note that never matched the type's selectors.
func matchSelectorCandidateTypes(doc *noteDoc, schema *Schema) []string {
	if doc == nil || schema == nil {
		return nil
	}
	out := make([]string, 0, len(schema.Types))
	for name, noteType := range schema.Types {
		if noteType == nil || noteType.Role == TypeRoleEmbeddedNode || !schema.MatchDoc(name, doc) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func matchCandidateTypes(doc *noteDoc, schema *Schema) []string {
	if doc == nil || schema == nil {
		return nil
	}
	declaredType := strings.TrimSpace(doc.TypeName)
	out := make([]string, 0, len(schema.Types))
	for name, noteType := range schema.Types {
		if noteType != nil && noteType.Role == TypeRoleEmbeddedNode {
			continue
		}
		if !schema.MatchDoc(name, doc) {
			continue
		}
		if declaredType == "" && !candidateIdentifierGateSatisfied(doc, noteType, schema) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func candidateIdentifierGateSatisfied(doc *noteDoc, noteType *NoteType, schema *Schema) bool {
	requiredIdentifiers := requiredIdentifierFields(noteType, schema)
	if len(requiredIdentifiers) == 0 {
		return true
	}
	for _, field := range requiredIdentifiers {
		if identifierFieldGateSatisfied(doc, field) {
			return true
		}
	}
	return false
}

func identifierFieldGateSatisfied(doc *noteDoc, field *Field) bool {
	if !fieldPresent(doc, field) {
		return false
	}
	values := extractFieldValues(doc, field)
	if field.IdentifierFormat == nil {
		return len(values) > 0
	}
	strategy, err := field.IdentifierFormat.StrategyContract()
	if err != nil {
		return false
	}
	for _, value := range values {
		if _, ok := strategy.Parse(value); ok {
			return true
		}
	}
	return false
}

func requiredIdentifierFields(noteType *NoteType, schema *Schema) []*Field {
	if noteType == nil {
		return nil
	}
	out := make([]*Field, 0, 1)
	seenInterfaces := make(map[string]struct{})
	for _, field := range noteType.Fields {
		if field == nil || !field.Required || !field.IsIdentifier {
			continue
		}
		out = append(out, field)
	}
	for _, ifaceName := range noteType.Implements {
		out = append(out, requiredIdentifierFieldsFromInterface(schema, ifaceName, seenInterfaces)...)
	}
	return out
}

func requiredIdentifierFieldsFromInterface(schema *Schema, ifaceName string, seen map[string]struct{}) []*Field {
	if schema == nil {
		return nil
	}
	ifaceName = strings.TrimSpace(ifaceName)
	if ifaceName == "" || ifaceName == "Section" {
		return nil
	}
	if _, ok := seen[ifaceName]; ok {
		return nil
	}
	seen[ifaceName] = struct{}{}
	iface := schema.Interfaces[ifaceName]
	if iface == nil {
		return nil
	}
	out := make([]*Field, 0, 1)
	for _, field := range iface.Fields {
		if field == nil || !field.Required || !field.IsIdentifier {
			continue
		}
		out = append(out, field)
	}
	for _, parentName := range iface.Implements {
		out = append(out, requiredIdentifierFieldsFromInterface(schema, parentName, seen)...)
	}
	return out
}

func appendIssuesToAssessments(assessmentByPath map[string]*NoteAssessment, issues []ValidationIssue) {
	for _, issue := range issues {
		if issue.NotePath == "" {
			continue
		}
		assessment := assessmentByPath[issue.NotePath]
		if assessment == nil {
			continue
		}
		assessment.Issues = append(assessment.Issues, issue)
	}
}

func assessScalarField(doc *noteDoc, noteType *NoteType, field *Field, schema *Schema) (FieldAssessment, []ValidationIssue) {
	assessment := AssessScalarFieldValues(doc.Path, noteType.Name, field, schema, extractFieldValues(doc, field), fieldPresent(doc, field))
	return assessment, assessment.Issues
}

func assessSectionField(doc *noteDoc, noteType *NoteType, field *Field, schema *Schema) (FieldAssessment, []ValidationIssue) {
	var nodes []*SectionNode
	if doc != nil && doc.Snapshot != nil {
		nodes = doc.Snapshot.Sections
	}
	return assessSectionFieldInNodes(doc.Path, noteType.Name, noteType.PropertyCase, field, nodes, schema, field.Name)
}

func assessSectionFieldInNodes(notePath string, typeName string, propertyCase PropertyCase, field *Field, nodes []*SectionNode, schema *Schema, fieldPath string) (FieldAssessment, []ValidationIssue) {
	fieldAssessment := baseFieldAssessment(field)
	matches := matchSectionsForAssessment(nodes, field)
	fieldAssessment.Present = len(matches) > 0
	for _, match := range matches {
		fieldAssessment.Values = append(fieldAssessment.Values, match.ID)
		fieldAssessment.ValidValues = append(fieldAssessment.ValidValues, match.ID)
	}

	issues := make([]ValidationIssue, 0)
	if len(matches) == 0 {
		if field.SectionRequired {
			code := "missing_required_section"
			message := fmt.Sprintf("required section %s is missing", fieldPath)
			if field.SectionHeading != "" && hasSectionHeadingAtOtherLevel(nodes, field.SectionHeading, field.SectionLevel) {
				code = "wrong_section_level"
				message = fmt.Sprintf("section %s must appear as %s heading %q", fieldPath, field.SectionLevel, field.SectionHeading)
			}
			issue := withTypeFieldVariant(ValidationIssue{
				Code:      code,
				NotePath:  notePath,
				TypeName:  typeName,
				FieldName: fieldPath,
				Message:   message,
			})
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			return fieldAssessment, []ValidationIssue{issue}
		}
		return fieldAssessment, nil
	}
	if !field.List && len(matches) > 1 {
		issue := withTypeFieldVariant(ValidationIssue{
			Code:      "duplicate_section",
			NotePath:  notePath,
			TypeName:  typeName,
			FieldName: fieldPath,
			Message:   fmt.Sprintf("section %s must match a single heading", fieldPath),
		})
		fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
		issues = append(issues, issue)
	}
	if field.SectionRequired {
		for _, match := range matches {
			if strings.TrimSpace(SectionBody(match)) != "" {
				continue
			}
			issue := withTypeFieldVariant(ValidationIssue{
				Code:      "empty_required_section",
				NotePath:  notePath,
				TypeName:  typeName,
				FieldName: fieldPath,
				Message:   fmt.Sprintf("required section %s must not be empty", fieldPath),
			})
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			issues = append(issues, issue)
		}
	}
	issues = append(issues, assessNestedSectionIssues(notePath, typeName, propertyCase, field.TypeName, matches, schema, fieldPath)...)
	return fieldAssessment, issues
}

// matchSectionsForAssessment picks the section nodes that a @contains field
// binds to. Fields with an explicit heading delegate to FindMatchingSections
// (recursive walk); list-typed fields without a heading match every direct
// child of the current scope at the requested level, preserving document
// order.
func matchSectionsForAssessment(nodes []*SectionNode, field *Field) []*SectionNode {
	if field == nil {
		return nil
	}
	if field.SectionHeading != "" {
		return FindMatchingSections(nodes, field.SectionLevel, field.SectionHeading)
	}
	if !field.List {
		return nil
	}
	nodes = UnwrapSingleH1SectionRoot(nodes)
	out := make([]*SectionNode, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if node.Level == field.SectionLevel {
			out = append(out, node)
		}
	}
	return out
}

func assessNestedSectionIssues(notePath string, enclosingTypeName string, propertyCase PropertyCase, sectionTypeName string, matches []*SectionNode, schema *Schema, parentPath string) []ValidationIssue {
	if schema == nil || len(matches) == 0 {
		return nil
	}
	sectionType := schema.Types[sectionTypeName]
	if sectionType == nil || (sectionType.Role != TypeRoleSection && sectionType.Role != TypeRoleEmbeddedNode) {
		return nil
	}
	issues := make([]ValidationIssue, 0)
	for _, match := range matches {
		if match == nil {
			continue
		}
		for _, field := range sectionType.Fields {
			if field == nil {
				continue
			}
			fieldPath := parentPath + "." + field.Name
			switch field.Kind {
			case FieldKindSection:
				_, fieldIssues := assessSectionFieldInNodes(notePath, enclosingTypeName, propertyCase, field, match.Children, schema, fieldPath)
				issues = append(issues, fieldIssues...)
			case FieldKindScalar, FieldKindEnum:
				_, fieldIssues := assessSectionScalarField(notePath, enclosingTypeName, propertyCase, field, match, schema, fieldPath)
				issues = append(issues, fieldIssues...)
			case FieldKindLink:
				_, fieldIssues := assessSectionLinkField(notePath, enclosingTypeName, propertyCase, field, match, fieldPath)
				issues = append(issues, fieldIssues...)
			}
		}
	}
	return issues
}

// assessSectionScalarField validates an inline `Key:: Value` property that a
// section type declares via `@field`. The inline key is derived from the
// enclosing note type's propertyCase so the same section type can be reused
// under note types with different casing conventions.
func assessSectionScalarField(notePath string, enclosingTypeName string, propertyCase PropertyCase, field *Field, node *SectionNode, schema *Schema, fieldPath string) (FieldAssessment, []ValidationIssue) {
	fieldAssessment := baseFieldAssessment(field)
	if node == nil {
		return fieldAssessment, nil
	}
	propertyName := defaultPropertyName(field.Name, propertyCase)
	inline := SectionInlineProperties(SectionOwnContent(node))
	values := normalizeFieldValues(field, normalizeStringValues(caseInsensitiveInlineValues(inline, propertyName)))
	fieldAssessment.Present = caseInsensitiveInlineHasKey(inline, propertyName)
	fieldAssessment.Values = append([]string(nil), values...)

	if len(values) == 0 {
		// WHY: derivable-identity fields (SPEC-0023.US8) get their semantic
		// id from structure when no `id::` line is authored. Treating a
		// missing authored line as a required-field violation would defeat
		// the lazy-`id::` lifecycle.
		if field.Required && !fieldAllowsMissingAuthoredValue(field) && (!field.List || !fieldAssessment.Present) {
			issue := withTypeFieldVariant(ValidationIssue{
				Code:      "missing_required_field",
				NotePath:  notePath,
				TypeName:  enclosingTypeName,
				FieldName: fieldPath,
				Message:   fmt.Sprintf("required field %s is missing", fieldPath),
			})
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			return fieldAssessment, []ValidationIssue{issue}
		}
		return fieldAssessment, nil
	}
	if !field.List && len(values) > 1 {
		issue := withTypeFieldVariant(ValidationIssue{
			Code:      "field_shape_mismatch",
			NotePath:  notePath,
			TypeName:  enclosingTypeName,
			FieldName: fieldPath,
			Message:   fmt.Sprintf("field %s expects a single value", fieldPath),
		})
		fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
		return fieldAssessment, []ValidationIssue{issue}
	}

	issues := make([]ValidationIssue, 0)
	for _, raw := range values {
		if !validateScalarValue(raw, field, schema) {
			issue := withTypeFieldVariant(ValidationIssue{
				Code:      "field_type_mismatch",
				NotePath:  notePath,
				TypeName:  enclosingTypeName,
				FieldName: fieldPath,
				Message:   fmt.Sprintf("field %s value %q does not match %s", fieldPath, raw, field.TypeName),
			})
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			issues = append(issues, issue)
			continue
		}
		fieldAssessment.ValidValues = append(fieldAssessment.ValidValues, raw)
	}
	return fieldAssessment, issues
}

func assessSectionLinkField(notePath string, enclosingTypeName string, propertyCase PropertyCase, field *Field, node *SectionNode, fieldPath string) (FieldAssessment, []ValidationIssue) {
	fieldAssessment := baseFieldAssessment(field)
	if node == nil {
		return fieldAssessment, nil
	}
	propertyName := defaultPropertyName(field.Name, propertyCase)
	inline := SectionInlineProperties(SectionOwnContent(node))
	values := normalizeStringValues(caseInsensitiveInlineValues(inline, propertyName))
	fieldAssessment.Present = caseInsensitiveInlineHasKey(inline, propertyName)
	fieldAssessment.Values = append([]string(nil), values...)

	if len(values) == 0 {
		if field.Required && (!field.List || !fieldAssessment.Present) {
			issue := withTypeFieldVariant(ValidationIssue{
				Code:      "missing_required_field",
				NotePath:  notePath,
				TypeName:  enclosingTypeName,
				FieldName: fieldPath,
				Message:   fmt.Sprintf("required link field %s is missing", fieldPath),
			})
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			return fieldAssessment, []ValidationIssue{issue}
		}
		return fieldAssessment, nil
	}
	if !field.List && len(values) > 1 {
		issue := withTypeFieldVariant(ValidationIssue{
			Code:      "field_shape_mismatch",
			NotePath:  notePath,
			TypeName:  enclosingTypeName,
			FieldName: fieldPath,
			Message:   fmt.Sprintf("field %s expects a single value", fieldPath),
		})
		fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
		return fieldAssessment, []ValidationIssue{issue}
	}
	fieldAssessment.ValidValues = append(fieldAssessment.ValidValues, values...)
	return fieldAssessment, nil
}

func assessLinkField(doc *noteDoc, field *Field, cache *obsidian.NotePathCache, resolveType func(string) string, schema *Schema, updatedAt int64) (FieldAssessment, RelationAssessment, []semdb.OntologyEdgeRow, []ValidationIssue) {
	values := extractFieldValues(doc, field)
	fieldAssessment := baseFieldAssessment(field)
	fieldAssessment.Present = fieldPresent(doc, field)
	fieldAssessment.Values = append([]string(nil), values...)
	relationAssessment := baseRelationAssessment(field)
	relationAssessment.Present = fieldAssessment.Present
	relationAssessment.Values = append([]string(nil), values...)
	issues := make([]ValidationIssue, 0)
	if len(values) == 0 {
		if field.Required && (!field.List || !fieldAssessment.Present) {
			issue := withTypeFieldVariant(ValidationIssue{
				Code:      "missing_required_field",
				NotePath:  doc.Path,
				TypeName:  doc.TypeName,
				FieldName: field.Name,
				Message:   fmt.Sprintf("required link field %s is missing", field.Name),
			})
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			relationAssessment.Issues = append(relationAssessment.Issues, issue)
			issues = append(issues, issue)
		}
		return fieldAssessment, relationAssessment, nil, issues
	}
	if !field.List && len(values) > 1 {
		issue := withTypeFieldVariant(ValidationIssue{
			Code:      "field_shape_mismatch",
			NotePath:  doc.Path,
			TypeName:  doc.TypeName,
			FieldName: field.Name,
			Message:   fmt.Sprintf("field %s expects a single value", field.Name),
		})
		fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
		relationAssessment.Issues = append(relationAssessment.Issues, issue)
		return fieldAssessment, relationAssessment, nil, []ValidationIssue{issue}
	}

	out := make([]semdb.OntologyEdgeRow, 0, len(values))
	for _, raw := range values {
		targetInput := unwrapLinkValue(raw)
		targetPath, ok := cache.ResolveNote(targetInput)
		if !ok {
			issue := withTypeFieldVariant(ValidationIssue{
				Code:      "link_target_missing",
				NotePath:  doc.Path,
				TypeName:  doc.TypeName,
				FieldName: field.Name,
				Message:   fmt.Sprintf("field %s target %q does not resolve to a note", field.Name, raw),
			})
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			relationAssessment.Issues = append(relationAssessment.Issues, issue)
			issues = append(issues, issue)
			continue
		}
		dstType := resolveType(targetPath)
		if !typeMatchesOrImplements(schema, dstType, field.TypeName) {
			issue := withTargetTypeVariant(ValidationIssue{
				Code:      "wrong_target_type",
				NotePath:  doc.Path,
				TypeName:  doc.TypeName,
				FieldName: field.Name,
				Message:   fmt.Sprintf("field %s expects %s but %s resolves to %s", field.Name, field.TypeName, raw, firstNonEmpty(dstType, "untyped note")),
			}, field.TypeName)
			fieldAssessment.Issues = append(fieldAssessment.Issues, issue)
			relationAssessment.Issues = append(relationAssessment.Issues, issue)
			issues = append(issues, issue)
			continue
		}
		fieldAssessment.ValidValues = append(fieldAssessment.ValidValues, targetPath)
		target := RelationTarget{
			Path:       targetPath,
			TypeName:   firstNonEmpty(dstType, field.TypeName),
			Provenance: "field",
			Structural: true,
		}
		relationAssessment.Targets = append(relationAssessment.Targets, target)
		out = append(out, semdb.OntologyEdgeRow{
			SrcPath:      doc.Path,
			RelationName: field.Name,
			DstPath:      targetPath,
			DstType:      target.TypeName,
			Provenance:   "field",
			Structural:   true,
			SchemaHash:   schema.Hash,
			UpdatedAt:    updatedAt,
		})
	}
	relationAssessment.Present = relationAssessment.Present || len(relationAssessment.Targets) > 0
	return fieldAssessment, relationAssessment, out, issues
}

func populateRelationAssessments(assessmentByPath map[string]*NoteAssessment, relationByPath map[string]map[string]*RelationAssessment, edges []semdb.OntologyEdgeRow) {
	for _, edge := range edges {
		byName := relationByPath[edge.SrcPath]
		if len(byName) == 0 {
			continue
		}
		relation := byName[edge.RelationName]
		if relation == nil {
			continue
		}
		if relationHasTarget(relation.Targets, edge) {
			continue
		}
		relation.Present = true
		relation.Targets = append(relation.Targets, RelationTarget{
			Path:       edge.DstPath,
			TypeName:   edge.DstType,
			Provenance: edge.Provenance,
			Structural: edge.Structural,
		})
	}
	for notePath, byName := range relationByPath {
		assessment := assessmentByPath[notePath]
		if assessment == nil {
			continue
		}
		for idx, relation := range assessment.Relations {
			if updated := byName[relation.Name]; updated != nil {
				assessment.Relations[idx] = *updated
			}
		}
	}
}

func relationHasTarget(targets []RelationTarget, edge semdb.OntologyEdgeRow) bool {
	for _, target := range targets {
		if target.Path == edge.DstPath && target.Provenance == edge.Provenance && target.Structural == edge.Structural {
			return true
		}
	}
	return false
}

func baseFieldAssessment(field *Field) FieldAssessment {
	return FieldAssessment{
		Name:                field.Name,
		Description:         field.Description,
		Kind:                field.Kind,
		TypeName:            field.TypeName,
		Required:            field.Required,
		List:                field.List,
		Source:              field.Source,
		SourceAliases:       append([]string(nil), field.SourceAliases...),
		SourceKind:          field.SourceKind,
		Semantics:           field.Semantics,
		Identifier:          field.IsIdentifier,
		PreferredIdentifier: field.IsPreferredIdentifier,
	}
}

func emptyFieldAssessment(field *Field) FieldAssessment {
	fieldAssessment := baseFieldAssessment(field)
	fieldAssessment.Present = false
	return fieldAssessment
}

func baseRelationAssessment(field *Field) RelationAssessment {
	return RelationAssessment{
		Name:          field.Name,
		Description:   field.Description,
		Kind:          field.Kind,
		TypeName:      field.TypeName,
		Required:      field.Required,
		List:          field.List,
		Source:        field.Source,
		SourceAliases: append([]string(nil), field.SourceAliases...),
		SourceKind:    field.SourceKind,
		Direction:     field.Direction,
		Semantics:     field.Semantics,
	}
}

func emptyRelationAssessment(field *Field) RelationAssessment {
	return baseRelationAssessment(field)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasSectionHeadingAtOtherLevel(nodes []*SectionNode, heading string, expected SectionLevel) bool {
	heading = strings.TrimSpace(heading)
	if heading == "" {
		return false
	}
	var walk func([]*SectionNode) bool
	walk = func(current []*SectionNode) bool {
		for _, node := range current {
			if node == nil {
				continue
			}
			if strings.TrimSpace(node.Title) == heading && node.Level != expected {
				return true
			}
			if walk(node.Children) {
				return true
			}
		}
		return false
	}
	return walk(nodes)
}

func fieldAllowsMissingAuthoredValue(field *Field) bool {
	return field != nil && field.IsPreferredIdentifier && field.IsDerivableIdentifier && field.IdentifierPopulate != IdentifierPopulateOnCreate
}
