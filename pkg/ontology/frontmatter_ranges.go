package ontology

import "strings"

type frontmatterSourceLine struct {
	text       string
	start, end int
}

func exactFrontmatterValueRanges(snapshot *DocumentSnapshot, field *Field, values []string) ([]ByteRange, bool) {
	if snapshot == nil || field == nil || !snapshot.FrontmatterRange.Valid(len(snapshot.Content)) || snapshot.FrontmatterRange.Len() == 0 {
		return nil, false
	}
	lines := frontmatterSourceLines(snapshot.Content, snapshot.FrontmatterRange)
	var found []ByteRange
	matches := 0
	for _, sourceName := range FieldSourceNames(field) {
		ranges, ok, present := exactFrontmatterRangesForKey(lines, sourceName)
		if !present {
			continue
		}
		matches++
		if !ok {
			return nil, false
		}
		found = ranges
	}
	if matches != 1 || len(found) != len(values) {
		return nil, false
	}
	if !valueRangesExactlyMatch(snapshot.Content, values, found) {
		return nil, false
	}
	return found, true
}

func valueRangesExactlyMatch(content string, values []string, ranges []ByteRange) bool {
	if len(values) != len(ranges) {
		return false
	}
	for index, valueRange := range ranges {
		if !valueRange.Valid(len(content)) || content[valueRange.Start:valueRange.End] != values[index] {
			return false
		}
	}
	return true
}

func frontmatterSourceLines(content string, sourceRange ByteRange) []frontmatterSourceLine {
	text := content[sourceRange.Start:sourceRange.End]
	out := make([]frontmatterSourceLine, 0, strings.Count(text, "\n")+1)
	start := sourceRange.Start
	for start < sourceRange.End {
		relEnd := strings.IndexByte(content[start:sourceRange.End], '\n')
		end := sourceRange.End
		if relEnd >= 0 {
			end = start + relEnd
		}
		lineEnd := end
		if lineEnd > start && content[lineEnd-1] == '\r' {
			lineEnd--
		}
		out = append(out, frontmatterSourceLine{text: content[start:lineEnd], start: start, end: lineEnd})
		if relEnd < 0 {
			break
		}
		start = end + 1
	}
	return out
}

func exactFrontmatterRangesForKey(lines []frontmatterSourceLine, wanted string) ([]ByteRange, bool, bool) {
	wanted = strings.TrimSpace(wanted)
	var result []ByteRange
	present := false
	for index, line := range lines {
		if line.text == "" || line.text[0] == ' ' || line.text[0] == '\t' {
			continue
		}
		colon := strings.IndexByte(line.text, ':')
		if colon <= 0 || !strings.EqualFold(strings.TrimSpace(line.text[:colon]), wanted) {
			continue
		}
		if present {
			return nil, false, true
		}
		present = true
		remainder := line.text[colon+1:]
		trimmed := strings.TrimSpace(remainder)
		if strings.HasPrefix(trimmed, "[") {
			ranges, ok := exactPlainFlowListRanges(line, colon+1)
			if !ok {
				return nil, false, true
			}
			result = ranges
			continue
		}
		if trimmed != "" {
			valueRange, ok := exactPlainValueRange(line, colon+1, len(line.text))
			if !ok {
				return nil, false, true
			}
			result = []ByteRange{valueRange}
			continue
		}
		ranges, ok := exactPlainBlockListRanges(lines, index+1)
		if !ok {
			return nil, false, true
		}
		result = ranges
	}
	return result, present, present
}

func exactPlainBlockListRanges(lines []frontmatterSourceLine, start int) ([]ByteRange, bool) {
	out := make([]ByteRange, 0)
	for index := start; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line.text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line.text[0] != ' ' {
			break
		}
		if strings.ContainsRune(line.text[:len(line.text)-len(strings.TrimLeft(line.text, " \t"))], '\t') || !strings.HasPrefix(trimmed, "- ") {
			return nil, false
		}
		marker := strings.Index(line.text, "- ")
		valueRange, ok := exactPlainValueRange(line, marker+2, len(line.text))
		if !ok {
			return nil, false
		}
		out = append(out, valueRange)
	}
	return out, true
}

func exactPlainFlowListRanges(line frontmatterSourceLine, valueStart int) ([]ByteRange, bool) {
	left := valueStart
	for left < len(line.text) && (line.text[left] == ' ' || line.text[left] == '\t') {
		left++
	}
	if left >= len(line.text) || line.text[left] != '[' {
		return nil, false
	}
	right := len(line.text)
	for right > left && (line.text[right-1] == ' ' || line.text[right-1] == '\t') {
		right--
	}
	if right <= left+1 || line.text[right-1] != ']' {
		return nil, false
	}
	if strings.TrimSpace(line.text[left+1:right-1]) == "" {
		return nil, true
	}
	out := make([]ByteRange, 0)
	itemStart := left + 1
	for cursor := itemStart; cursor <= right-1; cursor++ {
		if cursor != right-1 && line.text[cursor] != ',' {
			continue
		}
		valueRange, ok := exactPlainValueRange(line, itemStart, cursor)
		if !ok {
			return nil, false
		}
		out = append(out, valueRange)
		itemStart = cursor + 1
	}
	return out, true
}

func exactPlainValueRange(line frontmatterSourceLine, start, end int) (ByteRange, bool) {
	for start < end && (line.text[start] == ' ' || line.text[start] == '\t') {
		start++
	}
	for end > start && (line.text[end-1] == ' ' || line.text[end-1] == '\t') {
		end--
	}
	if start >= end || !safePlainYAMLValue(line.text[start:end]) {
		return ByteRange{}, false
	}
	return ByteRange{Start: line.start + start, End: line.start + end}, true
}

func safePlainYAMLValue(value string) bool {
	if value == "" || strings.ContainsAny(value, "\r\n\t") {
		return false
	}
	if strings.Contains(value, " #") || strings.Contains(value, ": ") {
		return false
	}
	switch value[0] {
	case '\'', '"', '|', '>', '&', '*', '!', '{', '}', '[', ']', ',', '#', '`':
		return false
	}
	return !strings.ContainsAny(value, "[]{}\"")
}
