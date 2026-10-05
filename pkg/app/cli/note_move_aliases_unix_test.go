//go:build !windows

package actions_test

import (
	"os"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRenameAndMoveNotesAuthoredParentAliasReferences(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, duplicate := range []bool{false, true} {
			name := map[bool]string{false: "rename", true: "batch"}[batch] + "/" + map[bool]string{false: "unique basename", true: "duplicate basename"}[duplicate]
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				seedMoveEndpoint(t, root, "notes/Source.md", "original source\n")
				if duplicate {
					seedMoveEndpoint(t, root, "docs/Source.md", "unrelated source\n")
				}
				require.NoError(t, os.Symlink(filepath.Join(root, "notes"), filepath.Join(root, "alias")))
				body := "[[notes/Source]] [[alias/Source]] [[docs/Source]] [canonical](notes/Source.md) [authored](alias/Source.md)\n"
				ref := seedMoveEndpoint(t, root, "Ref.md", body+"[[Source]]\n")
				code := seedMoveEndpoint(t, root, "refs.go", "package fixture\n// "+body)
				config := coderefs.NewConfig(true, []string{"*.go"}, nil)
				wantUpdates := 5
				if duplicate {
					wantUpdates = 4
				}
				if batch {
					result, err := actions.MoveNotes(namespaceVault{path: root}, &obsidian.Uri{}, actions.MoveParams{
						NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: []actions.MoveRequest{{Source: "alias/Source.md", Target: "Moved.md"}}, UpdateBacklinks: true, CodeRefConfig: config,
					})
					require.NoError(t, err)
					require.Equal(t, wantUpdates, result.TotalLinkUpdates)
					require.Equal(t, 4, result.TotalCodeRefUpdates)
				} else {
					result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
						NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "alias/Source.md", Target: "Moved.md", UpdateBacklinks: true, CodeRefConfig: config,
					})
					require.NoError(t, err)
					require.Equal(t, wantUpdates, result.LinkUpdates)
					require.Equal(t, 4, result.CodeRefUpdates)
				}
				want := "[[Moved]] [[Moved]] [[docs/Source]] [canonical](Moved.md) [authored](Moved.md)\n"
				bare := "[[Moved]]\n"
				if duplicate {
					bare = "[[Source]]\n"
				}
				require.Equal(t, want+bare, readMoveEndpoint(t, ref))
				updatedCode := readMoveEndpoint(t, code)
				require.Equal(t, "package fixture\n// "+want, updatedCode)
				cachePaths := []string{"Moved.md"}
				if duplicate {
					cachePaths = append(cachePaths, "docs/Source.md")
				}
				refs, err := coderefs.ScanFile("refs.go", []byte(updatedCode), obsidian.BuildNotePathCache(cachePaths))
				require.NoError(t, err)
				moved := 0
				for _, ref := range refs {
					if ref.Target == "Moved.md" {
						moved++
					} else {
						require.Equal(t, "docs/Source.md", ref.Target)
					}
				}
				require.Equal(t, 4, moved)
			})
		}
	}
}
