package namespacegit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate/namespacegit"
	"github.com/stretchr/testify/require"
)

func TestMovedSourceStateMatchesActualGit(t *testing.T) {
	for _, state := range []string{"assume-unchanged", "skip-worktree", "intent-to-add", "merge-stages", "resolve-undo"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t)
			move := namespacegit.Move{Source: "notes/Old.md", Destination: "notes/New.md"}
			switch state {
			case "assume-unchanged", "skip-worktree":
				f.git(t, "update-index", "--"+state, move.Source)
			case "intent-to-add":
				move.Source = "notes/Intent.md"
				writeFile(t, f.root, move.Source, "intent body\n", 0o644)
				f.git(t, "add", "-N", move.Source)
			case "merge-stages", "resolve-undo":
				base := strings.TrimSpace(f.git(t, "rev-parse", "HEAD:"+move.Source))
				ours, theirs := hashObject(t, f, "ours\n"), hashObject(t, f, "theirs\n")
				entries := "0 " + strings.Repeat("0", len(base)) + "\t" + move.Source + "\n100644 " + base + " 1\t" + move.Source + "\n100644 " + ours + " 2\t" + move.Source + "\n100644 " + theirs + " 3\t" + move.Source + "\n"
				cmd := f.command("", "update-index", "--index-info")
				cmd.Stdin = strings.NewReader(entries)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				if state == "resolve-undo" {
					writeFile(t, f.root, move.Source, "resolved source\n", 0o644)
					f.git(t, "add", move.Source)
				}
			}
			writeFile(t, f.root, move.Source, "dirty moved source\n", 0o644)
			original := semantics(t, f, "")
			before := inventory(t, f.root)
			direct := cloneFixture(t, f)
			output, directErr := direct.command("", "mv", "--", move.Source, move.Destination).CombinedOutput()
			prepared, err := namespacegit.Prepare(context.Background(), f.root, canonicalTemp(t), []namespacegit.Move{move}, originalFiles(t, f.root, []namespacegit.Move{move}))
			assertLiveUnchanged(t, before, inventory(t, f.root))
			if directErr != nil {
				// M1 falls back to a filesystem move when Git refuses the source;
				// preparation must not claim history or return a partial candidate.
				t.Logf("direct Git refuses %s: %s", state, output)
				require.ErrorIs(t, err, namespacegit.ErrFallback)
				require.Nil(t, prepared)
				require.NoError(t, os.Rename(filepath.Join(direct.root, move.Source), filepath.Join(direct.root, move.Destination)))
				require.Equal(t, original, semantics(t, direct, ""))
				return
			}
			require.NoError(t, err)
			require.NotNil(t, prepared)
			require.Equal(t, []namespacegit.Move{move}, prepared.GitMoves)
			rollback := installSnapshot(t, f, prepared.Rollback)
			candidate := installSnapshot(t, f, prepared.Candidate)
			require.Equal(t, original, semantics(t, rollback, ""), "original staging must remain independently restorable")
			require.Equal(t, semantics(t, direct, ""), semantics(t, candidate, ""), "moved entry must match actual Git flags/stages/resolve-undo")
			t.Logf("direct Git accepts moved-source %s", state)
		})
	}
}
