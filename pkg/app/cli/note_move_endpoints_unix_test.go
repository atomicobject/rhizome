//go:build !windows

package actions_test

import (
	"os"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRenameNoteFailedOverwritePreservesTargetAndGitIndex(t *testing.T) {
	for _, git := range []bool{false, true} {
		t.Run(map[bool]string{false: "filesystem", true: "git"}[git], func(t *testing.T) {
			root := t.TempDir()
			source := seedMoveEndpoint(t, root, "locked/Source.md", "original source\n")
			target := seedMoveEndpoint(t, root, "Target.md", "original target\n")
			var index string
			if git {
				initGitRepo(t, root)
				require.NoError(t, runMoveGit(root, "add", "."))
				commitGitFixture(t, root)
				index = moveGitOutput(t, root, "ls-files", "--stage")
			}
			dir := filepath.Dir(source)
			require.NoError(t, os.Chmod(dir, 0o555))
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			_, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "locked/Source.md", Target: "Target.md", Overwrite: true,
			})
			require.ErrorIs(t, err, os.ErrPermission)
			require.Equal(t, "original source\n", readMoveEndpoint(t, source))
			require.Equal(t, "original target\n", readMoveEndpoint(t, target))
			if git {
				require.Equal(t, index, moveGitOutput(t, root, "ls-files", "--stage"))
				require.Equal(t, "original target\n", moveGitOutput(t, root, "show", ":Target.md"))
			}
		})
	}
}

func TestRenameNoteRejectsPhysicalEndpointAlias(t *testing.T) {
	for _, alias := range []string{"symlink parent"} {
		t.Run(alias, func(t *testing.T) {
			root := t.TempDir()
			source := seedMoveEndpoint(t, root, "Source.md", "original source\n")
			target := "Alias.md"
			require.NoError(t, os.Symlink(root, filepath.Join(root, "alias")))
			target = "alias/Source.md"
			_, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "Source.md", Target: target, Overwrite: true,
			})
			require.Error(t, err)
			require.Equal(t, "original source\n", readMoveEndpoint(t, source))
			require.Equal(t, "original source\n", readMoveEndpoint(t, filepath.Join(root, target)))
		})
	}
}

func TestMoveNotesRejectsPhysicalBatchAliases(t *testing.T) {
	for _, alias := range []string{"source parent alias", "target parent alias"} {
		t.Run(alias, func(t *testing.T) {
			root := t.TempDir()
			a := seedMoveEndpoint(t, root, "A.md", "original A\n")
			b := seedMoveEndpoint(t, root, "B.md", "original B\n")
			require.NoError(t, os.Symlink(root, filepath.Join(root, "alias")))
			moves := []actions.MoveRequest{{Source: "A.md", Target: "dest/A.md"}, {Source: "B.md", Target: "dest/B.md"}}
			switch alias {
			case "source parent alias":
				moves[1].Source = "alias/A.md"
			case "target parent alias":
				moves[1].Target = "alias/dest/A.md"

			}
			summary, err := actions.MoveNotes(namespaceVault{path: root}, &obsidian.Uri{}, actions.MoveParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: moves, Overwrite: true,
			})
			require.Error(t, err)
			require.Empty(t, summary.Results)
			require.Equal(t, "original A\n", readMoveEndpoint(t, a))
			require.Equal(t, "original B\n", readMoveEndpoint(t, b))
			require.NoDirExists(t, filepath.Join(root, "dest"))
		})
	}
}

func TestMoveNotesPreflightsInvalidSymlinkEndpoints(t *testing.T) {
	for _, state := range []string{"source leaf", "target leaf", "dangling parent", "outside parent"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			a := seedMoveEndpoint(t, root, "A.md", "original A\n")
			b := seedMoveEndpoint(t, root, "B.md", "original B\n")
			moves := []actions.MoveRequest{{Source: "A.md", Target: "dest/A.md"}, {Source: "B.md", Target: "dest/B.md"}}
			switch state {
			case "source leaf":
				require.NoError(t, os.Symlink(b, filepath.Join(root, "link.md")))
				moves[1].Source = "link.md"
			case "target leaf":
				require.NoError(t, os.Symlink(b, filepath.Join(root, "link.md")))
				moves[1].Target = "link.md"
			case "dangling parent":
				require.NoError(t, os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "alias")))
				moves[1].Target = "alias/B.md"
			case "outside parent":
				require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "alias")))
				moves[1].Target = "alias/B.md"
			}
			summary, err := actions.MoveNotes(namespaceVault{path: root}, &obsidian.Uri{}, actions.MoveParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: moves, Overwrite: true,
			})
			require.Error(t, err)
			require.Empty(t, summary.Results)
			require.Equal(t, "original A\n", readMoveEndpoint(t, a))
			require.Equal(t, "original B\n", readMoveEndpoint(t, b))
			require.NoDirExists(t, filepath.Join(root, "dest"))
		})
	}
}

func TestRenameNoteCanonicalParentAliasKeepsBacklinksAndGit(t *testing.T) {
	root := t.TempDir()
	seedMoveEndpoint(t, root, "Source.md", "original source\n")
	ref := seedMoveEndpoint(t, root, "Ref.md", "[[Source]]\n")
	initGitRepo(t, root)
	require.NoError(t, runMoveGit(root, "add", "."))
	commitGitFixture(t, root)
	require.NoError(t, os.Symlink(root, filepath.Join(root, "alias")))
	result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "alias/Source.md", Target: "alias/dest/Target.md", UpdateBacklinks: true,
	})
	require.NoError(t, err)
	require.True(t, result.GitHistoryPreserved)
	require.Equal(t, "dest/Target.md", result.RenamedPath)
	require.Equal(t, "original source\n", readMoveEndpoint(t, filepath.Join(root, "dest/Target.md")))
	require.Equal(t, "[[dest/Target]]\n", readMoveEndpoint(t, ref))
	require.Equal(t, "Ref.md\ndest/Target.md\n", moveGitOutput(t, root, "ls-files"))
}

func TestMoveNotesPreflightsTargetInspectionError(t *testing.T) {
	root := t.TempDir()
	a := seedMoveEndpoint(t, root, "A.md", "original A\n")
	b := seedMoveEndpoint(t, root, "B.md", "original B\n")
	blocked := filepath.Join(root, "blocked")
	require.NoError(t, os.Mkdir(blocked, 0o755))
	require.NoError(t, os.Chmod(blocked, 0))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })
	summary, err := actions.MoveNotes(namespaceVault{path: root}, &obsidian.Uri{}, actions.MoveParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: []actions.MoveRequest{{Source: "A.md", Target: "dest/A.md"}, {Source: "B.md", Target: "blocked/B.md"}},
	})
	require.ErrorIs(t, err, os.ErrPermission)
	require.Empty(t, summary.Results)
	require.Equal(t, "original A\n", readMoveEndpoint(t, a))
	require.Equal(t, "original B\n", readMoveEndpoint(t, b))
	require.NoDirExists(t, filepath.Join(root, "dest"))
}

func TestMoveNotesRejectsTargetAncestryThroughParentAlias(t *testing.T) {
	root := t.TempDir()
	a := seedMoveEndpoint(t, root, "A.md", "original A\n")
	b := seedMoveEndpoint(t, root, "B.md", "original B\n")
	require.NoError(t, os.Symlink(root, filepath.Join(root, "alias")))
	result, err := actions.MoveNotes(namespaceVault{path: root}, &obsidian.Uri{}, actions.MoveParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: []actions.MoveRequest{{Source: "A.md", Target: "alias/New.md/A.md"}, {Source: "B.md", Target: "New.md"}}, Overwrite: true,
	})
	require.Error(t, err)
	require.Empty(t, result.Results)
	require.Equal(t, "original A\n", readMoveEndpoint(t, a))
	require.Equal(t, "original B\n", readMoveEndpoint(t, b))
	require.NoDirExists(t, filepath.Join(root, "New.md"))
}
