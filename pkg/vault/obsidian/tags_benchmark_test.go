package obsidian

import (
	"strings"
	"testing"
)

func BenchmarkExtractHashtagsLongProse(b *testing.B) {
	prose := strings.Repeat("A long paragraph with ordinary words and punctuation. ", 100)
	for _, tc := range []struct{ name, content string }{
		{"no_tags", strings.Repeat(prose+"\n", 100)},
		{"sparse_tags", strings.Repeat(prose+"\n", 100) + "#work/project\n"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(tc.content)))
			b.ReportAllocs()
			for b.Loop() {
				ExtractHashtags(tc.content)
			}
		})
	}
}
