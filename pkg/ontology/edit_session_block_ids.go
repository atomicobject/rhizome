package ontology

import (
	"errors"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

func (op ensureBlockIDOp) apply(state *documentState) error {
	snapshot, projection, err := projectState(state, op.Ref)
	if err != nil {
		return err
	}
	if updated, ok, err := ensureIdentifierBackedBlockID(snapshot, projection, op.BlockID); err != nil {
		return err
	} else if ok {
		state.setContent(updated)
		return nil
	}
	updated, _, err := EnsureBlockIDInSnapshot(snapshot, projection, op.BlockID)
	if err != nil {
		return err
	}
	state.setContent(updated)
	return nil
}

func ensureIdentifierBackedBlockID(snapshot *DocumentSnapshot, projection *NodeProjection, preferred string) (string, bool, error) {
	if projection == nil || projection.Ref.Kind != NodeKindEmbedded || projection.Type == nil {
		return "", false, nil
	}
	field := preferredBlockIdentifierField(projection.Type)
	if field == nil || field.SourceKind != ontoschema.FieldSourceInline {
		return "", false, nil
	}
	blockID := strings.TrimSpace(strings.TrimPrefix(preferred, "^"))
	if blockID == "" {
		blockID = GenerateBlockIDForProjection(snapshot, projection)
	}
	blockID = BlockSafeIdentifier(blockID)
	if !ValidBlockID(blockID) {
		return "", false, fmt.Errorf("%w: invalid block ID %q", errUnsupportedTarget, blockID)
	}
	if owner, ok := existingBlockIDs(snapshot)[blockID]; ok && owner != projection.Ref.NodeID {
		return "", false, fmt.Errorf("%w: duplicate block ID %q", errUnsupportedTarget, blockID)
	}
	updated, err := setInlineField(snapshot, projection, field, []string{"^" + blockID})
	if err != nil {
		return "", false, err
	}
	return updated, true, nil
}

func preferredBlockIdentifierField(noteType *ontoschema.NoteType) *ontoschema.Field {
	if noteType == nil {
		return nil
	}
	for _, field := range noteType.Fields {
		if field == nil || field.Kind != ontoschema.FieldKindScalar || field.List {
			continue
		}
		if field.IsPreferredIdentifier {
			return field
		}
	}
	for _, field := range noteType.Fields {
		if field == nil || field.Kind != ontoschema.FieldKindScalar || field.List {
			continue
		}
		if field.IsIdentifier {
			return field
		}
	}
	return nil
}

func (op setBlockIDOp) apply(state *documentState) error {
	snapshot, projection, err := projectState(state, op.Ref)
	if err != nil {
		return err
	}
	if projection.Ref.Kind != NodeKindEmbedded {
		return nil
	}
	blockID := strings.TrimSpace(strings.TrimPrefix(op.BlockID, "^"))
	if !ValidBlockID(blockID) {
		return fmt.Errorf("%w: invalid block ID %q", errUnsupportedTarget, blockID)
	}
	node := snapshot.SectionsByID[projection.Ref.NodeID]
	if node == nil {
		return fmt.Errorf("%w: embedded node %s not found", errMissingNode, projection.Ref.String())
	}
	if strings.TrimPrefix(strings.TrimSpace(node.BlockID), "^") == blockID {
		return nil
	}
	if owner, ok := existingBlockIDs(snapshot)[blockID]; ok && owner != node.ID {
		return fmt.Errorf("%w: duplicate block ID %q", errUnsupportedTarget, blockID)
	}
	blockRange, ok := sectionBlockIDRange(snapshot.Content, node)
	if !ok {
		updated, _, err := EnsureBlockIDInSnapshot(snapshot, projection, blockID)
		if err != nil {
			return err
		}
		state.setContent(updated)
		return nil
	}
	state.setContent(replaceRange(snapshot.Content, blockRange, "^"+blockID))
	return nil
}

func (op removeBlockIDOp) apply(state *documentState) error {
	snapshot, projection, err := projectState(state, op.Ref)
	if err != nil {
		return err
	}
	if projection.Ref.Kind != NodeKindEmbedded && projection.Ref.Kind != NodeKindSection {
		return nil
	}
	node := snapshot.SectionsByID[projection.Ref.NodeID]
	if node == nil {
		return fmt.Errorf("%w: node %s not found", errMissingNode, projection.Ref.String())
	}
	if strings.TrimSpace(node.BlockID) == "" {
		return nil
	}
	// WHY: per SPEC-0023.US8, identifier-backed lines on derivable-identity
	// fields with populate: ON_LINK become opt-in linkability — when
	// the orphan-cleanup pass strips an unreferenced anchor, the source line
	// to delete is the whole `id:: ^...` line, not just the standalone
	// `^block-id` byte range that sectionBlockIDRange finds.
	if removed, ok, err := tryRemoveDerivableIdentifierLine(state, snapshot, projection, node); err != nil {
		return err
	} else if ok {
		state.setContent(removed)
		return nil
	}
	blockRange, ok := sectionBlockIDRange(snapshot.Content, node)
	if !ok {
		return nil
	}
	// Extend the range to consume the surrounding line so removing the anchor
	// does not leave a blank `^id`-shaped hole behind. Eat the leading newline
	// when the line is anchor-only and the trailing newline likewise. This
	// keeps source markdown clean across iterated cleanup runs.
	start, end := blockRange.Start, blockRange.End
	for end < len(snapshot.Content) && snapshot.Content[end] == ' ' {
		end++
	}
	if end < len(snapshot.Content) && snapshot.Content[end] == '\n' {
		end++
	}
	if start > 0 && snapshot.Content[start-1] == '\n' {
		// Only eat the preceding newline when the anchor sat on its own line;
		// otherwise we would join two unrelated content lines.
		preceding := snapshot.Content[:start-1]
		if idx := strings.LastIndexByte(preceding, '\n'); idx == -1 || strings.TrimSpace(preceding[idx+1:]) == "" {
			start--
		}
	}
	state.setContent(replaceRange(snapshot.Content, ByteRange{Start: start, End: end}, ""))
	return nil
}

// tryRemoveDerivableIdentifierLine deletes the authored `id:: ^...` line on a
// derivable-identity embedded node when the orphan-cleanup pass targets the
// node's preferred-identifier field. Returns (newContent, true, nil) when a
// derivable identifier line was found and removed, or (_, false, nil) when the
// node's anchor is sourced from a standalone `^block-id` and the caller should
// fall back to the legacy byte-range removal.
func tryRemoveDerivableIdentifierLine(state *documentState, snapshot *DocumentSnapshot, projection *NodeProjection, node *SectionNode) (string, bool, error) {
	if projection == nil || projection.Type == nil {
		return "", false, nil
	}
	field := projection.Type.PreferredIdentifierField()
	if field == nil || !field.IsDerivableIdentifier || field.SourceKind != ontoschema.FieldSourceInline {
		return "", false, nil
	}
	binding, ok := projection.Fields[field.Name]
	if !ok || binding.Derived || len(binding.InlineSpans) == 0 {
		// Either the field is purely derived (no authored line to remove) or
		// the projection didn't surface a span to delete; either way nothing
		// to do via this path.
		return "", false, nil
	}
	span := binding.InlineSpans[0]
	start, end := span.LineRange.Start, span.LineRange.End
	if start < 0 || end <= start {
		return "", false, nil
	}
	// Walk back to consume the leading newline so the line removal does not
	// leave a blank line behind.
	if start > 0 && snapshot.Content[start-1] == '\n' {
		preceding := snapshot.Content[:start-1]
		if idx := strings.LastIndexByte(preceding, '\n'); idx == -1 || strings.TrimSpace(preceding[idx+1:]) == "" {
			start--
		}
	}
	// Walk forward through the line terminator.
	if end < len(snapshot.Content) && snapshot.Content[end] == '\n' {
		end++
	}
	updated := replaceRange(snapshot.Content, ByteRange{Start: start, End: end}, "")
	_ = state
	return updated, true, nil
}

func sameCollectionMembers(left, right []CollectionItem) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, item := range left {
		counts[strings.TrimSpace(strings.TrimPrefix(item.Ref.Fragment, "#"))]++
	}
	for _, item := range right {
		key := strings.TrimSpace(strings.TrimPrefix(item.Ref.Fragment, "#"))
		counts[key]--
		if counts[key] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

var (
	errMissingNode       = errors.New("missing node")
	errMissingField      = errors.New("missing field")
	errCollectionDrift   = errors.New("collection drift")
	errUnsupportedTarget = errors.New("unsupported target")
)
