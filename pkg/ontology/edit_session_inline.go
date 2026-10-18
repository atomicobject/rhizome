package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

func setInlineField(snapshot *DocumentSnapshot, projection *NodeProjection, field *ontoschema.Field, values []string) (string, error) {
	edits, err := inlineFieldEdits(snapshot, projection, field, values)
	if err != nil {
		return "", err
	}
	content := snapshot.Content
	for i := len(edits) - 1; i >= 0; i-- {
		content = replaceRange(content, edits[i].r, edits[i].replacement)
	}
	return content, nil
}

func unsetInlineField(snapshot *DocumentSnapshot, projection *NodeProjection, field *ontoschema.Field) (string, error) {
	if snapshot == nil || projection == nil || field == nil {
		return "", fmt.Errorf("%w: inline field projection is required", errMissingNode)
	}
	binding := projection.Fields[field.Name]
	if len(binding.InlineSpans) == 0 {
		return snapshot.Content, nil
	}
	content := snapshot.Content
	for index := len(binding.InlineSpans) - 1; index >= 0; index-- {
		span := binding.InlineSpans[index]
		rangeToRemove := inlineFieldEditRange(snapshot.Content, span)
		if !rangeToRemove.Valid(len(content)) {
			return "", fmt.Errorf("%w: invalid inline field range for %s", errCollectionDrift, field.Name)
		}
		content = replaceRange(content, rangeToRemove, "")
	}
	return content, nil
}

func inlineFieldEditRange(content string, span InlineFieldSpan) ByteRange {
	line, whole := span.LineRange, span.WholeRange
	if !line.Valid(len(content)) || !whole.Valid(len(content)) || whole.Start < line.Start || whole.End > line.End {
		return line
	}
	// Preserve surrounding prose and properties even when the schema does not declare them.
	if strings.TrimSpace(content[line.Start:whole.Start]) != "" || strings.TrimSpace(content[whole.End:line.End]) != "" {
		return whole
	}
	return line
}

func inlineFieldReplacementPrefix(content string, span InlineFieldSpan) string {
	if !span.LineRange.Valid(len(content)) || !span.KeyRange.Valid(len(content)) {
		return ""
	}
	if span.KeyRange.Start < span.LineRange.Start || span.KeyRange.Start > span.LineRange.End {
		return ""
	}
	prefix := content[span.LineRange.Start:span.KeyRange.Start]
	if isBulletInlinePropertyPrefix(prefix) {
		return prefix
	}
	return ""
}

func isBulletInlinePropertyPrefix(prefix string) bool {
	trimmed := strings.TrimSpace(prefix)
	return trimmed == "-" || trimmed == "*" || trimmed == "+"
}

func rangeStartsLine(content string, start int) bool {
	return start <= 0 || start > len(content) || content[start-1] == '\n'
}

func collectionInsertOffset(snapshot *DocumentSnapshot, parentProjection *NodeProjection, fieldName string) int {
	if collection, ok := parentProjection.Collections[fieldName]; ok && len(collection.Items) > 0 {
		return collection.Items[len(collection.Items)-1].Range.End
	}
	if parentProjection.Ref.Kind == NodeKindNote {
		return len(snapshot.Content)
	}
	return parentProjection.Ref.EndByte
}

func inlineInsertOffset(snapshot *DocumentSnapshot, projection *NodeProjection) int {
	if projection.Ref.Kind == NodeKindNote {
		return len(snapshot.Content)
	}
	node := snapshot.SectionsByID[projection.Ref.NodeID]
	if node == nil {
		return projection.Ref.EndByte
	}
	if strings.TrimSpace(node.BlockID) != "" {
		line := documentEOL(snapshot.Content) + "^" + strings.TrimPrefix(node.BlockID, "^")
		if idx := strings.LastIndex(node.Content, line); idx >= 0 {
			return node.StartByte + idx
		}
	}
	return node.EndByte
}

func embeddedNodeNeedsBlockID(snapshot *DocumentSnapshot, projection *NodeProjection) bool {
	if projection == nil || projection.Ref.Kind != NodeKindEmbedded {
		return false
	}
	node := snapshot.SectionsByID[projection.Ref.NodeID]
	if node == nil {
		return false
	}
	return strings.TrimSpace(node.BlockID) == ""
}

func replaceRange(content string, r ByteRange, replacement string) string {
	if !r.Valid(len(content)) {
		return content
	}
	return content[:r.Start] + replacement + content[r.End:]
}
