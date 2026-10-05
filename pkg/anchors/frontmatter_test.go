package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseNote_NoFrontmatter(t *testing.T) {
	note, err := ParseNote("docs/foo.md", "hello world")
	require.NoError(t, err)
	require.Equal(t, "docs/foo.md", note.Path)
	require.Equal(t, "foo", note.Title)
	require.Empty(t, note.DefinedAnchors)
}

func TestParseNote_TitleFromWindowsPath(t *testing.T) {
	note, err := ParseNote(`C:\vault\docs\foo.md`, "hello world")
	require.NoError(t, err)
	require.Equal(t, "foo", note.Title)
}

func TestParseNote_WithAnchorsAndRefs(t *testing.T) {
	content := `---
title: "Persistence rules"
code-anchors:
  java:
    - decorator: com.acme.docs.DomainDoc
      args:
        tag: Billing
---

Body goes here
`
	note, err := ParseNote("docs/persistence.md", content)
	require.NoError(t, err)
	require.Equal(t, "docs/persistence.md", note.Path)
	require.Equal(t, "Persistence rules", note.Title)
	require.Len(t, note.DefinedAnchors, 1)
	anchor := note.DefinedAnchors[0]
	require.Equal(t, "DomainDoc", anchor.Label)
	require.Equal(t, AnchorAnnotation, anchor.Kind)
	require.NotNil(t, anchor.Ann)
	require.Equal(t, SymbolRef{Lang: LangJava, Pkg: "com.acme.docs", Name: "DomainDoc"}, anchor.Ann.Symbol)
	require.Equal(t, map[string]string{"tag": "Billing"}, anchor.Ann.ArgFilters)
}

func TestParseNote_CodeAnchorsDir(t *testing.T) {
	content := `---
title: "Module docs"
code-anchors:
  python:
    - dir: src/todoapp/services
---
`
	note, err := ParseNote("docs/module.md", content)
	require.NoError(t, err)
	require.Equal(t, "Module docs", note.Title)
	require.Len(t, note.DefinedAnchors, 1)
	a := note.DefinedAnchors[0]
	require.Equal(t, AnchorGlob, a.Kind)
	require.Equal(t, LangPy, a.Lang)
	require.Equal(t, "src/todoapp/services", a.Label)
	require.Equal(t, []string{"src/todoapp/services"}, a.Globs)
}

func TestParseNote_CodeAnchorsGlobAndGlobs(t *testing.T) {
	content := `---
title: "Glob docs"
code-anchors:
  python:
    - label: "py-glob"
      glob: src/**/*.py
    - label: "multi"
      globs:
        - src/**/services/**
        - internal/**/handlers/*.go
---
`
	note, err := ParseNote("docs/glob.md", content)
	require.NoError(t, err)
	require.Equal(t, "Glob docs", note.Title)
	require.Len(t, note.DefinedAnchors, 2)

	require.Equal(t, AnchorGlob, note.DefinedAnchors[0].Kind)
	require.Equal(t, "py-glob", note.DefinedAnchors[0].Label)
	require.Equal(t, []string{"src/**/*.py"}, note.DefinedAnchors[0].Globs)

	require.Equal(t, AnchorGlob, note.DefinedAnchors[1].Kind)
	require.Equal(t, "multi", note.DefinedAnchors[1].Label)
	require.Equal(t, []string{"src/**/services/**", "internal/**/handlers/*.go"}, note.DefinedAnchors[1].Globs)
}

func TestParseNote_CodeAnchorsScalarShorthand(t *testing.T) {
	content := `---
title: "Scalar anchors"
code-anchors:
  go:
    - example.com/mod/pkg/embeddings.HashText
    - calls:example.com/mod/pkg/embeddings.HashText
    - baseClass:example.com/mod/pkg/embeddings.Service
  python:
    - decorator:decorator
    - glob:src/**/*.py
    - dir:src/services
---`
	note, err := ParseNote("docs/test.md", content)
	require.NoError(t, err)
	require.Equal(t, "Scalar anchors", note.Title)
	require.Len(t, note.DefinedAnchors, 6)

	funcCount := 0
	baseClassSeen := false
	decoratorSeen := false
	globSeen := false
	dirSeen := false

	for _, anchor := range note.DefinedAnchors {
		switch anchor.Kind {
		case AnchorFunc:
			if anchor.BaseSym != nil && *anchor.BaseSym == (SymbolRef{Lang: LangGo, Pkg: "example.com/mod/pkg/embeddings", Name: "HashText"}) {
				funcCount++
			}
		case AnchorBaseClass:
			if anchor.BaseSym != nil && *anchor.BaseSym == (SymbolRef{Lang: LangGo, Pkg: "example.com/mod/pkg/embeddings", Name: "Service"}) {
				baseClassSeen = true
			}
		case AnchorAnnotation:
			if anchor.Ann != nil && anchor.Ann.Symbol.Lang == LangPy && anchor.Ann.Symbol.Name == "decorator" {
				decoratorSeen = true
			}
		case AnchorGlob:
			if len(anchor.Globs) == 1 && anchor.Globs[0] == "src/**/*.py" {
				globSeen = true
			}
			if len(anchor.Globs) == 1 && anchor.Globs[0] == "src/services" {
				dirSeen = true
			}
		}
	}

	require.Equal(t, 2, funcCount, "expected symbol + calls anchors for HashText")
	require.True(t, baseClassSeen, "expected baseClass anchor for Service")
	require.True(t, decoratorSeen, "expected decorator anchor for python decorator")
	require.True(t, globSeen, "expected glob anchor for src/**/*.py")
	require.True(t, dirSeen, "expected dir anchor for src/services")
}

func TestParseNote_RejectsUnqualifiedSymbolAndCalls(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "symbol without pkg",
			content: `---
code-anchors:
  go:
    - label: foo
      symbol: Foo
---`,
		},
		{
			name: "calls without pkg",
			content: `---
code-anchors:
  py:
    - label: foo
      calls: Foo
---`,
		},
		{
			name: "scalar shorthand without pkg",
			content: `---
code-anchors:
  go:
    - Foo
---`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseNote("docs/test.md", tt.content)
			require.Error(t, err)
		})
	}
}

func TestParseNote_AllowsUnqualifiedDecorator(t *testing.T) {
	content := `---
code-anchors:
  python:
    - label: billing
      decorator: decorator
---`
	note, err := ParseNote("docs/test.md", content)
	require.NoError(t, err)
	require.Len(t, note.DefinedAnchors, 1)
	require.Equal(t, AnchorAnnotation, note.DefinedAnchors[0].Kind)
	require.NotNil(t, note.DefinedAnchors[0].Ann)
	require.Equal(t, "", note.DefinedAnchors[0].Ann.Symbol.Pkg)
}

func TestParseNote_DefaultsKindAndTitle(t *testing.T) {
	content := `---
anchors:
  - define:
      label: "BaseUser"
      lang: "kotlin"
      baseSymbol:
        pkg: "com.acme.domain"
        name: "BaseUser"
---
`
	note, err := ParseNote("notes/base.md", content)
	require.NoError(t, err)
	require.Equal(t, "base", note.Title)
	require.Len(t, note.DefinedAnchors, 1)
	require.Equal(t, AnchorBaseClass, note.DefinedAnchors[0].Kind)
	require.Equal(t, SymbolRef{Lang: LangKotlin, Pkg: "com.acme.domain", Name: "BaseUser"}, *note.DefinedAnchors[0].BaseSym)
}

func TestParseNote_MalformedFrontmatter(t *testing.T) {
	// Test that malformed YAML frontmatter degrades gracefully instead of failing.
	// These are real-world examples from Obsidian vaults that previously caused errors.
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "mapping values not allowed in context",
			content: `---
title: Test: Something
---
Body content here
`,
		},
		{
			name: "could not find expected colon",
			content: `---
<% tp.date.now("YYYY-MM-DD") %>
---
Template content
`,
		},
		{
			name: "did not find expected node content",
			content: `---
date:
topic:
---
Empty values with trailing content issue
`,
		},
		{
			name: "invalid yaml syntax",
			content: `---
tags: [unclosed
---
Body
`,
		},
		{
			name: "templater syntax",
			content: `---
date: <% tp.file.creation_date("YYYY-MM-DD") %>
week: <% tp.date.now("YYYY-[W]WW") %>
---
Weekly note content
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note, err := ParseNote("notes/test.md", tt.content)
			require.NoError(t, err, "malformed frontmatter should not cause an error")
			require.Equal(t, "notes/test.md", note.Path)
			require.Equal(t, "test", note.Title) // title derived from path since frontmatter failed
			require.Empty(t, note.DefinedAnchors, "no anchors should be extracted from malformed frontmatter")
		})
	}
}
