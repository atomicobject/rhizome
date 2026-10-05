package actions_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func seedMoveEndpoint(t *testing.T, root, path, content string) string {
	t.Helper()
	abs := filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	return abs
}

func readMoveEndpoint(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

func TestRenameNoteRejectsIdenticalEndpointBeforeOverwrite(t *testing.T) {
	root := t.TempDir()
	source := seedMoveEndpoint(t, root, "Same.md", "original source\n")
	_, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "Same.md", Target: "Same.md", Overwrite: true,
	})
	require.Error(t, err)
	require.Equal(t, "original source\n", readMoveEndpoint(t, source))
}

func TestMoveNotesPreflightsCompleteEndpointSet(t *testing.T) {
	for _, state := range []string{"missing source", "occupied target", "source directory", "target directory", "duplicate source", "source target overlap", "cycle", "non-directory target parent"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			a := seedMoveEndpoint(t, root, "A.md", "original A\n")
			b := seedMoveEndpoint(t, root, "B.md", "original B\n")
			c := seedMoveEndpoint(t, root, "C.md", "original C\n")
			ref := seedMoveEndpoint(t, root, "Ref.md", "[[A]] [[B]] [[C]]\n")
			moves := []actions.MoveRequest{{Source: "A.md", Target: "dest/A.md"}, {Source: "B.md", Target: "dest/B.md"}}
			overwrite := false
			switch state {
			case "missing source":
				moves[1].Source = "missing.md"
			case "occupied target":
				moves[1].Target = "C.md"
			case "source directory":
				require.NoError(t, os.Mkdir(filepath.Join(root, "directory.md"), 0o755))
				moves[1].Source = "directory.md"
			case "target directory":
				seedMoveEndpoint(t, root, "directory.md/child.md", "keep child\n")
				moves[1].Target, overwrite = "directory.md", true
			case "duplicate source":
				moves[1].Source = "A.md"
			case "source target overlap":
				moves[0].Target, overwrite = "B.md", true
			case "cycle":
				moves[0].Target, moves[1].Target, overwrite = "B.md", "A.md", true
			case "non-directory target parent":
				seedMoveEndpoint(t, root, "blocked", "parent is a file\n")
				moves[1].Target = "blocked/B.md"
			}
			summary, err := actions.MoveNotes(namespaceVault{path: root}, &obsidian.Uri{}, actions.MoveParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: moves, Overwrite: overwrite, UpdateBacklinks: true,
			})
			require.Error(t, err)
			require.Empty(t, summary.Results)
			for path, want := range map[string]string{a: "original A\n", b: "original B\n", c: "original C\n", ref: "[[A]] [[B]] [[C]]\n"} {
				require.Equal(t, want, readMoveEndpoint(t, path))
			}
			require.NoDirExists(t, filepath.Join(root, "dest"))
			if state == "target directory" {
				require.Equal(t, "keep child\n", readMoveEndpoint(t, filepath.Join(root, "directory.md", "child.md")))
			}
		})
	}
}

func TestRenameNoteRejectsDirectoryBeforeOverwrite(t *testing.T) {
	root := t.TempDir()
	source := seedMoveEndpoint(t, root, "Source.md", "original source\n")
	child := seedMoveEndpoint(t, root, "Target.md/child.md", "original child\n")
	_, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "Source.md", Target: "Target.md", Overwrite: true,
	})
	require.Error(t, err)
	require.Equal(t, "original source\n", readMoveEndpoint(t, source))
	require.Equal(t, "original child\n", readMoveEndpoint(t, child))
}

func TestRenameNoteRejectsHardlinkEndpointBeforeOverwrite(t *testing.T) {
	root := t.TempDir()
	source := seedMoveEndpoint(t, root, "Source.md", "original source\n")
	target := filepath.Join(root, "Alias.md")
	require.NoError(t, os.Link(source, target))
	_, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "Source.md", Target: "Alias.md", Overwrite: true,
	})
	require.Error(t, err)
	require.Equal(t, "original source\n", readMoveEndpoint(t, source))
	require.Equal(t, "original source\n", readMoveEndpoint(t, target))
}

func TestMoveNotesRejectsHardlinkBatchEndpoints(t *testing.T) {
	for _, alias := range []string{"source", "target", "cross source"} {
		t.Run(alias, func(t *testing.T) {
			root := t.TempDir()
			a := seedMoveEndpoint(t, root, "A.md", "original A\n")
			b := seedMoveEndpoint(t, root, "B.md", "original B\n")
			moves := []actions.MoveRequest{{Source: "A.md", Target: "dest/A.md"}, {Source: "B.md", Target: "dest/B.md"}}
			switch alias {
			case "source":
				require.NoError(t, os.Link(a, filepath.Join(root, "linked.md")))
				moves[1].Source = "linked.md"
			case "target":
				first := seedMoveEndpoint(t, root, "first.md", "old target\n")
				require.NoError(t, os.Link(first, filepath.Join(root, "second.md")))
				moves[0].Target, moves[1].Target = "first.md", "second.md"
			case "cross source":
				require.NoError(t, os.Link(b, filepath.Join(root, "linked.md")))
				moves[0].Target = "linked.md"
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

func TestRenameNoteCaseOnlyBasenamePreservesSource(t *testing.T) {
	for _, git := range []bool{false, true} {
		for _, overwrite := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "filesystem", true: "git"}[git], map[bool]string{false: "no overwrite", true: "overwrite"}[overwrite]}, " "), func(t *testing.T) {
				root := t.TempDir()
				seedMoveEndpoint(t, root, "Same.md", "original source\n")
				if git {
					initGitRepo(t, root)
					require.NoError(t, runMoveGit(root, "add", "."))
					commitGitFixture(t, root)
				}
				result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "Same.md", Target: "same.md", Overwrite: overwrite,
				})
				require.NoError(t, err)
				require.Equal(t, "same.md", result.RenamedPath)
				require.Equal(t, git, result.GitHistoryPreserved)
				require.Equal(t, "original source\n", readMoveEndpoint(t, filepath.Join(root, "same.md")))
				entries, err := os.ReadDir(root)
				require.NoError(t, err)
				var names []string
				for _, entry := range entries {
					if !strings.HasPrefix(entry.Name(), ".") {
						names = append(names, entry.Name())
					}
				}
				require.Equal(t, []string{"same.md"}, names)
				if git {
					require.Equal(t, "same.md\n", moveGitOutput(t, root, "ls-files"))
					require.Equal(t, "original source\n", moveGitOutput(t, root, "show", ":same.md"))
				}
			})
		}
	}
}

func TestMoveNotesRejectsTargetAncestryBeforeMutation(t *testing.T) {
	for _, git := range []bool{false, true} {
		for _, parentFirst := range []bool{false, true} {
			name := map[bool]string{false: "filesystem", true: "git"}[git] + "/" + map[bool]string{false: "child first", true: "parent first"}[parentFirst]
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				a := seedMoveEndpoint(t, root, "A.md", "original A\n")
				b := seedMoveEndpoint(t, root, "B.md", "original B\n")
				ref := seedMoveEndpoint(t, root, "Ref.md", "[[A]] [[B]]\n")
				var index string
				if git {
					initGitRepo(t, root)
					require.NoError(t, runMoveGit(root, "add", "."))
					commitGitFixture(t, root)
					index = moveGitOutput(t, root, "ls-files", "--stage")
				}
				moves := []actions.MoveRequest{{Source: "A.md", Target: "New.md/A.md"}, {Source: "B.md", Target: "New.md"}}
				if parentFirst {
					moves[0], moves[1] = moves[1], moves[0]
				}
				result, err := actions.MoveNotes(namespaceVault{path: root}, &obsidian.Uri{}, actions.MoveParams{
					NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: moves, Overwrite: true, UpdateBacklinks: true,
				})
				require.Error(t, err)
				require.Empty(t, result.Results)
				require.Equal(t, "original A\n", readMoveEndpoint(t, a))
				require.Equal(t, "original B\n", readMoveEndpoint(t, b))
				require.Equal(t, "[[A]] [[B]]\n", readMoveEndpoint(t, ref))
				require.NoFileExists(t, filepath.Join(root, "New.md"))
				require.NoDirExists(t, filepath.Join(root, "New.md"))
				if git {
					require.Equal(t, index, moveGitOutput(t, root, "ls-files", "--stage"))
				}
			})
		}
	}
}

func TestMoveNotesAllowsSimilarTargetPrefixes(t *testing.T) {
	for _, git := range []bool{false, true} {
		t.Run(map[bool]string{false: "filesystem", true: "git"}[git], func(t *testing.T) {
			root := t.TempDir()
			seedMoveEndpoint(t, root, "A.md", "original A\n")
			seedMoveEndpoint(t, root, "B.md", "original B\n")
			if git {
				initGitRepo(t, root)
				require.NoError(t, runMoveGit(root, "add", "."))
				commitGitFixture(t, root)
			}
			result, err := actions.MoveNotes(namespaceVault{path: root}, &obsidian.Uri{}, actions.MoveParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: []actions.MoveRequest{{Source: "A.md", Target: "New.md"}, {Source: "B.md", Target: "New.md-other/B.md"}},
			})
			require.NoError(t, err)
			require.Len(t, result.Results, 2)
			require.Equal(t, "New.md", result.Results[0].Target)
			require.Equal(t, "New.md-other/B.md", result.Results[1].Target)
			require.Equal(t, "original A\n", readMoveEndpoint(t, filepath.Join(root, "New.md")))
			require.Equal(t, "original B\n", readMoveEndpoint(t, filepath.Join(root, "New.md-other/B.md")))
			if git {
				require.Equal(t, "New.md\nNew.md-other/B.md\n", moveGitOutput(t, root, "ls-files"))
			}
		})
	}
}

func TestRenameNoteCaseOnlyRequestedSourceSpelling(t *testing.T) {
	for _, git := range []bool{false, true} {
		t.Run(map[bool]string{false: "filesystem", true: "git"}[git], func(t *testing.T) {
			root := t.TempDir()
			source := seedMoveEndpoint(t, root, "SOURCE.md", "original source\n")
			_, lookupErr := os.Stat(filepath.Join(root, "Source.md"))
			if lookupErr != nil {
				require.True(t, os.IsNotExist(lookupErr))
			}
			if git {
				initGitRepo(t, root)
				require.NoError(t, runMoveGit(root, "add", "."))
				commitGitFixture(t, root)
			}
			result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Source: "Source.md", Target: "source.md",
			})
			if lookupErr != nil {
				// On a sensitive filesystem the requested source does not exist.
				require.ErrorIs(t, err, os.ErrNotExist)
				require.Equal(t, "original source\n", readMoveEndpoint(t, source))
				if git {
					require.Equal(t, "SOURCE.md\n", moveGitOutput(t, root, "ls-files"))
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, "source.md", result.RenamedPath)
			require.Equal(t, git, result.GitHistoryPreserved)
			require.Equal(t, "original source\n", readMoveEndpoint(t, filepath.Join(root, "source.md")))
			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			var names []string
			for _, entry := range entries {
				if !strings.HasPrefix(entry.Name(), ".") {
					names = append(names, entry.Name())
				}
			}
			require.Equal(t, []string{"source.md"}, names)
			if git {
				require.Equal(t, "source.md\n", moveGitOutput(t, root, "ls-files"))
				require.Equal(t, "original source\n", moveGitOutput(t, root, "show", ":source.md"))
			}
		})
	}
}

func TestMoveNotesTargetAncestryHonorsUnicodeCase(t *testing.T) {
	for _, names := range [][2]string{{"NewΣ.md", "Newς.md"}, {"NewS.md", "Newſ.md"}} {
		t.Run(names[0], func(t *testing.T) {
			root := t.TempDir()
			a := seedMoveEndpoint(t, root, "A.md", "original A\n")
			b := seedMoveEndpoint(t, root, "B.md", "original B\n")
			result, err := actions.MoveNotes(namespaceVault{path: root}, &obsidian.Uri{}, actions.MoveParams{
				NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, root), Moves: []actions.MoveRequest{{Source: "A.md", Target: names[0] + "/A.md"}, {Source: "B.md", Target: names[1]}}, Overwrite: true,
			})
			if paths.CaseEqual(names[0], names[1]) {
				require.Error(t, err)
				require.Empty(t, result.Results)
				require.Equal(t, "original A\n", readMoveEndpoint(t, a))
				require.Equal(t, "original B\n", readMoveEndpoint(t, b))
				require.NoDirExists(t, filepath.Join(root, names[0]))
			} else {
				require.NoError(t, err)
				require.Len(t, result.Results, 2)
				require.Equal(t, "original A\n", readMoveEndpoint(t, filepath.Join(root, names[0], "A.md")))
				require.Equal(t, "original B\n", readMoveEndpoint(t, filepath.Join(root, names[1])))
			}
		})
	}
}
