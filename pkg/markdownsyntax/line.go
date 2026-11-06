package markdownsyntax

import "strings"

// FenceState tracks template fences: at most three leading spaces and matching
// delimiter runs. Unlike code-link protection, trailing delimiter text is allowed.
type FenceState struct {
	char   byte
	length int
}

// ConsumeLine advances at a line start and reports whether the line is fenced.
func (f *FenceState) ConsumeLine(s string, start int) bool {
	if ok, char, length, _ := FenceLine(s, start); ok {
		if f.length == 0 {
			f.char, f.length = char, length
		} else if char == f.char && length >= f.length {
			f.char, f.length = 0, 0
		}
		return true
	}
	return f.length != 0
}

func NextLineStart(s string, start int) int {
	if start >= len(s) {
		return len(s)
	}
	if idx := strings.IndexByte(s[start:], '\n'); idx >= 0 {
		return start + idx + 1
	}
	return len(s)
}

// FenceLine detects CommonMark-style fenced code block delimiters.
// It assumes start is at the beginning of a line.
//
// Returns ok plus the fence char/length, and next which is the index of the
// start of the next line.
func FenceLine(s string, start int) (ok bool, fenceChar byte, fenceLen int, next int) {
	i := start
	spaces := 0
	for i < len(s) && spaces < 3 && s[i] == ' ' {
		i++
		spaces++
	}
	if i >= len(s) {
		return false, 0, 0, NextLineStart(s, start)
	}
	c := s[i]
	if c != '`' && c != '~' {
		return false, 0, 0, NextLineStart(s, start)
	}
	run := ByteRun(s, i, c)
	if run < 3 {
		return false, 0, 0, NextLineStart(s, start)
	}
	return true, c, run, NextLineStart(s, start)
}

// UnorderedListItemContentStart recognizes one unordered marker and its first
// ASCII space or tab. Callers own trimming and any additional whitespace.
func UnorderedListItemContentStart(trimmed string) (int, bool) {
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
