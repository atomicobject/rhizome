package ontology

import (
	"fmt"
	"strings"
)

// AddSectionField stages an explicit schema-declared section scaffold. The
// field path may name a nested section (for example requirements.details).
// Replay resolves every parent node from current source and derives placement
// from schema field order; callers never supply byte offsets.
func (s *EditSession) AddSectionField(ref NodeRef, fieldPath string) error {
	return s.stage(addSectionFieldOp{Ref: ref, FieldPath: strings.TrimSpace(fieldPath)})
}

type addSectionFieldOp struct {
	Ref       NodeRef
	FieldPath string
}

func (op addSectionFieldOp) notePath() string { return op.Ref.NotePath }

func (op addSectionFieldOp) apply(state *documentState) error {
	snapshot, parent, err := projectState(state, op.Ref)
	if err != nil {
		return err
	}
	parts := splitSectionFieldPath(op.FieldPath)
	if len(parts) == 0 {
		return fmt.Errorf("%w: section field path is required", errMissingField)
	}
	for _, fieldName := range parts[:len(parts)-1] {
		field := parent.Type.ByName[fieldName]
		if field == nil || field.Kind != FieldKindSection {
			return fmt.Errorf("%w: section field %s not found on %s", errMissingField, fieldName, parent.Ref.String())
		}
		binding := parent.Fields[fieldName]
		if len(binding.SectionNodes) != 1 {
			return fmt.Errorf("%w: section field %s does not resolve to one parent node", errCollectionDrift, fieldName)
		}
		parent, err = ProjectBoundNodeFromSnapshot(snapshot, state.schema, binding.SectionNodes[0])
		if err != nil {
			return err
		}
	}

	targetName := parts[len(parts)-1]
	target := parent.Type.ByName[targetName]
	if target == nil || target.Kind != FieldKindSection {
		return fmt.Errorf("%w: section field %s not found on %s", errMissingField, targetName, parent.Ref.String())
	}
	if binding, ok := parent.Fields[targetName]; ok && len(binding.SectionNodes) > 0 {
		return nil
	}
	heading := strings.TrimSpace(target.SectionHeading)
	level := sectionLevelInt(target.SectionLevel)
	if heading == "" || level == 0 {
		return fmt.Errorf("%w: section field %s has no explicit heading placement", errUnsupportedTarget, op.FieldPath)
	}
	insertAt, err := orderedSectionInsertOffset(snapshot, parent, targetName)
	if err != nil {
		return err
	}
	snippet := sectionHeadingSnippet(state.content, insertAt, strings.Repeat("#", level)+" "+heading)
	state.setContent(state.content[:insertAt] + snippet + state.content[insertAt:])
	return nil
}

func splitSectionFieldPath(fieldPath string) []string {
	raw := strings.Split(strings.TrimSpace(fieldPath), ".")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func orderedSectionInsertOffset(snapshot *DocumentSnapshot, parent *NodeProjection, targetName string) (int, error) {
	if snapshot == nil || parent == nil || parent.Type == nil {
		return 0, fmt.Errorf("%w: section parent projection is required", errMissingNode)
	}
	targetIndex := -1
	for index, field := range parent.Type.Fields {
		if field != nil && field.Name == targetName {
			targetIndex = index
			break
		}
	}
	if targetIndex < 0 {
		return 0, fmt.Errorf("%w: section field %s not found on %s", errMissingField, targetName, parent.Ref.String())
	}
	insertAt := -1
	for _, field := range parent.Type.Fields[targetIndex+1:] {
		if field == nil || field.Kind != FieldKindSection {
			continue
		}
		for _, ref := range parent.Fields[field.Name].SectionNodes {
			if ref.StartByte >= 0 && (insertAt < 0 || ref.StartByte < insertAt) {
				insertAt = ref.StartByte
			}
		}
	}
	if insertAt < 0 {
		insertAt = inlineInsertOffset(snapshot, parent)
	}
	if insertAt < 0 || insertAt > len(snapshot.Content) {
		return 0, fmt.Errorf("%w: invalid insertion point for section field %s", errCollectionDrift, targetName)
	}
	return insertAt, nil
}

func sectionHeadingSnippet(content string, insertAt int, heading string) string {
	before, after := content[:insertAt], content[insertAt:]
	var b strings.Builder
	if before != "" {
		switch {
		case strings.HasSuffix(before, "\n\n"):
		case strings.HasSuffix(before, "\n"):
			b.WriteByte('\n')
		default:
			b.WriteString("\n\n")
		}
	}
	b.WriteString(heading)
	b.WriteByte('\n')
	if after != "" && !strings.HasPrefix(after, "\n") {
		b.WriteByte('\n')
	}
	return b.String()
}
