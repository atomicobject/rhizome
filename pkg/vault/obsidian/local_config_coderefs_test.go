package obsidian

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeCodeRefPatterns_DefaultEnabledIncludesWebCommentFormats(t *testing.T) {
	t.Parallel()

	includes, excludes := NormalizeCodeRefPatterns(LocalConfig{
		Code: LocalCodeConfig{
			Enabled: true,
		},
	})

	assert.Contains(t, includes, "**/*.html")
	assert.Contains(t, includes, "**/*.astro")
	assert.Contains(t, includes, "**/*.css")
	assert.Contains(t, includes, "**/*.scss")
	assert.Contains(t, includes, "**/*.mts")
	assert.Contains(t, includes, "**/*.cts")
	assert.Contains(t, includes, "**/*.mjs")
	assert.Contains(t, includes, "**/*.cjs")
	assert.Contains(t, excludes, "**/node_modules/**")
}
