package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

func (op setFieldOp) notePath() string { return op.Ref.NotePath }

func (op addEmbeddedNodeOp) notePath() string { return op.ParentRef.NotePath }

func (op deleteNodeOp) notePath() string { return op.Ref.NotePath }

func (op reorderCollectionOp) notePath() string { return op.ParentRef.NotePath }

func (op setNarrativeOp) notePath() string { return op.Ref.NotePath }

func (op insertNarrativeOp) notePath() string { return op.Ref.NotePath }

func (op ensureBlockIDOp) notePath() string { return op.Ref.NotePath }

func (op setBlockIDOp) notePath() string { return op.Ref.NotePath }

func (op removeBlockIDOp) notePath() string { return op.Ref.NotePath }

func (op rewriteFileOp) notePath() string { return op.NotePathValue }

func (op rewriteFileOp) apply(state *documentState) error {
	state.setContent(op.NewContent)
	return nil
}

func (op transformFileOp) notePath() string { return op.NotePathValue }

func (op transformFileOp) apply(state *documentState) error {
	updated, err := op.Transform(state.content)
	if err != nil {
		return err
	}
	state.setContent(updated)
	return nil
}

func (op setFieldOp) apply(state *documentState) error {
	if op.RawRootMetadata {
		if strings.TrimSpace(op.Field) == "" {
			return fmt.Errorf("%w: root metadata field is required", errUnsupportedTarget)
		}
		if op.Ref.Kind != NodeKindNote || strings.TrimSpace(op.Ref.Fragment) != "" || strings.TrimSpace(op.Ref.NodeID) != "" {
			return fmt.Errorf("%w: raw root metadata requires a note root", errUnsupportedTarget)
		}
		if op.Unset {
			updated, err := unsetFrontmatterField(state.content, &ontoschema.Field{
				Name: op.Field, Source: op.Field, SourceKind: ontoschema.FieldSourceFrontmatter,
				Kind: ontoschema.FieldKindScalar, TypeName: "String", List: true,
			})
			if err != nil {
				return err
			}
			state.setContent(updated)
			return nil
		}
		updated, err := setFrontmatterField(state.content, &ontoschema.Field{Name: op.Field, Source: op.Field, Kind: ontoschema.FieldKindScalar, TypeName: "String", List: true}, op.Values, false)
		if err != nil {
			return err
		}
		state.setContent(updated)
		return nil
	}
	snapshot, projection, err := projectState(state, op.Ref)
	if err != nil {
		return err
	}
	field, err := validateSetFieldOperation(projection, op, state.schema, state.allowSelectorRecovery)
	if err != nil {
		return err
	}
	if strings.EqualFold(op.Field, "title") {
		updated, err := setNodeTitleField(snapshot, projection, op.Values)
		if err != nil {
			return err
		}
		state.setContent(updated)
		return nil
	}
	if op.Unset {
		var updated string
		switch {
		case field.SourceKind == ontoschema.FieldSourceCheckbox:
			updated, err = setCheckboxField(snapshot, projection, field, []string{"false"})
		case projection.Ref.Kind == NodeKindNote && field.SourceKind == ontoschema.FieldSourceFrontmatter:
			updated, err = unsetFrontmatterField(state.content, field)
		case field.SourceKind == ontoschema.FieldSourceInline:
			updated, err = unsetInlineField(snapshot, projection, field)
		default:
			err = fmt.Errorf("%w: unsupported field target %s", errUnsupportedTarget, op.Field)
		}
		if err != nil {
			return err
		}
		state.setContent(updated)
		return nil
	}
	if field.SourceKind == ontoschema.FieldSourceCheckbox {
		updated, err := setCheckboxField(snapshot, projection, field, op.Values)
		if err != nil {
			return err
		}
		state.setContent(updated)
		return nil
	}
	if projection.Ref.Kind == NodeKindEmbedded && embeddedNodeNeedsBlockID(snapshot, projection) {
		updated, _, err := EnsureBlockIDInSnapshot(snapshot, projection, "")
		if err != nil {
			return err
		}
		state.setContent(updated)
		updatedRef := op.Ref
		updatedRef.NodeID = ""
		updatedRef.Structural = ""
		updatedRef.StartByte = 0
		updatedRef.EndByte = 0
		snapshot, projection, err = projectState(state, updatedRef)
		if err != nil {
			return err
		}
	}
	var updated string
	switch {
	case projection.Ref.Kind == NodeKindNote && field.SourceKind == ontoschema.FieldSourceFrontmatter:
		updated, err = setFrontmatterField(state.content, field, op.Values, op.IsLinks)
	case field.SourceKind == ontoschema.FieldSourceInline:
		updated, err = setInlineField(snapshot, projection, field, op.Values)
	default:
		err = fmt.Errorf("%w: unsupported field target %s", errUnsupportedTarget, op.Field)
	}
	if err != nil {
		return err
	}
	state.setContent(updated)
	return nil
}

func fieldSupportsValueList(field *ontoschema.Field) bool {
	return field != nil && field.List && (field.Kind == ontoschema.FieldKindScalar || field.Kind == ontoschema.FieldKindEnum)
}

func validateSetFieldOperation(projection *NodeProjection, op setFieldOp, schema *ontoschema.Schema, allowIdentifierRepair bool) (*ontoschema.Field, error) {
	if projection == nil || projection.Type == nil {
		if strings.EqualFold(op.Field, "title") {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: node %s has no resolved ontology type", errMissingNode, op.Ref.String())
	}
	field := projection.Type.ByName[op.Field]
	if field == nil && strings.EqualFold(op.Field, "title") {
		for name, candidate := range projection.Type.ByName {
			if strings.EqualFold(name, op.Field) {
				field = candidate
				break
			}
		}
	}
	if field == nil {
		if strings.EqualFold(op.Field, "title") {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: field %s not found on %s", errMissingField, op.Field, projection.Ref.String())
	}
	if field.IsIdentifier && !allowIdentifierRepair {
		return nil, fmt.Errorf("%w: identifier field %s requires the graph-safe identifier rename workflow", errUnsupportedTarget, op.Field)
	}
	if op.RequireScalarList && !fieldSupportsValueList(field) {
		return nil, fmt.Errorf("%w: field %s is not a scalar or enum list", errUnsupportedTarget, op.Field)
	}
	if op.IsLinks && field.Kind != ontoschema.FieldKindLink {
		return nil, fmt.Errorf("%w: field %s is not a relation", errUnsupportedTarget, op.Field)
	}
	if !op.IsLinks && field.Kind == ontoschema.FieldKindLink {
		return nil, fmt.Errorf("%w: relation field %s requires a link edit", errUnsupportedTarget, op.Field)
	}
	if op.Unset {
		if field.Required {
			return nil, fmt.Errorf("%w: required field %s cannot be unset", errUnsupportedTarget, op.Field)
		}
		return field, nil
	}
	if err := validateEditFieldValues(field, op.Values, schema); err != nil {
		return nil, err
	}
	if op.IsLinks {
		for _, value := range op.Values {
			if strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("%w: relation field %s contains an empty target", errUnsupportedTarget, op.Field)
			}
		}
	}
	return field, nil
}

func setNodeTitleField(snapshot *DocumentSnapshot, projection *NodeProjection, values []string) (string, error) {
	if snapshot == nil || projection == nil {
		return "", fmt.Errorf("%w: title projection is required", errMissingNode)
	}
	if len(values) != 1 {
		return "", fmt.Errorf("%w: title expects one value", errUnsupportedTarget)
	}
	value := oneLineTitleValue(values[0])
	switch projection.Ref.Kind {
	case NodeKindNote:
		if _, ok := snapshot.Frontmatter["title"]; ok || snapshot.FrontmatterRange.Len() > 0 {
			return setFrontmatterField(snapshot.Content, &ontoschema.Field{Name: "title", Source: "title", SourceKind: ontoschema.FieldSourceFrontmatter, TypeName: "String"}, []string{value}, false)
		}
		return setNotePrimaryHeadingTitle(snapshot, value)
	case NodeKindSection:
		return setSectionHeadingTitle(snapshot, projection, value)
	case NodeKindEmbedded:
		if span := snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
			if span.Shape == ontoschema.EmbeddedSourceShapeSection {
				return setSectionHeadingTitle(snapshot, projection, value)
			}
			return setSourceSpanTitleField(snapshot, projection, value)
		}
		return setSectionHeadingTitle(snapshot, projection, value)
	default:
		return "", fmt.Errorf("%w: unsupported title target %s", errUnsupportedTarget, projection.Ref.String())
	}
}

func setNotePrimaryHeadingTitle(snapshot *DocumentSnapshot, value string) (string, error) {
	if snapshot == nil {
		return "", fmt.Errorf("%w: title projection is required", errMissingNode)
	}
	h1s := make([]*SectionNode, 0, 1)
	for _, node := range snapshot.Sections {
		if node != nil && node.Level == ontoschema.SectionLevelH1 {
			h1s = append(h1s, node)
		}
	}
	if len(h1s) != 1 {
		return "", fmt.Errorf("%w: note title requires title frontmatter or one H1", errUnsupportedTarget)
	}
	return replaceHeadingLineTitle(snapshot.Content, h1s[0].StartByte, value)
}

func setSectionHeadingTitle(snapshot *DocumentSnapshot, projection *NodeProjection, value string) (string, error) {
	if snapshot == nil || projection == nil {
		return "", fmt.Errorf("%w: title projection is required", errMissingNode)
	}
	if node := snapshot.SectionsByID[projection.Ref.NodeID]; node != nil {
		return replaceHeadingLineTitle(snapshot.Content, node.StartByte, value)
	}
	return "", fmt.Errorf("%w: heading title not found for %s", errMissingNode, projection.Ref.String())
}

func replaceHeadingLineTitle(content string, lineStart int, value string) (string, error) {
	if lineStart < 0 || lineStart >= len(content) {
		return "", fmt.Errorf("%w: invalid heading title range", errCollectionDrift)
	}
	lineEnd := len(content)
	if nextLine := strings.IndexByte(content[lineStart:], '\n'); nextLine >= 0 {
		lineEnd = lineStart + nextLine
	}
	line := content[lineStart:lineEnd]
	leading := 0
	for leading < len(line) && line[leading] == ' ' {
		leading++
	}
	hashes := leading
	for hashes < len(line) && line[hashes] == '#' {
		hashes++
	}
	if hashes == leading || hashes-leading > 6 {
		return "", fmt.Errorf("%w: target line is not a heading", errUnsupportedTarget)
	}
	titleStart := hashes
	for titleStart < len(line) && (line[titleStart] == ' ' || line[titleStart] == '\t') {
		titleStart++
	}
	titleEnd := trimHeadingTitleEnd(line, titleStart, len(line))
	if titleEnd < titleStart {
		titleEnd = titleStart
	}
	return replaceRange(content, ByteRange{Start: lineStart + titleStart, End: lineStart + titleEnd}, value), nil
}

func trimHeadingTitleEnd(line string, start, end int) int {
	end = trimSpaceLeftBounded(line, start, end)
	cursor := end - 1
	for cursor >= start && line[cursor] == '#' {
		cursor--
	}
	if cursor < end-1 {
		beforeHashes := trimSpaceLeftBounded(line, start, cursor+1)
		if beforeHashes < cursor+1 {
			return beforeHashes
		}
	}
	return end
}

func setSourceSpanTitleField(snapshot *DocumentSnapshot, projection *NodeProjection, value string) (string, error) {
	span := snapshot.SourceSpansByID[projection.Ref.NodeID]
	if span == nil || (span.Shape != ontoschema.EmbeddedSourceShapeCheckboxItem && span.Shape != ontoschema.EmbeddedSourceShapeListItem) {
		return "", fmt.Errorf("%w: unsupported title target %s", errUnsupportedTarget, projection.Ref.String())
	}
	if !span.ContentRange.Valid(len(snapshot.Content)) || span.ContentRange.Len() == 0 {
		return "", fmt.Errorf("%w: invalid title range for %s", errCollectionDrift, projection.Ref.String())
	}
	edit, err := sourceSpanTitleEdit(snapshot, span, value)
	if err != nil {
		return "", err
	}
	return replaceRange(snapshot.Content, edit.r, edit.replacement), nil
}

func oneLineTitleValue(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\n", " ")), " ")
}

func setCheckboxField(snapshot *DocumentSnapshot, projection *NodeProjection, field *ontoschema.Field, values []string) (string, error) {
	if snapshot == nil || projection == nil || field == nil {
		return "", fmt.Errorf("%w: checkbox projection is required", errMissingNode)
	}
	if field.TypeName != "Boolean" || field.List {
		return "", fmt.Errorf("%w: checkbox field %s must be a singular Boolean", errUnsupportedTarget, field.Name)
	}
	if len(values) != 1 {
		return "", fmt.Errorf("%w: checkbox field %s expects one value", errUnsupportedTarget, field.Name)
	}
	desired, err := parseCheckboxEditValue(values[0])
	if err != nil {
		return "", err
	}
	span := snapshot.SourceSpansByID[projection.Ref.NodeID]
	if span == nil || span.Shape != ontoschema.EmbeddedSourceShapeCheckboxItem || span.Checkbox == nil {
		return "", fmt.Errorf("%w: %s is not a checkbox item", errUnsupportedTarget, projection.Ref.String())
	}
	r := span.Checkbox.TokenRange
	if !r.Valid(len(snapshot.Content)) || r.Len() != len("[ ]") {
		return "", fmt.Errorf("%w: invalid checkbox token range for %s", errCollectionDrift, projection.Ref.String())
	}
	if span.Checkbox.Checked == desired {
		return snapshot.Content, nil
	}
	token := "[ ]"
	if desired {
		token = "[x]"
	}
	return replaceRange(snapshot.Content, r, token), nil
}

func parseCheckboxEditValue(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "checked", "done", "x", "1":
		return true, nil
	case "false", "unchecked", "open", " ", "0":
		return false, nil
	default:
		return false, fmt.Errorf("%w: invalid checkbox value %q", errUnsupportedTarget, value)
	}
}
