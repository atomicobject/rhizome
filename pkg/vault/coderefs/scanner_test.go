package coderefs

import (
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTestCache creates a NotePathCache from a list of note paths.
func buildTestCache(notes []string) *obsidian.NotePathCache {
	return obsidian.BuildNotePathCache(notes)
}

func TestScanFile_Wikilinks(t *testing.T) {
	cache := buildTestCache([]string{
		"Notes/MyNote.md",
		"Projects/TodoApp.md",
		"README.md",
	})

	tests := []struct {
		name     string
		path     string
		content  string
		wantRefs []CodeRef
	}{
		{
			name: "basic wikilink in comment",
			path: "src/main.go",
			content: `package main
// See [[MyNote]] for details
func main() {}`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/main.go",
					Language:   "go",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindWikilink,
				},
			},
		},
		{
			name: "wikilink with path",
			path: "src/app.ts",
			content: `// Reference: [[Notes/MyNote]]
export default {}`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/app.ts",
					Language:   "typescript",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindWikilink,
				},
			},
		},
		{
			name:    "wikilink with anchor",
			path:    "src/test.js",
			content: `// Check [[MyNote#section]] for info`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/test.js",
					Language:   "javascript",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindWikilink,
				},
			},
		},
		{
			name:    "wikilink with alias",
			path:    "src/helper.go",
			content: `// Documentation: [[MyNote|My Custom Note]]`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/helper.go",
					Language:   "go",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindWikilink,
				},
			},
		},
		{
			name: "multiple wikilinks",
			path: "src/multi.go",
			content: `// See [[MyNote]] and [[TodoApp]]
/* Also check [[README]] */`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/multi.go",
					Language:   "go",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindWikilink,
				},
				{
					SourceFile: "src/multi.go",
					Language:   "go",
					Target:     "Projects/TodoApp.md",
					Kind:       RefKindWikilink,
				},
				{
					SourceFile: "src/multi.go",
					Language:   "go",
					Target:     "README.md",
					Kind:       RefKindWikilink,
				},
			},
		},
		{
			name:     "wikilink to non-existent note",
			path:     "src/missing.go",
			content:  `// See [[NonExistent]] for nothing`,
			wantRefs: nil,
		},
		{
			name:     "wikilink in string (not comment) - should not match",
			path:     "src/string.go",
			content:  `var s = "See [[MyNote]] for details"`,
			wantRefs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := ScanFile(tt.path, []byte(tt.content), cache)
			require.NoError(t, err)

			if tt.wantRefs == nil {
				assert.Empty(t, refs)
				return
			}

			require.Len(t, refs, len(tt.wantRefs))
			for i, want := range tt.wantRefs {
				assert.Equal(t, want.SourceFile, refs[i].SourceFile)
				assert.Equal(t, want.Language, refs[i].Language)
				assert.Equal(t, want.Target, refs[i].Target)
				assert.Equal(t, want.Kind, refs[i].Kind)
			}
		})
	}
}

func TestScanFile_PreservesPreciseLinkTargets(t *testing.T) {
	cache := buildTestCache([]string{"Notes/MyNote.md"})
	refs, err := ScanFile("src/main.go", []byte(`package main
// See [[MyNote#^story-a]] and [story](Notes/MyNote.md#^story-b)
func main() {}
`), cache)
	require.NoError(t, err)
	require.Len(t, refs, 2)
	require.Equal(t, "Notes/MyNote.md", refs[0].Target)
	require.Equal(t, "^story-a", refs[0].Fragment)
	require.Equal(t, "MyNote#^story-a", refs[0].RawTarget)
	require.Equal(t, "Notes/MyNote.md", refs[1].Target)
	require.Equal(t, "^story-b", refs[1].Fragment)
	require.Equal(t, "Notes/MyNote.md#^story-b", refs[1].RawTarget)
}

func TestScanFile_PreservesMixedCaseAuthoredNotePath(t *testing.T) {
	cache := buildTestCache([]string{"Decision.MD"})
	refs, err := ScanFile("src/main.go", []byte(`package main
// [[Decision]] [decision](Decision.MD) @Decision.MD
`), cache)
	require.NoError(t, err)
	require.Len(t, refs, 3)
	for _, ref := range refs {
		require.Equal(t, "Decision.MD", ref.Target)
		require.NotEqual(t, "Decision.MD.md", ref.Target)
	}
}

func TestScanFile_Mentions(t *testing.T) {
	cache := buildTestCache([]string{
		"Notes/MyNote.md",
		"Projects/TodoApp.md",
		"example.md",
		"example.com.md",
		"domain.md",
	})

	tests := []struct {
		name     string
		path     string
		content  string
		wantRefs []CodeRef
	}{
		{
			name: "basic mention",
			path: "src/main.py",
			content: `# See @MyNote for details
def main(): pass`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/main.py",
					Language:   "python",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindMention,
				},
			},
		},
		{
			name:    "mention with path",
			path:    "src/app.py",
			content: `# Reference: @Notes/MyNote`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/app.py",
					Language:   "python",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindMention,
				},
			},
		},
		{
			name:     "email should NOT match - user@domain",
			path:     "src/email.py",
			content:  `# Contact user@domain for help`,
			wantRefs: nil, // 'domain' note exists but email pattern blocks it
		},
		{
			name:     "email should NOT match - test@example.com",
			path:     "src/email2.py",
			content:  `# Send to test@example.com`,
			wantRefs: nil, // 'example' note exists but email pattern blocks it
		},
		{
			name:     "standalone dotted mention resolves",
			path:     "src/dotted.py",
			content:  `# See @example.com`,
			wantRefs: []CodeRef{{SourceFile: "src/dotted.py", Language: "python", Target: "example.com.md", Kind: RefKindMention}},
		},
		{
			name: "mention at start of comment",
			path: "src/start.sh",
			content: `# @MyNote describes this
echo "hello"`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/start.sh",
					Language:   "shell",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindMention,
				},
			},
		},
		{
			name:    "mention after punctuation",
			path:    "src/punct.rb",
			content: `# Check (@MyNote) for info`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/punct.rb",
					Language:   "ruby",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindMention,
				},
			},
		},
		{
			name:     "mention to non-existent note",
			path:     "src/missing.py",
			content:  `# See @NonExistent`,
			wantRefs: nil,
		},
		{
			name:    "mixed wikilinks and mentions",
			path:    "src/mixed.go",
			content: `// See [[MyNote]] and @TodoApp for context`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/mixed.go",
					Language:   "go",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindWikilink,
				},
				{
					SourceFile: "src/mixed.go",
					Language:   "go",
					Target:     "Projects/TodoApp.md",
					Kind:       RefKindMention,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := ScanFile(tt.path, []byte(tt.content), cache)
			require.NoError(t, err)

			if tt.wantRefs == nil {
				assert.Empty(t, refs)
				return
			}

			require.Len(t, refs, len(tt.wantRefs))
			for i, want := range tt.wantRefs {
				assert.Equal(t, want.SourceFile, refs[i].SourceFile)
				assert.Equal(t, want.Language, refs[i].Language)
				assert.Equal(t, want.Target, refs[i].Target)
				assert.Equal(t, want.Kind, refs[i].Kind)
			}
		})
	}
}

func TestScanFile_SkipBinaryFiles(t *testing.T) {
	cache := buildTestCache([]string{"MyNote.md"})

	// Content with null byte (binary indicator)
	binaryContent := []byte("// [[MyNote]]\x00binary data here")

	refs, err := ScanFile("src/data.bin", binaryContent, cache)
	require.NoError(t, err)
	assert.Nil(t, refs, "binary files should return nil refs")
}

func TestScanFile_SkipLargeFiles(t *testing.T) {
	cache := buildTestCache([]string{"MyNote.md"})

	// A resolvable ref at the limit proves the larger case reaches the size guard.
	const prefix = "// [[MyNote]]\n"
	atLimit := []byte(prefix + strings.Repeat(" ", MaxFileSizeBytes-len(prefix)))
	refs, err := ScanFile("src/limit.go", atLimit, cache)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	largeContent := append(atLimit, 'x')
	refs, err = ScanFile("src/large.go", largeContent, cache)
	require.NoError(t, err)
	assert.Nil(t, refs, "large files should return nil refs")
}

func TestScanFile_SnippetExtraction(t *testing.T) {
	cache := buildTestCache([]string{"MyNote.md"})

	content := `package main
// This is a comment with [[MyNote]] reference
func main() {}`

	refs, err := ScanFile("src/main.go", []byte(content), cache)
	require.NoError(t, err)
	require.Len(t, refs, 1)

	assert.Contains(t, refs[0].Snippet, "[[MyNote]]")
	assert.Contains(t, refs[0].Snippet, "comment")
}

func TestScanFile_SnippetTruncation(t *testing.T) {
	cache := buildTestCache([]string{"MyNote.md"})

	// Create a very long comment line
	longPrefix := strings.Repeat("x", 80)
	content := "// " + longPrefix + " [[MyNote]] " + strings.Repeat("y", 80)

	refs, err := ScanFile("src/long.go", []byte(content), cache)
	require.NoError(t, err)
	require.Len(t, refs, 1)

	// Snippet should be truncated to ~100 chars + "..."
	assert.True(t, len(refs[0].Snippet) <= 104, "snippet should be truncated")
	if len(refs[0].Snippet) > 100 {
		assert.True(t, strings.HasSuffix(refs[0].Snippet, "..."))
	}
}

func TestScanFile_LineNumbers(t *testing.T) {
	cache := buildTestCache([]string{"MyNote.md", "Other.md", "Last.md"})

	content := `package main
// First comment
// See [[MyNote]] here
func main() {
	// [[Other]] on line 5
}`

	refs, err := ScanFile("src/main.go", []byte(content), cache)
	require.NoError(t, err)
	require.Len(t, refs, 2)

	// Line numbers are approximate based on block start + newlines
	assert.Equal(t, 3, refs[0].Line)
	assert.Equal(t, 5, refs[1].Line)
	assert.Equal(t, "// See [[MyNote]] here", refs[0].Snippet)
	assert.Equal(t, "// [[Other]] on line 5", refs[1].Snippet)

	// One comment block exercises offsets within the block, including the
	// empty-target newline count used for mentions.
	content = `package main
/* [[MyNote]] first
 * middle [other](Other.md)
 * last @Last */`
	refs, err = ScanFile("src/main.go", []byte(content), cache)
	require.NoError(t, err)
	require.Len(t, refs, 3)
	require.Equal(t, CodeRef{SourceFile: "src/main.go", Language: "go", Target: "MyNote.md", RawTarget: "MyNote", Kind: RefKindWikilink, Line: 2, Snippet: "/* [[MyNote]] first"}, refs[0])
	require.Equal(t, CodeRef{SourceFile: "src/main.go", Language: "go", Target: "Other.md", RawTarget: "Other.md", Kind: RefKindMdLink, Line: 3, Snippet: "* middle [other](Other.md)"}, refs[1])
	require.Equal(t, CodeRef{SourceFile: "src/main.go", Language: "go", Target: "Last.md", RawTarget: "Last", Kind: RefKindMention, Line: 4, Snippet: "* last @Last */"}, refs[2])
}

func TestScanFile_MarkdownLinks(t *testing.T) {
	cache := buildTestCache([]string{
		"docs/hubs/MyHub.md",
		"docs/reference/guides/MyRef.md",
		"Notes/MyNote.md",
	})

	tests := []struct {
		name     string
		path     string
		content  string
		wantRefs []CodeRef
	}{
		{
			name: "basic markdown link in comment",
			path: "src/main.go",
			content: `package main
// See [My Hub](docs/hubs/MyHub.md) for details
func main() {}`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/main.go",
					Language:   "go",
					Target:     "docs/hubs/MyHub.md",
					Kind:       RefKindMdLink,
				},
			},
		},
		{
			name: "multiple markdown links",
			path: "src/multi.go",
			content: `// Docs:
// - [My Hub](docs/hubs/MyHub.md)
// - [My Ref](docs/reference/guides/MyRef.md)
func main() {}`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/multi.go",
					Language:   "go",
					Target:     "docs/hubs/MyHub.md",
					Kind:       RefKindMdLink,
				},
				{
					SourceFile: "src/multi.go",
					Language:   "go",
					Target:     "docs/reference/guides/MyRef.md",
					Kind:       RefKindMdLink,
				},
			},
		},
		{
			name: "markdown link in Python docstring",
			path: "src/main.py",
			content: `"""
Docs:
- [My Hub](docs/hubs/MyHub.md)
"""
def main(): pass`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/main.py",
					Language:   "python",
					Target:     "docs/hubs/MyHub.md",
					Kind:       RefKindMdLink,
				},
			},
		},
		{
			name:     "markdown link to non-existent note",
			path:     "src/missing.go",
			content:  `// See [Missing](docs/NonExistent.md)`,
			wantRefs: nil,
		},
		{
			name:     "external URL should not match",
			path:     "src/external.go",
			content:  `// See [Docs](https://example.com/docs)`,
			wantRefs: nil,
		},
		{
			name: "mixed markdown and wikilinks",
			path: "src/mixed.go",
			content: `// Docs:
// - [Hub](docs/hubs/MyHub.md)
// - [[MyNote]]`,
			wantRefs: []CodeRef{
				{
					SourceFile: "src/mixed.go",
					Language:   "go",
					Target:     "docs/hubs/MyHub.md",
					Kind:       RefKindMdLink,
				},
				{
					SourceFile: "src/mixed.go",
					Language:   "go",
					Target:     "Notes/MyNote.md",
					Kind:       RefKindWikilink,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := ScanFile(tt.path, []byte(tt.content), cache)
			require.NoError(t, err)

			if tt.wantRefs == nil {
				assert.Empty(t, refs)
				return
			}

			require.Len(t, refs, len(tt.wantRefs))
			for i, want := range tt.wantRefs {
				assert.Equal(t, want.SourceFile, refs[i].SourceFile)
				assert.Equal(t, want.Language, refs[i].Language)
				assert.Equal(t, want.Target, refs[i].Target)
				assert.Equal(t, want.Kind, refs[i].Kind)
			}
		})
	}
}

func TestScanFile_WebCommentFamilies(t *testing.T) {
	cache := buildTestCache([]string{
		"notes/task-flow.md",
		"notes/sync-strategy.md",
		"notes/product-brief.md",
		"notes/release-plan.md",
		"docs/reference/guides/MyRef.md",
	})

	tests := []struct {
		name     string
		path     string
		content  string
		wantRefs []CodeRef
	}{
		{
			name: "html comments support wikilinks and mentions",
			path: "web/index.html",
			content: `<!-- Docs: [[notes/task-flow]] and @notes/release-plan -->
<main></main>`,
			wantRefs: []CodeRef{
				{
					SourceFile: "web/index.html",
					Language:   "html",
					Target:     "notes/task-flow.md",
					Kind:       RefKindWikilink,
				},
				{
					SourceFile: "web/index.html",
					Language:   "html",
					Target:     "notes/release-plan.md",
					Kind:       RefKindMention,
				},
			},
		},
		{
			name: "css block comments support markdown links",
			path: "web/styles/site.css",
			content: `body {
  /* See [Ref](docs/reference/guides/MyRef.md) and [[notes/product-brief]] */
}`,
			wantRefs: []CodeRef{
				{
					SourceFile: "web/styles/site.css",
					Language:   "css",
					Target:     "notes/product-brief.md",
					Kind:       RefKindWikilink,
				},
				{
					SourceFile: "web/styles/site.css",
					Language:   "css",
					Target:     "docs/reference/guides/MyRef.md",
					Kind:       RefKindMdLink,
				},
			},
		},
		{
			name: "scss supports slash-line and block comments",
			path: "web/styles/theme.scss",
			content: `// [[notes/task-flow]]
.button {
  /* @notes/sync-strategy */
}`,
			wantRefs: []CodeRef{
				{
					SourceFile: "web/styles/theme.scss",
					Language:   "scss",
					Target:     "notes/task-flow.md",
					Kind:       RefKindWikilink,
				},
				{
					SourceFile: "web/styles/theme.scss",
					Language:   "scss",
					Target:     "notes/sync-strategy.md",
					Kind:       RefKindMention,
				},
			},
		},
		{
			name: "astro uses html comments only",
			path: "web/pages/dashboard.astro",
			content: `---
const docs = "ignore [[notes/task-flow]]";
---
<!-- [[notes/task-flow]] -->
<script>
// @notes/release-plan
</script>`,
			wantRefs: []CodeRef{
				{
					SourceFile: "web/pages/dashboard.astro",
					Language:   "astro",
					Target:     "notes/task-flow.md",
					Kind:       RefKindWikilink,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, err := ScanFile(tt.path, []byte(tt.content), cache)
			require.NoError(t, err)

			require.Len(t, refs, len(tt.wantRefs))
			for i, want := range tt.wantRefs {
				assert.Equal(t, want.SourceFile, refs[i].SourceFile)
				assert.Equal(t, want.Language, refs[i].Language)
				assert.Equal(t, want.Target, refs[i].Target)
				assert.Equal(t, want.Kind, refs[i].Kind)
			}
		})
	}
}
