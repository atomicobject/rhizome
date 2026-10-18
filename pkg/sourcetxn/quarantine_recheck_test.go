package sourcetxn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommittedArtifactTamperQuarantinesAndCanBeReclassified(t *testing.T) {
	root, options, intent := sourceFixture(t)
	var dir string
	err := apply(context.Background(), root, intent, options, &Hooks{AfterCleanupBoundary: func(boundary, path string) error {
		if boundary == cleanupAfterArtifactRemoved {
			dir = filepath.Dir(path)
			return ErrSimulatedInterruption
		}
		return nil
	}})
	require.ErrorIs(t, err, ErrSimulatedInterruption)
	journalRoot, err := repairJournalRoot(writerContext{VaultPath: root})
	require.NoError(t, err)
	entries, err := os.ReadDir(journalRoot)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	dir = filepath.Join(journalRoot, entries[0].Name())
	m, err := readRepairJournalManifest(dir)
	require.NoError(t, err)
	artifact := m.Entries[1].BackupPath
	require.NoError(t, os.WriteFile(artifact, []byte("tampered backup"), 0640))
	for range 2 {
		require.NoError(t, recoverSourceFixture(root, options))
	}
	require.Error(t, CheckAdmission(root, []string{"a.md"}))
	require.NoError(t, CheckAdmission(root, []string{"unrelated.md"}))
	content, err := os.ReadFile(artifact)
	require.NoError(t, err)
	require.Equal(t, "tampered backup", string(content))
	require.NoError(t, os.WriteFile(artifact, []byte("old"), 0640))
	require.NoError(t, recoverSourceFixture(root, options))
	require.NoError(t, CheckPending(root))
	require.NoFileExists(t, artifact)
	for _, file := range intent.Files {
		content, err := os.ReadFile(filepath.Join(root, file.Path))
		require.NoError(t, err)
		require.Equal(t, "new", string(content))
	}
}

func TestQuarantineReclassifiesRestoredSourceWitnesses(t *testing.T) {
	root, options, intent := sourceFixture(t)
	require.ErrorIs(t, apply(context.Background(), root, intent, options, &Hooks{AfterInstall: func(index int, _ string) error {
		if index == 0 {
			return ErrSimulatedInterruption
		}
		return nil
	}}), ErrSimulatedInterruption)
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.md"), []byte("external"), 0640))
	require.NoError(t, recoverSourceFixture(root, options))
	require.Error(t, CheckPending(root))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.md"), []byte("old"), 0640))
	require.NoError(t, recoverSourceFixture(root, options))
	require.NoError(t, CheckPending(root))
	for _, file := range intent.Files {
		content, err := os.ReadFile(filepath.Join(root, file.Path))
		require.NoError(t, err)
		require.Equal(t, "old", string(content))
	}
}

func TestQuarantinedPathAlsoBlocksUnicodeAlias(t *testing.T) {
	root, _, _ := sourceFixture(t)
	const nfc = "caf\u00e9.md"
	const nfd = "cafe\u0301.md"
	require.NoError(t, os.WriteFile(filepath.Join(root, nfc), []byte("external"), 0640))
	evidence := filepath.Join(root, ".rhizome", "edit-journal", "unicode.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(evidence), 0700))
	require.NoError(t, os.WriteFile(evidence, []byte("{"), 0600))
	require.NoError(t, recordQuarantine(root, evidence, []string{nfc}, nil, errors.New("source changed")))
	require.Error(t, CheckAdmission(root, []string{nfc}))
	require.Error(t, CheckAdmission(root, []string{nfd}))
	require.NoError(t, CheckAdmission(root, []string{"unrelated.md"}))
}
