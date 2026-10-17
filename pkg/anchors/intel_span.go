package codeanchor

import "strings"

// ExtractSpan returns the body slice for a symbol based on span metadata.
func ExtractSpan(content []byte, anchor IntelAnchor) string {
	if anchor.StartByte >= 0 && anchor.EndByte > anchor.StartByte && int(anchor.EndByte) <= len(content) {
		return string(content[anchor.StartByte:anchor.EndByte])
	}
	// fallback to line-based extraction if byte spans are missing
	if anchor.StartLine > 0 && anchor.EndLine >= anchor.StartLine {
		return extractLines(content, int(anchor.StartLine), int(anchor.EndLine))
	}
	return ""
}

func extractLines(content []byte, startLine, endLine int) string {
	if startLine <= 0 || endLine < startLine {
		return ""
	}
	lines := strings.SplitAfter(string(content), "\n")
	start := startLine - 1
	if start >= len(lines) {
		return ""
	}
	end := endLine
	if end > len(lines) {
		end = len(lines)
	}
	return strings.TrimSpace(strings.Join(lines[start:end], ""))
}
