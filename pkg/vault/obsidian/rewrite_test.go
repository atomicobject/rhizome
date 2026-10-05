package obsidian

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRewriteLinksInContent_Wikilinks(t *testing.T) {
	content := "See [[Old Note]] and [[Old Note|Alias]] and [[Old Note#Heading]] and [[Old Note#^block|Alias]]."
	rewritten, count := RewriteLinksInContent(content, "Old Note.md", "Folder/New Note.md")

	assert.Equal(t, 4, count)
	assert.Contains(t, rewritten, "[[Folder/New Note]]")
	assert.Contains(t, rewritten, "[[Folder/New Note|Alias]]")
	assert.Contains(t, rewritten, "[[Folder/New Note#Heading]]")
	assert.Contains(t, rewritten, "[[Folder/New Note#^block|Alias]]")
}

func TestRewriteLinksInContent_FragmentQualifiedTargetOnlyRewritesExactFragment(t *testing.T) {
	content := "See [[Old Note#A]] and [[Old Note#B|Alias]] and [section](Old Note.md#A)."
	rewritten, count := RewriteLinksInContent(content, "Old Note#A", "New Note#A")

	assert.Equal(t, 2, count)
	assert.Contains(t, rewritten, "[[New Note#A]]")
	assert.Contains(t, rewritten, "[[Old Note#B|Alias]]")
	assert.Contains(t, rewritten, "[section](New Note.md#A)")
}

func TestRewriteLinksInContent_EmbedAndNoExt(t *testing.T) {
	content := "![[Old Note]] and [[Other Note]]"
	rewritten, count := RewriteLinksInContent(content, "Old Note", "New Note.md")

	assert.Equal(t, 1, count)
	assert.Contains(t, rewritten, "![[New Note]]")
	assert.Contains(t, rewritten, "[[Other Note]]")
}

func TestRewriteLinksInContent_AttachmentWithExtension(t *testing.T) {
	content := "See ![[image.png]] and [[image.png|thumb]]"
	rewritten, count := RewriteLinksInContent(content, "image.png", "assets/image.png")

	assert.Equal(t, 2, count)
	assert.Contains(t, rewritten, "![[assets/image.png]]")
	assert.Contains(t, rewritten, "[[assets/image.png|thumb]]")
}

func TestRewriteLinksInContent_MarkdownLinks(t *testing.T) {
	content := "See [text](Old Note.md) and [section](Old Note.md#heading) and [ext](https://example.com)."
	rewritten, count := RewriteLinksInContent(content, "Old Note.md", "Folder/New Note.md")

	assert.Equal(t, 2, count)
	assert.Contains(t, rewritten, "[text](Folder/New Note.md)")
	assert.Contains(t, rewritten, "[section](Folder/New Note.md#heading)")
	assert.Contains(t, rewritten, "[ext](https://example.com)")
}

func TestRewriteLinksInContent_NoMatch(t *testing.T) {
	content := "Nothing to change [[Another Note]] and [link](another.md)."
	rewritten, count := RewriteLinksInContent(content, "Old Note.md", "New Note.md")

	assert.Equal(t, 0, count)
	assert.Equal(t, content, rewritten)
}

func TestRewriteLinksInContent_WikilinkWithoutFolder(t *testing.T) {
	// Bug fix: wikilinks often omit the folder path, but oldPath includes it
	content := "See [[Old Note]] and [[Old Note|Alias]]."
	rewritten, count := RewriteLinksInContent(content, "Notes/Old Note.md", "Notes/New Note.md")

	assert.Equal(t, 2, count)
	// Links should be rewritten to basename only (preserving original style)
	assert.Contains(t, rewritten, "[[New Note]]")
	assert.Contains(t, rewritten, "[[New Note|Alias]]")
}

func TestRewriteLinksInContent_WikilinkWithFolder(t *testing.T) {
	// When the wikilink includes the folder, preserve it
	content := "See [[Notes/Old Note]] and [[Notes/Old Note|Alias]]."
	rewritten, count := RewriteLinksInContent(content, "Notes/Old Note.md", "Archive/New Note.md")

	assert.Equal(t, 2, count)
	// Links should be rewritten with full path
	assert.Contains(t, rewritten, "[[Archive/New Note]]")
	assert.Contains(t, rewritten, "[[Archive/New Note|Alias]]")
}

func TestRewriteLinksInContent_MarkdownLinkWithoutFolder(t *testing.T) {
	// Bug fix: markdown links often omit the folder path
	content := "See [text](Old Note.md) and [section](Old Note.md#heading)."
	rewritten, count := RewriteLinksInContent(content, "Notes/Old Note.md", "Notes/New Note.md")

	assert.Equal(t, 2, count)
	// Links should be rewritten to basename only (preserving original style)
	assert.Contains(t, rewritten, "[text](New Note.md)")
	assert.Contains(t, rewritten, "[section](New Note.md#heading)")
}

func TestRewriteLinksInContentWithOptions_DuplicateBasename(t *testing.T) {
	// When basenameUnique is false, only match full paths (not basename-only links)
	// This prevents incorrectly rewriting links that might point to a different file with the same name
	content := "See [[Old Note]] and [[Notes/Old Note]] and [[Archive/Old Note]]."

	// With basenameUnique=false, only the explicit path match should be rewritten
	rewritten, count := RewriteLinksInContentWithOptions(content, "Notes/Old Note.md", "Notes/New Note.md", false)

	assert.Equal(t, 1, count)
	assert.Contains(t, rewritten, "[[Old Note]]")         // NOT rewritten (ambiguous)
	assert.Contains(t, rewritten, "[[Notes/New Note]]")   // Rewritten (explicit match)
	assert.Contains(t, rewritten, "[[Archive/Old Note]]") // NOT rewritten (different folder)
}

func TestRewriteLinksInContentWithOptions_UniqueBasename(t *testing.T) {
	// When basenameUnique is true, basename-only links should be rewritten
	content := "See [[Old Note]] and [[Notes/Old Note]] and [[Archive/Old Note]]."

	// With basenameUnique=true, both basename and explicit path matches should be rewritten
	rewritten, count := RewriteLinksInContentWithOptions(content, "Notes/Old Note.md", "Notes/New Note.md", true)

	assert.Equal(t, 2, count)
	assert.Contains(t, rewritten, "[[New Note]]")         // Rewritten (basename match, unique)
	assert.Contains(t, rewritten, "[[Notes/New Note]]")   // Rewritten (explicit match)
	assert.Contains(t, rewritten, "[[Archive/Old Note]]") // NOT rewritten (different folder)
}

func TestRewriteLinksInContent_CaseInsensitive(t *testing.T) {
	// On macOS/Windows, Obsidian links are case-insensitive
	// This test verifies the behavior matches the current OS
	content := "See [[old note]] and [[NOTES/OLD NOTE]] and [[Old Note]]."
	rewritten, count := RewriteLinksInContent(content, "Notes/Old Note.md", "Notes/New Note.md")

	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		// macOS/Windows: all three should match
		assert.Equal(t, 3, count)
		assert.Contains(t, rewritten, "[[New Note]]")       // Was [[old note]] - basename match
		assert.Contains(t, rewritten, "[[Notes/New Note]]") // Was [[NOTES/OLD NOTE]] - full path match
	} else {
		// Linux: only exact case matches
		assert.Equal(t, 1, count)
		assert.Contains(t, rewritten, "[[New Note]]")       // Rewritten (exact basename match)
		assert.Contains(t, rewritten, "[[old note]]")       // NOT rewritten (case mismatch)
		assert.Contains(t, rewritten, "[[NOTES/OLD NOTE]]") // NOT rewritten (case mismatch)
	}
}

func TestRewriteLinksInContent_URLEncodedMarkdownLinks(t *testing.T) {
	// URL-encoded markdown links should be matched and rewritten
	content := "[link](Old%20Note.md) and [normal](Old Note.md)"

	rewritten, count := RewriteLinksInContent(content, "Old Note.md", "New Note.md")

	assert.Equal(t, 2, count)
	assert.Contains(t, rewritten, "[link](New%20Note.md)") // Preserves URL encoding
	assert.Contains(t, rewritten, "[normal](New Note.md)")
}

func TestRewriteLinksInContent_URLEncodedWithFragment(t *testing.T) {
	// URL-encoded links with fragments should work correctly
	content := "[section](Old%20Note.md#heading)"

	rewritten, count := RewriteLinksInContent(content, "Old Note.md", "New Note.md")

	assert.Equal(t, 1, count)
	assert.Contains(t, rewritten, "[section](New%20Note.md#heading)")
}

func TestRewriteLinksInContent_URLEncodedPathWithFolder(t *testing.T) {
	// URL-encoded paths with folders
	content := "[link](Folder%2FOld%20Note.md)"

	rewritten, count := RewriteLinksInContent(content, "Folder/Old Note.md", "Archive/New Note.md")

	assert.Equal(t, 1, count)
	assert.Contains(t, rewritten, "[link](Archive%2FNew%20Note.md)")
}

func TestRewriteLinksInContent_MixedCodeAndLinks(t *testing.T) {
	cases := []struct {
		name, content, want string
		count               int
	}{
		{"backtick fence", "Regular [[Old Note]] link.\n\n```\n[[Old Note]] in code block\n```\n\nAnother [[Old Note]] link.", "Regular [[New Note]] link.\n\n```\n[[Old Note]] in code block\n```\n\nAnother [[New Note]] link.", 2},
		{"tilde fence", "Regular [[Old Note]] link.\n\n~~~\n[[Old Note]] in tilde code block\n~~~\n\nAnother [[Old Note]] link.", "Regular [[New Note]] link.\n\n~~~\n[[Old Note]] in tilde code block\n~~~\n\nAnother [[New Note]] link.", 2},
		{"inline code", "Regular [[Old Note]] and `[[Old Note]]` in inline code and [[Old Note]] again.", "Regular [[New Note]] and `[[Old Note]]` in inline code and [[New Note]] again.", 2},
		{"mixed", "# Title\n\nSee [[Old Note]] for details.\n\n```go\n// This is code: [[Old Note]]\nlink := \"[[Old Note]]\"\n```\n\nMore text with `[[Old Note]]` inline.\n\nAlso check [[Old Note#Section]] and:\n\n```\n[[Old Note]]\n```\n\nFinal [[Old Note]] reference.", "# Title\n\nSee [[New Note]] for details.\n\n```go\n// This is code: [[Old Note]]\nlink := \"[[Old Note]]\"\n```\n\nMore text with `[[Old Note]]` inline.\n\nAlso check [[New Note#Section]] and:\n\n```\n[[Old Note]]\n```\n\nFinal [[New Note]] reference.", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, count := RewriteLinksInContent(tc.content, "Old Note.md", "New Note.md")
			assert.Equal(t, tc.count, count)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRewriteLinksInContent_MarkdownLinksInCodeBlocks(t *testing.T) {
	// Markdown-style links in code blocks should also be skipped
	content := `Regular [link](Old Note.md).

` + "```" + `
[link](Old Note.md) in code
` + "```" + `

And ` + "`[link](Old Note.md)`" + ` inline.`

	rewritten, count := RewriteLinksInContent(content, "Old Note.md", "New Note.md")

	assert.Equal(t, 1, count) // Only the regular link
	assert.Contains(t, rewritten, "Regular [link](New Note.md)")
	assert.Contains(t, rewritten, "[link](Old Note.md) in code") // Preserved
	assert.Contains(t, rewritten, "`[link](Old Note.md)`")       // Preserved
}
