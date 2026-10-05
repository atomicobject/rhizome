package obsidian

import "strings"

type markdownDestination struct {
	targetStart int
	targetEnd   int
	bodyStart   int
	bodyEnd     int
	outerClose  int
}

func scanStructuredMarkdownLinks(content string, options MdLinkOptions, excluded []StructuredLinkSpan) []StructuredLink {
	var links []StructuredLink
	for i := 0; i < len(content); {
		next := strings.IndexByte(content[i:], '[')
		if next < 0 {
			break
		}
		idx := i + next
		if spanContains(excluded, idx) || isBackslashEscaped(content, idx) {
			i = idx + 1
			continue
		}
		if idx+1 < len(content) && content[idx+1] == '[' {
			i = idx + 2
			continue
		}

		embed := idx > 0 && content[idx-1] == '!'
		if embed && isBackslashEscaped(content, idx-1) {
			i = idx + 1
			continue
		}
		closeIdx, ok := markdownLabelClose(content, idx)
		if !ok {
			i = nextMarkdownLine(content, idx)
			continue
		}
		if closeIdx+1 >= len(content) || content[closeIdx+1] != '(' {
			i = idx + 1
			continue
		}
		destination, ok := parseMarkdownDestination(content, closeIdx+1)
		if !ok {
			i = closeIdx + 2
			continue
		}
		i = destination.outerClose + 1

		targetSpan := linkSpan(destination.targetStart, destination.targetEnd)
		target := targetSpan.Text(content)
		if target == "" || isExternalStructuredTarget(target) {
			continue
		}
		if embed && options.SkipEmbeds {
			continue
		}
		if options.SkipAnchors && strings.Contains(target, "#") {
			continue
		}

		rawStart := idx
		if embed {
			rawStart--
		}
		link := structuredLink(
			content,
			StructuredLinkMarkdown,
			embed,
			linkSpan(rawStart, destination.outerClose+1),
			targetSpan,
			linkSpan(idx+1, closeIdx),
		)
		link.markdownBody = content[destination.bodyStart:destination.bodyEnd]
		links = append(links, link)
	}
	return links
}

func isExternalStructuredTarget(target string) bool {
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "//") {
		return true
	}
	colon := strings.IndexByte(target, ':')
	if colon <= 0 || !isASCIILetter(target[0]) {
		return false
	}
	for i := 1; i < colon; i++ {
		value := target[i]
		if !isASCIILetter(value) && (value < '0' || value > '9') && value != '+' && value != '-' && value != '.' {
			return false
		}
	}
	return true
}

func isASCIILetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func nextMarkdownLine(content string, start int) int {
	if newline := strings.IndexByte(content[start:], '\n'); newline >= 0 {
		return start + newline + 1
	}
	return len(content)
}

func markdownLabelClose(content string, open int) (int, bool) {
	depth := 1
	for i := open + 1; i < len(content); i++ {
		if content[i] == '\\' && i+1 < len(content) {
			i++
			continue
		}
		switch content[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func parseMarkdownDestination(content string, openParen int) (markdownDestination, bool) {
	bodyStart := openParen + 1
	start := skipMarkdownSpace(content, bodyStart)
	if start >= len(content) {
		return markdownDestination{}, false
	}

	if content[start] == '<' {
		return parseAngleMarkdownDestination(content, bodyStart, start)
	}
	return parseBareMarkdownDestination(content, bodyStart, start)
}

func parseAngleMarkdownDestination(content string, bodyStart, angleStart int) (markdownDestination, bool) {
	for i := angleStart + 1; i < len(content); i++ {
		if content[i] == '\\' && i+1 < len(content) {
			i++
			continue
		}
		if content[i] == '\n' || content[i] == '<' {
			return markdownDestination{}, false
		}
		if content[i] != '>' {
			continue
		}
		outerClose, ok := markdownTitleAndClose(content, i+1)
		if !ok {
			return markdownDestination{}, false
		}
		return markdownDestination{
			targetStart: angleStart + 1,
			targetEnd:   i,
			bodyStart:   bodyStart,
			bodyEnd:     outerClose,
			outerClose:  outerClose,
		}, true
	}
	return markdownDestination{}, false
}

func parseBareMarkdownDestination(content string, bodyStart, start int) (markdownDestination, bool) {
	depth := 0
	for i := start; i < len(content); i++ {
		if content[i] == '\\' && i+1 < len(content) {
			i++
			continue
		}
		switch content[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return markdownDestination{
					targetStart: start,
					targetEnd:   i,
					bodyStart:   bodyStart,
					bodyEnd:     i,
					outerClose:  i,
				}, true
			}
			depth--
		default:
			if depth == 0 && isMarkdownSpace(content[i]) {
				outerClose, ok := markdownTitleAndClose(content, i)
				if !ok {
					return markdownDestination{}, false
				}
				return markdownDestination{
					targetStart: start,
					targetEnd:   i,
					bodyStart:   bodyStart,
					bodyEnd:     outerClose,
					outerClose:  outerClose,
				}, true
			}
		}
	}
	return markdownDestination{}, false
}

func markdownTitleAndClose(content string, start int) (int, bool) {
	i := skipMarkdownSpace(content, start)
	if i >= len(content) {
		return 0, false
	}
	if content[i] == ')' {
		return i, true
	}

	open := content[i]
	close := open
	if open == '(' {
		close = ')'
	} else if open != '\'' && open != '"' {
		return 0, false
	}
	for i++; i < len(content); i++ {
		if content[i] == '\\' && i+1 < len(content) {
			i++
			continue
		}
		if content[i] != close {
			continue
		}
		i = skipMarkdownSpace(content, i+1)
		if i < len(content) && content[i] == ')' {
			return i, true
		}
		return 0, false
	}
	return 0, false
}

func skipMarkdownSpace(content string, start int) int {
	for start < len(content) && isMarkdownSpace(content[start]) {
		start++
	}
	return start
}

func isMarkdownSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}

func isBackslashEscaped(content string, pos int) bool {
	count := 0
	for pos > 0 && content[pos-1] == '\\' {
		count++
		pos--
	}
	return count%2 == 1
}

func indexUnescapedToken(content string, start int, token string) int {
	for start < len(content) {
		relative := strings.Index(content[start:], token)
		if relative < 0 {
			return -1
		}
		found := start + relative
		if !isBackslashEscaped(content, found) {
			return found
		}
		start = found + len(token)
	}
	return -1
}
