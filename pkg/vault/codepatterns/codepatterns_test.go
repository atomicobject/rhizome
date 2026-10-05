package codepatterns

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultScanGlobs_IncludeWebCommentFormats(t *testing.T) {
	t.Parallel()

	assert.Contains(t, DefaultExtensions(), ".html")
	assert.Contains(t, DefaultExtensions(), ".astro")
	assert.Contains(t, DefaultExtensions(), ".css")
	assert.Contains(t, DefaultExtensions(), ".scss")

	globs := DefaultScanGlobs()
	assert.Contains(t, globs, "**/*.html")
	assert.Contains(t, globs, "**/*.astro")
	assert.Contains(t, globs, "**/*.css")
	assert.Contains(t, globs, "**/*.scss")
}

func TestTypeScriptJavaScriptGlobsIncludeNodeModuleFormats(t *testing.T) {
	t.Parallel()

	assert.ElementsMatch(t,
		[]string{"**/*.ts", "**/*.tsx", "**/*.mts", "**/*.cts", "**/*.js", "**/*.jsx", "**/*.mjs", "**/*.cjs"},
		DefaultTypeScriptGlobs(),
	)
	assert.ElementsMatch(t, []string{"**/*.ts", "**/*.tsx", "**/*.mts", "**/*.cts"}, DefaultTypeScriptOnlyGlobs())
	assert.ElementsMatch(t, []string{"**/*.js", "**/*.jsx", "**/*.mjs", "**/*.cjs"}, DefaultJavaScriptOnlyGlobs())
}
