package actions_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/stretchr/testify/require"
)

func TestNoteNamespaceBacklinksUseFinalEligibility(t *testing.T) {
	for _, entrypoint := range []string{"rename", "move"} {
		for _, tc := range []struct {
			name, source, target, want string
			updates                    int
		}{
			{"from ignored directory", "ignored/Old.md", "Visible.md", "[[Visible]]", 2},
			{"into ignored directory", "Old.md", "ignored/New.md", "[[Old]]", 1},
		} {
			t.Run(entrypoint+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(root, "ignored"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n*.skip\n"), 0o644))
				body := "[[" + strings.TrimSuffix(tc.source, ".md") + "]]"
				for name, content := range map[string]string{tc.source: body, "Ref.md": body, "unrelated.skip": "not markdown"} {
					require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
				}
				indexer := namespaceTestMetadata(t)
				var updates int
				var skipped []string
				if entrypoint == "rename" {
					result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
						NoteMetadata: indexer, PostApplyRefresher: namespaceTestRefresher(t, root), Source: tc.source, Target: tc.target, UpdateBacklinks: true,
					})
					require.NoError(t, err)
					updates, skipped = result.LinkUpdates, result.Skipped
				} else {
					result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
						NoteMetadata: indexer, PostApplyRefresher: namespaceTestRefresher(t, root), Moves: []actions.MoveRequest{{Source: tc.source, Target: tc.target}}, UpdateBacklinks: true,
					})
					require.NoError(t, err)
					updates, skipped = result.TotalLinkUpdates, result.Skipped
				}
				moved, err := os.ReadFile(filepath.Join(root, tc.target))
				require.NoError(t, err)
				require.Equal(t, tc.want, string(moved))
				require.Equal(t, tc.updates, updates)
				require.Equal(t, []string{"unrelated.skip"}, skipped)
				ref, err := os.ReadFile(filepath.Join(root, "Ref.md"))
				require.NoError(t, err)
				require.Equal(t, "[["+strings.TrimSuffix(tc.target, ".md")+"]]", string(ref))
			})
		}
	}
}
