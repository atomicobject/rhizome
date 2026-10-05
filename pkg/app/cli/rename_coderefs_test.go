package actions_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameAndMoveNotes_LiteralCodeRefDestination(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		move   bool
	}{
		{name: "rename", target: "Budget$USD"},
		{name: "move", target: "Archive${1}/Budget$$", move: true},
		{name: "rename hash", target: "Budget#USD"},
		{name: "move parenthesis", target: "Archive/Budget)", move: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vaultDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "Old.md"), []byte("# Old\n"), 0o644))
			codeFile := filepath.Join(vaultDir, "refs.go")
			content := "package fixture\n// [[Old.md#part|cost$1]] [[Old]] [cost ${1}](Old.md#part) @Old,\n"
			require.NoError(t, os.WriteFile(codeFile, []byte(content), 0o644))
			config := coderefs.NewConfig(true, []string{"*.go"}, nil)
			targetPath := tc.target + ".md"

			if tc.move {
				result, err := actions.MoveNotes(moveStubVault{path: vaultDir}, &mocks.MockUriManager{}, actions.MoveParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, vaultDir),
					Moves:           []actions.MoveRequest{{Source: "Old", Target: tc.target}},
					UpdateBacklinks: true,
					CodeRefConfig:   config,
				})
				require.NoError(t, err)
				assert.Equal(t, targetPath, result.Results[0].Target)
				assert.Equal(t, 1, result.CodeFilesUpdated)
				assert.Equal(t, 4, result.TotalCodeRefUpdates)
			} else {
				result, err := actions.RenameNote(namespaceVault{path: vaultDir}, actions.RenameParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, vaultDir),
					Source:          "Old",
					Target:          tc.target,
					UpdateBacklinks: true,
					CodeRefConfig:   config,
				})
				require.NoError(t, err)
				assert.Equal(t, targetPath, result.RenamedPath)
				assert.Equal(t, 1, result.CodeFilesUpdated)
				assert.Equal(t, 4, result.CodeRefUpdates)
			}

			want := fmt.Sprintf("package fixture\n// [[%s#part|cost$1]] [[%s]] [cost ${1}](%s#part) [[%s]],\n", targetPath, tc.target, obsidian.EncodeMarkdownPath(targetPath), targetPath)
			if tc.name == "rename hash" {
				want = "package fixture\n// [cost$1](Budget%23USD.md#part) [Budget#USD](Budget%23USD.md) [cost ${1}](Budget%23USD.md#part) [Budget#USD](Budget%23USD.md),\n"
			}
			updated, err := os.ReadFile(codeFile)
			require.NoError(t, err)
			assert.Equal(t, want, string(updated))
			refs, err := coderefs.ScanFile("refs.go", updated, obsidian.BuildNotePathCache([]string{targetPath, "Budget.md", "Archive.md"}))
			require.NoError(t, err)
			require.Len(t, refs, 4)
			for _, ref := range refs {
				assert.Equal(t, targetPath, ref.Target)
			}
			note, err := os.ReadFile(filepath.Join(vaultDir, targetPath))
			require.NoError(t, err)
			assert.Equal(t, "# Old\n", string(note))
			_, err = os.Stat(filepath.Join(vaultDir, "Old.md"))
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}
