package markdownsyntax

import "testing"

func TestFrontmatterBoundariesPreserveBytes(t *testing.T) {
	for _, tc := range []struct {
		source, yaml, body string
		valid              bool
	}{
		{"---\ntitle: Build --- Release\n---\n# Body", "title: Build --- Release\n", "# Body", true},
		{"\ufeff---\r\ntitle: Test\r\n...\r\nBody", "title: Test\r\n", "Body", true},
		{"---\na: b\n---", "a: b\n", "", true},
		{"---\n---", "", "", true},
		{"\n---\na: b\n---", "", "", false},
		{"----\na: b\n---", "", "", false},
		{"---x\na: b\n---", "", "", false},
		{"---\na: b\n---x", "", "", false},
	} {
		raw, start, ok := Frontmatter(tc.source)
		if ok != tc.valid || raw != tc.yaml || ok && tc.source[start:] != tc.body {
			t.Fatalf("Frontmatter(%q) = %q, %d, %v", tc.source, raw, start, ok)
		}
	}
}

func FuzzFrontmatterBoundary(f *testing.F) {
	for _, seed := range []string{"", "---\na: b\n---\nbody", "\ufeff---\r\na: b\r\n...", "---x\n---"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		raw, body, ok := Frontmatter(source)
		start, end, spanBody, spanOK := FrontmatterSpan(source)
		if ok != spanOK || body != spanBody {
			t.Fatal("boundary APIs disagree")
		}
		if ok && (start < 0 || end < start || body < end || body > len(source) || source[start:end] != raw) {
			t.Fatal("invalid source span")
		}
	})
}
