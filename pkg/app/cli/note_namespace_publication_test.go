package actions_test

import (
	"os"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestNamespaceMutationsComposeSharedHostsInRequestOrder(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "rename", true: "batch"}[batch], func(t *testing.T) {
			root := t.TempDir()
			seedMoveEndpoint(t, root, "A.md", "# A\n[[A]] [[B]]\n")
			seedMoveEndpoint(t, root, "B.md", "# B\n[[A]] [[B]]\n")
			ref := seedMoveEndpoint(t, root, "Ref.md", "[[A|a]] [[B#Heading]] `[[A]]`\n")
			require.NoError(t, os.Chmod(filepath.Join(root, "A.md"), 0o600))
			require.NoError(t, os.Chmod(ref, 0o640))
			modes := make(map[string]os.FileMode)
			for _, path := range []string{"A.md", "B.md", "Ref.md"} {
				info, err := os.Stat(filepath.Join(root, path))
				require.NoError(t, err)
				modes[path] = info.Mode().Perm()
			}
			wantBPath, wantBLink := "B.md", "B"
			if batch {
				result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), UpdateBacklinks: true,
					Moves: []actions.MoveRequest{{Source: "B.md", Target: "Dir/B2.md"}, {Source: "A.md", Target: "A2.md"}},
				})
				require.NoError(t, err)
				require.Equal(t, validate.NamespaceCommitted, result.Mutation.Current.Decision)
				require.False(t, result.Mutation.Current.RecoveryPending)
				require.Len(t, result.Results, 2)
				require.Equal(t, "B.md", result.Results[0].Source)
				require.Equal(t, "A.md", result.Results[1].Source)
				require.Equal(t, 3, result.Results[0].LinkUpdates)
				require.Equal(t, 3, result.Results[1].LinkUpdates)
				require.Equal(t, 6, result.TotalLinkUpdates)
				wantBPath, wantBLink = "Dir/B2.md", "Dir/B2"
			} else {
				result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), UpdateBacklinks: true,
					Source: "A.md", Target: "A2.md",
				})
				require.NoError(t, err)
				require.Equal(t, validate.NamespaceCommitted, result.Mutation.Current.Decision)
				require.False(t, result.Mutation.Current.RecoveryPending)
				require.Equal(t, 3, result.LinkUpdates)
			}
			for path, want := range map[string]string{
				"A2.md":   "# A\n[[A2]] [[" + wantBLink + "]]\n",
				wantBPath: "# B\n[[A2]] [[" + wantBLink + "]]\n",
				"Ref.md":  "[[A2|a]] [[" + wantBLink + "#Heading]] `[[A]]`\n",
			} {
				got := readMoveEndpoint(t, filepath.Join(root, path))
				require.Equal(t, want, got)
			}
			for original, final := range map[string]string{"A.md": "A2.md", "B.md": wantBPath, "Ref.md": "Ref.md"} {
				info, err := os.Stat(filepath.Join(root, final))
				require.NoError(t, err)
				require.Equal(t, modes[original], info.Mode().Perm())
				if original != final {
					require.NoFileExists(t, filepath.Join(root, original))
				}
			}
		})
	}
}

func TestNamespaceOverwriteExcludesDiscardedDestinationBody(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "rename", true: "batch"}[batch], func(t *testing.T) {
			root := t.TempDir()
			seedMoveEndpoint(t, root, "Source.md", "# Source\n[[Source|self]]\n")
			seedMoveEndpoint(t, root, "Target.md", "discarded [[Source]] [[Source#discarded]]\n")
			ref := seedMoveEndpoint(t, root, "Ref.md", "[[Source]]\n")
			if batch {
				result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), UpdateBacklinks: true, Overwrite: true,
					Moves: []actions.MoveRequest{{Source: "Source.md", Target: "Target.md"}},
				})
				require.NoError(t, err)
				require.Equal(t, 2, result.TotalLinkUpdates)
				require.Empty(t, result.HeadingPointers)
			} else {
				result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), UpdateBacklinks: true, Overwrite: true,
					Source: "Source.md", Target: "Target.md",
				})
				require.NoError(t, err)
				require.Equal(t, 2, result.LinkUpdates)
				require.Empty(t, result.HeadingPointers)
			}
			require.NoFileExists(t, filepath.Join(root, "Source.md"))
			require.Equal(t, "# Source\n[[Target|self]]\n", readMoveEndpoint(t, filepath.Join(root, "Target.md")))
			require.Equal(t, "[[Target]]\n", readMoveEndpoint(t, ref))
		})
	}
}

func TestNamespaceMutationRequiresProjectionCollaboratorBeforePublication(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "rename", true: "batch"}[batch], func(t *testing.T) {
			root := t.TempDir()
			source := seedMoveEndpoint(t, root, "Old.md", "# Old\n")
			if batch {
				result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: namespaceTestMetadata(t), Moves: []actions.MoveRequest{{Source: "Old.md", Target: "New.md"}},
				})
				require.Error(t, err)
				require.Equal(t, validate.NamespaceNotStarted, result.Mutation.Current.Decision)
				require.Empty(t, result.Results)
			} else {
				result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					NoteMetadata: namespaceTestMetadata(t), Source: "Old.md", Target: "New.md",
				})
				require.Error(t, err)
				require.Equal(t, validate.NamespaceNotStarted, result.Mutation.Current.Decision)
				require.Empty(t, result.RenamedPath)
			}
			require.Equal(t, "# Old\n", readMoveEndpoint(t, source))
			require.NoFileExists(t, filepath.Join(root, "New.md"))
		})
	}
}
