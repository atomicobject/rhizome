//go:build !cgo

package codeanchor

import "github.com/atomicobject/rhizome/pkg/paths"

// PHPIndexer is a stub when cgo/tree-sitter is unavailable.
type PHPIndexer struct{}

// NewPHPIndexer returns nil when PHP indexing is unavailable.
func NewPHPIndexer() *PHPIndexer { return nil }

// NewPHPIndexerWithRootsAndLimits returns nil when cgo is disabled.
func NewPHPIndexerWithRootsAndLimits(_ []string, _ TreeSitterLimits) *PHPIndexer { return nil }

// Lang identifies the language.
func (*PHPIndexer) Lang() Lang { return LangPhp }

// IndexFile returns an error when cgo is disabled.
func (*PHPIndexer) IndexFile(_ []byte, _ paths.CodePathRef) (FileSummary, error) {
	return FileSummary{}, ErrUnsupportedLanguage
}
