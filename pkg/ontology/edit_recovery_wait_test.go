package ontology

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type legacyRecoveryWaitJournal struct {
	Version       int                       `json:"version"`
	TransactionID string                    `json:"transactionId"`
	Phase         string                    `json:"phase"`
	Entries       []legacyRecoveryWaitEntry `json:"entries"`
}
type legacyRecoveryWaitEntry struct {
	NotePath       string `json:"notePath"`
	TempPath       string `json:"tempPath"`
	BackupPath     string `json:"backupPath"`
	OldFingerprint string `json:"oldFingerprint"`
	NewFingerprint string `json:"newFingerprint"`
}

func writeLegacyRecoveryWaitFixture(path string, value legacyRecoveryWaitJournal) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0600)
}
func mustReadRecoveryWaitFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}
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
	require.NoError(t, writeLegacyRecoveryWaitFixture(journalPath, legacyRecoveryWaitJournal{
		Version: 1, TransactionID: "partial", Phase: "prepared", Entries: []legacyRecoveryWaitEntry{{
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
	require.Equal(t, "new\n", mustReadRecoveryWaitFile(t, notePath))
	require.FileExists(t, journalPath)
	require.NoError(t, release())
	require.NoError(t, <-done)
	require.Equal(t, "old\n", mustReadRecoveryWaitFile(t, notePath))
	require.NoFileExists(t, journalPath)
}
