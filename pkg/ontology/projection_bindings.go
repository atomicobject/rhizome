package ontology

import (
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// noteFieldSource returns the frontmatter source key for a note field, falling
// back to the field name when no @field(source:) is set.
func noteFieldSource(field *Field) string {
	if field == nil {
		return ""
	}
	if s := strings.TrimSpace(field.Source); s != "" {
		return s
	}
	return field.Name
}

func (r *projectionResolver) noteSectionBinding(field *Field) FieldBinding {
	if field.EmbeddedSourceShape != "" && field.EmbeddedSourceShape != EmbeddedSourceShapeSection {
		return sourceSpanNodesBinding(r, field, matchSourceSpansForField(r.snapshot.SourceSpans, field))
	}
	matches := matchSectionsForAssessment(r.snapshot.Sections, field)
	return sectionNodesBinding(r, field, matches)
}

func (r *projectionResolver) sectionFieldBinding(parent *SectionNode, field *Field) FieldBinding {
	if field.EmbeddedSourceShape != "" && field.EmbeddedSourceShape != EmbeddedSourceShapeSection {
		parentSpan := r.snapshot.SourceSpansByID[parent.ID]
		return sourceSpanNodesBinding(r, field, matchSourceSpansForField(sourceSpanChildren(parentSpan), field))
	}
	matches := matchSectionsForAssessment(parent.Children, field)
	return sectionNodesBinding(r, field, matches)
}

func (r *projectionResolver) sourceSpanChildBinding(parent *MarkdownSourceSpan, field *Field) FieldBinding {
	if field.EmbeddedSourceShape != "" && field.EmbeddedSourceShape != EmbeddedSourceShapeSection {
		return sourceSpanNodesBinding(r, field, matchSourceSpansForField(sourceSpanChildren(parent), field))
	}
	return FieldBinding{
		FieldName:  field.Name,
		Kind:       BindingKindSectionList,
		SourceKind: FieldSourceInline,
	}
}

func noteFieldBinding(snapshot *DocumentSnapshot, doc *noteDoc, field *Field) FieldBinding {
	binding := FieldBinding{
		FieldName:  field.Name,
		SourceKind: field.SourceKind,
		Values:     nil,
	}
	switch field.SourceKind {
	case FieldSourceInline:
		spans := scanInlineFields(snapshot.Content, []ByteRange{{Start: snapshot.FrontmatterRange.End, End: len(snapshot.Content)}})
		values, matched := inlineFieldValues(spans, field)
		binding.Kind = BindingKindInlineField
		binding.Present = len(matched) > 0
		binding.Values = normalizeFieldValues(field, values)
		binding.InlineSpans = matched
		binding.ValueRanges = inlineValueRanges(matched)
		binding.ValueRangesExact = valueRangesExactlyMatch(snapshot.Content, binding.Values, binding.ValueRanges)
		binding.Range = unionRanges(binding.ValueRanges)
	default:
		values := extractFieldValues(doc, field)
		binding.Kind = BindingKindFrontmatterField
		binding.Present = fieldPresent(doc, field)
		binding.Values = normalizeFieldValues(field, values)
		binding.Range = snapshot.FrontmatterRange
		binding.ValueRanges, binding.ValueRangesExact = exactFrontmatterValueRanges(snapshot, field, binding.Values)
	}
	return binding
}

// ProjectRawFrontmatterFieldBinding returns the conservative source binding for
// a generic frontmatter field even when a resolved ontology type does not
// declare it. This keeps raw note metadata such as aliases source-owned while
// allowing trusted repair planners to require exact authored value ranges.
func ProjectRawFrontmatterFieldBinding(snapshot *DocumentSnapshot, fieldName string) FieldBinding {
	fieldName = strings.TrimSpace(fieldName)
	if snapshot == nil || fieldName == "" {
		return FieldBinding{}
	}
	field := &Field{
		Name: fieldName, Source: fieldName, SourceKind: FieldSourceFrontmatter,
		Kind: FieldKindScalar, TypeName: "String", List: true,
	}
	doc := &noteDoc{Path: snapshot.NotePath, Content: snapshot.Content, Frontmatter: cloneAnyMap(snapshot.Frontmatter)}
	return noteFieldBinding(snapshot, doc, field)
}

func sectionValueBinding(snapshot *DocumentSnapshot, node *SectionNode, field *Field, propertyCase PropertyCase) FieldBinding {
	spans := scanSectionInlineFields(snapshot.Content, sectionOwnRanges(snapshot.Content, node))
	return inlinePropertyBinding(snapshot, field, propertyCase, spans)
}

func sourceSpanValueBinding(snapshot *DocumentSnapshot, span *MarkdownSourceSpan, field *Field, propertyCase PropertyCase) FieldBinding {
	binding := FieldBinding{
		FieldName:  field.Name,
		SourceKind: field.SourceKind,
	}
	switch field.SourceKind {
	case FieldSourceCheckbox:
		binding.Kind = BindingKindCheckboxField
		if span != nil && span.Checkbox != nil {
			binding.Present = true
			binding.Values = []string{strconv.FormatBool(span.Checkbox.Checked)}
			binding.ValueRanges = []ByteRange{span.Checkbox.TokenRange}
			binding.ValueRangesExact = valueRangesExactlyMatch(snapshot.Content, binding.Values, binding.ValueRanges)
			binding.Range = span.Checkbox.TokenRange
		}
		return binding
	case FieldSourceItemTitle, FieldSourceItemSummary, FieldSourceItemDetail:
		binding.Kind = BindingKindInlineField
		value, ok := sourceSpanItemFieldValue(snapshot.Content, span, field.SourceKind)
		if ok {
			value = normalizeFieldValue(field, value)
			binding.Present = true
			binding.Values = []string{value}
			if span != nil {
				binding.ValueRanges = []ByteRange{span.ContentRange}
				binding.ValueRangesExact = valueRangesExactlyMatch(snapshot.Content, binding.Values, binding.ValueRanges)
				binding.Range = span.ContentRange
			}
		}
		return binding
	}
	spans := scanInlineFieldsInListOwnContent(snapshot.Content, sourceSpanOwnRanges(span))
	return inlinePropertyBinding(snapshot, field, propertyCase, spans)
}

func inlinePropertyBinding(snapshot *DocumentSnapshot, field *Field, propertyCase PropertyCase, spans []InlineFieldSpan) FieldBinding {
	binding := FieldBinding{
		FieldName:  field.Name,
		SourceKind: field.SourceKind,
		Kind:       BindingKindInlineField,
	}
	propertyName := DefaultPropertyName(field.Name, propertyCase)
	for _, span := range spans {
		if !strings.EqualFold(span.PropertyKey, propertyName) {
			continue
		}
		binding.InlineSpans = append(binding.InlineSpans, span)
		binding.Values = append(binding.Values, normalizeFieldValue(field, span.Value))
		binding.ValueRanges = append(binding.ValueRanges, span.ValueRange)
	}
	binding.Present = len(binding.InlineSpans) > 0
	binding.ValueRangesExact = valueRangesExactlyMatch(snapshot.Content, binding.Values, binding.ValueRanges)
	binding.Range = unionRanges(binding.ValueRanges)
	return binding
}

func sourceSpanItemFieldValue(content string, span *MarkdownSourceSpan, sourceKind FieldSource) (string, bool) {
	title, summary, detail := sourceSpanItemParts(content, span)
	var value string
	switch sourceKind {
	case FieldSourceItemTitle:
		value = title
	case FieldSourceItemSummary:
		value = summary
	case FieldSourceItemDetail:
		value = detail
	default:
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func sectionNodesBinding(r *projectionResolver, field *Field, matches []*SectionNode) FieldBinding {
	binding := FieldBinding{
		FieldName:    field.Name,
		Kind:         BindingKindSectionField,
		SourceKind:   FieldSourceInline,
		Present:      len(matches) > 0,
		SectionNodes: make([]NodeRef, 0, len(matches)),
	}
	if field.List {
		binding.Kind = BindingKindSectionList
	}
	ranges := make([]ByteRange, 0, len(matches))
	for _, match := range matches {
		if match == nil {
			continue
		}
		ref := r.sectionNodeRef(match)
		binding.SectionNodes = append(binding.SectionNodes, ref)
		binding.Values = append(binding.Values, ref.String())
		ranges = append(ranges, ByteRange{Start: match.StartByte, End: match.EndByte})
	}
	binding.ValueRanges = ranges
	binding.Range = unionRanges(ranges)
	return binding
}

func sourceSpanNodesBinding(r *projectionResolver, field *Field, matches []*MarkdownSourceSpan) FieldBinding {
	binding := FieldBinding{
		FieldName:    field.Name,
		Kind:         BindingKindSectionField,
		SourceKind:   FieldSourceInline,
		Present:      len(matches) > 0,
		SectionNodes: make([]NodeRef, 0, len(matches)),
	}
	if field.List {
		binding.Kind = BindingKindSectionList
	}
	ranges := make([]ByteRange, 0, len(matches))
	for _, match := range matches {
		if match == nil {
			continue
		}
		ref := r.sourceSpanNodeRef(match)
		binding.SectionNodes = append(binding.SectionNodes, ref)
		binding.Values = append(binding.Values, ref.String())
		ranges = append(ranges, match.Range)
	}
	binding.ValueRanges = ranges
	binding.Range = unionRanges(ranges)
	return binding
}

func collectionFromFieldBinding(fieldName string, binding FieldBinding) CollectionBinding {
	items := make([]CollectionItem, 0, len(binding.SectionNodes))
	for _, ref := range binding.SectionNodes {
		items = append(items, CollectionItem{
			Ref:   ref,
			Range: ByteRange{Start: ref.StartByte, End: ref.EndByte},
		})
	}
	return CollectionBinding{
		FieldName:        fieldName,
		Kind:             binding.Kind,
		Range:            binding.Range,
		Items:            items,
		OrderFingerprint: hashText(orderFingerprint(items)),
	}
}

func inlineFieldValues(spans []InlineFieldSpan, field *Field) ([]string, []InlineFieldSpan) {
	values := make([]string, 0)
	matched := make([]InlineFieldSpan, 0)
	for _, span := range spans {
		for _, name := range FieldSourceNames(field) {
			if strings.EqualFold(span.PropertyKey, name) {
				values = append(values, span.Value)
				matched = append(matched, span)
				break
			}
		}
	}
	return values, matched
}

func normalizeFieldValues(field *Field, values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, normalizeFieldValue(field, value))
	}
	return out
}

func normalizeFieldValue(field *Field, value string) string {
	value = strings.TrimSpace(value)
	if !isIdentifierLikeField(field) || !strings.HasPrefix(value, "^") {
		return value
	}
	blockID := strings.TrimSpace(strings.TrimPrefix(value, "^"))
	if blockID == "" || !ValidBlockID(blockID) {
		return value
	}
	return blockID
}

func isIdentifierLikeField(field *Field) bool {
	if field == nil {
		return false
	}
	return field.IsIdentifier
}

func inlineValueRanges(spans []InlineFieldSpan) []ByteRange {
	out := make([]ByteRange, 0, len(spans))
	for _, span := range spans {
		out = append(out, span.ValueRange)
	}
	return out
}

func frontmatterRange(content string) ByteRange {
	match := obsidian.FrontmatterRegex().FindStringIndex(content)
	if len(match) != 2 {
		return ByteRange{}
	}
	return ByteRange{Start: match[0], End: match[1]}
}

func sectionOwnRanges(content string, node *SectionNode) []ByteRange {
	if node == nil || node.StartByte >= node.EndByte || node.EndByte > len(content) {
		return nil
	}
	bodyStart := node.StartByte
	if rel := strings.IndexByte(node.Content, '\n'); rel >= 0 {
		bodyStart = node.StartByte + rel + 1
	}
	if bodyStart >= node.EndByte {
		return nil
	}
	if len(node.Children) == 0 {
		return []ByteRange{{Start: bodyStart, End: node.EndByte}}
	}
	ranges := make([]ByteRange, 0, len(node.Children)+1)
	cursor := bodyStart
	for _, child := range node.Children {
		if child == nil {
			continue
		}
		if child.StartByte > cursor {
			ranges = append(ranges, ByteRange{Start: cursor, End: child.StartByte})
		}
		if child.EndByte > cursor {
			cursor = child.EndByte
		}
	}
	if cursor < node.EndByte {
		ranges = append(ranges, ByteRange{Start: cursor, End: node.EndByte})
	}
	return normalizeRanges(ranges)
}

func normalizeRanges(ranges []ByteRange) []ByteRange {
	out := make([]ByteRange, 0, len(ranges))
	for _, r := range ranges {
		if r.Len() == 0 {
			continue
		}
		out = append(out, r)
	}
	return out
}

func unionRanges(ranges []ByteRange) ByteRange {
	if len(ranges) == 0 {
		return ByteRange{}
	}
	start := ranges[0].Start
	end := ranges[0].End
	for _, r := range ranges[1:] {
		if r.Len() == 0 {
			continue
		}
		if r.Start < start {
			start = r.Start
		}
		if r.End > end {
			end = r.End
		}
	}
	return ByteRange{Start: start, End: end}
}

func orderFingerprint(items []CollectionItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.Ref.String())
	}
	return strings.Join(parts, "|")
}
