package obsidian

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractMdLinks(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		options  MdLinkOptions
		expected []string
	}{
		{
			name:     "basic markdown link",
			content:  "Check out [this note](other.md) for more info.",
			options:  DefaultMdLinkOptions,
			expected: []string{"other.md"},
		},
		{
			name:     "markdown link with anchor",
			content:  "See [the section](note.md#section) for details.",
			options:  DefaultMdLinkOptions,
			expected: []string{"note.md#section"},
		},
		{
			name:     "skip anchors option",
			content:  "See [the section](note.md#section) and [full](other.md).",
			options:  MdLinkOptions{SkipAnchors: true},
			expected: []string{"other.md"},
		},
		{
			name:     "image embed",
			content:  "Here is an image ![alt text](image.png).",
			options:  DefaultMdLinkOptions,
			expected: []string{"image.png"},
		},
		{
			name:     "skip embeds option",
			content:  "Image ![alt](image.png) and [link](doc.md).",
			options:  MdLinkOptions{SkipEmbeds: true},
			expected: []string{"doc.md"},
		},
		{
			name:     "multiple links",
			content:  "See [one](first.md), [two](second.md), and [three](third.md).",
			options:  DefaultMdLinkOptions,
			expected: []string{"first.md", "second.md", "third.md"},
		},
		{
			name:     "relative path",
			content:  "Check [subdir note](subdir/note.md) out.",
			options:  DefaultMdLinkOptions,
			expected: []string{"subdir/note.md"},
		},
		{
			name:     "parent path",
			content:  "Go to [parent](../parent.md).",
			options:  DefaultMdLinkOptions,
			expected: []string{"../parent.md"},
		},
		{
			name:     "skip external URLs",
			content:  "Visit [site](https://example.com) and [doc](local.md).",
			options:  DefaultMdLinkOptions,
			expected: []string{"local.md"},
		},
		{
			name:     "skip mailto links",
			content:  "Contact [email](mailto:test@example.com) and [doc](local.md).",
			options:  DefaultMdLinkOptions,
			expected: []string{"local.md"},
		},
		{
			name:     "mixed with wikilinks - only extracts markdown",
			content:  "See [[wikilink]] and [markdown](file.md).",
			options:  DefaultMdLinkOptions,
			expected: []string{"file.md"},
		},
		{
			name:     "empty content",
			content:  "",
			options:  DefaultMdLinkOptions,
			expected: nil,
		},
		{
			name:     "no links",
			content:  "Just plain text without any links.",
			options:  DefaultMdLinkOptions,
			expected: nil,
		},
		{
			name:     "malformed link - no closing paren",
			content:  "Broken [link](path.md",
			options:  DefaultMdLinkOptions,
			expected: nil,
		},
		{
			name:     "malformed link - no opening paren",
			content:  "Broken [link]path.md)",
			options:  DefaultMdLinkOptions,
			expected: nil,
		},
		{
			name:     "link with title attribute",
			content:  `Check [link](path.md "title") out.`,
			options:  DefaultMdLinkOptions,
			expected: []string{`path.md "title"`}, // Title is part of the path in simple parsing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractMdLinks(tt.content, tt.options)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScanMdLinks(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		options  MdLinkOptions
		expected []MdLink
	}{
		{
			name:    "basic link type",
			content: "[text](note.md)",
			options: DefaultMdLinkOptions,
			expected: []MdLink{
				{Target: "note.md", Text: "text", LinkType: MdLinkTypeBasic},
			},
		},
		{
			name:    "heading link type",
			content: "[text](note.md#heading)",
			options: DefaultMdLinkOptions,
			expected: []MdLink{
				{Target: "note.md#heading", Text: "text", LinkType: MdLinkTypeHeading},
			},
		},
		{
			name:    "embed link type",
			content: "![alt](image.png)",
			options: DefaultMdLinkOptions,
			expected: []MdLink{
				{Target: "image.png", Text: "alt", LinkType: MdLinkTypeEmbed},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanMdLinks(tt.content, tt.options)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveMdLink(t *testing.T) {
	allNotes := []string{
		"notes/one.md",
		"notes/two.md",
		"docs/readme.md",
		"root.md",
	}
	cache := BuildNotePathCache(allNotes)

	tests := []struct {
		name       string
		link       string
		fromNote   string
		wantPath   string
		wantExists bool
	}{
		{
			name:       "relative link from same directory",
			link:       "two.md",
			fromNote:   "notes/one.md",
			wantPath:   "notes/two.md",
			wantExists: true,
		},
		{
			name:       "relative link with anchor",
			link:       "two.md#section",
			fromNote:   "notes/one.md",
			wantPath:   "notes/two.md",
			wantExists: true,
		},
		{
			name:       "link to subdirectory",
			link:       "docs/readme.md",
			fromNote:   "root.md",
			wantPath:   "docs/readme.md",
			wantExists: true,
		},
		{
			name:       "same-file anchor link",
			link:       "#section",
			fromNote:   "notes/one.md",
			wantPath:   "notes/one.md",
			wantExists: true,
		},
		{
			name:       "non-existent file",
			link:       "nonexistent.md",
			fromNote:   "notes/one.md",
			wantPath:   "",
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotExists := cache.ResolveMdLink(tt.link, tt.fromNote)
			assert.Equal(t, tt.wantExists, gotExists)
			if tt.wantExists {
				assert.Equal(t, tt.wantPath, gotPath)
			}
		})
	}
}

func TestResolveMdLink_ExactPathWinsBasenameCollision(t *testing.T) {
	cache := BuildNotePathCache([]string{
		"README.md",
		"docs/README.md",
		"docs/reference/README.md",
	})

	gotPath, exists := cache.ResolveMdLink("README.md", "AGENTS.md")

	assert.True(t, exists)
	assert.Equal(t, "README.md", gotPath)
}

func TestExtractAllLinks(t *testing.T) {
	content := `
# My Note

Check out [[wikilink note]] and [markdown link](other.md).

Also see [[another|aliased]] and [more docs](docs/readme.md).
`
	wikiOpts := DefaultWikilinkOptions
	mdOpts := DefaultMdLinkOptions

	links := ExtractAllLinks(content, wikiOpts, mdOpts)

	assert.Contains(t, links, "wikilink note")
	assert.Contains(t, links, "other.md")
	assert.Contains(t, links, "another")
	assert.Contains(t, links, "docs/readme.md")
	assert.Len(t, links, 4)
}

func TestScanAllLinks(t *testing.T) {
	content := `See [[wiki]] and [md](file.md).`

	details := ScanAllLinks(content, DefaultWikilinkOptions, DefaultMdLinkOptions)

	assert.Len(t, details, 2)

	// Check wikilink
	wikiFound := false
	mdFound := false
	for _, d := range details {
		if d.Target == "wiki" && d.LinkType == "wikilink" {
			wikiFound = true
		}
		if d.Target == "file.md" && d.LinkType == "mdlink" {
			mdFound = true
		}
	}
	assert.True(t, wikiFound, "Expected to find wikilink")
	assert.True(t, mdFound, "Expected to find markdown link")
}
