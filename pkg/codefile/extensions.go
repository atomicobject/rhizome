// Package codefile defines shared file-extension contracts for code discovery.
package codefile

import (
	"path/filepath"
	"strings"
)

var (
	typeScriptExtensions = []string{".ts", ".tsx", ".mts", ".cts"}
	javaScriptExtensions = []string{".js", ".jsx", ".mjs", ".cjs"}
	typeScriptSet        = extensionSet(typeScriptExtensions)
	javaScriptSet        = extensionSet(javaScriptExtensions)
)

// TypeScriptExtensions returns the supported TypeScript source extensions.
func TypeScriptExtensions() []string {
	return append([]string(nil), typeScriptExtensions...)
}

// JavaScriptExtensions returns the supported JavaScript source extensions.
func JavaScriptExtensions() []string {
	return append([]string(nil), javaScriptExtensions...)
}

// TypeScriptJavaScriptExtensions returns all supported TypeScript and JavaScript source extensions.
func TypeScriptJavaScriptExtensions() []string {
	out := make([]string, 0, len(typeScriptExtensions)+len(javaScriptExtensions))
	out = append(out, typeScriptExtensions...)
	out = append(out, javaScriptExtensions...)
	return out
}

// IsTypeScriptExtension reports whether ext is a supported TypeScript extension.
func IsTypeScriptExtension(ext string) bool {
	_, ok := typeScriptSet[normalizeExtension(ext)]
	return ok
}

// IsJavaScriptExtension reports whether ext is a supported JavaScript extension.
func IsJavaScriptExtension(ext string) bool {
	_, ok := javaScriptSet[normalizeExtension(ext)]
	return ok
}

// IsTypeScriptJavaScriptExtension reports whether ext belongs to either source family.
func IsTypeScriptJavaScriptExtension(ext string) bool {
	return IsTypeScriptExtension(ext) || IsJavaScriptExtension(ext)
}

// IsTypeScriptJavaScriptPath reports whether path has a supported source extension.
func IsTypeScriptJavaScriptPath(path string) bool {
	return IsTypeScriptJavaScriptExtension(filepath.Ext(path))
}

func normalizeExtension(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func extensionSet(exts []string) map[string]struct{} {
	out := make(map[string]struct{}, len(exts))
	for _, ext := range exts {
		out[ext] = struct{}{}
	}
	return out
}
