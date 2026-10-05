package ontology

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestRecoverInterruptedEditsContextWaitsForWriterBeforeReplayingJournal(t *testing.T) {
	root := paths.ResolveSymlinks(t.TempDir()).String()
	notePath := filepath.Join(root, "a.md")
	backupPath := filepath.Join(root, ".a.md.rhizome-backup-test")
	tempPath := filepath.Join(root, "a.md.rhizome-write-test")
	require.NoError(t, os.WriteFile(notePath, []byte("new\n"), 0o644))
	require.NoError(t, os.WriteFile(backupPath, []byte("old\n"), 0o644))
	require.NoError(t, os.WriteFile(tempPath, []byte("new\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.md"), []byte("other old\n"), 0o644))
	journalDir := filepath.Join(root, ".rhizome", "edit-journal")
	require.NoError(t, os.MkdirAll(journalDir, 0o700))
	journalPath := filepath.Join(journalDir, "partial.json")
	require.NoError(t, writeEditWriteJournal(journalPath, editWriteJournal{
		Version: 1, TransactionID: "partial", Phase: "prepared", Entries: []editWriteJournalEntry{{
			NotePath: "a.md", TempPath: tempPath, BackupPath: backupPath,
			OldFingerprint: hashText("old\n"), NewFingerprint: hashText("new\n"),
		}, {
			NotePath: "b.md", TempPath: filepath.Join(root, "b.md.rhizome-write-test"),
			BackupPath:     filepath.Join(root, ".b.md.rhizome-backup-test"),
			OldFingerprint: hashText("other old\n"), NewFingerprint: hashText("other new\n"),
		}},
	}))
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RecoverInterruptedEditsContext(ctx, root) }()
	select {
	case err := <-done:
		t.Fatalf("recovery returned while a writer held the vault: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.Equal(t, "new\n", mustReadEditTransactionFile(t, notePath))
	require.FileExists(t, journalPath)
	require.NoError(t, release())
	require.NoError(t, <-done)
	require.Equal(t, "old\n", mustReadEditTransactionFile(t, notePath))
	require.NoFileExists(t, journalPath)
}

func TestRecoverEditWriteJournalRollsBackPartialPublication(t *testing.T) {
	root := paths.ResolveSymlinks(t.TempDir()).String()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	oldA, oldB := "old a\n", "old b\n"
	newA, newB := "new a\n", "new b\n"
	aPath := filepath.Join(root, "notes", "a.md")
	bPath := filepath.Join(root, "notes", "b.md")
	aBackup := filepath.Join(root, "notes", ".a.md.rhizome-backup-test")
	bBackup := filepath.Join(root, "notes", ".b.md.rhizome-backup-test")
	bTemp := filepath.Join(root, "notes", "b.md.rhizome-write-test")
	require.NoError(t, os.WriteFile(aPath, []byte(newA), 0o644))
	require.NoError(t, os.WriteFile(aBackup, []byte(oldA), 0o644))
	require.NoError(t, os.WriteFile(bPath, []byte(oldB), 0o644))
	require.NoError(t, os.WriteFile(bBackup, nil, 0o644))
	require.NoError(t, os.WriteFile(bTemp, []byte(newB), 0o644))
	journal := editWriteJournal{Version: 1, TransactionID: "partial", Phase: "prepared", Entries: []editWriteJournalEntry{
		{NotePath: "notes/a.md", TempPath: filepath.Join(root, "notes", "a.md.rhizome-write-test"), BackupPath: aBackup, OldFingerprint: hashText(oldA), NewFingerprint: hashText(newA)},
		{NotePath: "notes/b.md", TempPath: bTemp, BackupPath: bBackup, OldFingerprint: hashText(oldB), NewFingerprint: hashText(newB)},
	}}
	journalDir := filepath.Join(root, ".rhizome", "edit-journal")
	require.NoError(t, os.MkdirAll(journalDir, 0o700))
	journalPath := filepath.Join(journalDir, "partial.json")
	require.NoError(t, writeEditWriteJournal(journalPath, journal))

	require.NoError(t, recoverEditWriteJournals(&vaultPaths))
	require.Equal(t, oldA, mustReadEditTransactionFile(t, aPath))
	require.Equal(t, oldB, mustReadEditTransactionFile(t, bPath))
	require.NoFileExists(t, journalPath)
	require.NoFileExists(t, aBackup)
	require.NoFileExists(t, bTemp)
}

func TestRecoverEditWriteJournalPreservesUnexpectedExternalChange(t *testing.T) {
	root := paths.ResolveSymlinks(t.TempDir()).String()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	vaultPaths, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	path := filepath.Join(root, "notes", "a.md")
	temp := filepath.Join(root, "notes", "a.md.rhizome-write-test")
	backup := filepath.Join(root, "notes", ".a.md.rhizome-backup-test")
	require.NoError(t, os.WriteFile(path, []byte("external\n"), 0o644))
	require.NoError(t, os.WriteFile(temp, []byte("new\n"), 0o644))
	require.NoError(t, os.WriteFile(backup, []byte("old\n"), 0o644))
	journalDir := filepath.Join(root, ".rhizome", "edit-journal")
	require.NoError(t, os.MkdirAll(journalDir, 0o700))
	journalPath := filepath.Join(journalDir, "external.json")
	require.NoError(t, writeEditWriteJournal(journalPath, editWriteJournal{Version: 1, TransactionID: "external", Phase: "prepared", Entries: []editWriteJournalEntry{{
		NotePath: "notes/a.md", TempPath: temp, BackupPath: backup,
		OldFingerprint: hashText("old\n"), NewFingerprint: hashText("new\n"),
	}}}))

	err = recoverEditWriteJournals(&vaultPaths)
	require.ErrorContains(t, err, "changed outside")
	require.Equal(t, "external\n", mustReadEditTransactionFile(t, path))
	require.FileExists(t, journalPath)
}

func TestRecoverEditWriteJournalTreatsCompletionReceiptAsPartOfPublication(t *testing.T) {
	for _, test := range []struct {
		name             string
		phase            string
		publishReceipt   bool
		wantNoteContents string
	}{
		{name: "missing receipt rolls notes back", phase: "prepared", wantNoteContents: "old\n"},
		{name: "published receipt completes transaction", phase: "committed", publishReceipt: true, wantNoteContents: "new\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := paths.ResolveSymlinks(t.TempDir()).String()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
			receiptDir := filepath.Join(root, ".rhizome", "edit-receipts")
			require.NoError(t, os.MkdirAll(receiptDir, 0o700))
			vaultPaths, err := paths.NewVaultPaths(root)
			require.NoError(t, err)
			notePath := filepath.Join(root, "notes", "a.md")
			backupPath := filepath.Join(root, "notes", ".a.md.rhizome-backup-test")
			tempPath := filepath.Join(root, "notes", "a.md.rhizome-write-test")
			require.NoError(t, os.WriteFile(notePath, []byte("new\n"), 0o644))
			require.NoError(t, os.WriteFile(backupPath, []byte("old\n"), 0o644))
			receiptPath := filepath.Join(receiptDir, "receipt.json")
			receiptTemp := filepath.Join(receiptDir, ".completion-test")
			require.NoError(t, os.WriteFile(tempPath, []byte("new\n"), 0o600))
			require.NoError(t, os.WriteFile(receiptTemp, []byte("receipt"), 0o600))
			if test.publishReceipt {
				require.NoError(t, os.WriteFile(receiptPath, []byte("receipt"), 0o600))
			}
			journal := editWriteJournal{
				Version: 1, TransactionID: "receipt", Phase: test.phase,
				Entries: []editWriteJournalEntry{{
					NotePath: "notes/a.md", TempPath: tempPath, BackupPath: backupPath,
					OldFingerprint: hashText("old\n"), NewFingerprint: hashText("new\n"),
				}},
				Completion: &editCompletionJournalEntry{
					TargetPath: receiptPath, TempPath: receiptTemp, Fingerprint: hashText("receipt"),
				},
			}
			journalDir := filepath.Join(root, ".rhizome", "edit-journal")
			require.NoError(t, os.MkdirAll(journalDir, 0o700))
			journalPath := filepath.Join(journalDir, "receipt.json")
			require.NoError(t, writeEditWriteJournal(journalPath, journal))

			require.NoError(t, recoverEditWriteJournals(&vaultPaths))
			require.Equal(t, test.wantNoteContents, mustReadEditTransactionFile(t, notePath))
			require.NoFileExists(t, journalPath)
			require.NoFileExists(t, tempPath)
			require.NoFileExists(t, backupPath)
			require.NoFileExists(t, receiptTemp)
			if test.publishReceipt {
				require.Equal(t, "receipt", mustReadEditTransactionFile(t, receiptPath))
			} else {
				require.NoFileExists(t, receiptPath)
			}
		})
	}
}

func TestRecoverEditWriteJournalRejectsUnsafeCompletionTempPath(t *testing.T) {
	for _, test := range []struct {
		name string
		path func(root, receiptDir string) string
	}{
		{
			name: "escaped directory",
			path: func(root, _ string) string {
				return filepath.Join(root, "outside", ".completion-escape")
			},
		},
		{
			name: "invalid name",
			path: func(_, receiptDir string) string {
				return filepath.Join(receiptDir, "receipt-temp")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := paths.ResolveSymlinks(t.TempDir()).String()
			notesDir := filepath.Join(root, "notes")
			receiptDir := filepath.Join(root, ".rhizome", "edit-receipts")
			require.NoError(t, os.MkdirAll(notesDir, 0o755))
			require.NoError(t, os.MkdirAll(receiptDir, 0o700))
			vaultPaths, err := paths.NewVaultPaths(root)
			require.NoError(t, err)

			notePath := filepath.Join(notesDir, "a.md")
			backupPath := filepath.Join(notesDir, ".a.md.rhizome-backup-test")
			noteTempPath := filepath.Join(notesDir, "a.md.rhizome-write-test")
			receiptTempPath := test.path(root, receiptDir)
			require.NoError(t, os.MkdirAll(filepath.Dir(receiptTempPath), 0o700))
			require.NoError(t, os.WriteFile(notePath, []byte("new\n"), 0o644))
			require.NoError(t, os.WriteFile(backupPath, []byte("old\n"), 0o644))
			require.NoError(t, os.WriteFile(receiptTempPath, []byte("receipt"), 0o600))

			journalDir := filepath.Join(root, ".rhizome", "edit-journal")
			require.NoError(t, os.MkdirAll(journalDir, 0o700))
			journalPath := filepath.Join(journalDir, "unsafe-completion.json")
			require.NoError(t, writeEditWriteJournal(journalPath, editWriteJournal{
				Version: 1, TransactionID: "unsafe-completion", Phase: "prepared",
				Entries: []editWriteJournalEntry{{
					NotePath: "notes/a.md", TempPath: noteTempPath, BackupPath: backupPath,
					OldFingerprint: hashText("old\n"), NewFingerprint: hashText("new\n"),
				}},
				Completion: &editCompletionJournalEntry{
					TargetPath: filepath.Join(receiptDir, "receipt.json"),
					TempPath:   receiptTempPath, Fingerprint: hashText("receipt"),
				},
			}))

			err = recoverEditWriteJournals(&vaultPaths)
			require.ErrorContains(t, err, "completion temp artifact")
			require.Equal(t, "new\n", mustReadEditTransactionFile(t, notePath))
			require.FileExists(t, journalPath)
			require.FileExists(t, receiptTempPath)
		})
	}
}

func mustReadEditTransactionFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}
