package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractRationale_Go(t *testing.T) {
	src := `package foo

// NOTE: this is important
func Foo() {}

// HACK: workaround for issue #42
// continuation of hack
func Bar() {}

// TODO: clean this up
func Baz() {}
`
	syms := []Symbol{
		{FQN: "foo.Foo", StartLine: 4, EndLine: 4},
		{FQN: "foo.Bar", StartLine: 7, EndLine: 9},
		{FQN: "foo.Baz", StartLine: 11, EndLine: 11},
	}
	got := ExtractRationale([]byte(src), "pkg/foo/foo.go", LangGo, syms)
	require.Len(t, got, 3)

	assert.Equal(t, RationaleNote, got[0].Kind)
	assert.Equal(t, int64(3), got[0].StartLine)
	assert.Equal(t, int64(3), got[0].EndLine)
	assert.Contains(t, got[0].Content, "this is important")
	// NOTE: comment is before Foo() on line 4, so it falls in Foo's range only if Foo starts at 3.
	// Actually line 4 is func Foo(){}, comment is on line 3 which is before Foo starts.
	// So symbol FQN should be "" (file-level).
	assert.Equal(t, "", got[0].SymbolFQN, "comment before func should be file-level")

	assert.Equal(t, RationaleHack, got[1].Kind)
	assert.Equal(t, int64(6), got[1].StartLine)
	assert.Equal(t, int64(7), got[1].EndLine)
	assert.Contains(t, got[1].Content, "workaround")
	assert.Contains(t, got[1].Content, "continuation")
	assert.Equal(t, "", got[1].SymbolFQN, "comment before Bar should be file-level")

	assert.Equal(t, RationaleTodo, got[2].Kind)
	assert.Equal(t, int64(10), got[2].StartLine)
}

func TestExtractRationale_MultiLine(t *testing.T) {
	src := `package foo

// NOTE: line1
// continuation
// more continuation
func Foo() {}
`
	got := ExtractRationale([]byte(src), "pkg/foo/foo.go", LangGo, nil)
	require.Len(t, got, 1)
	assert.Equal(t, RationaleNote, got[0].Kind)
	assert.Equal(t, int64(3), got[0].StartLine)
	assert.Equal(t, int64(5), got[0].EndLine)
	assert.Contains(t, got[0].Content, "line1")
	assert.Contains(t, got[0].Content, "continuation")
	assert.Contains(t, got[0].Content, "more continuation")
}

func TestExtractRationale_Python(t *testing.T) {
	src := `def foo():
    pass

# IMPORTANT: check this before calling
def bar():
    pass
`
	got := ExtractRationale([]byte(src), "scripts/foo.py", LangPy, nil)
	require.Len(t, got, 1)
	assert.Equal(t, RationaleImportant, got[0].Kind)
	assert.Equal(t, int64(4), got[0].StartLine)
}

func TestExtractRationale_BlockComment(t *testing.T) {
	src := `package foo

/* HACK: use mutex here */
func Foo() {}
`
	got := ExtractRationale([]byte(src), "pkg/foo/foo.go", LangGo, nil)
	require.Len(t, got, 1)
	assert.Equal(t, RationaleHack, got[0].Kind)
	assert.Contains(t, got[0].Content, "use mutex here")
}

func TestExtractRationale_FileLevel(t *testing.T) {
	src := `// NOTE: this is file-level
package foo

func Foo() {}
`
	syms := []Symbol{
		{FQN: "foo.Foo", StartLine: 4, EndLine: 4},
	}
	got := ExtractRationale([]byte(src), "pkg/foo/foo.go", LangGo, syms)
	require.Len(t, got, 1)
	assert.Equal(t, "", got[0].SymbolFQN, "line 1 is outside any symbol range")
}

func TestExtractRationale_SymbolAssociation(t *testing.T) {
	src := `package foo

func Foo() {
	// NOTE: important invariant inside function
	x := 1
	_ = x
}
`
	syms := []Symbol{
		{FQN: "pkg/foo.Foo", StartLine: 3, EndLine: 7},
	}
	got := ExtractRationale([]byte(src), "pkg/foo/foo.go", LangGo, syms)
	require.Len(t, got, 1)
	assert.Equal(t, "pkg/foo.Foo", got[0].SymbolFQN)
}

func TestExtractRationale_NoRationale(t *testing.T) {
	src := `package foo

// just a regular comment
func Foo() {
	// another regular comment
}
`
	got := ExtractRationale([]byte(src), "pkg/foo/foo.go", LangGo, nil)
	assert.Empty(t, got)
}

func TestExtractRationale_CaseInsensitive(t *testing.T) {
	src := `package foo

// note: lowercase note
// FIXME: uppercase fixme
// Hack: mixed case hack
`
	got := ExtractRationale([]byte(src), "pkg/foo/foo.go", LangGo, nil)
	require.Len(t, got, 3)
	assert.Equal(t, RationaleNote, got[0].Kind)
	assert.Equal(t, RationaleFixme, got[1].Kind)
	assert.Equal(t, RationaleHack, got[2].Kind)
}

func TestExtractRationale_ID(t *testing.T) {
	src := `// NOTE: test id generation
`
	got := ExtractRationale([]byte(src), "pkg/foo/foo.go", LangGo, nil)
	require.Len(t, got, 1)
	// ID should be 16 hex chars.
	assert.Len(t, got[0].ID, 16)
	// Fingerprint should be 64 hex chars (SHA256).
	assert.Len(t, got[0].Fingerprint, 64)
}

func TestExtractRationale_MultipleKindsNoOverlap(t *testing.T) {
	src := `package foo

// NOTE: note comment
// HACK: hack comment
`
	got := ExtractRationale([]byte(src), "pkg/foo/foo.go", LangGo, nil)
	// NOTE is followed by HACK — NOTE should stop at line 3, HACK starts at line 4.
	require.Len(t, got, 2)
	assert.Equal(t, RationaleNote, got[0].Kind)
	assert.Equal(t, int64(3), got[0].StartLine)
	assert.Equal(t, int64(3), got[0].EndLine)
	assert.Equal(t, RationaleHack, got[1].Kind)
	assert.Equal(t, int64(4), got[1].StartLine)
}
