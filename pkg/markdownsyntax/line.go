package markdownsyntax

import "strings"

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
