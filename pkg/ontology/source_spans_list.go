package ontology

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/ontology/sectionparse"

	"strings"
	"unicode"
)

type rawMarkdownItem struct {
	lineIndex     int
	parentIndex   int
	indent        int
	startByte     int
	markerRange   ByteRange
	checkbox      *CheckboxState
	contentStart  int
	blockID       string
	malformed     string
	sourceEndByte int
}

func parseListItemStart(line string, lineStart int) (rawMarkdownItem, bool) {
	indent := sectionparse.CountLeadingSpaces(line)
	pos := indent
	if pos >= len(line) {
		return rawMarkdownItem{}, false
	}
	markerEnd, ok := listMarkerEnd(line, pos)
	if !ok {
		return rawMarkdownItem{}, false
	}
	contentStart := markerEnd
	for contentStart < len(line) && (line[contentStart] == ' ' || line[contentStart] == '\t') {
		contentStart++
	}
	markerRange := ByteRange{Start: lineStart + pos, End: lineStart + contentStart}
	var checkbox *CheckboxState
	if checked, tokenEnd, ok := checkboxToken(line[contentStart:]); ok {
		tokenRange := ByteRange{Start: lineStart + contentStart, End: lineStart + contentStart + tokenEnd}
		checkbox = &CheckboxState{Checked: checked, TokenRange: tokenRange}
		contentStart += tokenEnd
		for contentStart < len(line) && (line[contentStart] == ' ' || line[contentStart] == '\t') {
			contentStart++
		}
		markerRange.End = lineStart + contentStart
	}
	if checkbox == nil {
		itemText := strings.TrimSpace(line[contentStart:])
		if _, ok := bulletMetadataFieldOccurrence(itemText); ok {
			return rawMarkdownItem{}, false
		}
	}
	blockID, malformed := blockIDFromItemLine(line[contentStart:])
	return rawMarkdownItem{
		parentIndex:  -1,
		indent:       indent,
		startByte:    lineStart,
		markerRange:  markerRange,
		checkbox:     checkbox,
		contentStart: lineStart + contentStart,
		blockID:      blockID,
		malformed:    malformed,
	}, true
}

func assignListParents(items []rawMarkdownItem) {
	stack := make([]int, 0)
	for idx := range items {
		for len(stack) > 0 && items[stack[len(stack)-1]].indent >= items[idx].indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			items[idx].parentIndex = stack[len(stack)-1]
		}
		stack = append(stack, idx)
	}
}

func listItemEndByte(items []rawMarkdownItem, idx int, lines []string, lineOffsets []int, contentLen int) int {
	item := items[idx]
	end := contentLen
	for next := idx + 1; next < len(items); next++ {
		if items[next].indent <= item.indent {
			end = items[next].startByte
			break
		}
	}
	for lineIdx := item.lineIndex + 1; lineIdx < len(lines); lineIdx++ {
		lineStart := lineOffsets[lineIdx]
		if lineStart <= item.startByte {
			continue
		}
		if lineStart >= end {
			break
		}
		if isTopLevelATXHeading(lines[lineIdx]) {
			return lineStart
		}
	}
	return end
}

func listMarkerEnd(line string, pos int) (int, bool) {
	if pos >= len(line) {
		return 0, false
	}
	switch line[pos] {
	case '-', '*', '+':
		if pos+1 < len(line) && (line[pos+1] == ' ' || line[pos+1] == '\t') {
			return pos + 1, true
		}
		return 0, false
	default:
		if !unicode.IsDigit(rune(line[pos])) {
			return 0, false
		}
		cursor := pos
		for cursor < len(line) && unicode.IsDigit(rune(line[cursor])) {
			cursor++
		}
		if cursor == pos || cursor >= len(line) {
			return 0, false
		}
		if line[cursor] != '.' && line[cursor] != ')' {
			return 0, false
		}
		if cursor+1 < len(line) && (line[cursor+1] == ' ' || line[cursor+1] == '\t') {
			return cursor + 1, true
		}
		return 0, false
	}
}

func checkboxToken(s string) (bool, int, bool) {
	if len(s) < 3 || s[0] != '[' || s[2] != ']' {
		return false, 0, false
	}
	switch s[1] {
	case ' ':
		return false, 3, true
	case 'x', 'X':
		return true, 3, true
	default:
		return false, 0, false
	}
}

func blockIDFromItemLine(line string) (string, string) {
	if candidate, malformed, ok := sectionparse.ProseBlockIDCandidate(strings.TrimSpace(line)); ok {
		if malformed {
			return "", candidate
		}
		return candidate, ""
	}
	if candidate, ok := sectionparse.InlineIdentifierBlockID(strings.TrimSpace(line)); ok {
		if !markdownsyntax.ValidBlockID(candidate) {
			return "", candidate
		}
		return candidate, ""
	}
	return "", ""
}

func markdownSourceSpanFromRaw(notePath, content string, item rawMarkdownItem) *MarkdownSourceSpan {
	shape := ontoschema.EmbeddedSourceShapeListItem
	if item.checkbox != nil {
		shape = ontoschema.EmbeddedSourceShapeCheckboxItem
	}
	contentRange := ByteRange{Start: item.contentStart, End: item.sourceEndByte}
	if contentRange.Start > contentRange.End {
		contentRange.Start = contentRange.End
	}
	sourceContent := sectionparse.SubstringBytes(content, item.startByte, item.sourceEndByte)
	itemContent := strings.TrimSpace(sectionparse.SubstringBytes(content, contentRange.Start, contentRange.End))
	id := fmt.Sprintf("%s#item-%d", notePath, item.startByte)
	if item.blockID != "" {
		id = fmt.Sprintf("%s#^%s", notePath, item.blockID)
	}
	markers := markerSpans(content, contentRange)
	title := sourceSpanItemTitle(content, contentRange, markers)
	if title == "" {
		title = firstLineTitle(itemContent)
	}
	return &MarkdownSourceSpan{
		ID:               id,
		NotePath:         notePath,
		Shape:            shape,
		Title:            title,
		BlockID:          item.blockID,
		MalformedBlockID: item.malformed,
		MarkerRange:      item.markerRange,
		Markers:          markers,
		Checkbox:         item.checkbox,
		Range:            ByteRange{Start: item.startByte, End: item.sourceEndByte},
		ContentRange:     contentRange,
		Content:          sourceContent,
	}
}
