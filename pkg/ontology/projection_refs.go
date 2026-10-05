package ontology

import (
	"fmt"
	"strconv"
	"strings"
)

func (r *projectionResolver) resolveSectionRef(ref NodeRef) (*SectionNode, NodeRef, error) {
	fragment := normalizeSectionFragment(ref.Fragment, ref.NotePath)
	if fragment == "" && strings.TrimSpace(ref.NodeID) == "" && strings.TrimSpace(ref.Structural) == "" {
		return nil, NodeRef{}, fmt.Errorf("section ref requires fragment, node id, or structural fingerprint")
	}
	// Resolution order matters for edit replay and caches: strong identity wins
	// before rendered heading/block fragments, so unrelated heading edits do not
	// silently retarget an operation that captured NodeID/Structural identity.
	if node := r.resolveSectionByNodeID(strings.TrimSpace(ref.NodeID)); node != nil {
		return node, r.sectionNodeRef(node), nil
	}
	if node := r.resolveSectionByStructural(strings.TrimSpace(ref.Structural)); node != nil {
		return node, r.sectionNodeRef(node), nil
	}
	if node := r.resolveSectionByRenderedID(fragment); node != nil {
		return node, r.sectionNodeRef(node), nil
	}
	node, err := r.resolveSectionByFragment(fragment)
	if err != nil {
		return nil, NodeRef{}, err
	}
	if node != nil {
		return node, r.sectionNodeRef(node), nil
	}
	return nil, NodeRef{}, fmt.Errorf("section %s not found in %s", ref.Fragment, ref.NotePath)
}

func (r *projectionResolver) resolveSourceSpanRef(ref NodeRef) (*MarkdownSourceSpan, NodeRef, bool, error) {
	nodeID := strings.TrimSpace(ref.NodeID)
	structural := strings.TrimSpace(ref.Structural)
	if nodeID != "" && (structural == "" || strings.Contains(nodeID, "#^")) {
		if span := r.snapshot.SourceSpansByID[nodeID]; span != nil {
			return span, r.sourceSpanNodeRef(span), true, nil
		}
	}
	if structural != "" {
		matches := r.sourceSpansForStructural(structural)
		switch len(matches) {
		case 1:
			return matches[0], r.sourceSpanNodeRef(matches[0]), true, nil
		case 0:
			fragment := normalizeSectionFragment(ref.Fragment, ref.NotePath)
			_, itemFragment := sourceSpanStartFromFragment(fragment)
			if strings.Contains(nodeID, "#item-") || itemFragment {
				return nil, NodeRef{}, false, fmt.Errorf("structural fingerprint did not resolve an embedded item in %s", ref.NotePath)
			}
		default:
			return nil, NodeRef{}, false, fmt.Errorf("%w: structural fingerprint matches %d unanchored items in %s", ErrAmbiguousSourceSpanIdentity, len(matches), ref.NotePath)
		}
	}
	if nodeID != "" {
		if span := r.snapshot.SourceSpansByID[nodeID]; span != nil {
			return span, r.sourceSpanNodeRef(span), true, nil
		}
	}
	fragment := normalizeSectionFragment(ref.Fragment, ref.NotePath)
	if fragment == "" {
		return nil, NodeRef{}, false, nil
	}
	if start, ok := sourceSpanStartFromFragment(fragment); ok {
		for _, span := range r.snapshot.SourceSpans {
			if span == nil || span.Shape == EmbeddedSourceShapeSection {
				continue
			}
			if span.Range.Start == start {
				return span, r.sourceSpanNodeRef(span), true, nil
			}
		}
	}
	for _, span := range r.snapshot.SourceSpans {
		if span == nil || span.Shape == EmbeddedSourceShapeSection {
			continue
		}
		if sourceSpanFragment(span) == fragment {
			return span, r.sourceSpanNodeRef(span), true, nil
		}
	}
	return nil, NodeRef{}, false, nil
}

func sourceSpanStartFromFragment(fragment string) (int, bool) {
	fragment = strings.TrimSpace(fragment)
	if !strings.HasPrefix(fragment, "item-") {
		return 0, false
	}
	start, err := strconv.Atoi(strings.TrimPrefix(fragment, "item-"))
	if err != nil {
		return 0, false
	}
	return start, true
}

func normalizeSectionFragment(fragment string, notePath string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(fragment, "#"))
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, strings.TrimSpace(notePath)+"#") {
		parts := strings.SplitN(trimmed, "#", 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}
	return trimmed
}

func (r *projectionResolver) resolveSectionByNodeID(nodeID string) *SectionNode {
	if nodeID == "" {
		return nil
	}
	if node := r.snapshot.SectionsByID[nodeID]; node != nil {
		return node
	}
	fragment := normalizeSectionFragment(nodeID, r.snapshot.NotePath)
	if fragment == "" {
		return nil
	}
	if node := r.resolveSectionByRenderedID(fragment); node != nil {
		return node
	}
	node, _ := r.resolveSectionByFragment(fragment)
	return node
}

func (r *projectionResolver) resolveSectionByStructural(structural string) *SectionNode {
	if structural == "" {
		return nil
	}
	var matched *SectionNode
	var walk func(nodes []*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil || matched != nil {
				continue
			}
			if hashText(r.stableSectionFingerprint(node)) == structural {
				matched = node
				return
			}
			walk(node.Children)
		}
	}
	walk(r.snapshot.Sections)
	return matched
}

func (r *projectionResolver) resolveSectionByRenderedID(fragment string) *SectionNode {
	if fragment == "" {
		return nil
	}
	return r.snapshot.SectionsByID[strings.TrimSpace(r.snapshot.NotePath)+"#"+strings.TrimPrefix(strings.TrimSpace(fragment), "#")]
}

func (r *projectionResolver) resolveSectionByFragment(fragment string) (*SectionNode, error) {
	if fragment == "" {
		return nil, nil
	}
	matches := make([]*SectionNode, 0, 1)
	var walk func(nodes []*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			if strings.TrimSpace(node.Title) == fragment {
				matches = append(matches, node)
				continue
			}
			for _, candidate := range sectionFragments(node) {
				if candidate == fragment {
					matches = append(matches, node)
					break
				}
			}
			walk(node.Children)
		}
	}
	walk(r.snapshot.Sections)
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("section fragment %q is ambiguous in %s", fragment, r.snapshot.NotePath)
	}
}

func (r *projectionResolver) sectionNodeRef(node *SectionNode) NodeRef {
	typeName := r.sectionTypeByID[node.ID]
	kind := NodeKindSection
	if noteType := r.schema.Types[typeName]; noteType != nil && noteType.Role == TypeRoleEmbeddedNode {
		kind = NodeKindEmbedded
	}
	return NodeRef{
		NotePath:   r.snapshot.NotePath,
		Fragment:   sectionFragment(node),
		NodeID:     node.ID,
		TypeName:   typeName,
		Kind:       kind,
		StartByte:  node.StartByte,
		EndByte:    node.EndByte,
		ParentID:   r.parentIDByID[node.ID],
		Structural: hashText(r.stableSectionFingerprint(node)),
	}
}

func (r *projectionResolver) sourceSpanNodeRef(span *MarkdownSourceSpan) NodeRef {
	typeName := r.sectionTypeByID[span.ID]
	kind := NodeKindSection
	if noteType := r.schema.Types[typeName]; noteType != nil && noteType.Role == TypeRoleEmbeddedNode {
		kind = NodeKindEmbedded
	}
	return NodeRef{
		NotePath:   r.snapshot.NotePath,
		Fragment:   sourceSpanFragment(span),
		NodeID:     span.ID,
		TypeName:   typeName,
		Kind:       kind,
		StartByte:  span.Range.Start,
		EndByte:    span.Range.End,
		ParentID:   r.parentIDByID[span.ID],
		Structural: hashText(r.stableSourceSpanFingerprint(span)),
	}
}
