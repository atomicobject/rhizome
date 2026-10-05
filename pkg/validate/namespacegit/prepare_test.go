package namespacegit_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate/namespacegit"
	"github.com/stretchr/testify/require"
)

func TestPreparationMatchesActualGitMoves(t *testing.T) {
	for _, variant := range []string{"ordinary", "dirty", "staged-dirty", "overwrite", "case-only", "executable", "literal-pathspec", "untracked", "mixed", "split", "v4", "flags", "merge-stages", "resolve-undo", "sparse", "global-config"} {
		t.Run(variant, func(t *testing.T) {
			f := newFixture(t)
			move := namespacegit.Move{Source: "notes/Old.md", Destination: "notes/New.md"}
			if variant != "ordinary" && variant != "dirty" {
				writeFile(t, f.root, move.Source, "staged source\n", 0o644)
				f.git(t, "add", move.Source)
			}
			if variant != "ordinary" {
				writeFile(t, f.root, move.Source, "dirty source\n", 0o644)
			}
			switch variant {
			case "overwrite":
				move.Destination, move.Overwrite = "notes/Existing.md", true
			case "case-only":
				move.Destination = "notes/old.md"
			case "executable":
				if runtime.GOOS == "windows" {
					t.Skip("Windows does not expose executable permission bits")
				}
				require.NoError(t, os.Chmod(filepath.Join(f.root, move.Source), 0o755))
				f.git(t, "add", move.Source)
				writeFile(t, f.root, move.Source, "dirty executable\n", 0o755)
			case "literal-pathspec":
				move.Source, move.Destination = "notes/[Old].md", "notes/[New].md"
				writeFile(t, f.root, move.Source, "literal source\n", 0o644)
				f.git(t, "add", move.Source)
			case "split", "flags", "v4":
				f.git(t, "update-index", "--assume-unchanged", "Assumed.md")
				f.git(t, "update-index", "--skip-worktree", "Skipped.md")
				writeFile(t, f.root, "Intent.md", "intent only\n", 0o644)
				f.git(t, "add", "-N", "Intent.md")
				if variant == "split" {
					f.git(t, "config", "core.splitIndex", "true")
					f.git(t, "update-index", "--split-index")
				}
				if variant == "v4" {
					f.git(t, "update-index", "--index-version", "4")
				}
			case "merge-stages", "resolve-undo":
				base := strings.TrimSpace(f.git(t, "rev-parse", "HEAD:Conflict.md"))
				ours, theirs := hashObject(t, f, "ours\n"), hashObject(t, f, "theirs\n")
				entries := "0 " + strings.Repeat("0", len(base)) + "\tConflict.md\n100644 " + base + " 1\tConflict.md\n100644 " + ours + " 2\tConflict.md\n100644 " + theirs + " 3\tConflict.md\n"
				cmd := f.command("", "update-index", "--index-info")
				cmd.Stdin = strings.NewReader(entries)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				if variant == "resolve-undo" {
					writeFile(t, f.root, "Conflict.md", "resolved\n", 0o644)
					f.git(t, "add", "Conflict.md")
				}
			case "sparse":
				f.git(t, "sparse-checkout", "set", "--cone", "--sparse-index", "notes")
			case "global-config":
				if runtime.GOOS != "windows" {
					require.NoError(t, os.Chmod(filepath.Join(f.root, move.Source), 0o755))
					f.git(t, "add", move.Source)
					require.NoError(t, os.Chmod(filepath.Join(f.root, move.Source), 0o644))
				}
				f.global = filepath.Join(canonicalTemp(t), "global-config")
				require.NoError(t, os.WriteFile(f.global, []byte("[core]\n splitIndex = true\n fileMode = false\n ignoreCase = true\n trustCTime = false\n[index]\n version = 4\n"), 0o600))
				f.git(t, "config", "--local", "--unset", "core.filemode")
				f.git(t, "update-index", "--split-index")
				t.Setenv("GIT_CONFIG_GLOBAL", f.global)
			}
			moves := []namespacegit.Move{move}
			if variant == "mixed" || variant == "untracked" {
				writeFile(t, f.root, "notes/Loose.md", "untracked source\n", 0o644)
				loose := namespacegit.Move{Source: "notes/Loose.md", Destination: "notes/NewLoose.md"}
				if variant == "untracked" {
					moves = []namespacegit.Move{loose}
				} else {
					moves = append(moves, loose)
				}
			}
			original := semantics(t, f, "")
			originalTree := ""
			if variant != "merge-stages" {
				copy, err := os.ReadFile(filepath.Join(f.root, ".git", "index"))
				require.NoError(t, err)
				treeIndex := filepath.Join(canonicalTemp(t), "tree.index")
				require.NoError(t, os.WriteFile(treeIndex, copy, 0o600))
				originalTree = f.indexGit(t, treeIndex, "-c", "core.splitIndex=false", "-c", "index.sparse=false", "write-tree")
			}
			raw, err := os.ReadFile(filepath.Join(f.root, ".git", "index"))
			require.NoError(t, err)
			before := inventory(t, f.root)
			direct := cloneFixture(t, f)
			var expectedMoves []namespacegit.Move
			for _, move := range moves {
				args := []string{"mv"}
				if move.Overwrite {
					args = append(args, "-f")
				}
				args = append(args, "--", move.Source, move.Destination)
				if output, err := direct.command("", args...).CombinedOutput(); err == nil {
					expectedMoves = append(expectedMoves, move)
				} else {
					require.Contains(t, string(output), "not under version control")
					require.NoError(t, os.Rename(filepath.Join(direct.root, move.Source), filepath.Join(direct.root, move.Destination)))
				}
			}
			prepared, err := namespacegit.Prepare(context.Background(), f.root, canonicalTemp(t), moves, originalFiles(t, f.root, moves))
			require.NoError(t, err)
			assertLiveUnchanged(t, before, inventory(t, f.root))
			if len(expectedMoves) == 0 {
				require.Nil(t, prepared)
				return
			}
			require.NotNil(t, prepared)
			require.Equal(t, expectedMoves, prepared.GitMoves)
			require.Equal(t, filepath.Join(f.root, ".git", "index"), prepared.IndexPath)
			require.Equal(t, prepared.IndexPath+".lock", prepared.LockPath)
			require.Equal(t, before[".git/index"].Hash, prepared.RawOriginal.Hash)
			require.Equal(t, before[".git/index"].Mode, prepared.RawOriginal.Mode)
			rollback := installSnapshot(t, f, prepared.Rollback)
			candidate := installSnapshot(t, f, prepared.Candidate)
			if variant == "v4" {
				require.Equal(t, "4\n", rollback.git(t, "-c", "core.splitIndex=false", "update-index", "--show-index-version"))
				require.Equal(t, "4\n", candidate.git(t, "-c", "core.splitIndex=false", "update-index", "--show-index-version"))
			}
			if variant == "split" {
				writeFile(t, rollback.root, "raw.index", string(raw), 0o600)
				_, err := rollback.command(filepath.Join(rollback.root, "raw.index"), "ls-files", "--stage").CombinedOutput()
				require.Error(t, err, "raw split index must require its former dependency")
				require.NotEqual(t, prepared.RawOriginal.Hash, prepared.Rollback.Hash)
			}
			require.Equal(t, original, semantics(t, rollback, ""))
			require.Equal(t, semantics(t, direct, ""), semantics(t, candidate, ""))
			if variant != "merge-stages" {
				require.Equal(t, originalTree, rollback.git(t, "-c", "index.sparse=false", "write-tree"))
				require.Equal(t, direct.git(t, "write-tree"), candidate.git(t, "-c", "index.sparse=false", "write-tree"))
				candidate.git(t, "cat-file", "-e", strings.TrimSpace(candidate.git(t, "write-tree")))
			}
		})
	}
}

func hashObject(t *testing.T, f gitFixture, content string) string {
	t.Helper()
	cmd := f.command("", "hash-object", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(content)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return strings.TrimSpace(string(output))
}

func TestUnbornAndAbsentIndexes(t *testing.T) {
	for _, variant := range []string{"unborn-tracked", "unborn-untracked", "absent"} {
		t.Run(variant, func(t *testing.T) {
			f := gitFixture{root: canonicalTemp(t)}
			f.git(t, "init", "-q")
			writeFile(t, f.root, "Old.md", "staged source\n", 0o644)
			if variant != "unborn-untracked" {
				f.git(t, "add", "Old.md")
			}
			if variant == "absent" {
				f.git(t, "-c", "user.name=Synthetic", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture")
				require.NoError(t, os.Rename(filepath.Join(f.root, ".git", "index"), filepath.Join(canonicalTemp(t), "saved.index")))
			}
			writeFile(t, f.root, "Old.md", "dirty source\n", 0o644)
			moves := []namespacegit.Move{{Source: "Old.md", Destination: "New.md"}}
			before := inventory(t, f.root)
			prepared, err := namespacegit.Prepare(context.Background(), f.root, canonicalTemp(t), moves, originalFiles(t, f.root, moves))
			require.NoError(t, err)
			assertLiveUnchanged(t, before, inventory(t, f.root))
			if variant != "unborn-tracked" {
				require.Nil(t, prepared)
				return
			}
			candidate := installSnapshot(t, f, prepared.Candidate)
			require.Equal(t, "staged source\n", candidate.git(t, "show", ":New.md"))
			candidate.git(t, "write-tree")
		})
	}
}
