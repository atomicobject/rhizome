package coderefs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteRefsInText_Wikilinks(t *testing.T) {
	tests := []struct {
		name    string
		content string
		oldPath string
		newPath string
		want    string
		count   int
	}{
		{
			name:    "basic wikilink",
			content: `// See [[MyNote]] for details`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `// See [[NewNote]] for details`,
			count:   1,
		},
		{
			name:    "wikilink with .md extension",
			content: `// See [[MyNote.md]] for details`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `// See [[NewNote.md]] for details`,
			count:   1,
		},
		{
			name:    "wikilink with anchor",
			content: `// See [[MyNote#section]] for details`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `// See [[NewNote#section]] for details`,
			count:   1,
		},
		{
			name:    "wikilink with alias",
			content: `// See [[MyNote|My Custom Name]] for details`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `// See [[NewNote|My Custom Name]] for details`,
			count:   1,
		},
		{
			name:    "wikilink with anchor and alias",
			content: `// See [[MyNote#section|Custom]] for details`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `// See [[NewNote#section|Custom]] for details`,
			count:   1,
		},
		{
			name:    "wikilink with full path",
			content: `// See [[Notes/MyNote]] for details`,
			oldPath: "Notes/MyNote.md",
			newPath: "Archive/MyNote.md",
			want:    `// See [[Archive/MyNote]] for details`,
			count:   1,
		},
		{
			name:    "wikilink basename only when path exists",
			content: `// See [[MyNote]] for details`,
			oldPath: "Notes/MyNote.md",
			newPath: "Archive/RenamedNote.md",
			want:    `// See [[RenamedNote]] for details`,
			count:   1,
		},
		{
			name:    "multiple wikilinks",
			content: `// See [[MyNote]] and [[MyNote#other]]`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `// See [[NewNote]] and [[NewNote#other]]`,
			count:   2,
		},
		{
			name:    "no match different note",
			content: `// See [[OtherNote]] for details`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `// See [[OtherNote]] for details`,
			count:   0,
		},
		{
			name:    "case insensitive basename",
			content: `// See [[mynote]] for details`,
			oldPath: "Notes/MyNote.md",
			newPath: "Notes/NewNote.md",
			want:    `// See [[NewNote]] for details`,
			count:   1,
		},
		{
			name:    "preserve path prefix in content",
			content: `// See [[sub/MyNote]] for details`,
			oldPath: "Notes/MyNote.md",
			newPath: "Notes/NewNote.md",
			// Should NOT replace - sub/MyNote is a different path
			want:  `// See [[sub/MyNote]] for details`,
			count: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := compileMapping(RefMapping{OldPath: tt.oldPath, NewPath: tt.newPath})
			got, count := rewriteRefsInText(tt.content, []compiledMapping{cm})
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.count, count)
		})
	}
}

func TestRewriteRefsInText_Mentions(t *testing.T) {
	tests := []struct {
		name    string
		content string
		oldPath string
		newPath string
		want    string
		count   int
	}{
		{
			name:    "basic mention",
			content: `# See @MyNote for details`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `# See @NewNote for details`,
			count:   1,
		},
		{
			name:    "mention with path",
			content: `# See @Notes/MyNote for details`,
			oldPath: "Notes/MyNote.md",
			newPath: "Archive/MyNote.md",
			want:    `# See @Archive/MyNote for details`,
			count:   1,
		},
		{
			name:    "mention at start",
			content: `@MyNote describes this`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `@NewNote describes this`,
			count:   1,
		},
		{
			name:    "mention after punctuation",
			content: `# Check (@MyNote) for info`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `# Check (@NewNote) for info`,
			count:   1,
		},
		{
			name:    "email should NOT match",
			content: `# Contact user@MyNote.com for help`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			// user@ precedes MyNote, so it shouldn't match
			want:  `# Contact user@MyNote.com for help`,
			count: 0,
		},
		{
			name:    "mention lookahead prevents partial",
			content: `# See @MyNoteExtra for details`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			// @MyNoteExtra should not match @MyNote (lookahead)
			want:  `# See @MyNoteExtra for details`,
			count: 0,
		},
		{
			name:    "mention followed by punctuation",
			content: `# See @MyNote, and more`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `# See @NewNote, and more`,
			count:   1,
		},
		{
			name:    "multiple mentions",
			content: `# @MyNote and @MyNote again`,
			oldPath: "MyNote.md",
			newPath: "NewNote.md",
			want:    `# @NewNote and @NewNote again`,
			count:   2,
		},
		{
			name:    "basename mention when path exists",
			content: `# See @MyNote for details`,
			oldPath: "Notes/MyNote.md",
			newPath: "Archive/RenamedNote.md",
			want:    `# See @RenamedNote for details`,
			count:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := compileMapping(RefMapping{OldPath: tt.oldPath, NewPath: tt.newPath})
			got, count := rewriteRefsInText(tt.content, []compiledMapping{cm})
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.count, count)
		})
	}
}

func TestRewriteCodeRefs_Integration(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create a code file
	codeDir := filepath.Join(tmpDir, "src")
	require.NoError(t, os.MkdirAll(codeDir, 0755))

	codeFile := filepath.Join(codeDir, "main.go")
	content := `package main
// See [[MyNote]] for details
// Also @MyNote has info
func main() {}`

	require.NoError(t, os.WriteFile(codeFile, []byte(content), 0644))

	// Config to scan .go files
	config := &Config{
		Enabled:  true,
		Includes: []string{"**/*.go"},
		Excludes: []string{},
	}

	result, err := RewriteCodeRefs(tmpDir, config, "MyNote.md", "NewNote.md")
	require.NoError(t, err)

	assert.Equal(t, 1, result.FilesUpdated)
	assert.Equal(t, 2, result.RefsUpdated) // wikilink + mention

	// Verify file was updated
	updated, err := os.ReadFile(codeFile)
	require.NoError(t, err)

	assert.Contains(t, string(updated), "[[NewNote]]")
	assert.Contains(t, string(updated), "@NewNote")
	assert.NotContains(t, string(updated), "[[MyNote]]")
	assert.NotContains(t, string(updated), "@MyNote")
}

func TestRewriteCodeRefs_WebCommentFormats(t *testing.T) {
	tmpDir := t.TempDir()

	htmlFile := filepath.Join(tmpDir, "index.html")
	cssFile := filepath.Join(tmpDir, "styles.css")

	require.NoError(t, os.WriteFile(htmlFile, []byte(`<!-- [[MyNote]] and @MyNote -->`), 0644))
	require.NoError(t, os.WriteFile(cssFile, []byte(`/* [Docs](MyNote.md) and [[MyNote]] */`), 0644))

	config := &Config{
		Enabled:  true,
		Includes: []string{"**/*.html", "**/*.css"},
		Excludes: []string{},
	}

	result, err := RewriteCodeRefs(tmpDir, config, "MyNote.md", "NewNote.md")
	require.NoError(t, err)

	assert.Equal(t, 2, result.FilesUpdated)
	assert.Equal(t, 4, result.RefsUpdated)

	htmlContent, err := os.ReadFile(htmlFile)
	require.NoError(t, err)
	assert.Contains(t, string(htmlContent), "[[NewNote]]")
	assert.Contains(t, string(htmlContent), "@NewNote")

	cssContent, err := os.ReadFile(cssFile)
	require.NoError(t, err)
	assert.Contains(t, string(cssContent), "[Docs](NewNote.md)")
	assert.Contains(t, string(cssContent), "[[NewNote]]")
}

func TestRewriteCodeRefs_WebFormats_OnlyRewriteCommentRegions(t *testing.T) {
	tmpDir := t.TempDir()

	cssFile := filepath.Join(tmpDir, "styles.css")
	content := `@media screen and (min-width: 600px) {
  display: block;
}

/* docs: @media and [[media]] */`
	require.NoError(t, os.WriteFile(cssFile, []byte(content), 0644))

	config := &Config{
		Enabled:  true,
		Includes: []string{"**/*.css"},
		Excludes: []string{},
	}

	result, err := RewriteCodeRefs(tmpDir, config, "media.md", "layout.md")
	require.NoError(t, err)

	assert.Equal(t, 1, result.FilesUpdated)
	assert.Equal(t, 2, result.RefsUpdated)

	updated, err := os.ReadFile(cssFile)
	require.NoError(t, err)
	assert.Contains(t, string(updated), "@media screen and (min-width: 600px)")
	assert.Contains(t, string(updated), "/* docs: @layout and [[layout]] */")
	assert.NotContains(t, string(updated), "/* docs: @media and [[media]] */")
}

func TestRewriteBatch_MultipleRenames(t *testing.T) {
	tmpDir := t.TempDir()

	codeFile := filepath.Join(tmpDir, "refs.go")
	content := `// [[NoteA]] and [[NoteB]] and @NoteC`

	require.NoError(t, os.WriteFile(codeFile, []byte(content), 0644))

	config := &Config{
		Enabled:  true,
		Includes: []string{"**/*.go"},
		Excludes: []string{},
	}

	mappings := []RefMapping{
		{OldPath: "NoteA.md", NewPath: "RenamedA.md"},
		{OldPath: "NoteB.md", NewPath: "RenamedB.md"},
		{OldPath: "NoteC.md", NewPath: "RenamedC.md"},
	}

	result, err := RewriteBatch(tmpDir, config, mappings)
	require.NoError(t, err)

	assert.Equal(t, 1, result.FilesUpdated)
	assert.Equal(t, 3, result.RefsUpdated)

	updated, err := os.ReadFile(codeFile)
	require.NoError(t, err)

	assert.Contains(t, string(updated), "[[RenamedA]]")
	assert.Contains(t, string(updated), "[[RenamedB]]")
	assert.Contains(t, string(updated), "@RenamedC")
}

func TestRewriteCodeRefs_RespectsIncludes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .go and .txt files
	goFile := filepath.Join(tmpDir, "main.go")
	txtFile := filepath.Join(tmpDir, "notes.txt")

	content := `// [[MyNote]]`
	require.NoError(t, os.WriteFile(goFile, []byte(content), 0644))
	require.NoError(t, os.WriteFile(txtFile, []byte(content), 0644))

	// Only include .go files
	config := &Config{
		Enabled:  true,
		Includes: []string{"**/*.go"},
		Excludes: []string{},
	}

	result, err := RewriteCodeRefs(tmpDir, config, "MyNote.md", "NewNote.md")
	require.NoError(t, err)

	assert.Equal(t, 1, result.FilesUpdated) // Only .go

	// .go should be updated
	goContent, _ := os.ReadFile(goFile)
	assert.Contains(t, string(goContent), "[[NewNote]]")

	// .txt should NOT be updated
	txtContent, _ := os.ReadFile(txtFile)
	assert.Contains(t, string(txtContent), "[[MyNote]]")
}

func TestRewriteCodeRefs_RespectsExcludes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files in vendor and src
	vendorDir := filepath.Join(tmpDir, "vendor")
	srcDir := filepath.Join(tmpDir, "src")
	require.NoError(t, os.MkdirAll(vendorDir, 0755))
	require.NoError(t, os.MkdirAll(srcDir, 0755))

	vendorFile := filepath.Join(vendorDir, "lib.go")
	srcFile := filepath.Join(srcDir, "main.go")

	content := `// [[MyNote]]`
	require.NoError(t, os.WriteFile(vendorFile, []byte(content), 0644))
	require.NoError(t, os.WriteFile(srcFile, []byte(content), 0644))

	config := &Config{
		Enabled:  true,
		Includes: []string{"**/*.go"},
		Excludes: []string{"**/vendor/**"},
	}

	result, err := RewriteCodeRefs(tmpDir, config, "MyNote.md", "NewNote.md")
	require.NoError(t, err)

	assert.Equal(t, 1, result.FilesUpdated) // Only src

	// src should be updated
	srcContent, _ := os.ReadFile(srcFile)
	assert.Contains(t, string(srcContent), "[[NewNote]]")

	// vendor should NOT be updated
	vendorContent, _ := os.ReadFile(vendorFile)
	assert.Contains(t, string(vendorContent), "[[MyNote]]")
}

func TestRewriteCodeRefs_DisabledConfig(t *testing.T) {
	tmpDir := t.TempDir()

	codeFile := filepath.Join(tmpDir, "main.go")
	content := `// [[MyNote]]`
	require.NoError(t, os.WriteFile(codeFile, []byte(content), 0644))

	// Disabled config
	config := &Config{
		Enabled:  false,
		Includes: []string{"**/*.go"},
	}

	result, err := RewriteCodeRefs(tmpDir, config, "MyNote.md", "NewNote.md")
	require.NoError(t, err)

	assert.Equal(t, 0, result.FilesUpdated)
	assert.Equal(t, 0, result.RefsUpdated)

	// File should be unchanged
	unchanged, _ := os.ReadFile(codeFile)
	assert.Contains(t, string(unchanged), "[[MyNote]]")
}

func TestRewriteCodeRefs_NilConfig(t *testing.T) {
	result, err := RewriteCodeRefs("/tmp", nil, "MyNote.md", "NewNote.md")
	require.NoError(t, err)
	assert.Equal(t, 0, result.FilesUpdated)
}

func TestRewriteCodeRefs_SkipsBinaryFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file with null byte (binary)
	binFile := filepath.Join(tmpDir, "data.go")
	content := []byte("// [[MyNote]]\x00binary")
	require.NoError(t, os.WriteFile(binFile, content, 0644))

	config := &Config{
		Enabled:  true,
		Includes: []string{"**/*.go"},
	}

	result, err := RewriteCodeRefs(tmpDir, config, "MyNote.md", "NewNote.md")
	require.NoError(t, err)

	assert.Equal(t, 0, result.FilesUpdated)

	// File should be unchanged
	unchanged, _ := os.ReadFile(binFile)
	assert.Contains(t, string(unchanged), "[[MyNote]]")
}

func TestRewriteCodeRefs_PreservesFileMode(t *testing.T) {
	tmpDir := t.TempDir()

	codeFile := filepath.Join(tmpDir, "script.go")
	content := `// [[MyNote]]`
	require.NoError(t, os.WriteFile(codeFile, []byte(content), 0755)) // executable

	// Capture the original permissions so we can assert they are preserved.
	originalInfo, err := os.Stat(codeFile)
	require.NoError(t, err)
	originalPerm := originalInfo.Mode().Perm()

	config := &Config{
		Enabled:  true,
		Includes: []string{"**/*.go"},
	}

	_, err = RewriteCodeRefs(tmpDir, config, "MyNote.md", "NewNote.md")
	require.NoError(t, err)

	info, err := os.Stat(codeFile)
	require.NoError(t, err)
	currentPerm := info.Mode().Perm()

	if runtime.GOOS == "windows" {
		// Windows filesystems typically drop exec bits; ensure we didn't lose RW bits.
		assert.Equal(t, originalPerm&0666, currentPerm&0666)
	} else {
		assert.Equal(t, originalPerm, currentPerm)
	}
}

func TestRewriteRefsInText_MarkdownLinks(t *testing.T) {
	tests := []struct {
		name    string
		content string
		oldPath string
		newPath string
		want    string
		count   int
	}{
		{
			name:    "basic markdown link",
			content: `// See [My Note](docs/MyNote.md) for details`,
			oldPath: "docs/MyNote.md",
			newPath: "docs/NewNote.md",
			want:    `// See [My Note](docs/NewNote.md) for details`,
			count:   1,
		},
		{
			name:    "markdown link without .md extension",
			content: `// See [My Note](docs/MyNote) for details`,
			oldPath: "docs/MyNote.md",
			newPath: "docs/NewNote.md",
			want:    `// See [My Note](docs/NewNote) for details`,
			count:   1,
		},
		{
			name:    "markdown link with anchor",
			content: `// See [Section](docs/MyNote.md#section) for details`,
			oldPath: "docs/MyNote.md",
			newPath: "docs/NewNote.md",
			want:    `// See [Section](docs/NewNote.md#section) for details`,
			count:   1,
		},
		{
			name:    "markdown link with anchor no ext",
			content: `// See [Section](docs/MyNote#section) for details`,
			oldPath: "docs/MyNote.md",
			newPath: "docs/NewNote.md",
			want:    `// See [Section](docs/NewNote#section) for details`,
			count:   1,
		},
		{
			name:    "multiple markdown links",
			content: `// [First](docs/MyNote.md) and [Second](docs/MyNote.md)`,
			oldPath: "docs/MyNote.md",
			newPath: "docs/NewNote.md",
			want:    `// [First](docs/NewNote.md) and [Second](docs/NewNote.md)`,
			count:   2,
		},
		{
			name:    "image embed should NOT match",
			content: `// ![Image](docs/MyNote.md)`,
			oldPath: "docs/MyNote.md",
			newPath: "docs/NewNote.md",
			want:    `// ![Image](docs/MyNote.md)`,
			count:   0,
		},
		{
			name:    "no match different path",
			content: `// See [Other](docs/OtherNote.md) for details`,
			oldPath: "docs/MyNote.md",
			newPath: "docs/NewNote.md",
			want:    `// See [Other](docs/OtherNote.md) for details`,
			count:   0,
		},
		{
			name:    "markdown link at start of line",
			content: `[My Note](docs/MyNote.md) is here`,
			oldPath: "docs/MyNote.md",
			newPath: "docs/NewNote.md",
			want:    `[My Note](docs/NewNote.md) is here`,
			count:   1,
		},
		{
			name:    "markdown link preserves complex text",
			content: `// See [My Hub (Hub)](docs/hubs/MyHub.md) for details`,
			oldPath: "docs/hubs/MyHub.md",
			newPath: "docs/hubs/NewHub.md",
			want:    `// See [My Hub (Hub)](docs/hubs/NewHub.md) for details`,
			count:   1,
		},
		{
			name:    "path move changes directory",
			content: `// See [Note](docs/reference/guides/MyNote.md)`,
			oldPath: "docs/reference/guides/MyNote.md",
			newPath: "docs/hubs/MyNote.md",
			want:    `// See [Note](docs/hubs/MyNote.md)`,
			count:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := compileMapping(RefMapping{OldPath: tt.oldPath, NewPath: tt.newPath})
			got, count := rewriteRefsInText(tt.content, []compiledMapping{cm})
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.count, count)
		})
	}
}

func TestRewriteRefsInText_MixedAllTypes(t *testing.T) {
	content := `// See [[MyNote]] for wikilink
// and @MyNote for mention
// and [Doc Link](docs/MyNote.md) for markdown
// [[MyNote#heading|alias]] with both`

	cm := compileMapping(RefMapping{OldPath: "docs/MyNote.md", NewPath: "docs/NewNote.md"})
	got, count := rewriteRefsInText(content, []compiledMapping{cm})

	expected := `// See [[NewNote]] for wikilink
// and @NewNote for mention
// and [Doc Link](docs/NewNote.md) for markdown
// [[NewNote#heading|alias]] with both`

	assert.Equal(t, expected, got)
	assert.Equal(t, 4, count) // 2 wikilinks + 1 mention + 1 markdown link
}

func TestRewriteCodeRefs_Integration_WithMarkdownLinks(t *testing.T) {
	tmpDir := t.TempDir()

	codeDir := filepath.Join(tmpDir, "src")
	require.NoError(t, os.MkdirAll(codeDir, 0755))

	codeFile := filepath.Join(codeDir, "main.go")
	content := `package main
// Docs:
// - [My Hub](docs/hubs/MyHub.md)
// - [My Ref](docs/reference/guides/MyRef.md)
func main() {}`

	require.NoError(t, os.WriteFile(codeFile, []byte(content), 0644))

	config := &Config{
		Enabled:  true,
		Includes: []string{"**/*.go"},
		Excludes: []string{},
	}

	result, err := RewriteCodeRefs(tmpDir, config, "docs/hubs/MyHub.md", "docs/hubs/RenamedHub.md")
	require.NoError(t, err)

	assert.Equal(t, 1, result.FilesUpdated)
	assert.Equal(t, 1, result.RefsUpdated)

	updated, err := os.ReadFile(codeFile)
	require.NoError(t, err)

	assert.Contains(t, string(updated), "[My Hub](docs/hubs/RenamedHub.md)")
	assert.Contains(t, string(updated), "[My Ref](docs/reference/guides/MyRef.md)") // unchanged
	assert.NotContains(t, string(updated), "MyHub.md")
}
