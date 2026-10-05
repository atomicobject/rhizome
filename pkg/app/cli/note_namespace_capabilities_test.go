package actions_test

import (
	"os"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/stretchr/testify/require"
)

func TestRenameNoteRejectsUnsupportedFormatBeforeFilesystemChanges(t *testing.T) {
	for _, pair := range [][2]string{
		{"report.html", "new/report.html"},
		{"report.HTM", "new/report.HTM"},
		{"report.html", "new/report.md"},
		{"report.md", "new/report.html"},
	} {
		t.Run(pair[0]+" to "+pair[1], func(t *testing.T) {
			root := t.TempDir()
			content := []byte("<html><body>authored source</body></html>\n")
			require.NoError(t, os.WriteFile(filepath.Join(root, pair[0]), content, 0o644))
			_, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: pair[0], Target: pair[1], UpdateBacklinks: true,
			})
			require.ErrorContains(t, err, "unsupported")
			got, err := os.ReadFile(filepath.Join(root, pair[0]))
			require.NoError(t, err)
			require.Equal(t, content, got)
			require.NoDirExists(t, filepath.Join(root, "new"))
		})
	}
}

func TestMoveNotesRejectsUnsupportedBatchBeforeMovingAnyFile(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one.md", "two.html"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(name), 0o644))
	}
	_, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
		Moves: []actions.MoveRequest{{Source: "one.md", Target: "archive/one.md"}, {Source: "two.html", Target: "archive/two.html"}},
	})
	require.ErrorContains(t, err, "unsupported")
	require.FileExists(t, filepath.Join(root, "one.md"))
	require.FileExists(t, filepath.Join(root, "two.html"))
	require.NoDirExists(t, filepath.Join(root, "archive"))
}

func TestRenameNoteRejectsHTMLBeforeOverwritingTarget(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte("source"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.html"), []byte("target"), 0o644))
	_, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "source.md", Target: "target.html", Overwrite: true,
	})
	require.ErrorContains(t, err, "unsupported")
	for name, expected := range map[string]string{"source.md": "source", "target.html": "target"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, expected, string(got))
	}
}

func TestRenameNoteRequiresExplicitFormatRuntimeBeforeMoving(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte("source"), 0o644))
	_, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{Source: "source.md", Target: "new/target.md", PostApplyRefresher: namespaceTestRefresher(t, root)})
	require.ErrorContains(t, err, "note format runtime is required")
	require.FileExists(t, filepath.Join(root, "source.md"))
	require.NoDirExists(t, filepath.Join(root, "new"))
}
