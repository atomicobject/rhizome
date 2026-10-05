package ontology

import (
	"sort"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func scanInlineFields(content string, ranges []ByteRange) []InlineFieldSpan {
	return scanInlineFieldRanges(content, ranges, scanInlineFieldsInSegment)
}

func scanInlineFieldRanges(content string, ranges []ByteRange, scan func(string, int) []InlineFieldSpan) []InlineFieldSpan {
	out := make([]InlineFieldSpan, 0)
	for _, r := range ranges {
		if r.Valid(len(content)) && r.Len() > 0 {
			out = append(out, scan(content[r.Start:r.End], r.Start)...)
		}
	}
	return out
}

func scanSectionInlineFields(content string, ranges []ByteRange) []InlineFieldSpan {
	out := scanInlineFields(content, ranges)
	out = append(out, scanBulletMetadataInlineFields(content, ranges)...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].WholeRange.Start < out[j].WholeRange.Start
	})
	return out
}

// SectionInlineProperties extracts inline properties from section-owned
// markdown. Unlike note-wide extraction, it also accepts lightweight metadata
// bullets such as `- status:: ready`, which are scoped to section-backed
// embedded nodes.
func SectionInlineProperties(content string) map[string][]string {
	props := obsidian.ExtractInlineProperties(content)
	for _, span := range scanBulletMetadataInlineFields(content, []ByteRange{{Start: 0, End: len(content)}}) {
		props[span.PropertyKey] = append(props[span.PropertyKey], span.Value)
	}
	return props
}

func scanInlineFieldsInSegment(segment string, baseOffset int) []InlineFieldSpan {
	lines := strings.SplitAfter(segment, "\n")
	if len(lines) == 0 {
		return nil
	}
	out := make([]InlineFieldSpan, 0)
	inCodeFence := false
	offset := baseOffset
	for _, rawLine := range lines {
		line := rawLine
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\n"))
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCodeFence = !inCodeFence
			offset += len(line)
			continue
		}
		if inCodeFence || trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "|") {
			offset += len(line)
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
			offset += len(line)
			continue
		}
		if len(trimmed) > 1 && trimmed[0] >= '0' && trimmed[0] <= '9' && strings.Contains(trimmed, ". ") {
			offset += len(line)
			continue
		}
		fields := inlineFieldOccurrences(trimmed, false)
		if len(fields) == 0 {
			offset += len(line)
			continue
		}
		lineTrimOffset := strings.Index(line, trimmed)
		if lineTrimOffset < 0 {
			lineTrimOffset = 0
		}
		absoluteStart := offset + lineTrimOffset
		for _, field := range fields {
			keyStart := absoluteStart + field.keyStart
			keyEnd := absoluteStart + field.keyEnd
			valueStart := absoluteStart + field.valueStart
			valueEnd := absoluteStart + field.valueEnd
			out = append(out, InlineFieldSpan{
				Key:            field.key,
				Value:          strings.TrimSpace(trimmed[field.valueStart:field.valueEnd]),
				LineRange:      ByteRange{Start: offset, End: offset + len(line)},
				KeyRange:       ByteRange{Start: keyStart, End: keyEnd},
				ValueRange:     ByteRange{Start: valueStart, End: valueEnd},
				WholeRange:     ByteRange{Start: keyStart, End: valueEnd},
				PropertyKey:    field.key,
				AuthoringStyle: FieldAuthoringStyleInlineProperty,
			})
		}
		offset += len(line)
	}
	return out
}

func scanBulletMetadataInlineFields(content string, ranges []ByteRange) []InlineFieldSpan {
	return scanInlineFieldRanges(content, ranges, scanBulletMetadataInlineFieldsInSegment)
}

func scanBulletMetadataInlineFieldsInSegment(segment string, baseOffset int) []InlineFieldSpan {
	lines := strings.SplitAfter(segment, "\n")
	out := make([]InlineFieldSpan, 0)
	inCodeFence := false
	offset := baseOffset
	for _, rawLine := range lines {
		line := rawLine
		trimmedLine := strings.TrimRight(line, "\n")
		trimmed := strings.TrimSpace(trimmedLine)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCodeFence = !inCodeFence
			offset += len(line)
			continue
		}
		if inCodeFence || trimmed == "" {
			offset += len(line)
			continue
		}
		itemStart, ok := unorderedListItemContentStart(trimmed)
		if !ok {
			offset += len(line)
			continue
		}
		for itemStart < len(trimmed) && (trimmed[itemStart] == ' ' || trimmed[itemStart] == '\t') {
			itemStart++
		}
		item := trimmed[itemStart:]
		field, ok := bulletMetadataFieldOccurrence(item)
		if !ok {
			offset += len(line)
			continue
		}
		lineTrimOffset := strings.Index(line, trimmed)
		if lineTrimOffset < 0 {
			lineTrimOffset = 0
		}
		absoluteTrimStart := offset + lineTrimOffset
		keyStart := absoluteTrimStart + itemStart + field.keyStart
		keyEnd := absoluteTrimStart + itemStart + field.keyEnd
		valueStart := absoluteTrimStart + itemStart + field.valueStart
		valueEnd := absoluteTrimStart + itemStart + field.valueEnd
		out = append(out, InlineFieldSpan{
			Key:            field.key,
			Value:          strings.TrimSpace(item[field.valueStart:field.valueEnd]),
			LineRange:      ByteRange{Start: offset, End: offset + len(line)},
			KeyRange:       ByteRange{Start: keyStart, End: keyEnd},
			ValueRange:     ByteRange{Start: valueStart, End: valueEnd},
			WholeRange:     ByteRange{Start: offset, End: valueEnd},
			PropertyKey:    field.key,
			AuthoringStyle: FieldAuthoringStyleListMetadata,
		})
		offset += len(line)
	}
	return out
}

func bulletMetadataFieldOccurrence(item string) (listInlineFieldOccurrence, bool) {
	idx := strings.Index(item, "::")
	if idx <= 0 {
		return listInlineFieldOccurrence{}, false
	}
	key := strings.TrimSpace(item[:idx])
	if key == "" || strings.Contains(key, " ") || !isInlineKeyLike(key) {
		return listInlineFieldOccurrence{}, false
	}
	valueStart := idx + 2
	for valueStart < len(item) && unicode.IsSpace(rune(item[valueStart])) {
		valueStart++
	}
	valueEnd := trimSpaceLeftBounded(item, valueStart, len(item))
	if valueEnd <= valueStart {
		return listInlineFieldOccurrence{}, false
	}
	return listInlineFieldOccurrence{
		key:        key,
		keyStart:   0,
		keyEnd:     idx,
		valueStart: valueStart,
		valueEnd:   valueEnd,
	}, true
}

func unorderedListItemContentStart(trimmed string) (int, bool) {
	if len(trimmed) < 2 || trimmed[1] != ' ' && trimmed[1] != '\t' {
		return 0, false
	}
	switch trimmed[0] {
	case '-', '*', '+':
		return 2, true
	default:
		return 0, false
	}
}

func scanInlineFieldsInListOwnContent(content string, ranges []ByteRange) []InlineFieldSpan {
	return scanInlineFieldRanges(content, ranges, scanInlineFieldsInListSegment)
}

func scanInlineFieldsInListSegment(segment string, baseOffset int) []InlineFieldSpan {
	lines := strings.SplitAfter(segment, "\n")
	out := make([]InlineFieldSpan, 0)
	offset := baseOffset
	for _, rawLine := range lines {
		line := rawLine
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\n"))
		if trimmed == "" {
			offset += len(line)
			continue
		}
		fields := inlineFieldOccurrences(trimmed, true)
		if len(fields) == 0 {
			offset += len(line)
			continue
		}
		lineTrimOffset := strings.Index(line, trimmed)
		if lineTrimOffset < 0 {
			lineTrimOffset = 0
		}
		absoluteTrimStart := offset + lineTrimOffset
		for _, field := range fields {
			keyStart := absoluteTrimStart + field.keyStart
			keyEnd := absoluteTrimStart + field.keyEnd
			valueStart := absoluteTrimStart + field.valueStart
			valueEnd := absoluteTrimStart + field.valueEnd
			out = append(out, InlineFieldSpan{
				Key:            field.key,
				Value:          strings.TrimSpace(trimmed[field.valueStart:field.valueEnd]),
				LineRange:      ByteRange{Start: keyStart, End: valueEnd},
				KeyRange:       ByteRange{Start: keyStart, End: keyEnd},
				ValueRange:     ByteRange{Start: valueStart, End: valueEnd},
				WholeRange:     ByteRange{Start: keyStart, End: valueEnd},
				PropertyKey:    field.key,
				AuthoringStyle: FieldAuthoringStyleInlineProperty,
			})
		}
		offset += len(line)
	}
	return out
}

type listInlineFieldOccurrence struct {
	key        string
	keyStart   int
	keyEnd     int
	valueStart int
	valueEnd   int
}

func inlineFieldOccurrences(line string, trimTrailingMarkers bool) []listInlineFieldOccurrence {
	found := make([]listInlineFieldOccurrence, 0)
	for cursor := 0; cursor < len(line); {
		relative := strings.Index(line[cursor:], "::")
		if relative < 0 {
			break
		}
		idx := cursor + relative
		keyStart := idx - 1
		for keyStart >= 0 && isInlineKeyLikeRune(rune(line[keyStart])) {
			keyStart--
		}
		keyStart++
		key := strings.TrimSpace(line[keyStart:idx])
		if key == "" || strings.Contains(key, " ") || !isInlineKeyLike(key) || !inlineKeyHasBoundary(line, keyStart) {
			cursor = idx + 2
			continue
		}
		valueStart := idx + 2
		for valueStart < len(line) && unicode.IsSpace(rune(line[valueStart])) {
			valueStart++
		}
		found = append(found, listInlineFieldOccurrence{
			key:        key,
			keyStart:   keyStart,
			keyEnd:     idx,
			valueStart: valueStart,
		})
		cursor = idx + 2
	}
	out := make([]listInlineFieldOccurrence, 0, len(found))
	for idx, field := range found {
		valueEnd := len(line)
		if idx+1 < len(found) {
			valueEnd = found[idx+1].keyStart
		}
		valueEnd = trimSpaceLeftBounded(line, field.valueStart, valueEnd)
		if trimTrailingMarkers {
			valueEnd = trimListInlineValueEnd(line, field.valueStart, valueEnd)
		}
		if valueEnd <= field.valueStart {
			continue
		}
		field.valueEnd = valueEnd
		out = append(out, field)
	}
	return out
}

func inlineKeyHasBoundary(line string, keyStart int) bool {
	if keyStart <= 0 {
		return true
	}
	return !isInlineKeyLikeRune(rune(line[keyStart-1]))
}

func trimListInlineValueEnd(line string, valueStart, valueEnd int) int {
	valueEnd = trimSpaceLeftBounded(line, valueStart, valueEnd)
	for {
		tokenStart := trailingListMetadataTokenStart(line, valueStart, valueEnd)
		if tokenStart < 0 {
			return valueEnd
		}
		valueEnd = trimSpaceLeftBounded(line, valueStart, tokenStart)
	}
}

func trimSpaceLeftBounded(line string, start, end int) int {
	for end > start && unicode.IsSpace(rune(line[end-1])) {
		end--
	}
	return end
}

func trailingListMetadataTokenStart(line string, start, end int) int {
	cursor := end - 1
	for cursor >= start && !unicode.IsSpace(rune(line[cursor])) {
		cursor--
	}
	tokenStart := cursor + 1
	if tokenStart >= end {
		return -1
	}
	token := line[tokenStart:end]
	switch {
	case strings.HasPrefix(token, "#") && len(token) > 1 && isMarkerCharString(token[1:]):
		return tokenStart
	case strings.HasPrefix(token, "^") && ValidBlockID(strings.TrimPrefix(token, "^")):
		return tokenStart
	default:
		return -1
	}
}

func isMarkerCharString(s string) bool {
	for _, r := range s {
		if !isMarkerChar(r) {
			return false
		}
	}
	return true
}

func isInlineKeyLikeRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_'
}

func isInlineKeyLike(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == ' ':
			if i == 0 && !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
