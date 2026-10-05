package ontology

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type CheckboxState struct {
	Checked    bool
	TokenRange ByteRange
}

type MarkerSpan struct {
	Value string
	Range ByteRange
}

// MarkdownSourceSpan is a schema-free parsed markdown ownership span.
//
// WHY: source spans answer "which bytes does this markdown construct own?"
// before ontology projection answers "does this construct become a typed
// embedded node?" Inline editing and replay need this ownership layer so list
// items, checkbox tokens, nested children, marker tags, and block locators have
// one source of truth independent of schema matching.
type MarkdownSourceSpan struct {
	ID               string
	NotePath         string
	Shape            EmbeddedSourceShape
	Title            string
	ParentID         string
	Children         []*MarkdownSourceSpan
	BlockID          string
	MalformedBlockID string
	MarkerRange      ByteRange
	Markers          []MarkerSpan
	Checkbox         *CheckboxState
	Range            ByteRange
	ContentRange     ByteRange
	OwnContentRanges []ByteRange
	Content          string
}

func ParseMarkdownListSourceSpans(notePath, content string) []*MarkdownSourceSpan {
	lines := strings.Split(content, "\n")
	lineOffsets := make([]int, len(lines)+1)
	for i := 0; i < len(lines); i++ {
		lineOffsets[i+1] = lineOffsets[i] + len(lines[i]) + 1
	}

	frontmatter := frontmatterRange(content)
	raw := make([]rawMarkdownItem, 0)
	fence := ""
	for i, line := range lines {
		start := lineOffsets[i]
		if frontmatter.Valid(len(content)) && start < frontmatter.End {
			continue
		}
		trim := strings.TrimSpace(line)
		if nextFence, toggled := fenceDelimiter(trim, fence); toggled {
			fence = nextFence
			continue
		}
		if fence != "" {
			continue
		}
		item, ok := parseListItemStart(line, start)
		if !ok {
			continue
		}
		item.lineIndex = i
		raw = append(raw, item)
	}
	assignListParents(raw)

	out := make([]*MarkdownSourceSpan, 0, len(raw))
	byIndex := make(map[int]*MarkdownSourceSpan, len(raw))
	for idx := range raw {
		raw[idx].sourceEndByte = listItemEndByte(raw, idx, lines, lineOffsets, len(content))
		span := markdownSourceSpanFromRaw(notePath, content, raw[idx])
		out = append(out, span)
		byIndex[idx] = span
	}
	for idx := range raw {
		parentIdx := raw[idx].parentIndex
		if parentIdx < 0 {
			continue
		}
		child := byIndex[idx]
		parent := byIndex[parentIdx]
		if child == nil || parent == nil {
			continue
		}
		child.ParentID = parent.ID
		parent.Children = append(parent.Children, child)
	}
	for idx := range raw {
		span := byIndex[idx]
		if span == nil {
			continue
		}
		childRanges := make([]ByteRange, 0, len(span.Children))
		for _, child := range span.Children {
			if child == nil {
				continue
			}
			childRanges = append(childRanges, child.Range)
		}
		span.OwnContentRanges = subtractByteRanges(span.ContentRange, childRanges)
	}
	return out
}

func sectionMarkdownSourceSpans(nodes []*SectionNode) []*MarkdownSourceSpan {
	out := make([]*MarkdownSourceSpan, 0)
	byID := make(map[string]*MarkdownSourceSpan)
	var walk func([]*SectionNode, string)
	walk = func(current []*SectionNode, parentID string) {
		for _, section := range current {
			if section == nil {
				continue
			}
			span := &MarkdownSourceSpan{
				ID:               section.ID,
				NotePath:         section.NotePath,
				Shape:            EmbeddedSourceShapeSection,
				Title:            section.Title,
				ParentID:         parentID,
				BlockID:          section.BlockID,
				MalformedBlockID: section.MalformedBlockID,
				Range:            ByteRange{Start: section.StartByte, End: section.EndByte},
				ContentRange:     ByteRange{Start: section.StartByte, End: section.EndByte},
				Content:          section.Content,
			}
			out = append(out, span)
			byID[span.ID] = span
			if parent := byID[parentID]; parent != nil {
				parent.Children = append(parent.Children, span)
			}
			walk(section.Children, section.ID)
			childRanges := make([]ByteRange, 0, len(span.Children))
			for _, child := range span.Children {
				if child == nil {
					continue
				}
				childRanges = append(childRanges, child.Range)
			}
			span.OwnContentRanges = subtractByteRanges(span.ContentRange, childRanges)
		}
	}
	walk(nodes, "")
	return out
}

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
	indent := countLeadingSpaces(line)
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
	if candidate, malformed, ok := proseBlockIDCandidate(strings.TrimSpace(line)); ok {
		if malformed {
			return "", candidate
		}
		return candidate, ""
	}
	if candidate, ok := inlineIdentifierBlockID(strings.TrimSpace(line)); ok {
		if !validBlockID(candidate) {
			return "", candidate
		}
		return candidate, ""
	}
	return "", ""
}

func markdownSourceSpanFromRaw(notePath, content string, item rawMarkdownItem) *MarkdownSourceSpan {
	shape := EmbeddedSourceShapeListItem
	if item.checkbox != nil {
		shape = EmbeddedSourceShapeCheckboxItem
	}
	contentRange := ByteRange{Start: item.contentStart, End: item.sourceEndByte}
	if contentRange.Start > contentRange.End {
		contentRange.Start = contentRange.End
	}
	sourceContent := substringBytes(content, item.startByte, item.sourceEndByte)
	itemContent := strings.TrimSpace(substringBytes(content, contentRange.Start, contentRange.End))
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
		indent := countLeadingSpaces(line)
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

func markerSpans(content string, owner ByteRange) []MarkerSpan {
	if !owner.Valid(len(content)) || owner.Len() == 0 {
		return nil
	}
	segment := content[owner.Start:owner.End]
	out := make([]MarkerSpan, 0)
	for offset := 0; offset < len(segment); offset++ {
		if segment[offset] != '#' {
			continue
		}
		if offset > 0 && !isMarkerBoundary(rune(segment[offset-1])) {
			continue
		}
		end := offset + 1
		for end < len(segment) && isMarkerChar(rune(segment[end])) {
			end++
		}
		if end == offset+1 {
			continue
		}
		value := segment[offset:end]
		out = append(out, MarkerSpan{
			Value: value,
			Range: ByteRange{
				Start: owner.Start + offset,
				End:   owner.Start + end,
			},
		})
	}
	return out
}

func isMarkerBoundary(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("([{", r)
}

func isMarkerChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '/'
}

func subtractByteRanges(owner ByteRange, children []ByteRange) []ByteRange {
	if owner.Len() == 0 {
		return nil
	}
	clean := make([]ByteRange, 0, len(children))
	for _, child := range children {
		start := maxInt(child.Start, owner.Start)
		end := minInt(child.End, owner.End)
		if start < end {
			clean = append(clean, ByteRange{Start: start, End: end})
		}
	}
	sort.Slice(clean, func(i, j int) bool {
		if clean[i].Start == clean[j].Start {
			return clean[i].End < clean[j].End
		}
		return clean[i].Start < clean[j].Start
	})
	out := make([]ByteRange, 0, len(clean)+1)
	cursor := owner.Start
	for _, child := range clean {
		if cursor < child.Start {
			out = append(out, ByteRange{Start: cursor, End: child.Start})
		}
		if cursor < child.End {
			cursor = child.End
		}
	}
	if cursor < owner.End {
		out = append(out, ByteRange{Start: cursor, End: owner.End})
	}
	return out
}

func firstLineTitle(s string) string {
	line := strings.TrimSpace(s)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	if line == "" {
		return ""
	}
	if len(line) > 80 {
		return strings.TrimSpace(line[:80])
	}
	return line
}

func isTopLevelATXHeading(line string) bool {
	leadingSpaces := countLeadingSpaces(line)
	if leadingSpaces > 3 {
		return false
	}
	left := strings.TrimLeft(line, " ")
	return strings.HasPrefix(left, "#") && strings.TrimSpace(left) != ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
