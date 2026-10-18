package ontology

import (
	"github.com/atomicobject/rhizome/pkg/ontology/sectionparse"

	"strings"
)

func sourceSpanItemTitle(content string, contentRange ByteRange, markers []MarkerSpan) string {
	headline := sourceSpanItemHeadline(content, contentRange, markers)
	if title, _, ok := splitTitledItemHeadline(headline); ok {
		return title
	}
	return firstLineTitle(headline)
}

func sourceSpanItemHeadline(content string, contentRange ByteRange, markers []MarkerSpan) string {
	if !contentRange.Valid(len(content)) || contentRange.Len() == 0 {
		return ""
	}
	lineEnd := contentRange.End
	if nextLine := strings.IndexByte(content[contentRange.Start:contentRange.End], '\n'); nextLine >= 0 {
		lineEnd = contentRange.Start + nextLine
	}
	titleEnd := lineEnd
	for _, marker := range markers {
		if marker.Range.Start >= contentRange.Start && marker.Range.Start < titleEnd {
			titleEnd = marker.Range.Start
		}
	}
	for _, inline := range scanInlineFieldsInListSegment(content[contentRange.Start:lineEnd], contentRange.Start) {
		if inline.WholeRange.Start >= contentRange.Start && inline.WholeRange.Start < titleEnd {
			titleEnd = inline.WholeRange.Start
		}
	}
	if tokenStart := trailingListMetadataTokenStart(content[contentRange.Start:lineEnd], 0, lineEnd-contentRange.Start); tokenStart >= 0 {
		absolute := contentRange.Start + tokenStart
		if absolute < titleEnd {
			titleEnd = absolute
		}
	}
	titleEnd = trimSpaceLeftBounded(content, contentRange.Start, titleEnd)
	return strings.TrimSpace(content[contentRange.Start:titleEnd])
}

func sourceSpanItemParts(content string, span *MarkdownSourceSpan) (title string, summary string, detail string) {
	if span == nil || !span.ContentRange.Valid(len(content)) || span.ContentRange.Len() == 0 {
		return "", "", ""
	}
	headline := sourceSpanItemHeadline(content, span.ContentRange, span.Markers)
	if itemTitle, itemSummary, ok := splitTitledItemHeadline(headline); ok {
		title = itemTitle
		summary = itemSummary
	} else {
		summary = firstLineTitle(headline)
	}
	segment := content[span.ContentRange.Start:span.ContentRange.End]
	if idx := strings.IndexByte(segment, '\n'); idx >= 0 && idx+1 < len(segment) {
		detail = normalizeSourceSpanItemDetail(segment[idx+1:])
	}
	return title, summary, detail
}

func splitTitledItemHeadline(headline string) (title string, summary string, ok bool) {
	headline = strings.TrimSpace(headline)
	if !strings.HasPrefix(headline, "**") {
		return "", "", false
	}
	end := strings.Index(headline[2:], "**")
	if end < 0 {
		return "", "", false
	}
	title = strings.TrimSpace(headline[2 : 2+end])
	rest := strings.TrimSpace(headline[2+end+2:])
	if title == "" || !strings.HasPrefix(rest, ":") {
		return "", "", false
	}
	summary = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
	return title, summary, true
}

func normalizeSourceSpanItemDetail(detail string) string {
	lines := strings.Split(strings.Trim(detail, "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	filtered := lines[:0]
	for _, line := range lines {
		if sourceSpanDetailLineIsInlineField(line) {
			continue
		}
		filtered = append(filtered, line)
	}
	lines = filtered
	if len(lines) == 0 {
		return ""
	}
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := sectionparse.CountLeadingSpaces(line)
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent <= 0 {
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		if len(line) >= minIndent {
			lines[i] = line[minIndent:]
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func sourceSpanDetailLineIsInlineField(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if itemStart, ok := unorderedListItemContentStart(trimmed); ok {
		item := strings.TrimSpace(trimmed[itemStart:])
		_, ok := bulletMetadataFieldOccurrence(item)
		return ok
	}
	idx := strings.Index(trimmed, "::")
	if idx <= 0 {
		return false
	}
	key := strings.TrimSpace(trimmed[:idx])
	if key == "" || strings.Contains(key, " ") || !isInlineKeyLike(key) {
		return false
	}
	value := strings.TrimSpace(trimmed[idx+2:])
	return value != ""
}
