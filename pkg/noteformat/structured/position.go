package structured

import (
	"bytes"
	"sort"
	"unicode/utf8"
)

type sourceIndex struct {
	data  []byte
	lines []int
}

func indexSource(data []byte, yamlBreaks bool) sourceIndex {
	start := 0
	if yamlBreaks && bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		start = 3
	}
	s := sourceIndex{data: data, lines: []int{start}}
	for i := start; i < len(data); i++ {
		if data[i] == '\r' {
			if i+1 < len(data) && data[i+1] == '\n' {
				i++
			}
			s.lines = append(s.lines, i+1)
		} else if data[i] == '\n' {
			s.lines = append(s.lines, i+1)
		} else if yamlBreaks && data[i] >= 0x80 {
			r, size := utf8.DecodeRune(data[i:])
			if r == '\u0085' || r == '\u2028' || r == '\u2029' {
				i += size - 1
				s.lines = append(s.lines, i+1)
			}
		}
	}
	return s
}
func (s sourceIndex) position(offset int) Position {
	if offset < 0 || offset > len(s.data) {
		return Position{}
	}
	line := sort.Search(len(s.lines), func(i int) bool { return s.lines[i] > offset }) - 1
	if line < 0 {
		line = 0
	}
	start := s.lines[line]
	if offset < start {
		offset = start
	}
	return Position{line + 1, utf8.RuneCount(s.data[start:offset]) + 1, offset}
}
func (s sourceIndex) at(line, column int) Position {
	if line < 1 || line > len(s.lines) || column < 1 {
		return Position{}
	}
	offset := s.lines[line-1]
	for n := 1; n < column && offset < len(s.data); n++ {
		_, size := utf8.DecodeRune(s.data[offset:])
		offset += size
	}
	return Position{line, column, offset}
}
