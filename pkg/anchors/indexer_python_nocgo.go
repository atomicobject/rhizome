//go:build !cgo

package codeanchor

import "github.com/atomicobject/rhizome/pkg/paths"

// PythonIndexer is a stub when cgo/tree-sitter is unavailable.
type PythonIndexer struct{}

// NewPythonIndexer returns nil when Python indexing is unavailable.
func NewPythonIndexer() *PythonIndexer { return nil }

// NewPythonIndexerWithRoots returns nil when cgo is disabled.
func NewPythonIndexerWithRoots(_ []string) *PythonIndexer { return nil }

// NewPythonIndexerWithRootsAndLimits returns nil when cgo is disabled.
func NewPythonIndexerWithRootsAndLimits(_ []string, _ TreeSitterLimits) *PythonIndexer { return nil }

// Lang identifies the language.
func (*PythonIndexer) Lang() Lang { return LangPy }

// IndexFile reports that Python indexing is unsupported without cgo.
func (*PythonIndexer) IndexFile(_ []byte, _ paths.CodePathRef) (FileSummary, error) {
	return FileSummary{}, ErrUnsupportedLanguage
}

// PythonIndexerAvailable reports whether Python indexing is supported in this build.
func PythonIndexerAvailable() bool { return false }
