package codepatterns

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/codefile"
)

var defaultExtensions = buildDefaultExtensions()

var nonScriptExtensions = []string{
	".html",
	".htm",
	".xhtml",
	".astro",
	".vue",
	".svelte",
	".css",
	".pcss",
	".scss",
	".sass",
	".less",
	".styl",
	".py",
	".java",
	".cs",
	".c",
	".cpp",
	".h",
	".hpp",
	".rs",
	".rb",
	".sh",
	".php",
}

var defaultGlobs = makeDefaultGlobs(defaultExtensions)

var defaultIgnoreGlobs = []string{
	"**/node_modules/**",
	"**/vendor/**",
	"**/dist/**",
	"**/build/**",
	"**/bin/**",
	"**/obj/**",
	"**/.vs/**",
	"**/.git/**",
}

// DefaultGoGlobs returns the default Go scan patterns.
func DefaultGoGlobs() []string {
	return []string{"**/*.go"}
}

// DefaultPythonGlobs returns the default Python scan patterns.
func DefaultPythonGlobs() []string {
	return []string{"**/*.py"}
}

// DefaultTypeScriptGlobs returns the default TS/JS scan patterns.
func DefaultTypeScriptGlobs() []string {
	return makeDefaultGlobs(codefile.TypeScriptJavaScriptExtensions())
}

// DefaultTypeScriptOnlyGlobs returns the default TypeScript-only scan patterns.
func DefaultTypeScriptOnlyGlobs() []string {
	return makeDefaultGlobs(codefile.TypeScriptExtensions())
}

// DefaultJavaScriptOnlyGlobs returns the default JavaScript-only scan patterns.
func DefaultJavaScriptOnlyGlobs() []string {
	return makeDefaultGlobs(codefile.JavaScriptExtensions())
}

func buildDefaultExtensions() []string {
	out := []string{".go"}
	out = append(out, codefile.TypeScriptJavaScriptExtensions()...)
	return append(out, nonScriptExtensions...)
}

// DefaultCSharpGlobs returns the default C# scan patterns.
func DefaultCSharpGlobs() []string {
	return []string{"**/*.cs"}
}

// DefaultPHPGlobs returns the default PHP scan patterns.
func DefaultPHPGlobs() []string {
	return []string{"**/*.php"}
}

// DefaultExtensions returns the default file extensions for code scanning.
func DefaultExtensions() []string {
	out := make([]string, len(defaultExtensions))
	copy(out, defaultExtensions)
	return out
}

// DefaultExtensionSet returns a lookup set for default code extensions.
func DefaultExtensionSet() map[string]struct{} {
	out := make(map[string]struct{}, len(defaultExtensions))
	for _, ext := range defaultExtensions {
		out[ext] = struct{}{}
	}
	return out
}

// DefaultScanGlobs returns the default glob patterns for code scanning.
//
// This is coderef-oriented coverage, not the structural code-anchor language
// set. Keep web/template/style extensions here when their comment syntax is
// supported by coderefs even if no tree-sitter indexer exists for them.
func DefaultScanGlobs() []string {
	out := make([]string, len(defaultGlobs))
	copy(out, defaultGlobs)
	return out
}

// DefaultIgnoreGlobs returns default exclusion patterns for code scanning.
func DefaultIgnoreGlobs() []string {
	out := make([]string, len(defaultIgnoreGlobs))
	copy(out, defaultIgnoreGlobs)
	return out
}

func makeDefaultGlobs(exts []string) []string {
	out := make([]string, 0, len(exts))
	for _, ext := range exts {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		out = append(out, "**/*"+ext)
	}
	return out
}
