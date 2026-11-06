package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLangForPath(t *testing.T) {
	for _, tc := range []struct {
		path string
		lang Lang
	}{
		{"src/main.py", LangPy},
		{"src/main.go", LangGo},
		{"src/main.cs", LangCs},
		{"src/main.php", LangPhp},
		{"src/main.ts", LangTS},
		{"src/main.tsx", LangTS},
		{"src/main.mts", LangTS},
		{"src/main.cts", LangTS},
		{"src/main.js", LangTS},
		{"src/main.jsx", LangTS},
		{"src/main.mjs", LangTS},
		{"src/main.cjs", LangTS},
		{"src/MAIN.PY", LangPy},
		{"src/MAIN.MJS", LangTS},
		{"src/main.html", ""},
		{"src/main.css", ""},
		{"src/main.md", ""},
		{"src/Makefile", ""},
		{"", ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			lang, ok := LangForPath(tc.path)
			require.Equal(t, tc.lang, lang)
			require.Equal(t, tc.lang != "", ok)
		})
	}
}
