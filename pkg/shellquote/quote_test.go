package shellquote

import "testing"

func TestQuotePreservesOneLiteralArgument(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"", "''"},
		{"docs/one.md", "docs/one.md"},
		{"--scope-note", "--scope-note"},
		{"A B.md", "'A B.md'"},
		{"Drew's.md", "'Drew'\"'\"'s.md'"},
		{"$(printf injected); x", "'$(printf injected); x'"},
		{"a\nb", "'a\nb'"},
	} {
		if got := Quote(test.input); got != test.want {
			t.Errorf("Quote(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
