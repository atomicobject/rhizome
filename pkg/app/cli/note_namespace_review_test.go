package actions_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestNoteNamespaceMovedIgnoredSourcePreservesVisibleBareTarget(t *testing.T) {
	for _, route := range []string{"rename", "move"} {
		t.Run(route, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(root, "ignored"), 0o755))
			for name, content := range map[string]string{
				".gitignore":     "ignored/\n",
				"ignored/Old.md": "# Hidden source\n",
				"Old.md":         "# Visible target\n",
				"Ref.md":         "[[Old]]\n[[ignored/Old]]\n",
			} {
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
			}
			var updates int
			if route == "rename" {
				result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
					Source: "ignored/Old.md", Target: "New.md", UpdateBacklinks: true,
				})
				require.NoError(t, err)
				require.Equal(t, validate.NamespaceCommitted, result.Mutation.Current.Decision)
				updates = result.LinkUpdates
			} else {
				result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
					Moves: []actions.MoveRequest{{Source: "ignored/Old.md", Target: "New.md"}}, UpdateBacklinks: true,
				})
				require.NoError(t, err)
				require.Equal(t, validate.NamespaceCommitted, result.Mutation.Current.Decision)
				updates = result.TotalLinkUpdates
			}
			ref, err := os.ReadFile(filepath.Join(root, "Ref.md"))
			require.NoError(t, err)
			require.Equal(t, "[[Old]]\n[[New]]\n", string(ref))
			require.Equal(t, 1, updates)
			visible, err := os.ReadFile(filepath.Join(root, "Old.md"))
			require.NoError(t, err)
			require.Equal(t, "# Visible target\n", string(visible))
			moved, err := os.ReadFile(filepath.Join(root, "New.md"))
			require.NoError(t, err)
			require.Equal(t, "# Hidden source\n", string(moved))
		})
	}
}

func TestNoteNamespaceLargeHeadingDiagnosticsDoNotBlockPublication(t *testing.T) {
	for _, route := range []string{"rename", "move"} {
		t.Run(route, func(t *testing.T) {
			root := t.TempDir()
			const pointers = 400
			require.NoError(t, os.WriteFile(filepath.Join(root, "Old.md"), []byte("# Old\n\n## Details\n"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "Ref.md"), []byte(strings.Repeat("[[Old#Details]]\n", pointers)), 0o644))
			if route == "rename" {
				result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
					Source: "Old.md", Target: "New.md", UpdateBacklinks: true,
				})
				require.NoError(t, err)
				require.Equal(t, validate.NamespaceCommitted, result.Mutation.Current.Decision)
				require.Equal(t, pointers, result.LinkUpdates)
				require.Len(t, result.HeadingPointers, pointers)
			} else {
				result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root),
					Moves: []actions.MoveRequest{{Source: "Old.md", Target: "New.md"}}, UpdateBacklinks: true,
				})
				require.NoError(t, err)
				require.Equal(t, validate.NamespaceCommitted, result.Mutation.Current.Decision)
				require.Equal(t, pointers, result.TotalLinkUpdates)
				require.Len(t, result.Results, 1)
				require.Len(t, result.Results[0].HeadingPointers, pointers)
				require.Len(t, result.HeadingPointers, pointers)
			}
			ref, err := os.ReadFile(filepath.Join(root, "Ref.md"))
			require.NoError(t, err)
			require.Equal(t, strings.Repeat("[[New#Details]]\n", pointers), string(ref))
			require.NoFileExists(t, filepath.Join(root, "Old.md"))
			moved, err := os.ReadFile(filepath.Join(root, "New.md"))
			require.NoError(t, err)
			require.Equal(t, "# Old\n\n## Details\n", string(moved))
		})
	}
}

func TestMoveNotesLargeBatchRetainsRequestOrder(t *testing.T) {
	root := t.TempDir()
	const count = 36
	moves := make([]actions.MoveRequest, 0, count)
	for i := count - 1; i >= 0; i-- {
		source := fmt.Sprintf("source-%03d-%s.md", i, strings.Repeat("&", 160))
		target := fmt.Sprintf("target-%03d-%s.md", i, strings.Repeat("&", 160))
		require.NoError(t, os.WriteFile(filepath.Join(root, source), []byte(fmt.Sprintf("# Source %d\n", i)), 0o644))
		moves = append(moves, actions.MoveRequest{Source: source, Target: target})
	}
	result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: moves,
	})
	require.NoError(t, err)
	require.Equal(t, validate.NamespaceCommitted, result.Mutation.Current.Decision)
	require.Len(t, result.Results, count)
	for i, move := range moves {
		require.Equal(t, move.Source, result.Results[i].Source)
		require.Equal(t, move.Target, result.Results[i].Target)
		require.NoFileExists(t, filepath.Join(root, move.Source))
		moved, err := os.ReadFile(filepath.Join(root, move.Target))
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("# Source %d\n", count-1-i), string(moved))
	}
}
