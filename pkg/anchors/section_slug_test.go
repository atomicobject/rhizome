package codeanchor

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIntelSectionsUseCanonicalHeadingSlugAndKeepAuthoredIdentity(t *testing.T) {
	for _, tc := range []struct{ content, source, id string }{
		{"# What's new?\nBody\n", "notes/example.md#what-s-new-0", "9212ee347996c1d6dbb8aa231e0b41ef2e097b868cb680bb38ef2bd447ce9372"},
		{"# !!!\nBody\n", "notes/example.md#-0", "6f2facd83a64317a2c2b2e2958b305f06639c924d1e8d685c831ee6a1a934bff"},
		{"# What's new?\n^Authored_ID\n", "notes/example.md#^Authored_ID", "9212ee347996c1d6dbb8aa231e0b41ef2e097b868cb680bb38ef2bd447ce9372"},
	} {
		t.Run(tc.content, func(t *testing.T) {
			sections, _, _ := extractMarkdownIntelDocSections("notes/example.md", tc.content)
			require.Len(t, sections, 1)
			require.Equal(t, tc.source, sections[0].SourceNodeID)
			require.Equal(t, tc.id, sections[0].SectionID)
		})
	}
}
