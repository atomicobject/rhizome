package codeanchor

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseNoteKeepsAuthoredTitlesAndPortableFilenameFallbacks(t *testing.T) {
	for _, tc := range []struct{ path, content, want string }{
		{`notes\release.v2.md`, "Body", "release.v2"},
		{"notes/release.v2.md", "---\ntitle: Authored title\n---\nBody", "Authored title"},
		{"notes/release.v2.md", "---\ntitle: ''\n---\nBody", "release.v2"},
	} {
		t.Run(tc.path+tc.content, func(t *testing.T) {
			note, err := ParseNote(tc.path, tc.content)
			require.NoError(t, err)
			require.Equal(t, tc.want, note.Title)
		})
	}
}
