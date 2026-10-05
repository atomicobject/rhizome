//go:build windows

package actions_test

import (
	"os"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/stretchr/testify/require"
)

func TestRenameNoteGitRequestedSourceSpelling(t *testing.T) {
	root := t.TempDir()
	seedMoveEndpoint(t, root, "SOURCE.md", "original source\n")
	_, err := os.Stat(filepath.Join(root, "Source.md"))
	require.NoError(t, err, "native Windows source alias must exist")
	initGitRepo(t, root)
	require.NoError(t, runMoveGit(root, "add", "."))
	commitGitFixture(t, root)
	result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "Source.md", Target: "Moved.md",
	})
	require.NoError(t, err)
	require.Equal(t, "Moved.md", result.RenamedPath)
	require.True(t, result.GitHistoryPreserved)
	require.Equal(t, "original source\n", readMoveEndpoint(t, filepath.Join(root, "Moved.md")))
	require.Equal(t, "Moved.md\n", moveGitOutput(t, root, "ls-files"))
	require.Equal(t, "original source\n", moveGitOutput(t, root, "show", ":Moved.md"))
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		if entry.Name() != ".git" && entry.Name() != ".rhizome" {
			names = append(names, entry.Name())
		}
	}
	require.Equal(t, []string{"Moved.md"}, names)
}
