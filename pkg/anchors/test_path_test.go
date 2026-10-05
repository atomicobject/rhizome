package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTestPathUsesOneCrossLanguageVocabulary(t *testing.T) {
	for _, path := range []string{
		"test_worker.py",
		"src/test_worker.py",
		"web/widget.test.ts",
		"web/widget.spec.ts",
		"web/widget_test.tsx",
		"web/widget_test.mts",
		"web/widget_test.cts",
		"web/widget_test.jsx",
		"web/widget_test.mjs",
		"web/widget_test.cjs",
		"php/worker_test.php",
		"__tests__/widget.ts",
		"src/__tests__/widget.ts",
		"tests/worker.cs",
		"src/tests/worker.cs",
		"testdata/fixture.go",
	} {
		require.True(t, IsTestPath(path), path)
	}
	for _, path := range []string{"src/worker.go", "src/contest/service.go", "docs/testing.md"} {
		require.False(t, IsTestPath(path), path)
	}
}
