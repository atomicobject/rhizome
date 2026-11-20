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
		rangeToRemove := inlineFieldRemovalRange(snapshot.Content, span)
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

// inlineFieldRemovalRange widens a removal to the whole line, newline included, when the
// property is alone on an indented line, so removing it leaves no blank line behind.
func inlineFieldRemovalRange(content string, span InlineFieldSpan) ByteRange {
	r := inlineFieldEditRange(content, span)
	if !r.Valid(len(content)) || (r.End > 0 && content[r.End-1] == '\n') {
		return r
	}
	start := strings.LastIndexByte(content[:r.Start], '\n') + 1
	end := len(content)
	if nl := strings.IndexByte(content[r.End:], '\n'); nl >= 0 {
		end = r.End + nl + 1
	}
	if strings.TrimSpace(content[start:r.Start]) != "" || strings.TrimSpace(content[r.End:end]) != "" {
		return r
	}
	return ByteRange{Start: start, End: end}
}

// listItemInlineInsert finds where a new inline property goes on a list or checkbox item:
// on its own line after the item's last line before any child item or any paragraph that a
// blank line has moved out of the item, indented like the item's last property line that
// reaches its content column, or to that column when it has none.
func listItemInlineInsert(content string, span *MarkdownSourceSpan) (int, string, bool) {
	if span.Shape != ontoschema.EmbeddedSourceShapeListItem && span.Shape != ontoschema.EmbeddedSourceShapeCheckboxItem {
		return 0, "", false
	}
	start, end := span.Range.Start, span.Range.End
	if len(span.OwnContentRanges) > 0 {
		end = span.OwnContentRanges[0].End
	}
	contentStart := span.ContentRange.Start
	if span.Checkbox != nil {
		contentStart = span.Checkbox.TokenRange.Start
	}
	if !(ByteRange{Start: start, End: end}).Valid(len(content)) || contentStart < start || contentStart > end {
		return 0, "", false
	}
	column := contentStart - start
	itemLine := content[start:end]
	itemLead := len(itemLine) - len(strings.TrimLeft(itemLine, " \t"))
	indent := itemLine[:min(itemLead, column)] + strings.Repeat(" ", max(column-itemLead, 0))
	insertAt, afterBlank := end, false
	for lineStart := start; lineStart < end; {
		lineEnd, next := end, end
		if nl := strings.IndexByte(content[lineStart:end], '\n'); nl >= 0 {
			lineEnd, next = lineStart+nl, lineStart+nl+1
		}
		line := strings.TrimRight(content[lineStart:lineEnd], "\r")
		trimmed := strings.TrimSpace(line)
		lead := len(line) - len(strings.TrimLeft(line, " \t"))
		switch {
		case trimmed == "":
			afterBlank = true
		case lineStart > start && afterBlank && lead < column:
			return insertAt, indent, true
		default:
			afterBlank = false
			insertAt = next
			if fields := scanInlineFieldsInListSegment(trimmed, 0); lineStart > start && lead >= column && len(fields) > 0 && fields[0].KeyRange.Start == 0 {
				indent = line[:lead]
			}
		}
		lineStart = next
	}
	return insertAt, indent, true
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
