package paths

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNoteTitle(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"notes/page.md", "page"},
		{"notes/page.html", "page"},
		{"notes/release.v2.md", "release.v2"},
		{`notes\release.v2.html`, "release.v2"},
		{`C:\vault/notes\Decision.MD`, "Decision"},
		{"notes/no-extension", "no-extension"},
		{"notes/ spaced title .md", " spaced title "},
		{"", ""},
	} {
		t.Run(tc.path, func(t *testing.T) { require.Equal(t, tc.want, NoteTitle(tc.path)) })
	}
}
