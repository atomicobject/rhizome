package ontology

import (
	"fmt"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"strings"
)

func EnsureBlockIDInSnapshot(snapshot *DocumentSnapshot, projection *NodeProjection, preferred string) (string, string, error) {
	if snapshot == nil || projection == nil {
		return "", "", fmt.Errorf("%w: projection is required", errMissingNode)
	}
	if projection.Ref.Kind != NodeKindEmbedded {
		return snapshot.Content, projection.Ref.Fragment, nil
	}
	node := snapshot.SectionsByID[projection.Ref.NodeID]
	if node == nil {
		if span := snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil && span.Shape != ontoschema.EmbeddedSourceShapeSection {
			return ensureSourceSpanBlockID(snapshot, projection, span, preferred)
		}
	}
	if node == nil {
		return "", "", fmt.Errorf("%w: embedded node %s not found", errMissingNode, projection.Ref.String())
	}
	if fragment := sectionFragment(node); strings.HasPrefix(fragment, "^") {
		return snapshot.Content, strings.TrimPrefix(fragment, "^"), nil
	}
	blockID := strings.TrimSpace(strings.TrimPrefix(preferred, "^"))
	if blockID == "" {
		blockID = GenerateBlockIDForProjection(snapshot, projection)
	}
	if !ValidBlockID(blockID) {
		return "", "", fmt.Errorf("%w: invalid block ID %q", errUnsupportedTarget, blockID)
	}
	blockID = uniqueBlockID(blockID, existingBlockIDs(snapshot), projection)
	insertAt := blockIDInsertOffset(node)
	if insertAt < 0 || insertAt > len(snapshot.Content) {
		return "", "", fmt.Errorf("%w: invalid block ID insert offset for %s", errUnsupportedTarget, projection.Ref.String())
	}
	eol := documentEOL(snapshot.Content)
	text := eol + "^" + blockID + eol
	if insertAt > 0 && snapshot.Content[insertAt-1] == '\n' {
		text = "^" + blockID + eol
	}
	updated := snapshot.Content[:insertAt] + text + snapshot.Content[insertAt:]
	return updated, blockID, nil
}

func ensureSourceSpanBlockID(snapshot *DocumentSnapshot, projection *NodeProjection, span *MarkdownSourceSpan, preferred string) (string, string, error) {
	if snapshot == nil || projection == nil || span == nil {
		return "", "", fmt.Errorf("%w: source span is required", errMissingNode)
	}
	if fragment := sourceSpanFragment(span); strings.HasPrefix(fragment, "^") {
		return snapshot.Content, strings.TrimPrefix(fragment, "^"), nil
	}
	blockID := strings.TrimSpace(strings.TrimPrefix(preferred, "^"))
	if blockID == "" {
		blockID = GenerateBlockIDForProjection(snapshot, projection)
	}
	if !ValidBlockID(blockID) {
		return "", "", fmt.Errorf("%w: invalid block ID %q", errUnsupportedTarget, blockID)
	}
	blockID = uniqueBlockID(blockID, existingBlockIDs(snapshot), projection)
	insertAt := sourceSpanBlockIDInsertOffset(snapshot.Content, span)
	if insertAt < 0 || insertAt > len(snapshot.Content) {
		return "", "", fmt.Errorf("%w: invalid block ID insert offset for %s", errUnsupportedTarget, projection.Ref.String())
	}
	updated := snapshot.Content[:insertAt] + " ^" + blockID + snapshot.Content[insertAt:]
	return updated, blockID, nil
}

func sourceSpanBlockIDInsertOffset(content string, span *MarkdownSourceSpan) int {
	if span == nil || !span.Range.Valid(len(content)) {
		return -1
	}
	lineEnd := span.Range.End
	if idx := strings.IndexByte(content[span.Range.Start:span.Range.End], '\n'); idx >= 0 {
		lineEnd = span.Range.Start + idx
	}
	for lineEnd > span.Range.Start && (content[lineEnd-1] == ' ' || content[lineEnd-1] == '\t') {
		lineEnd--
	}
	return lineEnd
}

func blockIDInsertOffset(node *SectionNode) int {
	if node == nil {
		return -1
	}
	insertAt := node.EndByte
	for _, child := range node.Children {
		if child != nil && child.StartByte > node.StartByte && child.StartByte < insertAt {
			insertAt = child.StartByte
		}
	}
	return insertAt
}
