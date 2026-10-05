package obsidian

import "strings"

// MarkdownTargetKind distinguishes heading-text fragments from block IDs.
type MarkdownTargetKind string

const (
	MarkdownTargetHeading MarkdownTargetKind = "heading"
	MarkdownTargetBlock   MarkdownTargetKind = "block"
)

// MarkdownTarget is one linkable heading or block ID in Markdown source.
// StartByte and EndByte are half-open source offsets. Ordinal is scoped to
// targets with the same kind and normalized text within one note.
type MarkdownTarget struct {
	Kind           MarkdownTargetKind
	Text           string
	NormalizedText string
	Ordinal        int
	Level          int
	Line           int
	StartByte      int
	EndByte        int
}

type markdownSourceLine struct {
	text       string
	startByte  int
	endByte    int
	lineNumber int
}

// EnumerateMarkdownTargets returns ATX/Setext headings and standalone/inline
// Obsidian block IDs in document order. Frontmatter, fenced code, indented
// code, and inline-code lookalikes are excluded.
func EnumerateMarkdownTargets(content string) []MarkdownTarget {
	lines := markdownSourceLines(content)
	maskedContent := maskMarkdownInlineCode(content)
	frontmatterEnd := markdownFrontmatterEnd(lines)
	ordinals := make(map[string]int)
	var targets []MarkdownTarget
	appendTarget := func(target MarkdownTarget) {
		key := string(target.Kind) + "\x00" + target.NormalizedText
		ordinals[key]++
		target.Ordinal = ordinals[key]
		targets = append(targets, target)
	}

	var fenceChar byte
	fenceLength := 0
	previousSetextCandidate := -1
	for i, line := range lines {
		if i <= frontmatterEnd {
			previousSetextCandidate = -1
			continue
		}

		if marker, length, closing := markdownFence(line.text, fenceChar, fenceLength); marker != 0 {
			previousSetextCandidate = -1
			if fenceChar == 0 && !closing {
				fenceChar = marker
				fenceLength = length
			} else if fenceChar == marker && closing {
				fenceChar = 0
				fenceLength = 0
			}
			continue
		}
		if fenceChar != 0 {
			previousSetextCandidate = -1
			continue
		}
		if markdownIndentedCode(line.text) {
			previousSetextCandidate = -1
			continue
		}

		masked := maskedContent[line.startByte:line.endByte]
		trimmed := strings.TrimSpace(masked)
		level, rawSetext := setextHeadingLevel(strings.TrimSpace(line.text))
		_, maskedSetext := setextHeadingLevel(trimmed)
		if rawSetext && maskedSetext {
			if previousSetextCandidate >= 0 {
				previous := lines[previousSetextCandidate]
				previousMasked := maskedContent[previous.startByte:previous.endByte]
				text := strings.TrimSpace(previous.text)
				if _, start, _, found := trailingMarkdownBlockID(previous.text, previousMasked); found {
					text = strings.TrimSpace(previous.text[:start])
				}
				if text != "" {
					appendTarget(MarkdownTarget{
						Kind:           MarkdownTargetHeading,
						Text:           text,
						NormalizedText: NormalizeMarkdownHeadingFragment(text),
						Level:          level,
						Line:           previous.lineNumber,
						StartByte:      previous.startByte,
						EndByte:        line.endByte,
					})
				}
			}
			previousSetextCandidate = -1
			continue
		}

		blockID, blockStart, blockEnd, hasBlock := trailingMarkdownBlockID(line.text, masked)

		if level, text, ok := atxMarkdownHeading(line.text); ok {
			if hasBlock {
				text = strings.TrimSpace(line.text[:blockStart])
				_, text, ok = atxMarkdownHeading(text)
			}
			if ok && text != "" {
				appendTarget(MarkdownTarget{
					Kind:           MarkdownTargetHeading,
					Text:           text,
					NormalizedText: NormalizeMarkdownHeadingFragment(text),
					Level:          level,
					Line:           line.lineNumber,
					StartByte:      line.startByte,
					EndByte:        line.endByte,
				})
			}
			previousSetextCandidate = -1
		} else if trimmed == "" || markdownContainerOpener(line.text) || markdownThematicBreak(line.text) {
			previousSetextCandidate = -1
		} else {
			previousSetextCandidate = i
		}

		if hasBlock {
			appendTarget(MarkdownTarget{
				Kind:           MarkdownTargetBlock,
				Text:           blockID,
				NormalizedText: blockID,
				Line:           line.lineNumber,
				StartByte:      line.startByte + blockStart,
				EndByte:        line.startByte + blockEnd,
			})
		}
	}
	return targets
}

// NormalizeMarkdownHeadingFragment preserves the legacy heading-fragment
// resolver contract: heading matching is case-insensitive with outer space
// ignored. Block IDs deliberately do not use this normalization.
func NormalizeMarkdownHeadingFragment(fragment string) string {
	return strings.ToLower(strings.TrimSpace(fragment))
}

func markdownSourceLines(content string) []markdownSourceLine {
	if content == "" {
		return nil
	}
	lines := make([]markdownSourceLine, 0, 1+strings.Count(content, "\n"))
	start := 0
	lineNumber := 1
	for start < len(content) {
		relEnd := strings.IndexByte(content[start:], '\n')
		end := len(content)
		next := len(content)
		if relEnd >= 0 {
			end = start + relEnd
			next = end + 1
		}
		textEnd := end
		if textEnd > start && content[textEnd-1] == '\r' {
			textEnd--
		}
		lines = append(lines, markdownSourceLine{
			text:       content[start:textEnd],
			startByte:  start,
			endByte:    textEnd,
			lineNumber: lineNumber,
		})
		start = next
		lineNumber++
	}
	return lines
}

func markdownFrontmatterEnd(lines []markdownSourceLine) int {
	if len(lines) == 0 || strings.TrimSpace(lines[0].text) != "---" {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i].text)
		if trimmed == "---" || trimmed == "..." {
			return i
		}
	}
	return -1
}

func markdownIndentedCode(line string) bool {
	spaces := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			spaces++
			if spaces >= 4 {
				return true
			}
		case '\t':
			return true
		default:
			return false
		}
	}
	return false
}

func markdownFence(line string, activeChar byte, activeLength int) (byte, int, bool) {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	if spaces > 3 || spaces >= len(line) {
		return 0, 0, false
	}
	marker := line[spaces]
	if marker != '`' && marker != '~' {
		return 0, 0, false
	}
	length := 0
	for spaces+length < len(line) && line[spaces+length] == marker {
		length++
	}
	if length < 3 {
		return 0, 0, false
	}
	if activeChar == 0 {
		return marker, length, false
	}
	if marker != activeChar || length < activeLength || strings.TrimSpace(line[spaces+length:]) != "" {
		return 0, 0, false
	}
	return marker, length, true
}

func maskMarkdownInlineCode(line string) string {
	masked := []byte(line)
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		run := 1
		for i+run < len(line) && line[i+run] == '`' {
			run++
		}
		closeStart := -1
		for j := i + run; j < len(line); {
			if line[j] != '`' {
				j++
				continue
			}
			closeRun := 1
			for j+closeRun < len(line) && line[j+closeRun] == '`' {
				closeRun++
			}
			if closeRun == run {
				closeStart = j
				break
			}
			j += closeRun
		}
		if closeStart < 0 {
			i += run
			continue
		}
		for j := i; j < closeStart+run; j++ {
			masked[j] = ' '
		}
		i = closeStart + run
	}
	return string(masked)
}

func setextHeadingLevel(trimmed string) (int, bool) {
	if len(trimmed) < 1 {
		return 0, false
	}
	marker := trimmed[0]
	if marker != '=' && marker != '-' {
		return 0, false
	}
	for i := 1; i < len(trimmed); i++ {
		if trimmed[i] != marker {
			return 0, false
		}
	}
	if marker == '=' {
		return 1, true
	}
	return 2, true
}

func markdownContainerOpener(line string) bool {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent >= len(line) {
		return false
	}

	rest := line[indent:]
	if rest[0] == '>' {
		return true
	}
	if rest[0] == '-' || rest[0] == '+' || rest[0] == '*' {
		return len(rest) == 1 || rest[1] == ' ' || rest[1] == '\t'
	}

	digits := 0
	for digits < len(rest) && digits < 9 && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits >= len(rest) || rest[digits] != '.' && rest[digits] != ')' {
		return false
	}
	markerEnd := digits + 1
	return markerEnd == len(rest) || rest[markerEnd] == ' ' || rest[markerEnd] == '\t'
}

func markdownThematicBreak(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	marker := byte(0)
	count := 0
	for i := 0; i < len(trimmed); i++ {
		char := trimmed[i]
		if char == ' ' || char == '\t' {
			continue
		}
		if char != '*' && char != '_' && char != '-' {
			return false
		}
		if marker == 0 {
			marker = char
		} else if char != marker {
			return false
		}
		count++
	}
	return count >= 3
}

func atxMarkdownHeading(line string) (int, string, bool) {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent >= len(line) || line[indent] != '#' {
		return 0, "", false
	}
	level := 0
	for indent+level < len(line) && line[indent+level] == '#' {
		level++
	}
	markerEnd := indent + level
	if level < 1 || level > 6 || markerEnd >= len(line) || (line[markerEnd] != ' ' && line[markerEnd] != '\t') {
		return 0, "", false
	}
	text := strings.TrimSpace(line[markerEnd:])
	closingStart := len(text)
	for closingStart > 0 && text[closingStart-1] == '#' {
		closingStart--
	}
	if closingStart < len(text) && closingStart > 0 && (text[closingStart-1] == ' ' || text[closingStart-1] == '\t') {
		text = strings.TrimSpace(text[:closingStart])
	}
	if text == "" {
		return 0, "", false
	}
	return level, text, true
}

func trailingMarkdownBlockID(line, masked string) (string, int, int, bool) {
	end := len(masked)
	for end > 0 && (masked[end-1] == ' ' || masked[end-1] == '\t') {
		end--
	}
	if end == 0 {
		return "", 0, 0, false
	}
	start := end
	for start > 0 && markdownBlockIDByte(masked[start-1]) {
		start--
	}
	if start == end || start == 0 || masked[start-1] != '^' {
		return "", 0, 0, false
	}
	caret := start - 1
	if caret > 0 && masked[caret-1] != ' ' && masked[caret-1] != '\t' {
		return "", 0, 0, false
	}
	// Masking code spans must only reject candidates inside code. It must not
	// expose an earlier token as a synthetic line-ending block target.
	if strings.TrimSpace(line[end:]) != "" {
		return "", 0, 0, false
	}
	return line[start:end], caret, end, true
}

func markdownBlockIDByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '-' || value == '_'
}
