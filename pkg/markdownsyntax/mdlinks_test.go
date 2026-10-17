package markdownsyntax

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

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
