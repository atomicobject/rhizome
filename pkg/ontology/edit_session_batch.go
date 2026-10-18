package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
	"strings"
)

func tryApplySetFieldOpsBatch(state *documentState, ops []setFieldOp) (string, []fieldReplayTarget, []contentEdit, bool, editOp, error) {
	snapshot, err := state.documentSnapshot()
	if err != nil {
		return "", nil, nil, false, ops[0], err
	}
	edits := make([]contentEdit, 0, len(ops))
	var targets []fieldReplayTarget
	if state.captureRefLineage {
		targets = make([]fieldReplayTarget, 0, len(ops))
	}
	for i, op := range ops {
		projection, err := projectSnapshot(snapshot, state.schema, op.Ref, state.allowSelectorRecovery)
		if err != nil {
			return "", nil, nil, false, op, err
		}
		field, err := validateSetFieldOperation(projection, op, state.schema, state.allowSelectorRecovery)
		if err != nil {
			return "", nil, nil, false, op, err
		}
		if op.Unset {
			return "", nil, nil, false, nil, nil
		}
		if state.captureRefLineage {
			targets = append(targets, fieldReplayTarget{Original: op.Ref, Before: projection.Ref})
		}
		opEdits, ok, err := setFieldBatchEdits(snapshot, projection, field, op)
		if err != nil {
			return "", nil, nil, false, op, err
		}
		if !ok {
			return "", nil, nil, false, nil, nil
		}
		for j := range opEdits {
			opEdits[j].order = i
			edits = append(edits, opEdits[j])
		}
	}
	if editsOverlap(edits) {
		return "", nil, nil, false, nil, nil
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].r.Start == edits[j].r.Start {
			return edits[i].order > edits[j].order
		}
		return edits[i].r.Start > edits[j].r.Start
	})
	content := snapshot.Content
	for _, edit := range edits {
		content = replaceRange(content, edit.r, edit.replacement)
	}
	return content, targets, edits, true, nil, nil
}

func captureFieldRefLineage(state *documentState, targets []fieldReplayTarget, edits []contentEdit) {
	if state == nil || !state.captureRefLineage || len(targets) == 0 {
		return
	}
	snapshot, err := state.documentSnapshot()
	if err != nil {
		return
	}
	resolver, err := newProjectionResolver(snapshot, state.schema)
	if err != nil {
		return
	}
	refsByStart := replayRefsByStart(resolver)
	starts := shiftedReplayTargetStarts(targets, edits)
	for i, target := range targets {
		if state.invalidRefStructural[strings.TrimSpace(target.Original.Structural)] {
			continue
		}
		preview, ok := replayRefAtStart(refsByStart[starts[i]], target.Before.Kind, target.Before.TypeName)
		if !ok || strings.TrimSpace(preview.Structural) == "" {
			continue
		}
		recordFieldRefLineage(state, target.Original, preview)
	}
}

func documentStructuralCounts(state *documentState) map[string]int {
	out := make(map[string]int)
	if state == nil {
		return out
	}
	snapshot, err := state.documentSnapshot()
	if err != nil {
		return out
	}
	resolver, err := newProjectionResolver(snapshot, state.schema)
	if err != nil {
		return out
	}
	for _, refs := range replayRefsByStart(resolver) {
		for _, ref := range refs {
			if structural := strings.TrimSpace(ref.Structural); structural != "" {
				out[structural]++
			}
		}
	}
	return out
}

func (state *documentState) recordStructuralInvalidations(before, after map[string]int, op editOp) {
	if state.invalidRefStructural == nil {
		state.invalidRefStructural = make(map[string]bool)
	}
	_, rewrite := op.(rewriteFileOp)
	_, transform := op.(transformFileOp)
	for structural, count := range before {
		afterCount := after[structural]
		if count > 0 && (count != 1 || afterCount != 1 || rewrite || transform) {
			state.invalidRefStructural[structural] = true
		}
	}
}

func replayRefsByStart(resolver *projectionResolver) map[int][]NodeRef {
	out := make(map[int][]NodeRef)
	for _, span := range resolver.snapshot.SourceSpans {
		if span == nil || span.Shape == ontoschema.EmbeddedSourceShapeSection {
			continue
		}
		out[span.Range.Start] = append(out[span.Range.Start], resolver.sourceSpanNodeRef(span))
	}
	var walk func([]*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			out[node.StartByte] = append(out[node.StartByte], resolver.sectionNodeRef(node))
			walk(node.Children)
		}
	}
	walk(resolver.snapshot.Sections)
	return out
}

func replayRefAtStart(refs []NodeRef, kind NodeKind, typeName string) (NodeRef, bool) {
	var found NodeRef
	for _, ref := range refs {
		if ref.Kind != kind || (strings.TrimSpace(typeName) != "" && ref.TypeName != typeName) {
			continue
		}
		if !found.IsZero() {
			return NodeRef{}, false
		}
		found = ref
	}
	return found, !found.IsZero()
}

func shiftedReplayTargetStarts(targets []fieldReplayTarget, edits []contentEdit) []int {
	type editDelta struct {
		end   int
		delta int
	}
	deltas := make([]editDelta, 0, len(edits))
	for _, edit := range edits {
		deltas = append(deltas, editDelta{end: edit.r.End, delta: len(edit.replacement) - edit.r.Len()})
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].end < deltas[j].end })
	order := make([]int, len(targets))
	for i := range targets {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		return targets[order[i]].Before.StartByte < targets[order[j]].Before.StartByte
	})
	out := make([]int, len(targets))
	deltaIndex, cumulative := 0, 0
	for _, targetIndex := range order {
		start := targets[targetIndex].Before.StartByte
		for deltaIndex < len(deltas) && deltas[deltaIndex].end <= start {
			cumulative += deltas[deltaIndex].delta
			deltaIndex++
		}
		out[targetIndex] = start + cumulative
	}
	return out
}

func editsOverlap(edits []contentEdit) bool {
	nonEmpty := make([]contentEdit, 0, len(edits))
	for _, edit := range edits {
		if edit.r.Len() == 0 {
			continue
		}
		nonEmpty = append(nonEmpty, edit)
	}
	sort.Slice(nonEmpty, func(i, j int) bool {
		return nonEmpty[i].r.Start < nonEmpty[j].r.Start
	})
	for i := 1; i < len(nonEmpty); i++ {
		if nonEmpty[i].r.Start < nonEmpty[i-1].r.End {
			return true
		}
	}
	return false
}

func setFieldBatchEdits(snapshot *DocumentSnapshot, projection *NodeProjection, field *ontoschema.Field, op setFieldOp) ([]contentEdit, bool, error) {
	if strings.EqualFold(op.Field, "title") {
		return titleBatchEdits(snapshot, projection, op.Values)
	}
	if field.SourceKind == ontoschema.FieldSourceCheckbox {
		return checkboxBatchEdits(snapshot, projection, field, op.Values)
	}
	if projection.Ref.Kind == NodeKindEmbedded && embeddedNodeNeedsBlockID(snapshot, projection) {
		return nil, false, nil
	}
	if field.SourceKind == ontoschema.FieldSourceInline {
		edits, err := inlineFieldEdits(snapshot, projection, field, op.Values)
		return edits, err == nil, err
	}
	return nil, false, nil
}

func titleBatchEdits(snapshot *DocumentSnapshot, projection *NodeProjection, values []string) ([]contentEdit, bool, error) {
	if len(values) != 1 {
		return nil, false, fmt.Errorf("%w: title expects one value", errUnsupportedTarget)
	}
	value := oneLineTitleValue(values[0])
	switch projection.Ref.Kind {
	case NodeKindEmbedded:
		if span := snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil &&
			(span.Shape == ontoschema.EmbeddedSourceShapeCheckboxItem || span.Shape == ontoschema.EmbeddedSourceShapeListItem) {
			edit, err := sourceSpanTitleEdit(snapshot, span, value)
			if err != nil {
				return nil, false, err
			}
			return []contentEdit{edit}, true, nil
		}
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

func sourceSpanTitleEdit(snapshot *DocumentSnapshot, span *MarkdownSourceSpan, value string) (contentEdit, error) {
	if !span.ContentRange.Valid(len(snapshot.Content)) || span.ContentRange.Len() == 0 {
		return contentEdit{}, fmt.Errorf("%w: invalid title range", errCollectionDrift)
	}
	lineEnd := span.ContentRange.End
	if nextLine := strings.IndexByte(snapshot.Content[span.ContentRange.Start:span.ContentRange.End], '\n'); nextLine >= 0 {
		lineEnd = span.ContentRange.Start + nextLine
	}
	titleEnd := lineEnd
	for _, marker := range span.Markers {
		if marker.Range.Start >= span.ContentRange.Start && marker.Range.Start < titleEnd {
			titleEnd = marker.Range.Start
		}
	}
	for _, inline := range scanInlineFieldsInListSegment(snapshot.Content[span.ContentRange.Start:lineEnd], span.ContentRange.Start) {
		if inline.WholeRange.Start >= span.ContentRange.Start && inline.WholeRange.Start < titleEnd {
			titleEnd = inline.WholeRange.Start
		}
	}
	if tokenStart := trailingListMetadataTokenStart(snapshot.Content[span.ContentRange.Start:lineEnd], 0, lineEnd-span.ContentRange.Start); tokenStart >= 0 {
		absolute := span.ContentRange.Start + tokenStart
		if absolute < titleEnd {
			titleEnd = absolute
		}
	}
	titleEnd = trimSpaceLeftBounded(snapshot.Content, span.ContentRange.Start, titleEnd)
	if titleEnd < lineEnd && value != "" && snapshot.Content[titleEnd] != ' ' && snapshot.Content[titleEnd] != '\t' {
		value += " "
	}
	return contentEdit{r: ByteRange{Start: span.ContentRange.Start, End: titleEnd}, replacement: value}, nil
}

func checkboxBatchEdits(snapshot *DocumentSnapshot, projection *NodeProjection, field *ontoschema.Field, values []string) ([]contentEdit, bool, error) {
	if field.TypeName != "Boolean" || field.List || len(values) != 1 {
		return nil, false, nil
	}
	desired, err := parseCheckboxEditValue(values[0])
	if err != nil {
		return nil, false, err
	}
	span := snapshot.SourceSpansByID[projection.Ref.NodeID]
	if span == nil || span.Shape != ontoschema.EmbeddedSourceShapeCheckboxItem || span.Checkbox == nil {
		return nil, false, nil
	}
	r := span.Checkbox.TokenRange
	if !r.Valid(len(snapshot.Content)) || r.Len() != len("[ ]") {
		return nil, false, fmt.Errorf("%w: invalid checkbox token range for %s", errCollectionDrift, projection.Ref.String())
	}
	if span.Checkbox.Checked == desired {
		return nil, true, nil
	}
	token := "[ ]"
	if desired {
		token = "[x]"
	}
	return []contentEdit{{r: r, replacement: token}}, true, nil
}

func inlineFieldEdits(snapshot *DocumentSnapshot, projection *NodeProjection, field *ontoschema.Field, values []string) ([]contentEdit, error) {
	binding := projection.Fields[field.Name]
	propertyName := ontoschema.DefaultPropertyName(field.Name, projection.PropertyCase)
	lineText := propertyName + ":: " + strings.Join(values, ", ")
	if len(binding.InlineSpans) > 0 {
		edits := make([]contentEdit, 0, len(binding.InlineSpans))
		first := binding.InlineSpans[0]
		editRange := inlineFieldEditRange(snapshot.Content, first)
		replacement := inlineFieldReplacementPrefix(snapshot.Content, first) + lineText
		if editRange != first.LineRange {
			replacement = lineText
		} else if rangeStartsLine(snapshot.Content, first.LineRange.Start) {
			replacement += documentEOL(snapshot.Content)
		}
		edits = append(edits, contentEdit{r: editRange, replacement: replacement})
		for i := 1; i < len(binding.InlineSpans); i++ {
			edits = append(edits, contentEdit{r: inlineFieldEditRange(snapshot.Content, binding.InlineSpans[i])})
		}
		return edits, nil
	}
	insertAt := inlineInsertOffset(snapshot, projection)
	if insertAt < 0 || insertAt > len(snapshot.Content) {
		return nil, fmt.Errorf("%w: invalid inline insert offset for %s", errUnsupportedTarget, field.Name)
	}
	prefix := ""
	if insertAt > 0 && snapshot.Content[insertAt-1] != '\n' {
		prefix = documentEOL(snapshot.Content)
	}
	return []contentEdit{{r: ByteRange{Start: insertAt, End: insertAt}, replacement: prefix + lineText + documentEOL(snapshot.Content)}}, nil
}
