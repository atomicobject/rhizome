//go:build !windows

package ontology

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoverPreparedEditPersistsCommitBeforeCleanupFailure(t *testing.T) {
	f := newEditRecoveryFixture(t, "prepared", true)
	noteDir := filepath.Dir(f.note)
	require.NoError(t, os.Chmod(noteDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(noteDir, 0o755) })
	err := RecoverInterruptedEdits(f.root)
	require.ErrorIs(t, err, os.ErrPermission, "exercise an actual artifact removal failure")
	journal, err := readEditWriteJournal(f.journalPath)
	require.NoError(t, err)
	require.Equal(t, "committed", journal.Phase)
	require.FileExists(t, f.backup)
	require.FileExists(t, f.temp)
	require.NoError(t, os.Chmod(noteDir, 0o755))
	require.NoError(t, os.WriteFile(f.note, []byte("later edit after interrupted cleanup\n"), 0o640))
	require.NoError(t, RecoverInterruptedEdits(f.root))
	require.NoError(t, RecoverInterruptedEdits(f.root))
	require.Equal(t, "later edit after interrupted cleanup\n", mustReadEditTransactionFile(t, f.note))
	require.Equal(t, "receipt\n", mustReadEditTransactionFile(t, journal.Completion.TargetPath))
	require.NoFileExists(t, f.journalPath)
}

func TestRecoverEditRejectsReplacedArtifactsAndParents(t *testing.T) {
	for _, state := range []string{"symlink backup", "hardlink backup", "symlink receipt", "symlink parent", "non-directory parent"} {
		t.Run(state, func(t *testing.T) {
			f := newEditRecoveryFixture(t, "committed", true)
			outside := filepath.Join(t.TempDir(), "outside")
			require.NoError(t, os.WriteFile(outside, []byte("old\n"), 0o640))
			keep := f.temp
			switch state {
			case "symlink backup", "hardlink backup":
				require.NoError(t, os.Remove(f.backup))
				if state == "symlink backup" {
					require.NoError(t, os.Symlink(outside, f.backup))
				} else {
					require.NoError(t, os.Link(outside, f.backup))
				}
			case "symlink receipt":
				require.NoError(t, os.WriteFile(outside, []byte("receipt\n"), 0o640))
				require.NoError(t, os.Remove(f.journal.Completion.TargetPath))
				require.NoError(t, os.Symlink(outside, f.journal.Completion.TargetPath))
			case "symlink parent", "non-directory parent":
				moved := filepath.Join(f.root, "moved")
				require.NoError(t, os.Rename(filepath.Dir(f.note), moved))
				keep = filepath.Join(moved, filepath.Base(f.temp))
				if state == "symlink parent" {
					require.NoError(t, os.Symlink(moved, filepath.Dir(f.note)))
				} else {
					require.NoError(t, os.WriteFile(filepath.Dir(f.note), []byte("replacement parent"), 0o640))
				}
			}
			require.Error(t, RecoverInterruptedEdits(f.root))
			require.FileExists(t, keep)
			require.FileExists(t, f.journalPath)
			require.FileExists(t, outside)
		})
	}
}

func TestRecoverCommittedEditDoesNotFollowReplacedNote(t *testing.T) {
	f := newEditRecoveryFixture(t, "committed", false)
	outside := filepath.Join(t.TempDir(), "later-note.md")
	require.NoError(t, os.WriteFile(outside, []byte("later note\n"), 0o640))
	require.NoError(t, os.Remove(f.note))
	require.NoError(t, os.Symlink(outside, f.note))
	require.NoError(t, RecoverInterruptedEdits(f.root))
	info, err := os.Lstat(f.note)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
	require.Equal(t, "later note\n", mustReadEditTransactionFile(t, outside))
	require.NoFileExists(t, f.journalPath)
}
