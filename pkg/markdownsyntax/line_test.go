package markdownsyntax

import "testing"

func TestUnorderedListItemContentStart(t *testing.T) {
	for _, tc := range []struct {
		line  string
		start int
		ok    bool
	}{
		{"- value", 2, true},
		{"* value", 2, true},
		{"+ value", 2, true},
		{"-\tvalue", 2, true},
		{"*\tvalue", 2, true},
		{"+\tvalue", 2, true},
		{"-  value", 2, true},
		{"- \tvalue", 2, true},
		{"- ", 2, true},
		{"-", 0, false},
		{"-value", 0, false},
		{"-\u00a0value", 0, false},
		{"1. value", 0, false},
		{"1) value", 0, false},
		{" - value", 0, false},
		{"\t- value", 0, false},
		{"", 0, false},
	} {
		t.Run(tc.line, func(t *testing.T) {
			start, ok := UnorderedListItemContentStart(tc.line)
			if start != tc.start || ok != tc.ok {
				t.Fatalf("UnorderedListItemContentStart(%q) = (%d, %t), want (%d, %t)", tc.line, start, ok, tc.start, tc.ok)
			}
		})
	}
}
