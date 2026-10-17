package paths

import (
	"strings"
	"testing"
)

func FuzzNormalizeIsIdempotent(f *testing.F) {
	for _, seed := range []string{"", "../../escape.md", "././a//b", "a/../b", "C:\\Users\\test", "/a/b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		canonical := Normalize(input)
		if Normalize(canonical.String()) != canonical {
			t.Fatal("normalization changed a canonical path")
		}
		strict, err := CleanRelPath(input)
		if err != nil {
			return
		}
		again, err := CleanRelPath(strict.String())
		if err != nil || strict != again || strings.HasPrefix(strict.String(), "/") || strict == ".." || strings.HasPrefix(strict.String(), "../") {
			t.Fatal("invalid strict relative path")
		}
	})
}
