//go:build !cgo

package codeanchor

import "github.com/atomicobject/rhizome/pkg/paths"

// CSharpIndexer is a stub when cgo/tree-sitter is unavailable.
type CSharpIndexer struct{}

// NewCSharpIndexer returns nil when C# indexing is unavailable (cgo disabled).
func NewCSharpIndexer() *CSharpIndexer { return nil }

// NewCSharpIndexerWithRoot returns nil when cgo is disabled.
func NewCSharpIndexerWithRoot(_ string) *CSharpIndexer { return nil }

// NewCSharpIndexerWithRootAndLimits returns nil when cgo is disabled.
func NewCSharpIndexerWithRootAndLimits(_ string, _ TreeSitterLimits) *CSharpIndexer { return nil }

// Lang identifies the language.
func (*CSharpIndexer) Lang() Lang { return LangCs }

// IndexFile reports that C# indexing is unsupported without cgo.
func (*CSharpIndexer) IndexFile(_ []byte, _ paths.CodePathRef) (FileSummary, error) {
	return FileSummary{}, ErrUnsupportedLanguage
}
