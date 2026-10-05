//go:build !windows

package actions_test

import (
	"os"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/stretchr/testify/require"
)

func TestNoteNamespaceUnreadableBacklinkPreventsPublication(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file read permissions")
	}
	for _, entrypoint := range []string{"rename", "move"} {
		t.Run(entrypoint, func(t *testing.T) {
			root := t.TempDir()
			originals := map[string]string{
				"Old.md": "# Old\n\n[[Old]]\n", "Second.md": "# Second\n",
				"ARef.md": "[[Old]] [[Second]]\n", "ZUnreadable.md": "[[Old]]\n",
			}
			for name, content := range originals {
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
			}
			unreadable := filepath.Join(root, "ZUnreadable.md")
			require.NoError(t, os.Chmod(unreadable, 0))
			t.Cleanup(func() { require.NoError(t, os.Chmod(unreadable, 0o644)) })
			_, readErr := os.ReadFile(unreadable)
			require.ErrorIs(t, readErr, os.ErrPermission)
			indexer := namespaceTestMetadata(t)
			var err error
			if entrypoint == "rename" {
				_, err = actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					NoteMetadata: indexer, PostApplyRefresher: namespaceTestRefresher(t, root), Source: "Old.md", Target: "New.md", UpdateBacklinks: true,
				})
			} else {
				_, err = actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: indexer, PostApplyRefresher: namespaceTestRefresher(t, root), UpdateBacklinks: true,
					Moves: []actions.MoveRequest{{Source: "Old.md", Target: "New.md"}, {Source: "Second.md", Target: "Moved.md"}},
				})
			}
			require.ErrorIs(t, err, os.ErrPermission)
			for name, want := range originals {
				if name == "ZUnreadable.md" {
					continue
				}
				got, readErr := os.ReadFile(filepath.Join(root, name))
				require.NoError(t, readErr, "required graph publication must leave %s intact", name)
				require.Equal(t, want, string(got))
			}
			for _, target := range []string{"New.md", "Moved.md"} {
				_, statErr := os.Stat(filepath.Join(root, target))
				require.ErrorIs(t, statErr, os.ErrNotExist)
			}
		})
	}
}

func TestNoteNamespaceInventoryKeepsExcludedLeafAliasDistinct(t *testing.T) {
	for _, entrypoint := range []string{"rename", "move"} {
		for _, leaf := range []string{"Alias.txt", "Alias.md"} {
			t.Run(entrypoint+"/"+leaf, func(t *testing.T) {
				root := t.TempDir()
				source := seedMoveEndpoint(t, root, "Old.md", "# Old\n")
				ref := seedMoveEndpoint(t, root, "Ref.md", "[[Old]]\n")
				alias := filepath.Join(root, leaf)
				require.NoError(t, os.Symlink("Ref.md", alias))
				if leaf == "Alias.md" {
					seedMoveEndpoint(t, root, ".gitignore", "Alias.md\n")
				}
				var skipped []string
				if entrypoint == "rename" {
					result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
						NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
						Source: "Old.md", Target: "New.md", UpdateBacklinks: true,
					})
					require.NoError(t, err)
					require.Equal(t, 1, result.LinkUpdates)
					skipped = result.Skipped
				} else {
					result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
						NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
						Moves: []actions.MoveRequest{{Source: "Old.md", Target: "New.md"}}, UpdateBacklinks: true,
					})
					require.NoError(t, err)
					require.Equal(t, 1, result.TotalLinkUpdates)
					skipped = result.Skipped
				}
				if leaf == "Alias.md" {
					require.Equal(t, []string{"Alias.md"}, skipped)
				} else {
					require.Empty(t, skipped)
				}
				require.NoFileExists(t, source)
				require.Equal(t, "# Old\n", readMoveEndpoint(t, filepath.Join(root, "New.md")))
				require.Equal(t, "[[New]]\n", readMoveEndpoint(t, ref))
				info, err := os.Lstat(alias)
				require.NoError(t, err)
				require.NotZero(t, info.Mode()&os.ModeSymlink)
				link, err := os.Readlink(alias)
				require.NoError(t, err)
				require.Equal(t, "Ref.md", link)
			})
		}
	}
}

func TestNoteNamespaceEligibleLeafAliasPreventsPublication(t *testing.T) {
	for _, entrypoint := range []string{"rename", "move"} {
		t.Run(entrypoint, func(t *testing.T) {
			root := t.TempDir()
			source := seedMoveEndpoint(t, root, "Old.md", "# Old\n")
			ref := seedMoveEndpoint(t, root, "Ref.md", "[[Old]]\n")
			alias := filepath.Join(root, "Alias.md")
			require.NoError(t, os.Symlink("Ref.md", alias))
			var err error
			if entrypoint == "rename" {
				_, err = actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
					Source: "Old.md", Target: "New.md", UpdateBacklinks: true,
				})
			} else {
				_, err = actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
					Moves: []actions.MoveRequest{{Source: "Old.md", Target: "New.md"}}, UpdateBacklinks: true,
				})
			}
			require.ErrorContains(t, err, "not a regular file: Alias.md")
			require.Equal(t, "# Old\n", readMoveEndpoint(t, source))
			require.Equal(t, "[[Old]]\n", readMoveEndpoint(t, ref))
			require.NoFileExists(t, filepath.Join(root, "New.md"))
			info, err := os.Lstat(alias)
			require.NoError(t, err)
			require.NotZero(t, info.Mode()&os.ModeSymlink)
			link, err := os.Readlink(alias)
			require.NoError(t, err)
			require.Equal(t, "Ref.md", link)
		})
	}
}
