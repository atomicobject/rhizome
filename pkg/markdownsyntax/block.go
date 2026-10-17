package markdownsyntax

import (
	"strings"
	"unicode"
)

func ValidBlockID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func TrailingBlockID(line string) (string, int, int, bool) {
	return trailingMarkdownBlockID(line, MaskProtected(line))
}

// FenceDelimiter recognizes a complete fence run and requires an equally long
// matching close with no trailing content. Its state is the authored run.
func FenceDelimiter(line, current string) (string, bool) {
	line = strings.TrimLeft(line, " \t")
	if line == "" || line[0] != '`' && line[0] != '~' {
		return current, false
	}
	run := ByteRun(line, 0, line[0])
	if run < 3 {
		return current, false
	}
	marker := line[:run]
	if current == "" {
		return marker, true
	}
	if line[0] == current[0] && run >= len(current) && strings.TrimSpace(line[run:]) == "" {
		return "", true
	}
	return current, false
}
