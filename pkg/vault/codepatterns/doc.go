// Package codepatterns centralizes the default code-file globs used by coderef
// scanning and rewrite paths.
//
// These patterns are deliberately broader than tree-sitter code-anchor indexing:
// web/template/style files may contain comments with coderefs even when they do
// not have structural anchors. Keep defaults aligned with coderefs parser
// support and keep generated slices copy-on-return so callers can customize
// config without mutating process-global defaults.
package codepatterns
