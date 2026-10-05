//go:build !cgo

package codeanchor

import "github.com/atomicobject/rhizome/pkg/paths"

// TSIndexer is a stub when cgo/tree-sitter is unavailable.
type TSIndexer struct{}

// NewTSIndexerWithRoot returns nil when cgo is disabled.
func NewTSIndexerWithRoot(_ string) *TSIndexer { return nil }

// NewTSIndexerWithRootAndTailIndex returns nil when cgo is disabled.
func NewTSIndexerWithRootAndTailIndex(_ string, _ *PathTailIndex) *TSIndexer { return nil }

// NewTSIndexerWithRootAndTailIndexAndLimits returns nil when cgo is disabled.
func NewTSIndexerWithRootAndTailIndexAndLimits(_ string, _ *PathTailIndex, _ TreeSitterLimits) *TSIndexer {
	return nil
}

// Lang identifies the language.
func (*TSIndexer) Lang() Lang { return LangTS }

// IndexFile reports that TS/JS indexing is unsupported without cgo.
func (*TSIndexer) IndexFile(_ []byte, _ paths.CodePathRef) (FileSummary, error) {
	return FileSummary{}, ErrUnsupportedLanguage
}

// TSIndexerAvailable reports whether TS indexing is supported in this build.
func TSIndexerAvailable() bool { return false }
