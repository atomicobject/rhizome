package coderefs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig_Disabled(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(false, nil, nil)
	assert.Nil(t, cfg)
}

func TestNewConfig_EnabledWithDefaults(t *testing.T) {
	t.Parallel()

	cfg := NewConfig(true, nil, nil)
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, DefaultIncludes, cfg.Includes)
	assert.Equal(t, DefaultExcludes, cfg.Excludes)
}

func TestNewConfig_EnabledWithCustomIncludes(t *testing.T) {
	t.Parallel()

	includes := []string{"**/*.go", "**/*.py"}
	cfg := NewConfig(true, includes, nil)

	require.NotNil(t, cfg)
	assert.Equal(t, includes, cfg.Includes)
	assert.Equal(t, DefaultExcludes, cfg.Excludes) // excludes default
}

func TestNewConfig_EnabledWithCustomExcludes(t *testing.T) {
	t.Parallel()

	excludes := []string{"**/test/**"}
	cfg := NewConfig(true, nil, excludes)

	require.NotNil(t, cfg)
	assert.Equal(t, DefaultIncludes, cfg.Includes) // includes default
	assert.Equal(t, excludes, cfg.Excludes)
}

func TestNewConfig_EnabledWithBothCustom(t *testing.T) {
	t.Parallel()

	includes := []string{"**/*.go"}
	excludes := []string{"**/vendor/**"}
	cfg := NewConfig(true, includes, excludes)

	require.NotNil(t, cfg)
	assert.Equal(t, includes, cfg.Includes)
	assert.Equal(t, excludes, cfg.Excludes)
}

func TestDefaultIncludes_Coverage(t *testing.T) {
	t.Parallel()

	// Verify default includes contain expected file types
	expected := []string{
		"**/*.go", "**/*.ts", "**/*.tsx", "**/*.mts", "**/*.cts",
		"**/*.js", "**/*.jsx", "**/*.mjs", "**/*.cjs",
		"**/*.html", "**/*.htm", "**/*.xhtml", "**/*.astro", "**/*.vue", "**/*.svelte",
		"**/*.css", "**/*.pcss", "**/*.scss", "**/*.sass", "**/*.less", "**/*.styl",
		"**/*.py", "**/*.java", "**/*.cs", "**/*.c", "**/*.cpp",
		"**/*.h", "**/*.hpp", "**/*.rs", "**/*.rb", "**/*.sh",
		"**/*.php",
	}
	assert.Equal(t, expected, DefaultIncludes)
}

func TestDefaultExcludes_Coverage(t *testing.T) {
	t.Parallel()

	// Verify default excludes contain expected patterns
	expected := []string{
		"**/node_modules/**", "**/vendor/**", "**/dist/**",
		"**/build/**", "**/bin/**", "**/obj/**", "**/.vs/**", "**/.git/**",
	}
	assert.Equal(t, expected, DefaultExcludes)
}
