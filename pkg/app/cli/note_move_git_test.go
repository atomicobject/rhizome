package actions_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/stretchr/testify/require"
)

func runMoveGit(root string, args ...string) error {
	return exec.Command("git", append([]string{"-C", root}, args...)...).Run()
}

func moveGitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	require.NoError(t, err, string(output))
	return string(output)
}

func TestRenameNoteOverwritePreservesGitState(t *testing.T) {
	for _, state := range []string{"tracked", "untracked source", "untracked target", "dirty source", "staged source", "dirty target", "staged target"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "Source.md")
			if state != "untracked source" {
				seedMoveEndpoint(t, root, "Source.md", "original source\n")
			}
			target := filepath.Join(root, "Target.md")
			if state != "untracked target" {
				seedMoveEndpoint(t, root, "Target.md", "original target\n")
			}
			unrelated := seedMoveEndpoint(t, root, "Unrelated.md", "original unrelated\n")
			initGitRepo(t, root)
			require.NoError(t, runMoveGit(root, "add", "."))
			commitGitFixture(t, root)
			wantContent, wantIndex := "original source\n", "original source\n"
			switch state {
			case "untracked target":
				seedMoveEndpoint(t, root, "Target.md", "untracked target\n")
			case "untracked source":
				seedMoveEndpoint(t, root, "Source.md", "untracked source\n")
				wantContent, wantIndex = "untracked source\n", "original target\n"
			case "dirty source", "staged source":
				wantContent = state + "\n"
				require.NoError(t, os.WriteFile(source, []byte(wantContent), 0o644))
				if state == "staged source" {
					require.NoError(t, runMoveGit(root, "add", "Source.md"))
					wantIndex = wantContent
				}
			case "dirty target", "staged target":
				require.NoError(t, os.WriteFile(target, []byte(state+"\n"), 0o644))
				if state == "staged target" {
					require.NoError(t, runMoveGit(root, "add", "Target.md"))
				}
			}
			require.NoError(t, os.WriteFile(unrelated, []byte("staged unrelated\n"), 0o644))
			require.NoError(t, runMoveGit(root, "add", "Unrelated.md"))
			result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "Source.md", Target: "Target.md", Overwrite: true,
			})
			require.NoError(t, err)
			require.Equal(t, state != "untracked source", result.GitHistoryPreserved)
			require.NoFileExists(t, source)
			require.Equal(t, wantContent, readMoveEndpoint(t, target))
			require.Equal(t, "Target.md\nUnrelated.md\n", moveGitOutput(t, root, "ls-files"))
			require.Equal(t, wantIndex, moveGitOutput(t, root, "show", ":Target.md"))
			require.Equal(t, "staged unrelated\n", moveGitOutput(t, root, "show", ":Unrelated.md"))
			require.Equal(t, "staged unrelated\n", readMoveEndpoint(t, unrelated))
		})
	}
}
