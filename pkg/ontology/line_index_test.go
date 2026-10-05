package ontology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLineIndex_LineAt(t *testing.T) {
	const lf = "a\nbb\n\nccc"     // lines: "a"(0-1) "bb"(2-4) ""(5) "ccc"(6-8), len 9
	const crlf = "a\r\nbb\r\nccc" // lines: "a"(0-2) "bb"(3-6) "ccc"(7-9), len 10
	const trailing = "a\nb\n"     // len 4, ends on newline

	cases := []struct {
		name    string
		content string
		offset  int
		want    int
	}{
		{"empty content offset 0", "", 0, 1},
		{"empty content past end", "", 5, 1},
		{"negative offset", lf, -1, 0},
		{"offset 0", lf, 0, 1},
		{"first newline byte stays on its line", lf, 1, 1},
		{"first byte after newline", lf, 2, 2},
		{"second newline byte", lf, 4, 2},
		{"blank line newline byte", lf, 5, 3},
		{"first byte of last line", lf, 6, 4},
		{"final byte", lf, 8, 4},
		{"offset == len", lf, 9, 4},
		{"offset past end clamps", lf, 100, 4},
		{"crlf: CR byte", crlf, 1, 1},
		{"crlf: LF byte", crlf, 2, 1},
		{"crlf: after CRLF", crlf, 3, 2},
		{"crlf: second LF", crlf, 6, 2},
		{"crlf: last line", crlf, 7, 3},
		{"crlf: past end", crlf, 50, 3},
		{"trailing newline: final byte", trailing, 3, 2},
		{"trailing newline: offset == len counts trailing newline", trailing, 4, 3},
		{"trailing newline: past end", trailing, 9, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, newLineIndex(tc.content).lineAt(tc.offset))
		})
	}
}
