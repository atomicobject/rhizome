package ontology

import (
	"fmt"
	"strconv"
	"strings"
)

// deriveIdentifierValue computes the lazy semantic id for an embedded
// derivable-identity node (SPEC-0023.US8). The id format is
// `${ancestorSemanticID}-${field.DerivedSuffix}${n}` where `n` is the
// lowest positive integer not already in use by an authored same-type
// sibling. Authored siblings keep their numbers; derived ids fill the gaps
// in document order.
func (r *projectionResolver) deriveIdentifierValue(node *SectionNode, field *Field) (string, bool) {
	if node == nil || field == nil || !field.IsDerivableIdentifier || field.DerivedSuffix == "" {
		return "", false
	}
	parentSem := r.ancestorSemanticID(node.ID)
	if parentSem == "" {
		return "", false
	}
	parentID := strings.TrimSpace(r.parentIDByID[node.ID])
	var siblings []*SectionNode
	if parentID == "" {
		siblings = r.snapshot.Sections
	} else if parent := r.snapshot.SectionsByID[parentID]; parent != nil {
		siblings = parent.Children
	}
	nodeType := r.sectionTypeByID[node.ID]
	prefix := parentSem + "-" + field.DerivedSuffix
	infos := make([]derivedIdentifierSibling, 0, len(siblings))
	for _, sibling := range siblings {
		if sibling == nil || r.sectionTypeByID[sibling.ID] != nodeType {
			continue
		}
		authored := authoredIdentifierValueForNode(r.snapshot, sibling, field)
		infos = append(infos, derivedIdentifierSibling{id: sibling.ID, authored: authored})
	}
	return deriveSiblingIdentifier(node.ID, prefix, infos)
}

func (r *projectionResolver) deriveSourceSpanIdentifierValue(span *MarkdownSourceSpan, field *Field) (string, bool) {
	if span == nil || field == nil || !field.IsDerivableIdentifier || field.DerivedSuffix == "" {
		return "", false
	}
	parentSem := r.ancestorSemanticID(span.ID)
	if parentSem == "" {
		return "", false
	}
	parentID := strings.TrimSpace(r.parentIDByID[span.ID])
	nodeType := r.sectionTypeByID[span.ID]
	prefix := parentSem + "-" + field.DerivedSuffix
	infos := make([]derivedIdentifierSibling, 0)
	for _, sibling := range r.snapshot.SourceSpans {
		if sibling == nil || r.sectionTypeByID[sibling.ID] != nodeType || strings.TrimSpace(r.parentIDByID[sibling.ID]) != parentID {
			continue
		}
		authored := r.authoredIdentifierValueForSourceSpan(sibling, field)
		infos = append(infos, derivedIdentifierSibling{id: sibling.ID, authored: authored})
	}
	return deriveSiblingIdentifier(span.ID, prefix, infos)
}

type derivedIdentifierSibling struct {
	id       string
	authored string
}

func deriveSiblingIdentifier(nodeID, prefix string, infos []derivedIdentifierSibling) (string, bool) {
	used := map[int]bool{}
	for _, info := range infos {
		if n, ok := trailingNumberAfterPrefix(info.authored, prefix); ok {
			used[n] = true
		}
	}
	next := 1
	for _, info := range infos {
		if info.authored != "" {
			continue
		}
		for used[next] {
			next++
		}
		if info.id == nodeID {
			return fmt.Sprintf("%s%d", prefix, next), true
		}
		used[next] = true
		next++
	}
	return "", false
}

// ancestorSemanticID walks up structural parents and returns the semantic id
// of the nearest enclosing @node(locator: EMBEDDED) ancestor, or the note's
// preferred-identifier value if no embedded ancestor exists.
func (r *projectionResolver) ancestorSemanticID(nodeID string) string {
	for parentID := strings.TrimSpace(r.parentIDByID[nodeID]); parentID != ""; parentID = strings.TrimSpace(r.parentIDByID[parentID]) {
		if parentSpan := r.snapshot.SourceSpansByID[parentID]; parentSpan != nil && parentSpan.Shape != EmbeddedSourceShapeSection {
			parentType := r.schema.Types[r.sectionTypeByID[parentID]]
			if parentType == nil || parentType.Role != TypeRoleEmbeddedNode {
				continue
			}
			if id := r.semanticIDForSourceSpan(parentSpan, parentType); id != "" {
				return id
			}
			continue
		}
		parentNode := r.snapshot.SectionsByID[parentID]
		if parentNode == nil {
			continue
		}
		parentType := r.schema.Types[r.sectionTypeByID[parentID]]
		if parentType == nil || parentType.Role != TypeRoleEmbeddedNode {
			continue
		}
		parentProjection := r.projectSection(parentNode, r.sectionNodeRef(parentNode))
		if field := preferredIdentifierField(parentType); field != nil {
			for _, value := range parentProjection.Fields[field.Name].Values {
				if id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "^")); id != "" {
					return id
				}
			}
		}
	}
	if r.noteType != nil {
		for _, f := range r.noteType.Fields {
			if f != nil && f.IsPreferredIdentifier {
				if v := stringValue(r.noteDoc.Frontmatter[strings.TrimSpace(noteFieldSource(f))]); v != "" {
					return strings.TrimSpace(strings.TrimPrefix(v, "^"))
				}
			}
		}
	}
	return ""
}

func (r *projectionResolver) semanticIDForSourceSpan(span *MarkdownSourceSpan, nodeType *NoteType) string {
	field := preferredIdentifierField(nodeType)
	if field == nil {
		return ""
	}
	if v := r.authoredIdentifierValueForSourceSpan(span, field); v != "" {
		return v
	}
	if !field.IsDerivableIdentifier {
		return ""
	}
	if derived, ok := r.deriveSourceSpanIdentifierValue(span, field); ok {
		return derived
	}
	return ""
}

func (r *projectionResolver) authoredIdentifierValueForSourceSpan(span *MarkdownSourceSpan, field *Field) string {
	if r == nil || r.snapshot == nil || span == nil || field == nil {
		return ""
	}
	propertyCase := r.propertyCaseByID[span.ID]
	if propertyCase == "" {
		propertyCase = propertyCaseOrDefault(r.noteType)
	}
	binding := sourceSpanValueBinding(r.snapshot, span, field, propertyCase)
	for _, v := range binding.Values {
		v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "^"))
		if v != "" {
			return v
		}
	}
	return ""
}

// authoredIdentifierValueForNode returns the trimmed authored value of the
// preferred-identifier inline field on the given section node, or "" if no
// `id::` line is authored.
func authoredIdentifierValueForNode(snapshot *DocumentSnapshot, node *SectionNode, field *Field) string {
	if snapshot == nil || node == nil || field == nil {
		return ""
	}
	binding := sectionValueBinding(snapshot, node, field, PropertyCaseKebab)
	for _, v := range binding.Values {
		v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "^"))
		if v != "" {
			return v
		}
	}
	return authoredIdentifierValueFromRawNodeContent(node, field)
}

func authoredIdentifierValueFromRawNodeContent(node *SectionNode, field *Field) string {
	if node == nil || field == nil {
		return ""
	}
	for _, line := range strings.Split(node.Content, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, name := range FieldSourceNames(field) {
			prefix := name + "::"
			if !strings.EqualFold(trimmed[:min(len(trimmed), len(prefix))], prefix) {
				continue
			}
			value := strings.TrimSpace(trimmed[len(prefix):])
			value = strings.TrimSpace(strings.TrimPrefix(value, "^"))
			if value != "" {
				return value
			}
		}
	}
	return ""
}

// trailingNumberAfterPrefix returns the trailing integer in s after `prefix`
// when s exactly matches `${prefix}<digits>`; otherwise returns ok=false.
func trailingNumberAfterPrefix(s, prefix string) (int, bool) {
	if !strings.HasPrefix(s, prefix) {
		return 0, false
	}
	rest := s[len(prefix):]
	if rest == "" {
		return 0, false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
