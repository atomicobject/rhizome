package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

func validateEmbeddedProjectionFields(snapshot *DocumentSnapshot, schema *ontoschema.Schema, root *NodeProjection) []ValidationIssue {
	if snapshot == nil || schema == nil || root == nil {
		return nil
	}
	resolver, err := newProjectionResolver(snapshot, schema)
	if err != nil {
		return []ValidationIssue{{Code: "projection_failed", NotePath: snapshot.NotePath, Message: err.Error()}}
	}
	return validateEmbeddedFieldsWithResolver(snapshot, schema, root, resolver)
}

func validateEmbeddedFieldsWithResolver(snapshot *DocumentSnapshot, schema *ontoschema.Schema, root *NodeProjection, resolver *projectionResolver) []ValidationIssue {
	if snapshot == nil || schema == nil || root == nil {
		return nil
	}
	lines := newLineIndex(snapshot.Content)
	seen := map[string]struct{}{}
	var issues []ValidationIssue
	var walk func(*NodeProjection)
	visit := func(ref NodeRef) {
		if resolver == nil {
			return
		}
		if key := ref.String(); key != "" {
			if _, ok := seen[key]; ok {
				return
			}
		}
		child, err := resolver.projectCurrent(ref)
		if err != nil {
			issues = append(issues, ValidationIssue{Code: "projection_failed", NotePath: snapshot.NotePath, NodeRef: ref.String(), Message: err.Error()})
			return
		}
		walk(child)
	}
	walk = func(projection *NodeProjection) {
		if projection == nil {
			return
		}
		key := projection.Ref.String()
		if key != "" {
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
		}
		if projection.Type != nil && projection.Type.Role == ontoschema.TypeRoleEmbeddedNode {
			issues = append(issues, validateTitleConstraint(
				snapshot.NotePath,
				projection.Type,
				projection.Ref.String(),
				projectionTitle(snapshot, projection.Ref),
				projection.Ref.Structural,
			)...)
			for _, field := range projection.Type.Fields {
				if field == nil || (field.Kind != ontoschema.FieldKindScalar && field.Kind != ontoschema.FieldKindEnum) {
					continue
				}
				binding := projection.Fields[field.Name]
				issues = append(issues, validateEmbeddedScalarBinding(snapshot, lines, projection, field, binding, schema)...)
			}
		}
		for collectionName, collection := range projection.Collections {
			if projection.Type == nil {
				continue
			}
			field := projection.Type.ByName[collectionName]
			if field == nil || field.Kind != ontoschema.FieldKindSection {
				continue
			}
			issues = append(issues, validateCollectionCardinality(snapshot, lines, projection, field, collection)...)
		}
		for _, binding := range projection.Fields {
			for _, ref := range binding.SectionNodes {
				visit(ref)
			}
		}
		for _, collection := range projection.Collections {
			for _, item := range collection.Items {
				visit(item.Ref)
			}
		}
	}
	walk(root)
	if resolver != nil {
		for _, ref := range resolver.globalSourceNodeRefs() {
			visit(ref)
		}
	}
	return issues
}

func projectionTitle(snapshot *DocumentSnapshot, ref NodeRef) string {
	if snapshot == nil {
		return ""
	}
	if section := snapshot.SectionsByID[ref.NodeID]; section != nil {
		return section.Title
	}
	if span := snapshot.SourceSpansByID[ref.NodeID]; span != nil {
		return span.Title
	}
	return ""
}

func validateCollectionCardinality(snapshot *DocumentSnapshot, lines lineIndex, projection *NodeProjection, field *ontoschema.Field, collection CollectionBinding) []ValidationIssue {
	if snapshot == nil || projection == nil || field == nil {
		return nil
	}
	count := len(collection.Items)
	if field.ContainsMin > 0 && count < field.ContainsMin {
		return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
			Code:       "contains_min_not_met",
			NotePath:   snapshot.NotePath,
			TypeName:   projection.ResolvedType,
			FieldName:  field.Name,
			NodeRef:    projection.Ref.String(),
			NodeID:     projection.Ref.NodeID,
			Structural: projection.Ref.Structural,
			Line:       lines.lineAt(projection.Ref.StartByte),
			Message:    fmt.Sprintf("field %s must contain at least %d item(s)", field.Name, field.ContainsMin),
		})}
	}
	if field.ContainsMax >= 0 && count > field.ContainsMax {
		return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
			Code:       "contains_max_exceeded",
			NotePath:   snapshot.NotePath,
			TypeName:   projection.ResolvedType,
			FieldName:  field.Name,
			NodeRef:    projection.Ref.String(),
			NodeID:     projection.Ref.NodeID,
			Structural: projection.Ref.Structural,
			Line:       lines.lineAt(collection.Range.Start),
			Message:    fmt.Sprintf("field %s must contain at most %d item(s)", field.Name, field.ContainsMax),
		})}
	}
	return nil
}

func validateEmbeddedScalarBinding(snapshot *DocumentSnapshot, lines lineIndex, projection *NodeProjection, field *ontoschema.Field, binding FieldBinding, schema *ontoschema.Schema) []ValidationIssue {
	if len(binding.Values) == 0 {
		if field.Required && !fieldAllowsMissingAuthoredValue(field) {
			return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
				Code:       "missing_required_field",
				NotePath:   snapshot.NotePath,
				TypeName:   projection.ResolvedType,
				FieldName:  field.Name,
				NodeRef:    projection.Ref.String(),
				NodeID:     projection.Ref.NodeID,
				Structural: projection.Ref.Structural,
				Line:       lines.lineAt(projection.Ref.StartByte),
				Message:    fmt.Sprintf("required field %s is missing", field.Name),
			})}
		}
		return nil
	}
	if !field.List && len(binding.Values) > 1 {
		return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
			Code:       "field_shape_mismatch",
			NotePath:   snapshot.NotePath,
			TypeName:   projection.ResolvedType,
			FieldName:  field.Name,
			NodeRef:    projection.Ref.String(),
			NodeID:     projection.Ref.NodeID,
			Structural: projection.Ref.Structural,
			Line:       embeddedFieldLine(lines, binding, projection.Ref.StartByte),
			Message:    fmt.Sprintf("field %s expects a single value", field.Name),
		})}
	}
	issues := make([]ValidationIssue, 0)
	issues = append(issues, validateEmbeddedAuthoringStyle(snapshot, lines, projection, field, binding)...)
	for _, raw := range binding.Values {
		if !validateScalarValue(raw, field, schema) {
			issues = append(issues, withTypeFieldVariant(ValidationIssue{
				Code:       "field_type_mismatch",
				NotePath:   snapshot.NotePath,
				TypeName:   projection.ResolvedType,
				FieldName:  field.Name,
				NodeRef:    projection.Ref.String(),
				NodeID:     projection.Ref.NodeID,
				Structural: projection.Ref.Structural,
				Line:       embeddedFieldLine(lines, binding, projection.Ref.StartByte),
				Message:    fmt.Sprintf("field %s value %q does not match %s", field.Name, raw, field.TypeName),
			}))
			continue
		}
		if issue := validateFormattedValue(snapshot.NotePath, projection.ResolvedType, field.Name, projection.Ref.String(), projection.Ref.Structural, raw, field.Format); issue != nil {
			issue.NodeID = projection.Ref.NodeID
			issue.Line = embeddedFieldLine(lines, binding, projection.Ref.StartByte)
			issues = append(issues, *issue)
		}
	}
	return issues
}

func validateEmbeddedAuthoringStyle(snapshot *DocumentSnapshot, lines lineIndex, projection *NodeProjection, field *ontoschema.Field, binding FieldBinding) []ValidationIssue {
	if field == nil || field.AuthoringStyle == "" || field.AuthoringStyle == ontoschema.FieldAuthoringStyleAny {
		return nil
	}
	valid := true
	switch field.AuthoringStyle {
	case ontoschema.FieldAuthoringStyleListMetadata, ontoschema.FieldAuthoringStyleInlineProperty:
		if len(binding.InlineSpans) == 0 {
			valid = false
			break
		}
		for _, span := range binding.InlineSpans {
			if span.AuthoringStyle != field.AuthoringStyle {
				valid = false
				break
			}
		}
	case ontoschema.FieldAuthoringStyleItemText:
		valid = field.SourceKind == ontoschema.FieldSourceItemTitle || field.SourceKind == ontoschema.FieldSourceItemSummary || field.SourceKind == ontoschema.FieldSourceItemDetail
	case ontoschema.FieldAuthoringStyleCheckbox:
		valid = field.SourceKind == ontoschema.FieldSourceCheckbox
	case ontoschema.FieldAuthoringStyleFrontmatter:
		valid = field.SourceKind == ontoschema.FieldSourceFrontmatter
	}
	if valid {
		return nil
	}
	return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
		Code:       "field_authoring_style_mismatch",
		NotePath:   snapshot.NotePath,
		TypeName:   projection.ResolvedType,
		FieldName:  field.Name,
		NodeRef:    projection.Ref.String(),
		NodeID:     projection.Ref.NodeID,
		Structural: projection.Ref.Structural,
		Line:       embeddedFieldLine(lines, binding, projection.Ref.StartByte),
		Message:    fmt.Sprintf("field %s must be authored as %s", field.Name, field.AuthoringStyle),
	})}
}

func embeddedFieldLine(lines lineIndex, binding FieldBinding, fallback int) int {
	if len(binding.ValueRanges) > 0 {
		return lines.lineAt(binding.ValueRanges[0].Start)
	}
	return lines.lineAt(fallback)
}

// WHY: Per SPEC-0023's usage-driven block-id lifecycle, default validation
// reports drift (duplicate, malformed) but not absence. The
// missing_embedded_block_id diagnostic is intentionally dropped here so vault-
// wide sweeps cannot eagerly insert block IDs on un-cited embedded nodes.
// ValidateEmbeddedBlockIDs remains callable for explicit/scoped runs.
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us2]]
func validationIssuesFromBlockIDs(doc *noteDoc, schema *ontoschema.Schema) []ValidationIssue {
	if doc == nil || schema == nil {
		return nil
	}
	snapshot, err := documentSnapshotForDoc(doc)
	if err != nil {
		return nil
	}
	blockIssues := ValidateEmbeddedBlockIDs(snapshot, schema)
	lines := newLineIndex(snapshot.Content)
	out := make([]ValidationIssue, 0, len(blockIssues))
	for _, issue := range blockIssues {
		if issue.Code == "missing_embedded_block_id" {
			continue
		}
		out = append(out, ValidationIssue{
			Code:       issue.Code,
			NotePath:   issue.NotePath,
			TypeName:   issue.TypeName,
			NodeRef:    issue.Ref.String(),
			NodeID:     issue.Ref.NodeID,
			Structural: issue.Ref.Structural,
			Line:       lines.lineAt(issue.Ref.StartByte),
			LinkKind:   "block",
			LinkTarget: issue.BlockID,
			Message:    issue.Message,
			FixTarget:  issue.NotePath,
		})
	}
	return out
}
