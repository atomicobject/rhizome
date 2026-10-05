package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnchor_ScopeEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        Anchor
		b        Anchor
		expected bool
	}{
		{
			name:     "identical empty anchors",
			a:        Anchor{},
			b:        Anchor{},
			expected: true,
		},
		{
			name:     "different kind",
			a:        Anchor{Kind: AnchorFunc},
			b:        Anchor{Kind: AnchorBaseClass},
			expected: false,
		},
		{
			name:     "different lang",
			a:        Anchor{Lang: LangGo},
			b:        Anchor{Lang: LangPy},
			expected: false,
		},
		{
			name:     "different path prefix",
			a:        Anchor{PathPrefix: "pkg/foo"},
			b:        Anchor{PathPrefix: "pkg/bar"},
			expected: false,
		},
		{
			name:     "identical BaseSym",
			a:        Anchor{BaseSym: &SymbolRef{Lang: LangGo, Pkg: "pkg/foo", Name: "Bar"}},
			b:        Anchor{BaseSym: &SymbolRef{Lang: LangGo, Pkg: "pkg/foo", Name: "Bar"}},
			expected: true,
		},
		{
			name:     "one nil BaseSym",
			a:        Anchor{BaseSym: &SymbolRef{Lang: LangGo, Pkg: "pkg/foo", Name: "Bar"}},
			b:        Anchor{BaseSym: nil},
			expected: false,
		},
		{
			name:     "different BaseSym",
			a:        Anchor{BaseSym: &SymbolRef{Lang: LangGo, Pkg: "pkg/foo", Name: "Bar"}},
			b:        Anchor{BaseSym: &SymbolRef{Lang: LangGo, Pkg: "pkg/foo", Name: "Baz"}},
			expected: false,
		},
		{
			name: "identical annotation",
			a: Anchor{Ann: &AnnotationSelector{
				Symbol:     SymbolRef{Lang: LangPy, Pkg: "pkg", Name: "decorator"},
				ArgFilters: map[string]string{"key": "value"},
			}},
			b: Anchor{Ann: &AnnotationSelector{
				Symbol:     SymbolRef{Lang: LangPy, Pkg: "pkg", Name: "decorator"},
				ArgFilters: map[string]string{"key": "value"},
			}},
			expected: true,
		},
		{
			name: "different annotation args",
			a: Anchor{Ann: &AnnotationSelector{
				Symbol:     SymbolRef{Lang: LangPy, Pkg: "pkg", Name: "decorator"},
				ArgFilters: map[string]string{"key": "value1"},
			}},
			b: Anchor{Ann: &AnnotationSelector{
				Symbol:     SymbolRef{Lang: LangPy, Pkg: "pkg", Name: "decorator"},
				ArgFilters: map[string]string{"key": "value2"},
			}},
			expected: false,
		},
		{
			name:     "identical globs same order",
			a:        Anchor{Globs: []string{"*.go", "*.md"}},
			b:        Anchor{Globs: []string{"*.go", "*.md"}},
			expected: true,
		},
		{
			name:     "identical globs different order",
			a:        Anchor{Globs: []string{"*.go", "*.md"}},
			b:        Anchor{Globs: []string{"*.md", "*.go"}},
			expected: true, // Order doesn't matter for glob matching
		},
		{
			name:     "different globs",
			a:        Anchor{Globs: []string{"*.go", "*.md"}},
			b:        Anchor{Globs: []string{"*.go", "*.txt"}},
			expected: false,
		},
		{
			name:     "different glob count",
			a:        Anchor{Globs: []string{"*.go"}},
			b:        Anchor{Globs: []string{"*.go", "*.md"}},
			expected: false,
		},
		{
			name:     "empty vs nil globs",
			a:        Anchor{Globs: nil},
			b:        Anchor{Globs: []string{}},
			expected: true,
		},
		{
			name:     "ID and Label ignored",
			a:        Anchor{ID: 1, Label: "foo", Kind: AnchorFunc},
			b:        Anchor{ID: 2, Label: "bar", Kind: AnchorFunc},
			expected: true, // ID and Label are not compared
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.a.ScopeEqual(tt.b)
			assert.Equal(t, tt.expected, result)
			// Also test symmetry
			resultReverse := tt.b.ScopeEqual(tt.a)
			assert.Equal(t, tt.expected, resultReverse, "ScopeEqual should be symmetric")
		})
	}
}
