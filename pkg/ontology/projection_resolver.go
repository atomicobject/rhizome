package ontology

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func (r *projectionResolver) project(ref NodeRef) (*NodeProjection, error) {
	if ref.IsZero() {
		return nil, fmt.Errorf("node ref is required")
	}
	if strings.TrimSpace(ref.NotePath) != r.snapshot.NotePath {
		return nil, fmt.Errorf("node ref %s does not belong to snapshot %s", ref.NotePath, r.snapshot.NotePath)
	}
	if ref.Kind == NodeKindNote || (strings.TrimSpace(ref.Fragment) == "" && strings.TrimSpace(ref.NodeID) == "" && strings.TrimSpace(ref.Structural) == "") {
		return r.projectNote(), nil
	}
	if span, spanRef, ok, resolveErr := r.resolveSourceSpanRef(ref); resolveErr != nil {
		return nil, resolveErr
	} else if ok && span.Shape != EmbeddedSourceShapeSection {
		return r.projectSourceSpan(span, spanRef), nil
	}
	node, sectionRef, err := r.resolveSectionRef(ref)
	if err != nil {
		return nil, err
	}
	return r.projectSection(node, sectionRef), nil
}

// projectCurrent resolves refs produced by this resolver's current snapshot.
// Internal traversals use it so identical unanchored siblings can both be
// materialized without weakening fail-closed resolution of external refs.
func (r *projectionResolver) projectCurrent(ref NodeRef) (*NodeProjection, error) {
	if span := r.snapshot.SourceSpansByID[ref.NodeID]; span != nil && span.Shape != EmbeddedSourceShapeSection &&
		ref.StartByte == span.Range.Start && ref.EndByte == span.Range.End {
		current := r.sourceSpanNodeRef(span)
		if current.Structural == ref.Structural {
			return r.projectSourceSpan(span, current), nil
		}
	}
	return r.project(ref)
}

func (r *projectionResolver) projectNote() *NodeProjection {
	ref := NodeRef{
		NotePath:   r.snapshot.NotePath,
		Kind:       NodeKindNote,
		TypeName:   typeNameOrEmpty(r.noteType),
		StartByte:  0,
		EndByte:    len(r.snapshot.Content),
		Structural: r.snapshot.StructureFingerprint,
	}
	projection := &NodeProjection{
		Ref:          ref,
		Snapshot:     r.snapshot,
		ResolvedType: typeNameOrEmpty(r.noteType),
		Type:         r.noteType,
		Assessment:   r.assessment,
		PropertyCase: propertyCaseOrDefault(r.noteType),
		Fields:       map[string]FieldBinding{},
		Collections:  map[string]CollectionBinding{},
	}
	if r.noteType == nil {
		return projection
	}
	for _, field := range r.noteType.Fields {
		if field == nil {
			continue
		}
		switch field.Kind {
		case FieldKindSection:
			binding := r.noteSectionBinding(field)
			projection.Fields[field.Name] = binding
			if field.List {
				projection.Collections[field.Name] = collectionFromFieldBinding(field.Name, binding)
			}
		default:
			projection.Fields[field.Name] = noteFieldBinding(r.snapshot, r.noteDoc, field)
		}
	}
	return projection
}

func (r *projectionResolver) projectSection(node *SectionNode, ref NodeRef) *NodeProjection {
	typeName := r.sectionTypeByID[node.ID]
	noteType := r.schema.Types[typeName]
	propertyCase := r.propertyCaseByID[node.ID]
	if propertyCase == "" {
		propertyCase = propertyCaseOrDefault(r.noteType)
	}
	projection := &NodeProjection{
		Ref:          ref,
		Snapshot:     r.snapshot,
		ResolvedType: typeName,
		Type:         noteType,
		Assessment:   r.assessment,
		PropertyCase: propertyCase,
		Fields:       map[string]FieldBinding{},
		Collections:  map[string]CollectionBinding{},
	}
	if noteType == nil {
		return projection
	}
	for _, field := range noteType.Fields {
		if field == nil {
			continue
		}
		switch field.Kind {
		case FieldKindSection:
			binding := r.sectionFieldBinding(node, field)
			projection.Fields[field.Name] = binding
			if field.List {
				projection.Collections[field.Name] = collectionFromFieldBinding(field.Name, binding)
			}
		case FieldKindScalar, FieldKindEnum, FieldKindLink:
			projection.Fields[field.Name] = sectionValueBinding(r.snapshot, node, field, propertyCase)
		}
	}
	// WHY: per SPEC-0023.US8, embedded preferred-identifier fields whose
	// `derivable: true` arg holds (default for @node(locator: EMBEDDED) types)
	// surface a derived semantic id when no `id::` line is authored. Without
	// this pass, typed queries on uncited UserStory/AcceptanceCriterion nodes
	// would expose an empty id, breaking NodeRef resolution and semantic
	// rendering for the lazy-`id::` lifecycle.
	if field := preferredIdentifierField(noteType); field != nil && field.IsDerivableIdentifier {
		if binding, ok := projection.Fields[field.Name]; ok && len(binding.Values) == 0 {
			if derived, ok := r.deriveIdentifierValue(node, field); ok {
				binding.Values = []string{derived}
				binding.Derived = true
				projection.Fields[field.Name] = binding
			}
		}
	}
	return projection
}

func (r *projectionResolver) projectSourceSpan(span *MarkdownSourceSpan, ref NodeRef) *NodeProjection {
	typeName := r.sectionTypeByID[span.ID]
	noteType := r.schema.Types[typeName]
	propertyCase := r.propertyCaseByID[span.ID]
	if propertyCase == "" {
		propertyCase = propertyCaseOrDefault(r.noteType)
	}
	projection := &NodeProjection{
		Ref:          ref,
		Snapshot:     r.snapshot,
		ResolvedType: typeName,
		Type:         noteType,
		Assessment:   r.assessment,
		PropertyCase: propertyCase,
		Fields:       map[string]FieldBinding{},
		Collections:  map[string]CollectionBinding{},
	}
	if noteType == nil {
		return projection
	}
	for _, field := range noteType.Fields {
		if field == nil {
			continue
		}
		switch field.Kind {
		case FieldKindSection:
			binding := r.sourceSpanChildBinding(span, field)
			projection.Fields[field.Name] = binding
			if field.List {
				projection.Collections[field.Name] = collectionFromFieldBinding(field.Name, binding)
			}
		case FieldKindScalar, FieldKindEnum, FieldKindLink:
			projection.Fields[field.Name] = sourceSpanValueBinding(r.snapshot, span, field, propertyCase)
		}
	}
	if field := preferredIdentifierField(noteType); field != nil && field.IsDerivableIdentifier {
		if binding, ok := projection.Fields[field.Name]; ok && len(binding.Values) == 0 {
			if derived, ok := r.deriveSourceSpanIdentifierValue(span, field); ok {
				binding.Values = []string{derived}
				binding.Derived = true
				projection.Fields[field.Name] = binding
			}
		}
	}
	return projection
}

func (r *projectionResolver) indexSectionTypesForNote(noteType *NoteType) {
	if noteType == nil {
		return
	}
	r.indexNestedSectionTypes(noteType.Name, noteType.PropertyCase, r.snapshot.Sections, "")
}

func (r *projectionResolver) indexGlobalSourceTypes() {
	if r == nil || r.schema == nil || r.snapshot == nil {
		return
	}
	typeNames := make([]string, 0, len(r.schema.Types))
	for typeName, noteType := range r.schema.Types {
		if noteType == nil || noteType.Role != TypeRoleEmbeddedNode || noteType.SourceShape == "" {
			continue
		}
		if !sourceMatchersMatchNote(noteType.SourceMatchers, r.noteDoc) {
			continue
		}
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)
	for _, typeName := range typeNames {
		noteType := r.schema.Types[typeName]
		for _, match := range matchSourceSpansForSource(r.snapshot.SourceSpans, noteType.SourceShape, noteType.SourceMarker) {
			if match == nil {
				continue
			}
			if existing := strings.TrimSpace(r.sectionTypeByID[match.ID]); existing != "" {
				continue
			}
			r.sectionTypeByID[match.ID] = noteType.Name
			r.parentIDByID[match.ID] = parentIDForMatchedSourceSpan(match, "")
			r.propertyCaseByID[match.ID] = propertyCaseOrDefault(noteType)
			r.indexNestedSectionTypes(noteType.Name, noteType.PropertyCase, nil, match.ID)
		}
	}
}

func (r *projectionResolver) indexNestedSectionTypes(typeName string, propertyCase PropertyCase, nodes []*SectionNode, parentID string) {
	noteType := r.schema.Types[typeName]
	if noteType == nil {
		return
	}
	for _, field := range noteType.Fields {
		if field == nil || field.Kind != FieldKindSection {
			continue
		}
		if field.EmbeddedSourceShape != "" && field.EmbeddedSourceShape != EmbeddedSourceShapeSection {
			parentSpan := r.snapshot.SourceSpansByID[parentID]
			candidates := r.snapshot.SourceSpans
			if parentSpan != nil {
				candidates = sourceSpanChildren(parentSpan)
			}
			matches := matchSourceSpansForField(candidates, field)
			for _, match := range matches {
				if match == nil {
					continue
				}
				r.sectionTypeByID[match.ID] = field.TypeName
				r.parentIDByID[match.ID] = parentIDForMatchedSourceSpan(match, parentID)
				r.propertyCaseByID[match.ID] = propertyCase
				r.indexNestedSectionTypes(field.TypeName, propertyCase, nil, match.ID)
			}
			continue
		}
		matches := matchSectionsForAssessment(nodes, field)
		for _, match := range matches {
			if match == nil {
				continue
			}
			r.sectionTypeByID[match.ID] = field.TypeName
			r.parentIDByID[match.ID] = parentID
			r.propertyCaseByID[match.ID] = propertyCase
			r.indexNestedSectionTypes(field.TypeName, propertyCase, match.Children, match.ID)
		}
	}
}

func linkListSourceSpansToSections(spans []*MarkdownSourceSpan) {
	if len(spans) == 0 {
		return
	}
	byID := make(map[string]*MarkdownSourceSpan, len(spans))
	var sections []*MarkdownSourceSpan
	for _, span := range spans {
		if span == nil {
			continue
		}
		byID[span.ID] = span
		if span.Shape == EmbeddedSourceShapeSection {
			sections = append(sections, span)
		}
	}
	for _, span := range spans {
		if span == nil || span.Shape == EmbeddedSourceShapeSection || strings.TrimSpace(span.ParentID) != "" {
			continue
		}
		parent := nearestContainingSection(sections, span.Range)
		if parent == nil {
			continue
		}
		span.ParentID = parent.ID
		parent.Children = append(parent.Children, span)
	}
	for _, span := range spans {
		if span == nil || strings.TrimSpace(span.ParentID) == "" {
			continue
		}
		if parent := byID[span.ParentID]; parent != nil && !sourceSpanHasChild(parent, span.ID) {
			parent.Children = append(parent.Children, span)
		}
	}
}

func nearestContainingSection(sections []*MarkdownSourceSpan, child ByteRange) *MarkdownSourceSpan {
	var best *MarkdownSourceSpan
	for _, section := range sections {
		if section == nil || section.Shape != EmbeddedSourceShapeSection {
			continue
		}
		if section.Range.Start > child.Start || section.Range.End < child.End {
			continue
		}
		if best == nil || section.Range.Len() < best.Range.Len() {
			best = section
		}
	}
	return best
}

func sourceSpanHasChild(parent *MarkdownSourceSpan, childID string) bool {
	for _, child := range parent.Children {
		if child != nil && child.ID == childID {
			return true
		}
	}
	return false
}

func sourceSpanChildren(parent *MarkdownSourceSpan) []*MarkdownSourceSpan {
	if parent == nil {
		return nil
	}
	out := make([]*MarkdownSourceSpan, 0, len(parent.Children))
	for _, child := range parent.Children {
		if child == nil || child.Shape == EmbeddedSourceShapeSection {
			continue
		}
		out = append(out, child)
	}
	return out
}

func matchSourceSpansForField(candidates []*MarkdownSourceSpan, field *Field) []*MarkdownSourceSpan {
	if field == nil || field.EmbeddedSourceShape == "" || field.EmbeddedSourceShape == EmbeddedSourceShapeSection {
		return nil
	}
	return matchSourceSpansForSource(candidates, field.EmbeddedSourceShape, field.EmbeddedSourceMarker)
}

func matchSourceSpansForSource(candidates []*MarkdownSourceSpan, shape EmbeddedSourceShape, marker string) []*MarkdownSourceSpan {
	if shape == "" || shape == EmbeddedSourceShapeSection {
		return nil
	}
	marker = strings.TrimSpace(marker)
	out := make([]*MarkdownSourceSpan, 0)
	for _, span := range candidates {
		if span == nil || span.Shape != shape {
			continue
		}
		if marker != "" && !sourceSpanHasMarker(span, marker) {
			continue
		}
		out = append(out, span)
	}
	return out
}

func sourceMatchersMatchNote(matchers []*NoteMatcher, doc *noteDoc) bool {
	if len(matchers) == 0 {
		return true
	}
	for _, matcher := range matchers {
		if matcher != nil && matcher.matches(doc) {
			return true
		}
	}
	return false
}

func sourceSpanHasMarker(span *MarkdownSourceSpan, marker string) bool {
	ownRanges := sourceSpanOwnRanges(span)
	for _, candidate := range span.Markers {
		if candidate.Value == marker && markerRangeWithinAny(candidate.Range, ownRanges) {
			return true
		}
	}
	return false
}

func markerRangeWithinAny(marker ByteRange, ranges []ByteRange) bool {
	for _, own := range ranges {
		if marker.Start >= own.Start && marker.End <= own.End {
			return true
		}
	}
	return false
}

func sourceSpanOwnRanges(span *MarkdownSourceSpan) []ByteRange {
	if span == nil {
		return nil
	}
	if len(span.OwnContentRanges) > 0 {
		return normalizeRanges(span.OwnContentRanges)
	}
	return normalizeRanges([]ByteRange{span.ContentRange})
}

func parentIDForMatchedSourceSpan(span *MarkdownSourceSpan, fallback string) string {
	if span == nil {
		return fallback
	}
	if parentID := strings.TrimSpace(span.ParentID); parentID != "" {
		return parentID
	}
	return fallback
}

func typeNameOrEmpty(noteType *NoteType) string {
	if noteType == nil {
		return ""
	}
	return noteType.Name
}

func propertyCaseOrDefault(noteType *NoteType) PropertyCase {
	if noteType == nil || noteType.PropertyCase == "" {
		return PropertyCaseKebab
	}
	return noteType.PropertyCase
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func projectionNoteTitle(notePath string, fm map[string]any, sections []*SectionNode) string {
	for _, key := range []string{"title", "name"} {
		if value := strings.TrimSpace(stringValue(fm[key])); value != "" {
			return value
		}
	}
	h1Titles := make([]string, 0, 1)
	for _, section := range sections {
		if section == nil || section.Level != SectionLevelH1 {
			continue
		}
		// Section titles keep authored Markdown for heading identity; the
		// note title is what a reader sees.
		if value := obsidian.PlainMarkdownTitle(section.Title); value != "" {
			h1Titles = append(h1Titles, value)
		}
	}
	if len(h1Titles) == 1 {
		return h1Titles[0]
	}
	if len(h1Titles) == 0 {
		if value := strings.TrimSpace(stringValue(fm["summary"])); value != "" {
			return value
		}
	}
	base, ok := (&obsidian.Note{}).Title(notePath)
	if ok {
		return base
	}
	if len(h1Titles) > 0 {
		return h1Titles[0]
	}
	if value := strings.TrimSpace(stringValue(fm["summary"])); value != "" {
		return value
	}
	return strings.TrimSpace(notePath)
}
