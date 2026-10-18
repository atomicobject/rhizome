package sectionparse

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseKeepsCanonicalHeadingIdentityAndAuthoredIDs(t *testing.T) {
	for _, tc := range []struct{ content, id, block string }{
		{"# What's new?\nBody\n", "notes/example.md#what-s-new-0", ""},
		{"# ÄRGER 東京\nBody\n", "notes/example.md#ärger-東京-0", ""},
		{"# !!!\nBody\n", "notes/example.md#-0", ""},
		{"# What's new?\n^Authored_ID\n", "notes/example.md#^Authored_ID", "Authored_ID"},
	} {
		t.Run(tc.content, func(t *testing.T) {
			nodes := Parse("notes/example.md", tc.content)
			require.Len(t, nodes, 1)
			require.Equal(t, tc.id, nodes[0].ID)
			require.Equal(t, tc.block, nodes[0].BlockID)
		})
	}
}
