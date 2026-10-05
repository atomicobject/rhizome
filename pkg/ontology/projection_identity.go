package ontology

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

func sourceSpanFragment(span *MarkdownSourceSpan) string {
	if span == nil {
		return ""
	}
	if blockID := strings.TrimSpace(span.BlockID); blockID != "" {
		return "^" + strings.TrimPrefix(blockID, "^")
	}
	return fmt.Sprintf("item-%d", span.Range.Start)
}

func (r *projectionResolver) stableSourceSpanFingerprint(span *MarkdownSourceSpan) string {
	if span == nil {
		return ""
	}
	if fingerprint := r.sourceSpanFingerprintByID[span.ID]; fingerprint != "" {
		return fingerprint
	}
	var fingerprint string
	if blockID := strings.TrimSpace(span.BlockID); blockID != "" {
		fingerprint = "block:" + strings.TrimPrefix(blockID, "^")
	} else {
		fingerprint = r.unanchoredSourceSpanFingerprint(span)
	}
	if r.sourceSpanFingerprintByID == nil {
		r.sourceSpanFingerprintByID = make(map[string]string, len(r.snapshot.SourceSpans))
	}
	r.sourceSpanFingerprintByID[span.ID] = fingerprint
	return fingerprint
}

func (r *projectionResolver) unanchoredSourceSpanFingerprint(span *MarkdownSourceSpan) string {
	if span == nil {
		return ""
	}
	if span.Shape == EmbeddedSourceShapeSection {
		if section := r.snapshot.SectionsByID[span.ID]; section != nil {
			return r.stableSectionFingerprint(section)
		}
		return ""
	}
	parentID := strings.TrimSpace(r.parentIDByID[span.ID])
	parentPath := "root"
	if parentID != "" {
		if parent := r.snapshot.SourceSpansByID[parentID]; parent != nil {
			parentPath = r.stableSourceSpanFingerprint(parent)
		}
	}
	ownRanges := sourceSpanOwnRanges(span)
	markers := make([]string, 0, len(span.Markers))
	for _, marker := range span.Markers {
		if markerRangeWithinAny(marker.Range, ownRanges) {
			markers = append(markers, marker.Value)
		}
	}
	return fmt.Sprintf("%s|%s|%s|%s", parentPath, span.Shape, strings.Join(markers, ","), r.sourceSpanIdentityText(span))
}

func (r *projectionResolver) sourceSpansForStructural(structural string) []*MarkdownSourceSpan {
	if r.sourceSpansByStructural == nil {
		r.sourceSpansByStructural = make(map[string][]*MarkdownSourceSpan, len(r.snapshot.SourceSpans))
		for _, span := range r.snapshot.SourceSpans {
			if span == nil || span.Shape == EmbeddedSourceShapeSection {
				continue
			}
			fingerprints := []string{hashText(r.stableSourceSpanFingerprint(span))}
			if strings.TrimSpace(span.BlockID) != "" {
				fingerprints = append(fingerprints, hashText(r.unanchoredSourceSpanFingerprint(span)))
			}
			for _, fingerprint := range fingerprints {
				r.sourceSpansByStructural[fingerprint] = append(r.sourceSpansByStructural[fingerprint], span)
			}
		}
	}
	return r.sourceSpansByStructural[structural]
}

func (r *projectionResolver) sourceSpanIdentityText(span *MarkdownSourceSpan) string {
	if span == nil {
		return ""
	}
	headline := sourceSpanIdentityHeadline(r.snapshot.Content, span)
	detailParts := make([]string, 0, len(span.OwnContentRanges))
	for _, sourceRange := range sourceSpanOwnRanges(span) {
		if !sourceRange.Valid(len(r.snapshot.Content)) {
			continue
		}
		start := sourceRange.Start
		if sourceRange.Start <= span.ContentRange.Start && span.ContentRange.Start < sourceRange.End {
			firstLine := r.snapshot.Content[span.ContentRange.Start:sourceRange.End]
			lineEnd := strings.IndexByte(firstLine, '\n')
			if lineEnd < 0 {
				continue
			}
			start = span.ContentRange.Start + lineEnd + 1
		}
		if start < sourceRange.End {
			detailParts = append(detailParts, substringBytes(r.snapshot.Content, start, sourceRange.End))
		}
	}
	detail := strings.TrimSpace(strings.Join(detailParts, "\n"))
	return strings.Join(strings.Fields(strings.TrimSpace(headline+"\n"+detail)), " ")
}

func sourceSpanIdentityHeadline(content string, span *MarkdownSourceSpan) string {
	if span == nil || !span.ContentRange.Valid(len(content)) || span.ContentRange.Len() == 0 {
		return ""
	}
	lineEnd := span.ContentRange.End
	if nextLine := strings.IndexByte(content[span.ContentRange.Start:span.ContentRange.End], '\n'); nextLine >= 0 {
		lineEnd = span.ContentRange.Start + nextLine
	}
	excluded := make([]ByteRange, 0, len(span.Markers)+1)
	for _, marker := range span.Markers {
		if marker.Range.Start >= span.ContentRange.Start && marker.Range.End <= lineEnd {
			excluded = append(excluded, marker.Range)
		}
	}
	if span.MarkerRange.Start >= span.ContentRange.Start && span.MarkerRange.End <= lineEnd {
		excluded = append(excluded, span.MarkerRange)
	}
	metadataEnd := lineEnd - span.ContentRange.Start
	line := content[span.ContentRange.Start:lineEnd]
	for {
		tokenStart := trailingListMetadataTokenStart(line, 0, metadataEnd)
		if tokenStart < 0 {
			break
		}
		excluded = append(excluded, ByteRange{Start: span.ContentRange.Start + tokenStart, End: span.ContentRange.Start + metadataEnd})
		metadataEnd = trimSpaceLeftBounded(line, 0, tokenStart)
	}
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].Start < excluded[j].Start })
	parts := make([]string, 0, len(excluded)+1)
	cursor := span.ContentRange.Start
	for _, sourceRange := range excluded {
		if sourceRange.Start < cursor {
			if sourceRange.End > cursor {
				cursor = sourceRange.End
			}
			continue
		}
		if cursor < sourceRange.Start {
			parts = append(parts, content[cursor:sourceRange.Start])
		}
		cursor = sourceRange.End
	}
	if cursor < lineEnd {
		parts = append(parts, content[cursor:lineEnd])
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func (r *projectionResolver) stableSectionFingerprint(node *SectionNode) string {
	if node == nil {
		return ""
	}
	if blockID := strings.TrimSpace(node.BlockID); blockID != "" {
		return "block:" + strings.TrimPrefix(blockID, "^")
	}
	return r.stableSectionHeadingFingerprint(node)
}

func (r *projectionResolver) stableSectionHeadingFingerprint(node *SectionNode) string {
	if node == nil {
		return ""
	}
	parentID := strings.TrimSpace(r.parentIDByID[node.ID])
	parentPath := "root"
	if parentID != "" {
		if parent := r.snapshot.SectionsByID[parentID]; parent != nil {
			parentPath = r.stableSectionFingerprint(parent)
		}
	}
	return fmt.Sprintf(
		"%s|%s|%s|%d",
		parentPath,
		node.Level,
		slugifyHeading(node.Title),
		r.siblingTitleOrdinal(node),
	)
}

func (r *projectionResolver) siblingTitleOrdinal(node *SectionNode) int {
	if node == nil {
		return 0
	}
	parentID := strings.TrimSpace(r.parentIDByID[node.ID])
	siblings := r.snapshot.Sections
	if parentID != "" {
		if parent := r.snapshot.SectionsByID[parentID]; parent != nil {
			siblings = parent.Children
		}
	}
	ordinal := 0
	want := strings.TrimSpace(node.Title)
	for _, sibling := range siblings {
		if sibling == nil || sibling.Level != node.Level || strings.TrimSpace(sibling.Title) != want {
			continue
		}
		if sibling.ID == node.ID {
			return ordinal
		}
		ordinal++
	}
	return ordinal
}

func sectionFragment(node *SectionNode) string {
	fragments := sectionFragments(node)
	if len(fragments) == 0 {
		return ""
	}
	return fragments[0]
}

func sectionFragments(node *SectionNode) []string {
	if node == nil {
		return nil
	}
	out := make([]string, 0, 2)
	if strings.TrimSpace(node.BlockID) != "" {
		out = append(out, "^"+strings.TrimPrefix(strings.TrimSpace(node.BlockID), "^"))
	}
	legacy := fmt.Sprintf("%s-%d", slugifyHeading(node.Title), node.StartByte)
	if legacy != "-" && legacy != "" {
		out = append(out, legacy)
	}
	if len(out) == 0 {
		if idx := strings.Index(node.ID, "#"); idx >= 0 && idx < len(node.ID)-1 {
			out = append(out, node.ID[idx+1:])
		}
	}
	return out
}

func sectionFingerprint(node *SectionNode) string {
	if node == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%d|%d", sectionFragment(node), strings.TrimSpace(node.Title), node.StartByte, node.EndByte)
}

func structureFingerprintText(notePath string, sections []*SectionNode) string {
	parts := []string{notePath}
	var walk func(nodes []*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			parts = append(parts, sectionFingerprint(node))
			walk(node.Children)
		}
	}
	walk(sections)
	sort.Strings(parts[1:])
	return strings.Join(parts, "\n")
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
