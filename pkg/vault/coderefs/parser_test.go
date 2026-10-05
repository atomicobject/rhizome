package coderefs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"module.mts", "typescript"},
		{"common.cts", "typescript"},
		{"script.js", "javascript"},
		{"component.jsx", "javascript"},
		{"module.mjs", "javascript"},
		{"common.cjs", "javascript"},
		{"Main.java", "java"},
		{"lib.c", "c"},
		{"lib.h", "c"},
		{"app.cpp", "cpp"},
		{"app.hpp", "cpp"},
		{"app.cc", "cpp"},
		{"main.rs", "rust"},
		{"script.py", "python"},
		{"app.rb", "ruby"},
		{"deploy.sh", "shell"},
		{"setup.bash", "shell"},
		{"config.zsh", "shell"},
		{"index.html", "html"},
		{"partial.HTM", "html"},
		{"component.xhtml", "html"},
		{"page.astro", "astro"},
		{"App.vue", "vue"},
		{"Panel.svelte", "svelte"},
		{"site.css", "css"},
		{"tokens.pcss", "css"},
		{"theme.scss", "scss"},
		{"theme.sass", "sass"},
		{"theme.less", "less"},
		{"theme.styl", "styl"},
		{"unknown.xyz", ""},
		{"no_extension", ""},
		{"path/to/file.go", "go"},
		{"FILE.GO", "go"}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := DetectLanguage(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractCStyleComments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []TextBlock
	}{
		{
			name:   "single line comment",
			source: `// this is a comment`,
			want: []TextBlock{
				{Text: "// this is a comment", Line: 1, Offset: 0},
			},
		},
		{
			name: "multiple line comments",
			source: `// first comment
// second comment`,
			want: []TextBlock{
				{Text: "// first comment", Line: 1, Offset: 0},
				{Text: "// second comment", Line: 2, Offset: 17},
			},
		},
		{
			name:   "block comment single line",
			source: `/* block comment */`,
			want: []TextBlock{
				{Text: "/* block comment */", Line: 1, Offset: 0},
			},
		},
		{
			name: "block comment multi line",
			source: `/* line one
line two
line three */`,
			want: []TextBlock{
				{Text: "/* line one\nline two\nline three */", Line: 1, Offset: 0},
			},
		},
		{
			name: "mixed comments",
			source: `// line comment
/* block comment */
// another line`,
			want: []TextBlock{
				{Text: "// line comment", Line: 1, Offset: 0},
				{Text: "/* block comment */", Line: 2, Offset: 16},
				{Text: "// another line", Line: 3, Offset: 36},
			},
		},
		{
			name:   "ignore double-quoted strings",
			source: `var s = "// not a comment"`,
			want:   nil,
		},
		{
			name:   "ignore single-quoted chars",
			source: `var c = '/' // real comment`,
			want: []TextBlock{
				{Text: "// real comment", Line: 1, Offset: 12},
			},
		},
		{
			name:   "comment after code",
			source: `func main() { // inline comment`,
			want: []TextBlock{
				{Text: "// inline comment", Line: 1, Offset: 14},
			},
		},
		{
			name:   "escaped quote in string",
			source: `var s = "hello \"world\"" // comment`,
			want: []TextBlock{
				{Text: "// comment", Line: 1, Offset: 26},
			},
		},
		{
			name: "string with fake block comment",
			source: `var s = "/* not a comment */"
// real comment`,
			want: []TextBlock{
				{Text: "// real comment", Line: 2, Offset: 30},
			},
		},
		{
			name:   "no comments",
			source: `var x = 1 + 2`,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCommentBlocks("go", tt.source)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.Text, got[i].Text, "text mismatch at %d", i)
				assert.Equal(t, want.Line, got[i].Line, "line mismatch at %d", i)
				assert.Equal(t, want.Offset, got[i].Offset, "offset mismatch at %d", i)
			}
		})
	}
}

func TestExtractBlockOnlyComments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []TextBlock
	}{
		{
			name:   "block comment single line",
			source: `/* block comment */`,
			want: []TextBlock{
				{Text: "/* block comment */", Line: 1, Offset: 0},
			},
		},
		{
			name: "multiline block comment",
			source: `body {
/* docs [[Note]] */
color: red;
}`,
			want: []TextBlock{
				{Text: "/* docs [[Note]] */", Line: 2, Offset: 7},
			},
		},
		{
			name:   "ignore line-comment-like url",
			source: `background-image: url(https://example.com/bg.png);`,
			want:   nil,
		},
		{
			name:   "ignore block comment markers inside string",
			source: `content: "/* not a comment */";`,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBlockOnlyComments(tt.source)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.Text, got[i].Text, "text mismatch at %d", i)
				assert.Equal(t, want.Line, got[i].Line, "line mismatch at %d", i)
				assert.Equal(t, want.Offset, got[i].Offset, "offset mismatch at %d", i)
			}
		})
	}
}

func TestExtractHTMLComments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []TextBlock
	}{
		{
			name:   "single html comment",
			source: `<!-- docs [[Note]] -->`,
			want: []TextBlock{
				{Text: "<!-- docs [[Note]] -->", Line: 1, Offset: 0},
			},
		},
		{
			name: "multiline html comment",
			source: `<div>
<!-- docs
@notes/task-flow
-->
</div>`,
			want: []TextBlock{
				{Text: "<!-- docs\n@notes/task-flow\n-->", Line: 2, Offset: 6},
			},
		},
		{
			name:   "ignore comment opener inside quoted text",
			source: `<div data-example="<!-- [[Note]] -->"></div>`,
			want:   nil,
		},
		{
			name:   "ignore comment opener inside script string",
			source: `<script>const tpl = "<!-- [[Note]] -->";</script>`,
			want:   nil,
		},
		{
			name:   "no comments",
			source: `<div>plain markup</div>`,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHTMLComments(tt.source)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.Text, got[i].Text, "text mismatch at %d", i)
				assert.Equal(t, want.Line, got[i].Line, "line mismatch at %d", i)
				assert.Equal(t, want.Offset, got[i].Offset, "offset mismatch at %d", i)
			}
		})
	}
}

func TestExtractPythonComments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []TextBlock
	}{
		{
			name:   "hash comment",
			source: `# this is a comment`,
			want: []TextBlock{
				{Text: "# this is a comment", Line: 1, Offset: 0},
			},
		},
		{
			name: "multiple hash comments",
			source: `# first
# second`,
			want: []TextBlock{
				{Text: "# first", Line: 1, Offset: 0},
				{Text: "# second", Line: 2, Offset: 8},
			},
		},
		{
			name:   "triple double-quoted docstring",
			source: `"""This is a docstring"""`,
			want: []TextBlock{
				{Text: `"""This is a docstring"""`, Line: 1, Offset: 0},
			},
		},
		{
			name:   "triple single-quoted docstring",
			source: `'''Another docstring'''`,
			want: []TextBlock{
				{Text: `'''Another docstring'''`, Line: 1, Offset: 0},
			},
		},
		{
			name: "multiline docstring",
			source: `"""
Line one
Line two
"""`,
			want: []TextBlock{
				{Text: "\"\"\"\nLine one\nLine two\n\"\"\"", Line: 1, Offset: 0},
			},
		},
		{
			name:   "ignore regular double-quoted string",
			source: `x = "not a docstring # or comment"`,
			want:   nil,
		},
		{
			name:   "ignore regular single-quoted string",
			source: `x = 'not a docstring # or comment'`,
			want:   nil,
		},
		{
			name: "docstring and comment",
			source: `"""docstring"""
# comment`,
			want: []TextBlock{
				{Text: `"""docstring"""`, Line: 1, Offset: 0},
				{Text: "# comment", Line: 2, Offset: 16},
			},
		},
		{
			name:   "hash inside string should not match",
			source: `url = "https://example.com#anchor"`,
			want:   nil,
		},
		{
			name:   "comment after code",
			source: `x = 1  # inline comment`,
			want: []TextBlock{
				{Text: "# inline comment", Line: 1, Offset: 7},
			},
		},
		{
			name: "function with docstring",
			source: `def foo():
    """
    Function docstring with [[Note]] reference
    """
    pass`,
			want: []TextBlock{
				{Text: "\"\"\"\n    Function docstring with [[Note]] reference\n    \"\"\"", Line: 2, Offset: 15},
			},
		},
		{
			name:   "empty source",
			source: ``,
			want:   nil,
		},
		{
			name:   "string followed by comment",
			source: `print("hello")  # greeting`,
			want: []TextBlock{
				{Text: "# greeting", Line: 1, Offset: 16},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPythonComments(tt.source)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.Text, got[i].Text, "text mismatch at %d", i)
				assert.Equal(t, want.Line, got[i].Line, "line mismatch at %d", i)
				assert.Equal(t, want.Offset, got[i].Offset, "offset mismatch at %d", i)
			}
		})
	}
}

func TestExtractScriptStyleComments(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []TextBlock
	}{
		{
			name:   "hash comment",
			source: `# this is a comment`,
			want: []TextBlock{
				{Text: "# this is a comment", Line: 1, Offset: 0},
			},
		},
		{
			name: "multiple comments",
			source: `# first
# second`,
			want: []TextBlock{
				{Text: "# first", Line: 1, Offset: 0},
				{Text: "# second", Line: 2, Offset: 8},
			},
		},
		{
			name:   "ignore hash in double-quoted string",
			source: `echo "hello # not a comment"`,
			want:   nil,
		},
		{
			name:   "ignore hash in single-quoted string",
			source: `echo 'hello # not a comment'`,
			want:   nil,
		},
		{
			name:   "comment after command",
			source: `echo "hello"  # this is a comment`,
			want: []TextBlock{
				{Text: "# this is a comment", Line: 1, Offset: 14},
			},
		},
		{
			name: "shebang line",
			source: `#!/bin/bash
# Script description`,
			want: []TextBlock{
				{Text: "#!/bin/bash", Line: 1, Offset: 0},
				{Text: "# Script description", Line: 2, Offset: 12},
			},
		},
		{
			name:   "escaped quote in string",
			source: `echo "he said \"hi\"" # comment`,
			want: []TextBlock{
				{Text: "# comment", Line: 1, Offset: 22},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractScriptStyleComments(tt.source)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.Text, got[i].Text, "text mismatch at %d", i)
				assert.Equal(t, want.Line, got[i].Line, "line mismatch at %d", i)
				assert.Equal(t, want.Offset, got[i].Offset, "offset mismatch at %d", i)
			}
		})
	}
}

func TestExtractCommentBlocks_LanguageRouting(t *testing.T) {
	source := `# Python comment
"""docstring"""`

	// Python should use extractPythonComments
	pyBlocks := ExtractCommentBlocks("python", source)
	require.Len(t, pyBlocks, 2)
	assert.Equal(t, "# Python comment", pyBlocks[0].Text)
	assert.Equal(t, `"""docstring"""`, pyBlocks[1].Text)

	// Shell should use extractScriptStyleComments (no docstrings)
	shBlocks := ExtractCommentBlocks("shell", source)
	require.Len(t, shBlocks, 1)
	assert.Equal(t, "# Python comment", shBlocks[0].Text)

	cssBlocks := ExtractCommentBlocks("css", `body { /* css comment */ }`)
	require.Len(t, cssBlocks, 1)
	assert.Equal(t, "/* css comment */", cssBlocks[0].Text)

	htmlBlocks := ExtractCommentBlocks("astro", `<!-- html comment -->`)
	require.Len(t, htmlBlocks, 1)
	assert.Equal(t, "<!-- html comment -->", htmlBlocks[0].Text)

	// Unknown falls back to C-style (won't match #)
	unknownBlocks := ExtractCommentBlocks("unknown", "// C comment")
	require.Len(t, unknownBlocks, 1)
	assert.Equal(t, "// C comment", unknownBlocks[0].Text)
}
