package structure

import "strings"

func linkListSourceSpansToSections(spans []*SourceSpan) {
	if len(spans) == 0 {
		return
	}
	byID := make(map[string]*SourceSpan, len(spans))
	var sections []*SourceSpan
	for _, span := range spans {
		if span == nil {
			continue
		}
		byID[span.ID] = span
		if span.Shape == Section {
			sections = append(sections, span)
		}
	}
	for _, span := range spans {
		if span == nil || span.Shape == Section || strings.TrimSpace(span.ParentID) != "" {
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

func nearestContainingSection(sections []*SourceSpan, child ByteRange) *SourceSpan {
	var best *SourceSpan
	for _, section := range sections {
		if section == nil || section.Shape != Section {
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

func sourceSpanHasChild(parent *SourceSpan, childID string) bool {
	for _, child := range parent.Children {
		if child != nil && child.ID == childID {
			return true
		}
	}
	return false
}
