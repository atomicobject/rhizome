package markdownsyntax

import "strings"

// Frontmatter returns the YAML source and first body byte. Only an exact ---
// first line opens frontmatter; a UTF-8 BOM and CRLF are accepted. The closing
// line is exactly --- or ... and may be the final line of the file.
func Frontmatter(content string) (yaml string, bodyStart int, ok bool) {
	start, end, bodyStart, ok := FrontmatterSpan(content)
	if !ok {
		return "", 0, false
	}
	return content[start:end], bodyStart, true
}

// FrontmatterSpan returns YAML byte bounds, excluding both delimiter lines,
// and the byte immediately after the closing delimiter line.
func FrontmatterSpan(content string) (contentStart, contentEnd, bodyStart int, ok bool) {
	opening := 0
	if strings.HasPrefix(content, "\xef\xbb\xbf") {
		opening = 3
	}
	end, next := lineEnd(content, opening)
	if content[opening:end] != "---" || next == end {
		return 0, 0, 0, false
	}
	contentStart = next
	for start := next; start <= len(content); {
		end, next = lineEnd(content, start)
		line := content[start:end]
		if line == "---" || line == "..." {
			return contentStart, start, next, true
		}
		if next >= len(content) {
			break
		}
		start = next
	}
	return 0, 0, 0, false
}

func lineEnd(content string, start int) (end, next int) {
	relative := strings.IndexByte(content[start:], '\n')
	if relative < 0 {
		return len(content), len(content)
	}
	next = start + relative + 1
	end = next - 1
	if end > start && content[end-1] == '\r' {
		end--
	}
	return end, next
}
