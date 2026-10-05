package ontology

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

type editRecoveryFixture struct {
	root, note, temp, backup, journalPath string
	journal                               editWriteJournal
}

func newEditRecoveryFixture(t *testing.T, phase string, receipt bool) editRecoveryFixture {
	t.Helper()
	vaultPaths, err := paths.NewVaultPaths(t.TempDir())
	require.NoError(t, err)
	root := vaultPaths.Root()
	f := editRecoveryFixture{root: root, note: filepath.Join(root, "notes", "a.md")}
	require.NoError(t, os.MkdirAll(filepath.Dir(f.note), 0o755))
	f.temp, f.backup = f.note+".rhizome-write-test", filepath.Join(filepath.Dir(f.note), ".a.md.rhizome-backup-test")
	require.NoError(t, os.WriteFile(f.note, []byte("new\n"), 0o640))
	require.NoError(t, os.WriteFile(f.temp, []byte("new\n"), 0o640))
	require.NoError(t, os.WriteFile(f.backup, []byte("old\n"), 0o640))
	var completion *editCompletionJournalEntry
	if receipt {
		completion, err = prepareEditCompletionArtifact(&vaultPaths, &CommitCompletionArtifact{
			Path: filepath.Join(root, ".rhizome", "edit-receipts", "receipt.json"), Content: []byte("receipt\n"),
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(completion.TargetPath, []byte("receipt\n"), 0o600))
	}
	f.journalPath, err = prepareEditWriteJournal(&vaultPaths, "recovery-test", []pendingWrite{{
		notePath: "notes/a.md", temp: f.temp, backup: f.backup,
		oldFingerprint: hashText("old\n"), newFingerprint: hashText("new\n"), mode: 0o640,
	}}, completion)
	require.NoError(t, err)
	if phase == "committed" {
		require.NoError(t, markEditWriteJournalCommitted(f.journalPath))
	}
	f.journal, err = readEditWriteJournal(f.journalPath)
	require.NoError(t, err)
	return f
}

func TestRecoverCommittedEditPreservesLaterSource(t *testing.T) {
	for _, receipt := range []bool{false, true} {
		for _, state := range []string{"unchanged", "edited", "restored old", "deleted", "directory deleted", "directory moved"} {
			t.Run(state+map[bool]string{false: " without receipt", true: " with receipt"}[receipt], func(t *testing.T) {
				f := newEditRecoveryFixture(t, "committed", receipt)
				current := "new\n"
				switch state {
				case "edited":
					current = "later authored bytes\n"
					require.NoError(t, os.WriteFile(f.note, []byte(current), 0o640))
				case "restored old":
					current = "old\n"
					require.NoError(t, os.WriteFile(f.note, []byte(current), 0o640))
				case "deleted":
					require.NoError(t, os.Remove(f.note))
				case "directory deleted":
					for _, path := range []string{f.note, f.temp, f.backup, filepath.Dir(f.note)} {
						require.NoError(t, os.Remove(path))
					}
				case "directory moved":
					require.NoError(t, os.Rename(filepath.Dir(f.note), filepath.Join(f.root, "moved")))
				}
				var mode os.FileMode
				if state == "unchanged" || state == "edited" || state == "restored old" {
					info, err := os.Stat(f.note)
					require.NoError(t, err)
					mode = info.Mode()
				}
				require.NoError(t, RecoverInterruptedEdits(f.root))
				require.NoError(t, RecoverInterruptedEdits(f.root), "recovery is idempotent")
				if mode != 0 {
					require.Equal(t, current, mustReadEditTransactionFile(t, f.note))
					info, err := os.Stat(f.note)
					require.NoError(t, err)
					require.Equal(t, mode, info.Mode())
				} else {
					require.NoFileExists(t, f.note)
				}
				for _, path := range []string{f.temp, f.backup, f.journalPath} {
					require.NoFileExists(t, path)
				}
				if state == "directory moved" {
					require.Equal(t, current, mustReadEditTransactionFile(t, filepath.Join(f.root, "moved", "a.md")))
					require.Equal(t, "old\n", mustReadEditTransactionFile(t, filepath.Join(f.root, "moved", filepath.Base(f.backup))))
					require.Equal(t, "new\n", mustReadEditTransactionFile(t, filepath.Join(f.root, "moved", filepath.Base(f.temp))))
				}
				if receipt {
					require.Equal(t, "receipt\n", mustReadEditTransactionFile(t, f.journal.Completion.TargetPath))
					require.NoFileExists(t, f.journal.Completion.TempPath)
				}
			})
		}
	}
}

func TestRecoverCommittedEditPreflightsAllEvidence(t *testing.T) {
	states := []string{"changed backup", "changed temp", "changed receipt temp", "changed receipt", "missing receipt", "artifact directory", "escaped artifact", "invalid name", "noncanonical note", "overlapping source", "unknown phase"}
	if paths.CaseEqual("A", "a") {
		states = append(states, "overlapping source case")
	}
	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			f := newEditRecoveryFixture(t, "committed", true)
			keep := f.temp
			switch state {
			case "changed backup":
				require.NoError(t, os.WriteFile(f.backup, []byte("replacement backup\n"), 0o640))
			case "changed temp":
				require.NoError(t, os.WriteFile(f.temp, []byte("replacement temp\n"), 0o640))
				keep = f.backup
			case "changed receipt temp":
				require.NoError(t, os.WriteFile(f.journal.Completion.TempPath, []byte("other receipt\n"), 0o600))
			case "changed receipt":
				require.NoError(t, os.WriteFile(f.journal.Completion.TargetPath, []byte("other receipt\n"), 0o600))
			case "missing receipt":
				require.NoError(t, os.Remove(f.journal.Completion.TargetPath))
			case "artifact directory":
				require.NoError(t, os.Remove(f.backup))
				require.NoError(t, os.Mkdir(f.backup, 0o700))
			case "escaped artifact":
				f.journal.Entries[0].BackupPath = filepath.Join(t.TempDir(), filepath.Base(f.backup))
			case "invalid name":
				f.journal.Entries[0].BackupPath = filepath.Join(filepath.Dir(f.note), "other")
			case "noncanonical note":
				f.journal.Entries[0].NotePath = "notes/../notes/a.md"
			case "overlapping source", "overlapping source case":
				base := filepath.Base(f.temp)
				if state == "overlapping source case" {
					base = strings.ToUpper(base)
				}
				f.journal.Entries = append(f.journal.Entries, editWriteJournalEntry{
					NotePath: "notes/" + base, TempPath: filepath.Join(filepath.Dir(f.note), base+".rhizome-write-second"), BackupPath: filepath.Join(filepath.Dir(f.note), "."+base+".rhizome-backup-second"),
					OldFingerprint: hashText("new\n"), NewFingerprint: hashText("new\n"),
				})
			case "unknown phase":
				f.journal.Phase = "other"
			}
			require.NoError(t, writeEditWriteJournal(f.journalPath, f.journal))
			evidence := make(map[string]string)
			for _, path := range []string{f.journalPath, f.temp, f.backup, f.journal.Completion.TargetPath, f.journal.Completion.TempPath} {
				if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
					evidence[path] = mustReadEditTransactionFile(t, path)
				}
			}
			require.Error(t, RecoverInterruptedEdits(f.root))
			for path, content := range evidence {
				require.Equal(t, content, mustReadEditTransactionFile(t, path))
			}
			require.FileExists(t, keep, "preflight cannot remove valid evidence before finding an invalid artifact")
			require.FileExists(t, f.journalPath)
			require.Equal(t, "new\n", mustReadEditTransactionFile(t, f.note))
		})
	}
}

func TestRecoverEditToleratesRemovedArtifacts(t *testing.T) {
	for _, phase := range []string{"committed", "prepared"} {
		t.Run(phase, func(t *testing.T) {
			f := newEditRecoveryFixture(t, phase, true)
			for _, path := range []string{f.temp, f.backup, f.journal.Completion.TempPath} {
				require.NoError(t, os.Remove(path))
			}
			require.NoError(t, RecoverInterruptedEdits(f.root))
			require.NoError(t, RecoverInterruptedEdits(f.root))
			require.Equal(t, "new\n", mustReadEditTransactionFile(t, f.note))
			require.Equal(t, "receipt\n", mustReadEditTransactionFile(t, f.journal.Completion.TargetPath))
			require.NoFileExists(t, f.journalPath)
		})
	}
}

func TestRecoverPreparedEditKeepsAmbiguousSourceAndMissingParent(t *testing.T) {
	for _, state := range []string{"edited", "directory moved"} {
		t.Run(state, func(t *testing.T) {
			f := newEditRecoveryFixture(t, "prepared", false)
			if state == "edited" {
				require.NoError(t, os.WriteFile(f.note, []byte("unrelated\n"), 0o640))
			} else {
				require.NoError(t, os.Rename(filepath.Dir(f.note), filepath.Join(f.root, "moved")))
			}
			require.Error(t, RecoverInterruptedEdits(f.root))
			require.FileExists(t, f.journalPath)
			if state == "edited" {
				require.Equal(t, "unrelated\n", mustReadEditTransactionFile(t, f.note))
				require.FileExists(t, f.backup)
				require.FileExists(t, f.temp)
			} else {
				require.Equal(t, "old\n", mustReadEditTransactionFile(t, filepath.Join(f.root, "moved", filepath.Base(f.backup))))
			}
		})
	}
}

func TestRecoverPreparedEditPreflightsEveryRollbackBeforeMutation(t *testing.T) {
	f := newEditRecoveryFixture(t, "prepared", false)
	other := filepath.Join(f.root, "notes", "b.md")
	backup := filepath.Join(filepath.Dir(other), ".b.md.rhizome-backup-test")
	temp := other + ".rhizome-write-test"
	require.NoError(t, os.WriteFile(other, []byte("other old\n"), 0o640))
	require.NoError(t, os.WriteFile(backup, []byte("replaced backup\n"), 0o640))
	require.NoError(t, os.WriteFile(temp, []byte("other new\n"), 0o640))
	f.journal.Entries = append(f.journal.Entries, editWriteJournalEntry{
		NotePath: "notes/b.md", BackupPath: backup, TempPath: temp,
		OldFingerprint: hashText("other old\n"), NewFingerprint: hashText("other new\n"),
	})
	require.NoError(t, writeEditWriteJournal(f.journalPath, f.journal))
	require.ErrorContains(t, RecoverInterruptedEdits(f.root), "cannot verify artifact")
	require.Equal(t, "new\n", mustReadEditTransactionFile(t, f.note))
	require.Equal(t, "other old\n", mustReadEditTransactionFile(t, other))
	for _, artifact := range []string{f.journalPath, f.backup, f.temp, backup, temp} {
		require.FileExists(t, artifact)
	}
}

func TestRecoverPreparedEditPreservesUnchangedSourceWithBackupReservation(t *testing.T) {
	f := newEditRecoveryFixture(t, "prepared", false)
	f.journal.Entries[0].OldFingerprint = f.journal.Entries[0].NewFingerprint
	require.NoError(t, os.WriteFile(f.backup, nil, 0o640))
	require.NoError(t, writeEditWriteJournal(f.journalPath, f.journal))
	require.NoError(t, RecoverInterruptedEdits(f.root))
	require.Equal(t, "new\n", mustReadEditTransactionFile(t, f.note))
	require.NoFileExists(t, f.journalPath)
}
